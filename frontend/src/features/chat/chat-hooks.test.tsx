import { render, renderHook, act, fireEvent, screen, waitFor } from "@testing-library/react";
import { useEffect, useLayoutEffect, useRef, type ComponentProps } from "react";
import { describe, expect, it, vi } from "vitest";
import { createApplication, type Application } from "../../app/application";
import { jsonResponse, portal, type PortalRequest } from "../../app/portal-double";
import { trackApplications } from "../../app/test-harness";
import type { ChatConversationMessage } from "../../services/api";
import { ChatComposer, type ChatComposerEdit } from "./chat-sections";
import { useChatScopeStore, type ChatHistoryPage, type ChatScopeStore } from "./scopes";
import { chatScopeParams } from "./use-chat-scope-state";
import { useChatScrollRestoration, type RouterRenderedEvents } from "./use-chat-scroll-restoration";
import { useOlderPageLoading } from "./use-older-page-loading";

const projectId = "10000000-0000-4000-8000-000000000001";
const secondProjectId = "10000000-0000-4000-8000-000000000002";
const params = chatScopeParams({ scope: "project", projectId });
const key = params.key;
const messageUuid = (index: number) => `90000000-0000-4000-8000-${String(index).padStart(12, "0")}`;
const boundaryUuid = (index: number) => `80000000-0000-4000-8000-${String(index).padStart(12, "0")}`;

const applications = trackApplications();

function applicationAt(path: string): Application {
  const application = createApplication({ location: { href: `http://localhost${path}`, replace: vi.fn() } });
  applications.push(application);
  return application;
}

function chatMessage(id: string, content: string): ChatConversationMessage {
  return { id, role: "human", intent: "discuss", content, inserted_at: "2026-11-16T09:00:00.000000Z", work_item_id: null, command: null, metadata: null };
}

function conversationBody(messages: readonly unknown[], nextBefore: string | null): Record<string, unknown> {
  return { scope: "project", project_id: projectId, run_id: null, messages, runs: [], next_before: nextBefore };
}

function olderRequest(request: PortalRequest): string | null {
  return request.url.pathname === "/portal/api/chat" ? request.url.searchParams.get("before") : null;
}

function useOlderHarness(application: Application, cursor: string | null, boundaryId: string | null) {
  const store = useChatScopeStore();
  const latestStateOf = useRef(store.stateOf);
  useLayoutEffect(() => { latestStateOf.current = store.stateOf; });
  const older = useOlderPageLoading({
    key,
    identity: params.identity,
    cursor,
    boundaryId,
    latestStateOf,
    application,
    queryClient: application.queryClient,
    updateWith: store.updateWith,
  });
  return { store, older };
}

