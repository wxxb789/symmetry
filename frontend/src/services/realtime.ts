import type { QueryClient } from "@tanstack/react-query";
import { Socket } from "phoenix";

/**
 * The operator invalidation client.
 *
 * Real-time delivery is invalidation only: the server pushes the changed
 * resource kind with its identity, version and durable event cursor (never a
 * transcript, never business authority), and this client turns that into
 * TanStack Query invalidations so every surface refetches the authoritative read
 * it already owns. There is no client-side event log, no cursor store and no
 * policy: the only durable state here is the connection status and the
 * reconnect epoch.
 *
 * Loss recovery is structural rather than message-based: on every (re)connect the
 * client refetches every read, and while the socket is down it polls on the
 * retired classic cadence, so a missed invalidation costs freshness, never state.
 */

export interface InvalidationPayload {
  kind: string;
  id?: string;
  project_id?: string;
  work_item_id?: string;
  goal_id?: string;
  run_id?: string;
  version?: number;
  cursor?: number;
}

export type RealtimeStatus = "connecting" | "connected" | "disconnected";

/** The subset of the Phoenix client this module drives (a test seam, not a wrapper). */
export interface RealtimePush {
  receive(status: string, callback: (reply?: unknown) => void): RealtimePush;
}

export interface RealtimeChannel {
  on(event: string, callback: (payload: unknown) => void): number;
  join(): RealtimePush;
}

export interface RealtimeSocket {
  connect(): void;
  disconnect(callback?: () => void): void;
  onOpen(callback: () => void): void;
  onClose(callback: () => void): void;
  onError(callback: (error?: unknown) => void): void;
  channel(topic: string): RealtimeChannel;
}

export interface RealtimeOptions {
  queryClient: QueryClient;
  /** The current short-lived socket token, read on every connect attempt. */
  token: () => string;
  /** Called after a lost/failed connection so the next attempt carries a fresh token. */
  refreshToken?: () => void;
  socketFactory?: (token: () => string) => RealtimeSocket;
  pollIntervalMs?: number;
  batchMs?: number;
  visibility?: () => boolean;
}

export interface RealtimeHandle {
  connect(): void;
  close(): void;
  status(): RealtimeStatus;
  /** Bumped on every established connection; surfaces holding a served cursor catch up on change. */
  epoch(): number;
  subscribe(listener: () => void): () => void;
}

export const REALTIME_TOPIC = "portal:invalidations";
const DEFAULT_POLL_INTERVAL_MS = 5_000;
const DEFAULT_BATCH_MS = 200;

function phoenixSocket(token: () => string): RealtimeSocket {
  // `authToken` is a function, so every connect attempt (including the client's
  // own reconnects) carries the token the bootstrap most recently issued.
  return new Socket("/portal/socket", {
    authToken: token,
    params: {},
  });
}

