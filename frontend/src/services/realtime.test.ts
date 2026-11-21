import { QueryClient, QueryObserver } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  createRealtime,
  invalidationKeys,
  type RealtimePush,
  type RealtimeSocket,
} from "./realtime";

/** A scripted Phoenix socket: the test drives open/close and pushes messages. */
function fakeSocket() {
  const handlers = {
    open: [] as (() => void)[],
    close: [] as (() => void)[],
    error: [] as ((error?: unknown) => void)[],
  };
  const received: ((payload: unknown) => void)[] = [];
  const joins: string[] = [];
  const replies = { ok: [] as (() => void)[], error: [] as (() => void)[] };
  let joined = false;
  let disconnects = 0;
  // One push object across rejoins: Phoenix hands the same `joinPush` back from
  // `Channel.join` and resends it on every rejoin (`channel.js` `rejoin` ->
  // `joinPush.resend`, whose `reset` keeps the receive hooks), so a later reply
  // fires the hooks an earlier reply fired.
  const push: RealtimePush = {
    receive: (status, callback) => {
      if (status === "ok") replies.ok.push(callback);
      if (status === "error") replies.error.push(callback);
      return push;
    },
  };
  const socket: RealtimeSocket = {
    connect: () => undefined,
    disconnect: () => { disconnects += 1; },
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
    disconnects: () => disconnects,
    isJoined: () => joined,
    open: () => { handlers.open.forEach((callback) => { callback(); }); },
    close: () => { handlers.close.forEach((callback) => { callback(); }); },
    fail: (error?: unknown) => { handlers.error.forEach((callback) => { callback(error); }); },
    deliver: (payload: unknown) => { received.forEach((callback) => { callback(payload); }); },
    joinOk: () => { replies.ok.forEach((callback) => { callback(); }); },
    joinError: () => { replies.error.forEach((callback) => { callback(); }); },
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
    // The literal topic, not the module's own constant: a drift in either would
    // otherwise be compared against itself and never noticed.
    expect(fake.joins).toEqual(["portal:invalidations"]);
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

  it("reconciles every read when the channel join completes, on the read path an invalidation takes", () => {
    // The socket opening and the channel join are two moments: the transport can be
    // open — status `connected`, the polling fallback already stopped — while the
    // server has not yet subscribed this browser, and a frame published in between
    // is dropped. The join reply is the epoch, and the read it triggers must be the
    // unclassified-invalidation pass itself rather than a narrower key list.
    const { fake, invalidate, refreshToken, realtime } = harness();
    realtime.connect();
    fake.open();
    expect(invalidate).toHaveBeenCalledTimes(1);

    fake.joinOk();
    expect(invalidate).toHaveBeenCalledTimes(2);
    const reconcile = invalidate.mock.calls[1]?.[0];
    expect(reconcile?.predicate).toBeTypeOf("function");
    expect(reconcile?.queryKey).toBeUndefined();
    expect(reconcile?.predicate?.({ queryKey: ["session"] } as never)).toBe(false);
    expect(reconcile?.predicate?.({ queryKey: ["attention"] } as never)).toBe(true);

    // A reconnect's rejoin completes on the same hooks Phoenix resends, so the
    // frames published while the channel was errored reconcile there too.
    fake.close();
    fake.open();
    expect(invalidate).toHaveBeenCalledTimes(3);
    fake.joinOk();
    expect(invalidate).toHaveBeenCalledTimes(4);

    // A refused join is not an epoch: the client's own rejoin timer retries the
    // join, and the reconcile belongs to the reply that completes it. The refusal
    // refreshes the token so that retry carries a live one.
    refreshToken.mockClear();
    fake.joinError();
    expect(refreshToken).toHaveBeenCalledTimes(1);
    expect(invalidate).toHaveBeenCalledTimes(4);
  });

  it("records the every-read pass that issues the reads, and its completion", async () => {
    // The record is written where the pass is: a surface states its freshness from
    // the client's own act, so it cannot be set without the reads having issued.
    const { fake, invalidate, realtime } = harness();
    expect(realtime.lastRefresh()).toBeNull();
    const snapshot = Promise.withResolvers<void>();
    const reconcile = Promise.withResolvers<void>();
    invalidate.mockReturnValueOnce(snapshot.promise).mockReturnValueOnce(reconcile.promise);

    realtime.connect();
    fake.open();
    expect(realtime.lastRefresh()).toMatchObject({ source: "connection", settledAt: null });
    expect(realtime.lastRefresh()?.issuedAt).toBeTypeOf("number");

    fake.joinOk();
    const issued = realtime.lastRefresh();
    expect(issued).toMatchObject({ source: "join", settledAt: null });
    expect(issued?.issuedAt).toBeTypeOf("number");

    // A pass that a newer one replaced records no completion: its settle speaks
    // for reads that are no longer the ones on screen.
    snapshot.resolve();
    await Promise.resolve();
    expect(realtime.lastRefresh()).toBe(issued);
    expect(realtime.lastRefresh()?.settledAt).toBeNull();

    // The current pass records when the reads it issued finished.
    reconcile.resolve();
    await Promise.resolve();
    expect(realtime.lastRefresh()?.source).toBe("join");
    expect(realtime.lastRefresh()?.issuedAt).toBe(issued?.issuedAt);
    expect(realtime.lastRefresh()?.settledAt).toBeTypeOf("number");
  });

  it("names the fallback tick, the returning tab and an unclassified change as the sources of their passes", () => {
    let visible = true;
    const { fake, invalidate, realtime } = harness({ pollIntervalMs: 50, visibility: () => visible });
    realtime.connect();
    invalidate.mockClear();

    // While the socket is down the fallback's own tick is the pass that refreshed
    // every read, and the record says so.
    vi.advanceTimersByTime(50);
    expect(realtime.lastRefresh()).toMatchObject({ source: "fallback" });

    // A hidden document refreshes nothing, so nothing is recorded for it.
    fake.open();
    expect(realtime.lastRefresh()).toMatchObject({ source: "connection" });
    visible = false;
    document.dispatchEvent(new Event("visibilitychange"));
    expect(realtime.lastRefresh()).toMatchObject({ source: "connection" });

    visible = true;
    document.dispatchEvent(new Event("visibilitychange"));
    expect(realtime.lastRefresh()).toMatchObject({ source: "visibility" });

    fake.deliver({ kind: "something_new" });
    vi.advanceTimersByTime(10);
    expect(realtime.lastRefresh()).toMatchObject({ source: "invalidation" });

    // A change classified to specific keys refreshes those reads alone and cannot
    // speak for the reads on screen as a whole, so the record keeps the every-read
    // pass it already names.
    const everyRead = realtime.lastRefresh();
    fake.deliver({ kind: "work_item", id: "w1" });
    vi.advanceTimersByTime(10);
    expect(invalidate.mock.calls.map((call) => JSON.stringify(call[0]?.queryKey)))
      .toContain('["work-item",{"workItemId":"w1"}]');
    expect(realtime.lastRefresh()).toBe(everyRead);
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

  it("leaves an in-flight read alone on a fallback tick", () => {
    // The fallback is a timer, not a change: TanStack Query's default
    // (`cancelRefetch: true`) cancels the in-flight fetch of a query that already
    // has data and starts it again, so a read slower than the poll interval would
    // be cancelled and restarted on every tick and never settle while the socket is
    // down. The tick must let the read it interrupted converge instead.
    const { queryClient, invalidate, realtime } = harness({ pollIntervalMs: 50 });
    queryClient.setQueryData(["held"], "loaded");
    const held = Promise.withResolvers<string>();
    const queryFn = vi.fn(() => held.promise);
    const observer = new QueryObserver(queryClient, { queryKey: ["held"], queryFn });
    const unsubscribe = observer.subscribe(() => {});
    // One read in flight over data the surface already shows: exactly the state the
    // default cancels and restarts.
    expect(queryFn).toHaveBeenCalledTimes(1);

    realtime.connect();
    invalidate.mockClear();
    vi.advanceTimersByTime(50);

    expect(invalidate).toHaveBeenCalledTimes(1);
    expect(queryFn).toHaveBeenCalledTimes(1);

    held.resolve("settled");
    unsubscribe();
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

  it("keeps the tab-return refresh after a closed handle connects again", () => {
    // `close()` removes the document listener, so the attempt that revives the
    // handle must register it again — the StrictMode setup/cleanup/setup a mounted
    // shell runs is exactly a close followed by another connect. Without the
    // re-registration every later tab return is a refresh that never happens.
    const { invalidate, realtime } = harness({ pollIntervalMs: 50 });
    realtime.connect();
    realtime.close();
    realtime.connect();
    invalidate.mockClear();

    document.dispatchEvent(new Event("visibilitychange"));
    expect(invalidate).toHaveBeenCalledTimes(1);
    expect(realtime.lastRefresh()).toMatchObject({ source: "visibility" });
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
    // The socket is released, not just relabelled: the Phoenix client's own
    // reconnect timer must be gone with it.
    expect(fake.disconnects()).toBe(1);
    vi.advanceTimersByTime(200);
    expect(invalidate).not.toHaveBeenCalled();

    // And the document listener went with the connection: a tab returning after
    // the handle closed refreshes nothing.
    document.dispatchEvent(new Event("visibilitychange"));
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