describe("older-page presses", () => {
  it("captures the press's cursor and boundary at press time and retains the page under them (mutant: settle-time arguments)", async () => {
    const application = applicationAt("/portal/chat");
    const requested: string[] = [];
    const held = Promise.withResolvers<Response>();
    portal.serve((request) => {
      const before = olderRequest(request);
      if (before === null) return undefined;
      requested.push(before);
      return held.promise;
    });
    const { result, rerender } = renderHook(
      (props: { cursor: string; boundaryId: string }) => useOlderHarness(application, props.cursor, props.boundaryId),
      { initialProps: { cursor: "cursor-a", boundaryId: boundaryUuid(1) } },
    );

    act(() => { result.current.older.load(); });
    await waitFor(() => { expect(requested).toEqual(["cursor-a"]); });
    expect(result.current.older.pending).toBe(true);

    // The surface re-signs its cursor at the same boundary while the press is in flight.
    rerender({ cursor: "cursor-b", boundaryId: boundaryUuid(1) });
    expect(result.current.older.pending).toBe(true);
    act(() => { result.current.older.load(); });
    expect(requested).toEqual(["cursor-a"]);

    await act(async () => {
      held.resolve(jsonResponse(conversationBody([chatMessage(messageUuid(7), "seven")], "cursor-0")));
      await held.promise;
    });
    await waitFor(() => { expect(result.current.store.stateOf(key).pages).toHaveLength(1); });
    const retained = result.current.store.stateOf(key).pages[0];
    expect(retained?.fetchBefore).toBe("cursor-a");
    expect(retained?.fetchBeforeId).toBe(boundaryUuid(1));
    expect(requested).toEqual(["cursor-a"]);
  });

  it("admits a press for a boundary the in-flight load never captured at the same cursor bytes (mutant: cursor-byte comparison)", async () => {
    const application = applicationAt("/portal/chat");
    const requested: string[] = [];
    const held = Promise.withResolvers<Response>();
    portal.serve((request) => {
      const before = olderRequest(request);
      if (before === null) return undefined;
      requested.push(before);
      return held.promise;
    });
    const { result, rerender } = renderHook(
      (props: { cursor: string; boundaryId: string }) => useOlderHarness(application, props.cursor, props.boundaryId),
      { initialProps: { cursor: "cursor-a", boundaryId: boundaryUuid(1) } },
    );

    act(() => { result.current.older.load(); });
    await waitFor(() => { expect(requested).toEqual(["cursor-a"]); });
    rerender({ cursor: "cursor-a", boundaryId: boundaryUuid(2) });
    expect(result.current.older.pending).toBe(false);

    act(() => { result.current.older.load(); });
    await act(async () => {
      held.resolve(jsonResponse(conversationBody([chatMessage(messageUuid(7), "seven")], "cursor-0")));
      await held.promise;
    });
    await waitFor(() => { expect(result.current.store.stateOf(key).pages).toHaveLength(2); });
    expect(result.current.store.stateOf(key).pages.map((page) => page.fetchBeforeId)).toEqual([boundaryUuid(1), boundaryUuid(2)]);
    expect(requested).toEqual(["cursor-a"]);
  });

  it("resets a refused chain while the press owns the boundary across a re-signed cursor (mutant: cursor-byte ownership)", async () => {
    const application = applicationAt("/portal/chat");
    const held = Promise.withResolvers<Response>();
    portal.serve((request) => (olderRequest(request) === null ? undefined : held.promise));
    const { result } = renderHook(
      (props: { cursor: string; boundaryId: string }) => useOlderHarness(application, props.cursor, props.boundaryId),
      { initialProps: { cursor: "cursor-a", boundaryId: boundaryUuid(1) } },
    );
    const servedPage: ChatHistoryPage = { messages: [chatMessage(boundaryUuid(1), "one")], fetchBefore: null, nextBefore: "served-token", fetchBeforeId: null };
    act(() => { result.current.store.update(key, { pages: [servedPage], nextCursor: "cursor-a", nextBoundaryId: boundaryUuid(1) }); });

    act(() => { result.current.older.load(); });
    // The served page re-signs its token at the same boundary while the press is in flight.
    act(() => { result.current.store.update(key, { nextCursor: "served-token-2" }); });
    await act(async () => {
      held.resolve(jsonResponse({ error: { code: "invalid_request", message: "request payload is invalid" } }, 400));
      await held.promise;
    });
    await waitFor(() => { expect(result.current.store.stateOf(key).status.kind).toBe("notice"); });
    expect(result.current.store.stateOf(key).nextCursor).toBe("served-token");
    expect(result.current.store.stateOf(key).nextBoundaryId).toBe(boundaryUuid(1));
  });

  it("leaves a refused chain alone when a moved boundary supersedes the press (mutant: dropped supersession guard)", async () => {
    const application = applicationAt("/portal/chat");
    const held = Promise.withResolvers<Response>();
    portal.serve((request) => (olderRequest(request) === null ? undefined : held.promise));
    const { result } = renderHook(
      (props: { cursor: string; boundaryId: string }) => useOlderHarness(application, props.cursor, props.boundaryId),
      { initialProps: { cursor: "cursor-a", boundaryId: boundaryUuid(1) } },
    );
    const servedPage: ChatHistoryPage = { messages: [chatMessage(boundaryUuid(1), "one")], fetchBefore: null, nextBefore: "served-token", fetchBeforeId: null };
    act(() => { result.current.store.update(key, { pages: [servedPage], nextCursor: "cursor-a", nextBoundaryId: boundaryUuid(1) }); });

    act(() => { result.current.older.load(); });
    act(() => { result.current.store.update(key, { nextCursor: "cursor-b", nextBoundaryId: "boundary-b" }); });
    await act(async () => {
      held.resolve(jsonResponse({ error: { code: "invalid_request", message: "request payload is invalid" } }, 400));
      await held.promise;
    });
    await waitFor(() => { expect(result.current.store.stateOf(key).pages).toHaveLength(1); });
    expect(result.current.store.stateOf(key).status.kind).toBe("idle");
    expect(result.current.store.stateOf(key).nextCursor).toBe("cursor-b");
    expect(result.current.store.stateOf(key).nextBoundaryId).toBe("boundary-b");
  });
});

interface PaneModel {
  value: number;
  accept(wanted: number): number;
  height(): number;
}

function paneModel(max: number | null): PaneModel {
  return {
    value: 0,
    accept: (wanted) => (max === null ? Math.max(0, wanted) : Math.min(Math.max(0, wanted), max)),
    height: () => (max === null ? 1000 : max + 160),
  };
}

function routerHub(): RouterRenderedEvents & { fire: () => void } {
  const listeners = new Map<string, (() => void)[]>();
  return {
    subscribe: (event, listener) => {
      const list = listeners.get(event) ?? [];
      list.push(listener);
      listeners.set(event, list);
      return () => { listeners.set(event, list.filter((candidate) => candidate !== listener)); };
    },
    fire: () => { for (const list of listeners.values()) for (const listener of [...list]) listener(); },
  };
}

