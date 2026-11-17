import { QueryClient } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  createRealtime,
  invalidationKeys,
  REALTIME_TOPIC,
  type RealtimePush,
  type RealtimeSocket,
} from "./realtime";

/** A scripted Phoenix socket: the test drives open/close and pushes messages. */
function fakeSocket() {
  const handlers = {
    open: [] as Array<() => void>,
    close: [] as Array<() => void>,
    error: [] as Array<(error?: unknown) => void>,
  };
  const received: Array<(payload: unknown) => void> = [];
  const joins: string[] = [];
  let joined = false;
  const push: RealtimePush = { receive: () => push };
  const socket: RealtimeSocket = {
    connect: () => undefined,
    disconnect: () => undefined,
    onOpen: (callback) => {
      handlers.open.push(callback);
    },
    onClose: (callback) => {
      handlers.close.push(callback);
    },
    onError: (callback) => {
      handlers.error.push(callback);
    },
    channel: (topic) => {
      joins.push(topic);
      return {
        on: (_event, callback) => {
          received.push(callback);
          return 1;
        },
        join: () => {
          joined = true;
          return push;
        },
      };
    },
  };
  return {
    socket,
    joins,
    isJoined: () => joined,
    open: () => handlers.open.forEach((callback) => callback()),
    close: () => handlers.close.forEach((callback) => callback()),
    fail: (error?: unknown) => handlers.error.forEach((callback) => callback(error)),
    deliver: (payload: unknown) => received.forEach((callback) => callback(payload)),
  };
}

function harness(options: { pollIntervalMs?: number; visibility?: () => boolean } = {}) {
  const queryClient = new QueryClient();
  const invalidate = vi.spyOn(queryClient, "invalidateQueries");
  const fake = fakeSocket();
  const refreshToken = vi.fn();
  const realtime = createRealtime({
    queryClient,
    token: () => "socket-token",
    refreshToken,
    socketFactory: () => fake.socket,
    batchMs: 10,
    pollIntervalMs: options.pollIntervalMs ?? 50,
    visibility: options.visibility ?? (() => true),
  });
  /** The invalidated key list of one pass, in call order. */
  const keys = () => invalidate.mock.calls.map((call) => JSON.stringify(call[0]?.queryKey));
  return { queryClient, invalidate, fake, refreshToken, realtime, keys };
}

/**
 * A scripted WebSocket, the transport the Phoenix client resolves for itself from
 * the global `WebSocket` when the portal builds its production socket. Stubbing
 * that global is how this file drives the shipped endpoint resolution — the
 * factory, the endpoint and the client between them — instead of replacing the
 * socket with a seam the production path does not have.
 */
class ScriptedWebSocket {
  static readonly instances: ScriptedWebSocket[] = [];
  readonly url: string;
  readyState = 0;
  binaryType = "blob";
  onopen: (() => void) | null = null;
  onerror: ((error?: unknown) => void) | null = null;
  onmessage: ((event: unknown) => void) | null = null;
  onclose: ((event: unknown) => void) | null = null;
  constructor(url: string) {
    this.url = url;
    ScriptedWebSocket.instances.push(this);
  }
  send(): void {}
  close(): void {
    this.readyState = 3;
  }
  /** Refuse the connection the way a browser does before the socket ever opens. */
  refuse(): void {
    this.readyState = 3;
    this.onerror?.("connection refused");
    this.onclose?.({ code: 1006, reason: "", wasClean: false });
  }
}

/** The window globals the checks below take away or replace for one body each. */
type WindowGlobal = "location" | "document" | "window";

/**
 * Run `body` with one window global taken away (`undefined`) or replaced, then
 * restore it. Taking one away is how a check faces the environment the reported
 * error came from — a document that has closed while the timer it armed is still
 * ticking — and replacing the location is how it faces a deployment served from
 * another origin.
 */
