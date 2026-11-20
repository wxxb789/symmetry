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
 * policy: the only durable state here is the connection status, the reconnect
 * epoch and the record of the last every-read pass — which the shell states as
 * the reads' freshness.
 *
 * Loss recovery is structural rather than message-based: on every (re)connect the
 * client refetches every read — at the socket opening, and again when the channel
 * join completes, because only the join makes this browser a subscriber and a
 * frame published in between is dropped — and while the socket is down it polls on
 * the retired classic cadence, so a missed invalidation costs freshness, never
 * state.
 *
 * The socket endpoint is this module's own decision, not the client's: it is
 * resolved once per socket, absolutely, from the page's location (see
 * `socketEndpoint`), so no connect attempt — the client's own reconnects
 * included — reads a global the page no longer has.
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

/**
 * Which of the client's own events refreshed every read a surface is showing.
 *
 * The client refreshes every read on four of its own events — the connection
 * opening (`connection`), a completed channel join (`join`), a tick of the
 * polling fallback while the socket is unavailable (`fallback`) and a document
 * returning to the visible state (`visibility`) — plus the every-read pass an
 * unclassified invalidation takes (`invalidation`).
 */
export type RefreshSource = "connection" | "join" | "fallback" | "visibility" | "invalidation";

/**
 * The reads the client last refreshed, and the pass that refreshed them.
 *
 * `issuedAt` is when the pass issued its reads — the read epoch of a completed
 * join, for `join` — and `settledAt` is when the reads that one pass issued
 * finished, `null` while they are still in flight. Only an every-read pass is
 * recorded: a change classified to specific keys refreshes those reads alone and
 * cannot speak for the reads on screen as a whole.
 *
 * The record states the pass, not its outcome: a read that was refused did end,
 * and the surface that owns it states that itself (`Refresh failed — showing
 * last loaded data`). Only the pass that is still current records its own
 * completion, so a late settle can never speak for reads a newer pass replaced.
 */
export interface RefreshPass {
  readonly source: RefreshSource;
  readonly issuedAt: number;
  readonly settledAt: number | null;
}

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
  /** The reads this client last refreshed, and the pass that refreshed them. */
  lastRefresh(): RefreshPass | null;
  subscribe(listener: () => void): () => void;
}

export const REALTIME_TOPIC = "portal:invalidations";
const SOCKET_PATH = "/portal/socket";
const DEFAULT_POLL_INTERVAL_MS = 5_000;
const DEFAULT_BATCH_MS = 200;

/**
 * The absolute endpoint the Phoenix client is handed, resolved once per socket
 * from the page's own location.
 *
 * The client resolves the bare `location` global itself — `Socket.endPointURL`
 * reads `location.protocol` and `location.host`, and every `transportConnect`
 * reaches it, the client's own reconnect timer included — but only for a
 * *relative* endpoint: an absolute one is used as handed. Resolving it here is
 * therefore what keeps that read off the portal's path, so a retry that outlives
 * the document it was started in has nothing left to read. That is the
 * `ReferenceError: location is not defined` a finished test file reported twice
 * while every assertion passed: the retry outlived the shell that owned it.
 *
 * Scheme and host come from the page, so the socket follows the origin the portal
 * is served from — a different port, a proxy, a later deployment — exactly as the
 * relative path did: `https:` pages get `wss:`, everything else `ws:`.
 *
 * A non-browser import has no page origin, and the relative path it used to get
 * in that case is the bare-global read this resolution exists to remove, so the
 * absence is answered in as many words rather than deferred to the client.
 */
export function socketEndpoint(): string {
  if (typeof window === "undefined") {
    throw new Error("the realtime socket needs a page origin: there is no window.location to resolve an absolute endpoint from");
  }
  const page = window.location;
  return `${page.protocol === "https:" ? "wss" : "ws"}://${page.host}${SOCKET_PATH}`;
}

function phoenixSocket(token: () => string): RealtimeSocket {
  // `authToken` is a function, so every connect attempt (including the client's
  // own reconnects) carries the token the bootstrap most recently issued; the
  // endpoint is resolved once here, so no attempt resolves it from the page again.
  return new Socket(socketEndpoint(), {
    authToken: token,
    params: {},
  });
}