export function createRealtime(options: RealtimeOptions): RealtimeHandle {
  const queryClient = options.queryClient;
  const pollIntervalMs = options.pollIntervalMs ?? DEFAULT_POLL_INTERVAL_MS;
  const batchMs = options.batchMs ?? DEFAULT_BATCH_MS;
  const visible = options.visibility ?? (() => document.visibilityState === "visible");
  const socketFactory = options.socketFactory ?? phoenixSocket;

  const listeners = new Set<() => void>();
  let status: RealtimeStatus = "disconnected";
  let connectionEpoch = 0;
  let socket: RealtimeSocket | null = null;
  let closed = false;
  let pollTimer: ReturnType<typeof setInterval> | null = null;
  let batchTimer: ReturnType<typeof setTimeout> | null = null;
  let pending: InvalidationPayload[] = [];

  const notify = () => {
    for (const listener of listeners) listener();
  };

  const invalidateReads = () => {
    // Every portal read refetches; the session read is excluded because it is a
    // bootstrap document with its own lifetime, not a workspace read.
    void queryClient.invalidateQueries({ predicate: (query) => query.queryKey[0] !== "session" });
  };

  const flush = () => {
    batchTimer = null;
    const payloads = pending;
    pending = [];
    const keys = new Map<string, readonly unknown[]>();
    let allReads = false;
    for (const payload of payloads) {
      const payloadKeys = invalidationKeys(payload);
      if (payloadKeys.length === 0) allReads = true;
      for (const key of payloadKeys) keys.set(JSON.stringify(key), key);
    }
    if (allReads) {
      invalidateReads();
      return;
    }
    for (const key of keys.values()) void queryClient.invalidateQueries({ queryKey: key });
  };

  const receive = (payload: unknown) => {
    if (payload === null || typeof payload !== "object") return;
    const candidate = payload as InvalidationPayload;
    if (typeof candidate.kind !== "string") return;
    pending.push(candidate);
    if (batchTimer !== null) return;
    batchTimer = setTimeout(flush, batchMs);
  };

  const stopPolling = () => {
    if (pollTimer === null) return;
    clearInterval(pollTimer);
    pollTimer = null;
  };

  const syncPolling = () => {
    if (closed || status === "connected") {
      stopPolling();
      return;
    }
    if (pollTimer !== null) return;
    // The fallback keeps a disconnected (or never-connected) browser convergent.
    // A hidden document refreshes nothing; the visibility handler refreshes once
    // when it returns, exactly like the retired classic surface.
    pollTimer = setInterval(() => {
      if (visible()) invalidateReads();
    }, pollIntervalMs);
  };

  const setStatus = (next: RealtimeStatus) => {
    if (status === next) return;
    status = next;
    syncPolling();
    notify();
  };

  /**
   * A tab that becomes visible again is a reconnect from the browser's point of
   * view: the reads refresh from the server whatever the socket status, so a
   * window that was backgrounded never shows stale work and no surface needs a
   * poll of its own. The listener is registered for the whole handle lifetime,
   * because a browser whose bootstrap token has not arrived yet still returns to
   * a stale document.
   */
  const onVisibilityChange = () => {
    if (!visible()) return;
    syncPolling();
    invalidateReads();
  };

  if (typeof document !== "undefined") {
    document.addEventListener("visibilitychange", onVisibilityChange);
  }

  const connect = () => {
    closed = false;
    if (socket !== null) return;
    if (options.token() === "") {
      // No bootstrap token yet: opening a socket would only produce a refused
      // connect. The caller connects again once the bootstrap read resolved, and
      // the polling fallback covers the wait.
      setStatus("disconnected");
      syncPolling();
      return;
    }
    const client = socketFactory(options.token);
    socket = client;
    setStatus("connecting");

    client.onOpen(() => {
      connectionEpoch += 1;
      setStatus("connected");
      // Snapshot refetch: a reconnect loses no state because every read is a
      // server document that is fetched again here.
      invalidateReads();
      notify();
    });

    client.onClose(() => {
      setStatus("disconnected");
      options.refreshToken?.();
    });

    client.onError(() => {
      // The token may have expired while the socket was down; refresh it so the
      // client's own retry (with backoff) connects with a live token.
      options.refreshToken?.();
    });

    const channel = client.channel(REALTIME_TOPIC);
    channel.on("invalidation", receive);
    channel.join().receive("ok", () => undefined).receive("error", () => {
      options.refreshToken?.();
    });

    client.connect();
    syncPolling();
  };

  const close = () => {
    closed = true;
    if (typeof document !== "undefined") {
      document.removeEventListener("visibilitychange", onVisibilityChange);
    }
    stopPolling();
    if (batchTimer !== null) {
      clearTimeout(batchTimer);
      batchTimer = null;
    }
    const client = socket;
    socket = null;
    client?.disconnect();
    status = "disconnected";
    notify();
  };

  return {
    connect,
    close,
    status: () => status,
    epoch: () => connectionEpoch,
    subscribe: (listener: () => void) => {
      listeners.add(listener);
      return () => {
        listeners.delete(listener);
      };
    },
  };
}

/**
 * The query-key roots one invalidation affects. An empty list means "every
 * read": an unclassified change is refetched broadly instead of being dropped.
 */
export function invalidationKeys(payload: InvalidationPayload): readonly (readonly unknown[])[] {
  const identity = payload.id ?? payload.work_item_id;
  switch (payload.kind) {
    case "work_item":
      // The attention read merges the Goal feed with the project's blocked work
      // items, so a work item's own change (a blocker resolving, a status move)
      // changes that read too: without this key a focused Attention page keeps
      // the blocked rows it read before the change until a refocus or reload.
      return [...(identity === undefined ? [["work-item"]] : [["work-item", { workItemId: identity }]]), ["work-item-timeline"], ["workspace"], ["attention"]];
    case "run":
      // A run-scoped change is a change to its work item's execution truth; the
      // payload names the run, and the surfaces key by work item.
      return [["work-item"], ["work-item-timeline"], ["workspace"]];
    case "goal":
      // The receipts list reads its loaded pages under `goal-events` and its
      // reconnect continuation under `goal-events-catch-up`; both are reads of
      // the same served log, so both are invalidated.
      return [...(identity === undefined ? [["goal"]] : [["goal", { goalId: identity }]]), ["goal-events"], ["goal-events-catch-up"], ["attention"]];
    case "chat":
      return [["chat"]];
    case "connection":
    case "resource":
      return [["connections"], ["workspace"]];
    case "project":
      return [["workspace"], ["connections"]];
    case "runtime":
      return [["workspace"]];
    default:
      return [];
  }
}