function ScrollPane({ scopeKey, router, onStore }: {
  readonly scopeKey: string;
  readonly router: RouterRenderedEvents;
  readonly onStore: (store: ChatScopeStore) => void;
}) {
  const store = useChatScopeStore();
  useEffect(() => { onStore(store); });
  const paneRef = useRef<HTMLDivElement | null>(null);
  useChatScrollRestoration(scopeKey, {
    scrollOffsetOf: store.scrollOffsetOf,
    recordScrollOffset: store.recordScrollOffset,
    scrollRestoreOf: store.scrollRestoreOf,
    markScrollRestore: store.markScrollRestore,
    settleScrollRestore: store.settleScrollRestore,
  }, router, paneRef);
  return <div ref={paneRef} />;
}

function installPaneModel(node: HTMLElement, model: PaneModel): void {
  Object.defineProperty(node, "scrollTop", {
    configurable: true,
    get: () => model.value,
    set: (wanted: number) => { model.value = model.accept(wanted); },
  });
  Object.defineProperty(node, "scrollHeight", { configurable: true, get: () => model.height() });
  Object.defineProperty(node, "clientHeight", { configurable: true, get: () => 160 });
}

function renderPane(scopeKey: string, model: PaneModel, router: RouterRenderedEvents) {
  const captured: { store: ChatScopeStore | null } = { store: null };
  const onStore = (store: ChatScopeStore) => { captured.store = store; };
  const pane = (nextKey: string) => <ScrollPane scopeKey={nextKey} router={router} onStore={onStore} />;
  const view = render(pane(scopeKey));
  const node = view.container.querySelector("div");
  if (node === null) throw new Error("the pane node was not rendered");
  installPaneModel(node, model);
  const storeOf = (): ChatScopeStore => {
    if (captured.store === null) throw new Error("the pane store was not captured");
    return captured.store;
  };
  return { view, pane, storeOf };
}

const secondKey = chatScopeParams({ scope: "project", projectId: secondProjectId }).key;

describe("scroll restoration", () => {
  it("applies the owed offset before the recorded offset and settles the owed restore (mutant: swapped effect order)", () => {
    const model = paneModel(null);
    const { view, pane, storeOf } = renderPane(key, model, routerHub());
    act(() => { storeOf().recordScrollOffset(secondKey, 40); storeOf().markScrollRestore(secondKey, 120); });

    view.rerender(pane(secondKey));
    expect(model.value).toBe(40);
    expect(storeOf().scrollRestoreOf(secondKey)).toBeNull();
  });

  it("keeps the owed restore owed while the pane clamps it (mutant: inverted settle condition)", () => {
    const model = paneModel(60);
    const { view, pane, storeOf } = renderPane(key, model, routerHub());
    act(() => { storeOf().recordScrollOffset(secondKey, 40); storeOf().markScrollRestore(secondKey, 120); });

    view.rerender(pane(secondKey));
    expect(model.value).toBe(40);
    expect(storeOf().scrollRestoreOf(secondKey)).toBe(120);

    view.rerender(pane(secondKey));
    expect(model.value).toBe(60);
    expect(storeOf().scrollRestoreOf(secondKey)).toBe(120);
  });

  it("re-asserts the recorded offset and marks it owed on the router's own restore (mutant: dropped owed-mark)", () => {
    const model = paneModel(20);
    const router = routerHub();
    const { storeOf } = renderPane(key, model, router);
    act(() => { storeOf().recordScrollOffset(key, 40); storeOf().markScrollRestore(key, 90); });
    expect(storeOf().scrollRestoreOf(key)).toBe(90);

    act(() => { router.fire(); });
    expect(model.value).toBe(20);
    expect(storeOf().scrollRestoreOf(key)).toBe(40);
  });
});

function composerProps(overrides: Partial<ComponentProps<typeof ChatComposer>> = {}): ComponentProps<typeof ChatComposer> {
  return {
    scope: "workspace",
    complete: true,
    intent: "start_work",
    intents: ["discuss", "start_work"],
    startFields: true,
    projectId,
    scopeProject: null,
    projects: [{ id: projectId, name: "Project One", key: "ONE" }, { id: secondProjectId, name: "Project Two", key: "TWO" }],
    targetProjectId: "",
    draft: "",
    workTitle: "",
    repositoryId: "50000000-0000-4000-8000-000000000001",
    ciId: "50000000-0000-4000-8000-000000000002",
    repositories: [{ id: "50000000-0000-4000-8000-000000000001", name: "Repo One" }],
    ciResources: [{ id: "50000000-0000-4000-8000-000000000002", name: "CI One" }],
    sending: false,
    status: { kind: "idle" },
    onEdit: vi.fn(),
    onSubmit: vi.fn(),
    ...overrides,
  };
}

describe("composer section", () => {
  it("emits the target-project change with the cleared resource selections in one patch (mutant: dropped clearing)", () => {
    const patches: ChatComposerEdit[] = [];
    render(<ChatComposer {...composerProps({ onEdit: (patch) => { patches.push(patch); } })} />);
    fireEvent.change(screen.getByLabelText("Target project"), { target: { value: secondProjectId } });
    expect(patches).toEqual([{ targetProjectId: secondProjectId, repositoryId: "", ciId: "" }]);
  });
});