function withGlobal(name: WindowGlobal, replacement: PropertyDescriptor | undefined, body: () => void): void {
  const original = Object.getOwnPropertyDescriptor(globalThis, name);
  if (original === undefined) throw new Error(`this environment exposes no ${name} global to work with`);
  if (replacement === undefined) delete (globalThis as unknown as Record<string, unknown>)[name];
  else Object.defineProperty(globalThis, name, replacement);
  try {
    body();
  } finally {
    Object.defineProperty(globalThis, name, original);
  }
}

describe("operator invalidation client", () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });
  afterEach(() => {
    vi.useRealTimers();
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
  });

  it("joins the operator topic and reports connection state", () => {
    const { fake, realtime } = harness();
    expect(realtime.status()).toBe("disconnected");
    realtime.connect();
    expect(realtime.status()).toBe("connecting");
    expect(fake.joins).toEqual([REALTIME_TOPIC]);
    expect(fake.isJoined()).toBe(true);
    fake.open();
    expect(realtime.status()).toBe("connected");
    fake.close();
    expect(realtime.status()).toBe("disconnected");
  });

  it("refetches every read once per reconnect and keeps an epoch for cursor catch-up", () => {
    const { fake, invalidate, realtime } = harness();
    realtime.connect();
    expect(realtime.epoch()).toBe(0);
    fake.open();
    expect(realtime.epoch()).toBe(1);
    expect(invalidate).toHaveBeenCalledTimes(1);

    // The session bootstrap is not a workspace read: it keeps its own lifetime.
    const predicate = invalidate.mock.calls[0]?.[0]?.predicate;
    expect(predicate?.({ queryKey: ["session"] } as never)).toBe(false);
    expect(predicate?.({ queryKey: ["workspace"] } as never)).toBe(true);

    fake.close();
    fake.open();
    expect(realtime.epoch()).toBe(2);
    expect(invalidate).toHaveBeenCalledTimes(2);
  });

  it("maps a resource invalidation onto the query keys that read it", () => {
    expect(invalidationKeys({ kind: "work_item", id: "w1" })).toEqual([
      ["work-item", { workItemId: "w1" }],
      ["work-item-timeline"],
      ["workspace"],
      ["attention"],
    ]);
    // The attention read is project-scoped by its own query keys
    // (`["attention", { projectId, cursor }]`), so the work-item kind must
    // invalidate the whole root: a focused Attention page shows the project's
    // blocked items and loses them to staleness otherwise.
    expect(invalidationKeys({ kind: "work_item" })).toEqual([
      ["work-item"],
      ["work-item-timeline"],
      ["workspace"],
      ["attention"],
    ]);
    expect(invalidationKeys({ kind: "run", id: "r1" })).toEqual([
      ["work-item"],
      ["work-item-timeline"],
      ["workspace"],
    ]);
    expect(invalidationKeys({ kind: "goal", id: "g1", cursor: 4 })).toEqual([
      ["goal", { goalId: "g1" }],
      ["goal-events"],
      ["goal-events-catch-up"],
      ["attention"],
    ]);
    expect(invalidationKeys({ kind: "chat" })).toEqual([["chat"]]);
    expect(invalidationKeys({ kind: "connection" })).toEqual([["connections"], ["workspace"]]);
    expect(invalidationKeys({ kind: "resource", id: "res1" })).toEqual([
      ["connections"],
      ["workspace"],
    ]);
    expect(invalidationKeys({ kind: "project" })).toEqual([["workspace"], ["connections"]]);
    expect(invalidationKeys({ kind: "runtime" })).toEqual([["workspace"]]);
    // An unclassified kind refetches broadly instead of being dropped.
    expect(invalidationKeys({ kind: "workspace" })).toEqual([]);
    expect(invalidationKeys({ kind: "something_new" })).toEqual([]);
  });

  it("batches a burst of changes into one invalidation pass", () => {
    const { fake, invalidate, realtime, keys } = harness();
    realtime.connect();
    fake.open();
    invalidate.mockClear();

    fake.deliver({ kind: "chat" });
    fake.deliver({ kind: "chat" });
    fake.deliver({ kind: "goal", id: "g1", cursor: 2 });
    expect(invalidate).not.toHaveBeenCalled();

    vi.advanceTimersByTime(10);
    expect(keys().sort()).toEqual(
      [
        '["chat"]',
        '["goal",{"goalId":"g1"}]',
        '["goal-events"]',
        '["goal-events-catch-up"]',
        '["attention"]',
      ].sort(),
    );
  });

  it("refetches every read for an unclassified change", () => {
    const { fake, invalidate, realtime } = harness();
    realtime.connect();
    fake.open();
    invalidate.mockClear();

    fake.deliver({ kind: "something_new" });
    vi.advanceTimersByTime(10);

    expect(invalidate).toHaveBeenCalledTimes(1);
    expect(invalidate.mock.calls[0]?.[0]?.predicate).toBeTypeOf("function");
  });

  it("ignores a payload that is not an invalidation", () => {
    const { fake, invalidate, realtime } = harness();
    realtime.connect();
    fake.open();
    invalidate.mockClear();

    fake.deliver(null);
    fake.deliver("transcript text");
    fake.deliver({ no_kind: true });
    vi.advanceTimersByTime(50);

    expect(invalidate).not.toHaveBeenCalled();
  });

  it("polls while the socket is down and stops once it connects", () => {
    const { fake, invalidate, realtime } = harness({ pollIntervalMs: 50 });
    realtime.connect();
    invalidate.mockClear();

    vi.advanceTimersByTime(120);
    expect(invalidate.mock.calls.length).toBeGreaterThanOrEqual(2);

    fake.open();
    invalidate.mockClear();
    vi.advanceTimersByTime(200);
    expect(invalidate).not.toHaveBeenCalled();

    fake.close();
    vi.advanceTimersByTime(60);
    expect(invalidate.mock.calls.length).toBeGreaterThanOrEqual(1);
  });

  it("does not refresh a hidden document and refreshes once it returns", () => {
    let visible = false;
    const { invalidate, realtime } = harness({ pollIntervalMs: 50, visibility: () => visible });
    realtime.connect();
    invalidate.mockClear();

    vi.advanceTimersByTime(200);
    expect(invalidate).not.toHaveBeenCalled();

    visible = true;
    document.dispatchEvent(new Event("visibilitychange"));
    expect(invalidate).toHaveBeenCalledTimes(1);

    vi.advanceTimersByTime(60);
    expect(invalidate.mock.calls.length).toBeGreaterThanOrEqual(2);
  });

  it("refreshes the snapshot when a connected tab returns", () => {
    // The socket keeps delivering while the tab is hidden, but a browser that
    // comes back must not trust what it rendered before: every read refetches
    // from the server, exactly as it does on a reconnect.
    let visible = true;
    const { fake, invalidate, realtime } = harness({ pollIntervalMs: 50, visibility: () => visible });
    realtime.connect();
    fake.open();
    invalidate.mockClear();

    visible = false;
    document.dispatchEvent(new Event("visibilitychange"));
    expect(invalidate).not.toHaveBeenCalled();

    visible = true;
    document.dispatchEvent(new Event("visibilitychange"));
    expect(invalidate).toHaveBeenCalledTimes(1);
  });

  it("refreshes the short-lived token after a lost or refused connection", () => {
    const { fake, refreshToken, realtime } = harness();
    realtime.connect();
    fake.open();
    expect(refreshToken).not.toHaveBeenCalled();

    fake.close();
    expect(refreshToken).toHaveBeenCalledTimes(1);

    fake.fail();
    expect(refreshToken).toHaveBeenCalledTimes(2);
  });

  it("stops everything on close", () => {
    const { fake, invalidate, realtime } = harness({ pollIntervalMs: 50 });
    realtime.connect();
    fake.open();
    invalidate.mockClear();

    realtime.close();
    expect(realtime.status()).toBe("disconnected");
    vi.advanceTimersByTime(200);
    expect(invalidate).not.toHaveBeenCalled();
  });

  it("hands the client an absolute endpoint, so its own retry reads no global", () => {
    ScriptedWebSocket.instances.length = 0;
    vi.stubGlobal("WebSocket", ScriptedWebSocket);
    const queryClient = new QueryClient();
    const realtime = createRealtime({ queryClient, token: () => "socket-token", pollIntervalMs: 50 });
    const urls = () => ScriptedWebSocket.instances.map((transport) => transport.url);
    const expected = `ws://${window.location.host}/portal/socket/websocket?vsn=2.0.0`;

    // The endpoint is resolved from the page before the client exists and handed
    // over absolutely: the client's own URL assembly has nothing left to resolve.
    realtime.connect();
    expect(urls()).toEqual([expected]);

    // A refused attempt arms the client's retry, 10 ms out. It fires in an
    // environment whose `location` is gone — the `ReferenceError` the contracts job
    // reported twice, from a retry that outlived the document that owned the global
    // — and a relative endpoint would be resolved from that global again. The
    // absolute one is reused, so the retry targets the same page without reading
    // anything.
    withGlobal("location", undefined, () => {
      ScriptedWebSocket.instances[0]?.refuse();
      vi.advanceTimersByTime(20);
    });
    expect(urls()).toEqual([expected, expected]);

    realtime.close();
  });

  it("targets the origin the portal is served from, port and scheme included", () => {
    ScriptedWebSocket.instances.length = 0;
    vi.stubGlobal("WebSocket", ScriptedWebSocket);
    const queryClient = new QueryClient();
    const realtime = createRealtime({ queryClient, token: () => "socket-token", pollIntervalMs: 50 });
    const elsewhere: PropertyDescriptor = { configurable: true, value: { protocol: "https:", host: "portal.example.test:8443" } };

    // The endpoint follows the page: another host, another port, a secure origin
    // (the `wss` half of the scheme rule) — the socket goes where the portal came
    // from, exactly as the relative path it replaced did.
    withGlobal("location", elsewhere, () => {
      realtime.connect();
    });
    expect(ScriptedWebSocket.instances.map((transport) => transport.url)).toEqual([
      "wss://portal.example.test:8443/portal/socket/websocket?vsn=2.0.0",
    ]);

    realtime.close();
  });

  it("refuses to invent an endpoint where there is no page, instead of a relative path", () => {
    ScriptedWebSocket.instances.length = 0;
    vi.stubGlobal("WebSocket", ScriptedWebSocket);
    const queryClient = new QueryClient();
    const realtime = createRealtime({ queryClient, token: () => "socket-token", pollIntervalMs: 50 });

    // A non-browser import has no page origin, and the relative path it used to be
    // handed is the bare-global read the absolute endpoint exists to remove. The
    // absence is said in as many words, before any socket exists to fail later —
    // and before the client is left to read a `location` that is not there either.
    withGlobal("window", undefined, () => {
      withGlobal("location", undefined, () => {
        expect(() => { realtime.connect(); }).toThrow("the realtime socket needs a page origin: there is no window.location to resolve an absolute endpoint from");
      });
    });
    expect(ScriptedWebSocket.instances).toHaveLength(0);
    expect(realtime.status()).toBe("disconnected");

    realtime.close();
  });

  it("polls without a document rather than reading the global it no longer has", () => {
    const queryClient = new QueryClient();
    const invalidate = vi.spyOn(queryClient, "invalidateQueries");
    // No bootstrap token yet: the client waits in its polling fallback, as a shell
    // whose session read has not resolved does.
    const realtime = createRealtime({ queryClient, token: () => "", pollIntervalMs: 50 });
    realtime.connect();
    expect(realtime.status()).toBe("disconnected");

    // The tick keeps coming in an environment whose document has closed — the
    // jsdom a finished test file tears down, or an import that never had one. The
    // fallback refreshes a visible page and none is there: the read makes that
    // decision itself instead of reading a global that is gone, and refreshes
    // nothing.
    withGlobal("document", undefined, () => {
      expect(() => vi.advanceTimersByTime(120)).not.toThrow();
    });
    expect(invalidate).not.toHaveBeenCalled();

    realtime.close();
  });
});