export function createRealtime(options: RealtimeOptions): RealtimeHandle {
  const queryClient = options.queryClient;
  const pollIntervalMs = options.pollIntervalMs ?? DEFAULT_POLL_INTERVAL_MS;
  const batchMs = options.batchMs ?? DEFAULT_BATCH_MS;
  // The fallback poll refreshes only a document the browser reports as visible, and
  // only a document can report anything: this module is imported where none exists,
  // so the environment decision is made where the read happens — the same one the
  // visibility listener below makes — instead of leaving the ticking timer to read a
  // global its environment no longer has.
  const visible = options.visibility ?? (() => typeof document !== "undefined" && document.visibilityState === "visible");
  const socketFactory = options.socketFactory ?? phoenixSocket;

  const listeners = new Set<() => void>();
  let status: RealtimeStatus = "disconnected";
  let connectionEpoch = 0;
  let socket: RealtimeSocket | null = null;
  let closed = false;
  let pollTimer: ReturnType<typeof setInterval> | null = null;
  let batchTimer: ReturnType<typeof setTimeout> | null = null;
  let pending: InvalidationPayload[] = [];
  let lastRefresh: RefreshPass | null = null;

  const notify = () => {
    for (const listener of listeners) listener();
  };

  /**
   * The one pass an every-read refresh takes: every portal read refetches, and
   * the session read is excluded because it is a bootstrap document with its own
   * lifetime, not a workspace read.
   *
   * The pass records itself — the source that caused it, when it issued its
   * reads, and when those reads finished — because that record is the only place
   * the browser can state what it refreshed and when: nothing else on this side
   * observes those reads, and a mark written anywhere else could be set without
   * the pass having happened. The record is published with the same `notify` the
   * connection status uses, so a surface renders it as the state it is.
   */
  const invalidateReads = (source: RefreshSource) => {
    const pass: RefreshPass = { source, issuedAt: Date.now(), settledAt: null };
    lastRefresh = pass;
    notify();
    const settled = () => {
      // Only the pass that is still current records its own completion: a newer
      // pass has replaced these reads, so its own settle is the one that counts.
      if (lastRefresh !== pass) return;
      lastRefresh = { source: pass.source, issuedAt: pass.issuedAt, settledAt: Date.now() };
      notify();
    };
    // The pass's reads end either way — a refused read ended too — so both arms
    // record the completion and no rejection is left unhandled.
    //
    // A change-driven pass keeps the query client's default and cancels a read
    // that is already in flight, because the change it answers need not be in
    // that read. The fallback tick is a timer, not a change, and says so with
    // `cancelRefetch: false`: cancelling a read slower than the poll interval
    // would restart it on every tick and it would never settle.
    void queryClient
      .invalidateQueries(
        { predicate: (query) => query.queryKey[0] !== "session" },
        { cancelRefetch: source !== "fallback" },
      )
      .then(settled, settled);
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
      invalidateReads("invalidation");
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
      if (visible()) invalidateReads("fallback");
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
   * poll of its own. The listener belongs to a connection attempt — registered by
   * `connect()` and removed by `close()` — because `close()` takes it away: a
   * registration made once at construction would leave a handle that is closed
   * and later reconnected without it for the rest of the document's life, and the
   * StrictMode setup/cleanup/setup a mounted shell runs is exactly that
   * close-then-reconnect.
   */
  const onVisibilityChange = () => {
    if (!visible()) return;
    syncPolling();
    invalidateReads("visibility");
  };

  // Adding the same listener twice is a DOM no-op, so `connect()` can attach
  // unconditionally on every attempt without tracking whether it already did.
  const attachVisibilityListener = () => {
    if (typeof document === "undefined") return;
    document.addEventListener("visibilitychange", onVisibilityChange);
  };

  const detachVisibilityListener = () => {
    if (typeof document === "undefined") return;
    document.removeEventListener("visibilitychange", onVisibilityChange);
  };

  const connect = () => {
    closed = false;
    // A handle that was closed before its token arrived (or by a StrictMode
    // cleanup) is revived here, so the listener goes back with the connection.
    attachVisibilityListener();
    if (socket !== null) return;
    if (options.token() === "") {
      // No bootstrap token: opening a socket would only produce a refused
      // connect. The shell connects as soon as the bootstrap read resolved — with
      // a token when the session serves one — so this branch is the client's
      // fallback state: it reports `disconnected` and the polling fallback covers
      // the reads, while a later token-bearing session read calls `connect()`
      // again and opens the socket then.
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
      invalidateReads("connection");
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

    /**
     * The channel-join epoch: `channel.join()`'s `ok` reply is the moment the
     * server has run `PortalChannel.join/3` and subscribed this browser to the
     * invalidation topic, and therefore the earliest instant at which a published
     * frame can arrive here.
     *
     * The socket opening is not that instant. The join is a round trip — the
     * client's own `join()` call is buffered until the transport is open and the
     * reply comes back one exchange later — so there is a window in which the
     * transport is open (status `connected`, and `syncPolling` has therefore
     * already stopped the polling fallback) while the server has no subscription
     * for this client at all. A frame published in that window is broadcast to a
     * topic this browser is not on: it is dropped, and Phoenix never replays it.
     *
     * Nothing on this side can tell whether the window held a miss. The
     * invalidation payload carries a durable `cursor`, but this client keeps no
     * cursor store by design (discovery only), and the join reply carries no
     * position at all. The reconcile is therefore on the join event itself, and it
     * is the read an invalidation takes: the unclassified payload's own pass,
     * every read refetched through one `invalidateQueries` predicate call. It is
     * the same call the socket-open snapshot above makes, and that one stays — it
     * is the read that covers the disconnected window, and like any read it can be
     * overtaken by a change committed before the join landed. Exactly one reconcile
     * pass is added per completed join; polling is not restarted, and no timer is
     * involved, because the epoch is an event.
     *
     * Phoenix resends this same push on every rejoin (`channel.js` `rejoin` ->
     * `joinPush.resend`, whose `reset` keeps its receive hooks), so a reconnect's
     * rejoin — the frames published while the channel was errored are dropped just
     * the same — reconciles through this hook too. The pass also records itself as
     * the `join` pass, so the reads a completed join refreshed are the reads the
     * shell reports as refreshed by the join, and a browser case can wait on that
     * record instead of on a subscription it cannot see.
     */
    const onJoined = () => {
      invalidateReads("join");
    };

    const channel = client.channel(REALTIME_TOPIC);
    channel.on("invalidation", receive);
    channel.join()
      .receive("ok", onJoined)
      .receive("error", () => {
        options.refreshToken?.();
      });

    client.connect();
    syncPolling();
  };

  const close = () => {
    closed = true;
    detachVisibilityListener();
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
    lastRefresh: () => lastRefresh,
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
