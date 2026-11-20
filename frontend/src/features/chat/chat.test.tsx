import { QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory, RouterProvider, type RouterHistory } from "@tanstack/react-router";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { createApplication, type Application } from "../../app/application";
import { createAppRouter } from "../../app/router";
import { readWorkDetail, readWorkItem } from "../../services/api.fixtures";
import { asRecord } from "../work/presentation";

/**
 * `/portal/chat`: one scope key keeps its own draft, intent, start-work
 * selections and history; a late read or write for one scope never lands in
 * another; receipts render only from durable evidence; history pages are
 * retained across refreshes; and served text is inert.
 */

const projectId = "10000000-0000-4000-8000-000000000001";
const workItemId = "20000000-0000-4000-8000-000000000001";
const runId = "30000000-0000-4000-8000-000000000001";
const runtimeId = "40000000-0000-4000-8000-000000000001";
const taskId = "60000000-0000-4000-8000-000000000001";
const repositoryId = "50000000-0000-4000-8000-000000000001";
const ciId = "50000000-0000-4000-8000-000000000002";

const uuid = (prefix: string, index: number) => `${prefix}-0000-4000-8000-${String(index).padStart(12, "0")}`;
const messageId = (index: number) => uuid("90000000", index);

interface ServedMessage {
  id: string;
  role: "human" | "assistant";
  intent: string;
  content: string;
  inserted_at: string;
  work_item_id: string | null;
  command: Record<string, unknown> | null;
  metadata: null;
}

function message(index: number, options: { role?: "human" | "assistant"; content?: string; intent?: string; command?: Record<string, unknown> | null; insertedAt?: string } = {}): ServedMessage {
  return {
    id: messageId(index),
    role: options.role ?? "human",
    intent: options.intent ?? "discuss",
    content: options.content ?? `message ${index}`,
    inserted_at: options.insertedAt ?? `2026-11-16T09:${String(index % 60).padStart(2, "0")}:00.000000Z`,
    work_item_id: null,
    command: options.command ?? null,
    metadata: null,
  };
}

function command(options: { id?: number; kind?: string; state: string; outcome?: string | null }): Record<string, unknown> {
  return {
    command_id: uuid("70000000", options.id ?? 1),
    task_id: taskId,
    run_id: runId,
    generation: 1,
    kind: options.kind ?? "guidance",
    payload: {},
    state: options.state,
    issued_at: "2026-11-16T09:00:00.000000Z",
    applied_at: options.outcome === "applied" ? "2026-11-16T09:01:00.000000Z" : null,
    acknowledgement_id: null,
    acknowledgement_outcome: options.outcome ?? null,
    acknowledged_at: null,
  };
}

function conversationBody(options: {
  scope: "workspace" | "project" | "run";
  projectId?: string | null;
  runId?: string | null;
  messages?: readonly unknown[];
  runs?: readonly unknown[];
  nextBefore?: string | null;
}): Record<string, unknown> {
  return {
    scope: options.scope,
    project_id: options.projectId ?? null,
    run_id: options.runId ?? null,
    messages: options.messages ?? [],
    runs: options.runs ?? [],
    next_before: options.nextBefore ?? null,
  };
}

function runtimeRow(options: { guidance?: string; pause?: string } = {}): Record<string, unknown> {
  return {
    machine_id: uuid("80000000", 1),
    machine_name: "fixture machine",
    runtime_id: runtimeId,
    runtime_key: "fixture-native",
    runtime_name: "Fixture runtime",
    status: "online",
    last_heartbeat_at: null,
    connection_epoch: 1,
    capacity: 1,
    reserved_capacity: 0,
    agent_profile: "default",
    workspace: "primary",
    repository_resource_id: null,
    capabilities: {
      adapter: {
        kind: "pi",
        native_version: "1.5.2",
        implementation_version: "pi-rpc-v1",
        protocol_version: 1,
        operations: {
          start: true, events: true, cancel: true, resume: true, handoff: false,
          guidance: options.guidance ?? "next_turn", pause: options.pause ?? "unsupported",
          approval_response: false, usage: "unknown", hard_cost_limit: false,
        },
      },
    },
    active_runs: [],
  };
}

function workspaceBody(options: { execution?: Record<string, unknown>; runtimes?: readonly unknown[] } = {}): Record<string, unknown> {
  return {
    selected_project_id: projectId,
    projects: [{
      id: projectId,
      key: "ONE",
      name: "Project One",
      description: null,
      status: "active",
      default_agent_profile: "default",
      default_workspace: "primary",
      resources: [
        { id: repositoryId, project_id: projectId, kind: "repository", name: "Repo One", version: 1 },
        { id: ciId, project_id: projectId, kind: "ci", name: "CI One", version: 1 },
      ],
      work_items: [readWorkItem({
        id: workItemId,
        projectId,
        title: "Subject work item",
        key: "ONE-1",
        execution: options.execution ?? { run_id: runId, generation: 3, state: "running", runtime_id: runtimeId, can_guide: true, can_pause: true, can_resume: false },
      })],
    }],
    health: { status: "unknown" },
    runtimes: options.runtimes ?? [],
    registered_runtimes: [],
  };
}

interface RecordedRequest {
  method: string;
  path: string;
  headers: Record<string, string>;
  body: Record<string, unknown> | undefined;
}

interface HarnessOptions {
  /** The conversation read; `readIndex` starts at 1. */
  conversation?: (url: URL, readIndex: number) => Response | Promise<Response>;
  /** The message POST; `postIndex` starts at 1. */
  message?: (body: Record<string, unknown>, postIndex: number) => Response | Promise<Response>;
  workspace?: () => Record<string, unknown>;
}

const applications: Application[] = [];
afterEach(() => {
  for (const application of applications) application.queryClient.clear();
  applications.length = 0;
});

function renderChat(path: string, options: HarnessOptions = {}) {
  const requests: RecordedRequest[] = [];
  let chatReads = 0;
  let messagePosts = 0;
  const fetcher = vi.fn<typeof fetch>().mockImplementation(async (input, init) => {
    const url = new URL(input instanceof Request ? input.url : String(input), "http://localhost");
    const method = init?.method ?? "GET";
    const body = typeof init?.body === "string" ? (JSON.parse(init.body) as Record<string, unknown>) : undefined;
    requests.push({ method, path: url.pathname + url.search, headers: (init?.headers ?? {}) as Record<string, string>, body });
    if (url.pathname === "/portal/api/session") return new Response(JSON.stringify({ csrf_token: "session-token" }));
    if (url.pathname === "/portal/api/workspace") return new Response(JSON.stringify((options.workspace ?? workspaceBody)()));
    if (url.pathname === "/portal/api/chat") {
      chatReads += 1;
      const respond = options.conversation ?? (() => new Response(JSON.stringify(conversationBody({ scope: "workspace" }))));
      return await respond(url, chatReads);
    }
    if (url.pathname === "/portal/api/chat/messages") {
      messagePosts += 1;
      const respond = options.message ?? (() => new Response("{}", { status: 500 }));
      return await respond(body ?? {}, messagePosts);
    }
    return new Response("{}", { status: 404 });
  });
  const application = createApplication({ fetcher, location: { href: `http://localhost${path}`, replace: vi.fn() } });
  applications.push(application);
  const history: RouterHistory = createMemoryHistory({ initialEntries: [path] });
  const router = createAppRouter(application, history);
  render(<QueryClientProvider client={application.queryClient}><RouterProvider router={router} /></QueryClientProvider>);
  return {
    application,
    history,
    requests,
    posts: () => requests.filter((request) => request.method === "POST"),
  };
}

async function openChat() {
  await screen.findByRole("heading", { level: 1, name: "Chat" });
}

function scopeSelect(): HTMLSelectElement {
  return screen.getByLabelText<HTMLSelectElement>("Scope");
}
function intentSelect(): HTMLSelectElement {
  return screen.getByLabelText<HTMLSelectElement>("Intent");
}
function contentField(): HTMLTextAreaElement {
  return screen.getByLabelText<HTMLTextAreaElement>("Message");
}
function statusRegion(): HTMLElement {
  const node = document.querySelector("#chat-send-status");
  if (node === null) throw new Error("#chat-send-status is not rendered");
  return node as HTMLElement;
}
function scrollNode(): HTMLElement {
  const node = document.querySelector("#chat-scroll");
  if (node === null) throw new Error("#chat-scroll is not rendered");
  return node as HTMLElement;
}
function messageIds(): readonly string[] {
  return [...document.querySelectorAll<HTMLElement>("#chat-messages [data-message-id]")].map((node) => node.dataset.messageId ?? "");
}
function setScroll(value: number): void {
  const node = scrollNode();
  fireEvent.scroll(node, { target: { scrollTop: value } });
  expect(node.scrollTop).toBe(value);
}

describe("per-scope state", () => {
  it("keeps each scope's draft, intent and scroll and restores them on a switch", async () => {
    const user = userEvent.setup();
    renderChat(`/portal/chat?scope=workspace&projectId=${projectId}`, {
      conversation: (url) => new Response(JSON.stringify(url.searchParams.get("scope") === "project"
        ? conversationBody({ scope: "project", projectId, messages: [message(2, { content: "project history" })] })
        : conversationBody({ scope: "workspace", messages: [message(1, { content: "workspace history" })] }))),
    });
    await openChat();
    await screen.findByText("Workspace conversation");
    await screen.findByText("workspace history");

    await user.selectOptions(intentSelect(), "status");
    await user.type(contentField(), "workspace draft");
    setScroll(40);

    await user.selectOptions(scopeSelect(), "project");
    await screen.findByText("Project conversation — Project One (ONE)");
    expect(contentField()).toHaveValue("");
    await user.selectOptions(intentSelect(), "discuss");
    await user.type(contentField(), "project draft");
    setScroll(120);

    await user.selectOptions(scopeSelect(), "workspace");
    await screen.findByText("Workspace conversation");
    expect(contentField()).toHaveValue("workspace draft");
    expect(intentSelect()).toHaveValue("status");
    expect(scrollNode().scrollTop).toBe(40);

    await user.selectOptions(scopeSelect(), "project");
    expect(contentField()).toHaveValue("project draft");
    expect(intentSelect()).toHaveValue("discuss");
    expect(scrollNode().scrollTop).toBe(120);
    expect(screen.getByText("project history")).toBeInTheDocument();
  });

  it("keeps the start-work selections per scope and restores them", async () => {
    const user = userEvent.setup();
    renderChat(`/portal/chat?scope=workspace&projectId=${projectId}`, {
      conversation: (url) => new Response(JSON.stringify(url.searchParams.get("scope") === "project"
        ? conversationBody({ scope: "project", projectId })
        : conversationBody({ scope: "workspace" }))),
    });
    await openChat();
    await waitFor(() => expect(document.querySelector("#chat-intent option[value=\"start_work\"]")).not.toBeNull());
    await user.selectOptions(intentSelect(), "start_work");
    await waitFor(() => expect(document.querySelector(`#chat-target-project option[value="${projectId}"]`)).not.toBeNull());
    await user.selectOptions(screen.getByLabelText("Target project"), projectId);
    await user.selectOptions(screen.getByLabelText("Repository resource"), repositoryId);
    await user.selectOptions(screen.getByLabelText("CI resource"), ciId);

    await user.selectOptions(scopeSelect(), "project");
    await screen.findByText("Project conversation — Project One (ONE)");
    await user.selectOptions(scopeSelect(), "workspace");
    await screen.findByText("Workspace conversation");

    expect(intentSelect()).toHaveValue("start_work");
    expect(screen.getByLabelText("Target project")).toHaveValue(projectId);
    expect(screen.getByLabelText("Repository resource")).toHaveValue(repositoryId);
    expect(screen.getByLabelText("CI resource")).toHaveValue(ciId);
  });

  it("keeps the scope and explains when no project is selected for a project conversation", async () => {
    const user = userEvent.setup();
    renderChat("/portal/chat", {
      conversation: () => new Response(JSON.stringify(conversationBody({ scope: "workspace" }))),
    });
    await openChat();
    await screen.findByText("Workspace conversation");
    await user.selectOptions(scopeSelect(), "project");
    expect(scopeSelect()).toHaveValue("workspace");
    expect(statusRegion()).toHaveTextContent("No project is selected, so the scope was unchanged.");
  });

  it("never lets a late read for another scope write this scope's draft", async () => {
    const user = userEvent.setup();
    const held = Promise.withResolvers<Response>();
    renderChat(`/portal/chat?scope=workspace&projectId=${projectId}`, {
      conversation: (url) => url.searchParams.get("scope") === "project"
        ? held.promise
        : new Response(JSON.stringify(conversationBody({ scope: "workspace", messages: [message(1, { content: "workspace history" })] }))),
    });
    await openChat();
    await screen.findByText("workspace history");
    await user.type(contentField(), "workspace draft");

    await user.selectOptions(scopeSelect(), "project");
    await user.selectOptions(scopeSelect(), "workspace");
    await screen.findByText("Workspace conversation");
    await user.type(contentField(), " after late read");

    held.resolve(new Response(JSON.stringify(conversationBody({ scope: "project", projectId, messages: [message(2, { content: "project secret" })] }))));
    await waitFor(() => expect(screen.queryByText("project secret")).not.toBeInTheDocument());
    expect(screen.getByText("Workspace conversation")).toBeInTheDocument();
    expect(contentField()).toHaveValue("workspace draft after late read");
  });

  it("never lets a late write for another scope land in this scope's status or draft", async () => {
    const user = userEvent.setup();
    const held = Promise.withResolvers<Response>();
    renderChat(`/portal/chat?scope=workspace&projectId=${projectId}`, {
      conversation: (url) => new Response(JSON.stringify(url.searchParams.get("scope") === "project"
        ? conversationBody({ scope: "project", projectId })
        : conversationBody({ scope: "workspace" }))),
      message: () => held.promise,
    });
    await openChat();
    await user.selectOptions(scopeSelect(), "project");
    await screen.findByText("Project conversation — Project One (ONE)");
    await user.type(contentField(), "project question");
    await user.click(screen.getByRole("button", { name: "Send" }));
    await waitFor(() => expect(statusRegion()).toHaveTextContent("Sending…"));

    await user.selectOptions(scopeSelect(), "workspace");
    await screen.findByText("Workspace conversation");
    expect(statusRegion()).toHaveTextContent("Messages are saved to this context.");
    await user.type(contentField(), "workspace draft");

    held.resolve(new Response(JSON.stringify({ message: message(9), reply: message(10, { role: "assistant" }), work_item_id: null, command: null }), { status: 201 }));
    await waitFor(() => expect(screen.queryByText("Sending…")).not.toBeInTheDocument());
    expect(statusRegion()).toHaveTextContent("Messages are saved to this context.");
    expect(contentField()).toHaveValue("workspace draft");

    await user.selectOptions(scopeSelect(), "project");
    expect(statusRegion()).toHaveTextContent("Saved to this context.");
    expect(contentField()).toHaveValue("");
  });

  it("leaves another scope's draft and selection alone when one scope refreshes", async () => {
    const user = userEvent.setup();
    const { requests } = renderChat(`/portal/chat?scope=workspace&projectId=${projectId}`, {
      conversation: (url) => new Response(JSON.stringify(url.searchParams.get("scope") === "project"
        ? conversationBody({ scope: "project", projectId })
        : conversationBody({ scope: "workspace", messages: [message(1, { content: "workspace history" })] }))),
    });
    const projectReads = () => requests.filter((request) => request.path.startsWith("/portal/api/chat?") && request.path.includes("scope=project")).length;
    await openChat();
    await screen.findByText("workspace history");
    await user.selectOptions(intentSelect(), "status");
    await user.type(contentField(), "workspace draft");

    await user.selectOptions(scopeSelect(), "project");
    await screen.findByText("Project conversation — Project One (ONE)");
    await user.type(contentField(), "project draft");
    const before = projectReads();
    await user.click(screen.getByRole("button", { name: "Refresh" }));
    await waitFor(() => expect(projectReads()).toBeGreaterThan(before));

    await user.selectOptions(scopeSelect(), "workspace");
    expect(contentField()).toHaveValue("workspace draft");
    expect(intentSelect()).toHaveValue("status");
  });
});

describe("sending", () => {
  it("resends the identical frozen request on a retry and renders one replayed message", async () => {
    const user = userEvent.setup();
    const bodies: Record<string, unknown>[] = [];
    const served: unknown[] = [];
    renderChat(`/portal/chat?scope=project&projectId=${projectId}`, {
      conversation: () => new Response(JSON.stringify(conversationBody({ scope: "project", projectId, messages: served }))),
      message: (body) => {
        bodies.push(body);
        if (bodies.length === 1) throw new TypeError("offline");
        served.push(message(3, { content: String(body["content"]) }), message(4, { role: "assistant", content: "reply" }));
        return new Response(JSON.stringify({ message: message(3), reply: message(4), work_item_id: null, command: null }), { status: 200 });
      },
    });
    await openChat();
    await user.type(contentField(), "uncertain question");
    await user.click(screen.getByRole("button", { name: "Send" }));
    await screen.findByText(/Send not confirmed/);
    expect(contentField()).toHaveValue("uncertain question");

    await user.click(screen.getByRole("button", { name: "Send" }));
    await screen.findByText("Saved to this context.");
    expect(bodies).toHaveLength(2);
    expect(bodies[1]).toEqual(bodies[0]);
    expect(JSON.stringify(bodies[1])).toBe(JSON.stringify(bodies[0]));
    expect(bodies[1]?.["action_id"]).toBe(bodies[0]?.["action_id"]);
    await screen.findByText("uncertain question");
    const rows = [...document.querySelectorAll("#chat-messages [data-message-id]")].filter((node) => node.textContent?.includes("uncertain question"));
    expect(rows).toHaveLength(1);
  });

  it("renders a served refusal as the server's message and never as an applied state", async () => {
    const user = userEvent.setup();
    renderChat(`/portal/chat?scope=project&projectId=${projectId}`, {
      conversation: () => new Response(JSON.stringify(conversationBody({ scope: "project", projectId }))),
      message: () => new Response(JSON.stringify({ error: { code: "idempotency_conflict", message: "This action id was already used with a different request." } }), { status: 409 }),
    });
    await openChat();
    await user.type(contentField(), "surface question");
    await user.click(screen.getByRole("button", { name: "Send" }));
    await screen.findByText(/Refused: This action id was already used/);
    expect(contentField()).toHaveValue("surface question");
    expect(document.querySelector("#chat-messages")?.textContent).not.toContain("surface question");
    expect(document.querySelector("[data-outcome=\"applied\"]")).toBeNull();
  });

  it("keeps a guidance draft that exceeds the server's byte bound and names the bound", async () => {
    const user = userEvent.setup();
    renderChat(`/portal/chat?scope=run&runId=${runId}&projectId=${projectId}`, {
      conversation: () => new Response(JSON.stringify(conversationBody({ scope: "run", projectId, runId }))),
      workspace: () => workspaceBody({ execution: { run_id: runId, generation: 3, state: "running", runtime_id: runtimeId, can_guide: true } }),
    });
    await openChat();
    await waitFor(() => expect(document.querySelector("#chat-intent option[value=\"guidance\"]")).not.toBeNull());
    await user.selectOptions(intentSelect(), "guidance");
    const oversized = "x".repeat(32_769);
    fireEvent.change(contentField(), { target: { value: oversized } });
    await user.click(screen.getByRole("button", { name: "Send" }));
    expect(statusRegion().textContent).toBe("Guidance must be 32,768 UTF-8 bytes or fewer.");
    expect(contentField()).toHaveValue(oversized);
    expect(screen.queryByText("Sending…")).not.toBeInTheDocument();
  });

  it("settles a scope's pane on its own offset after a switch, even while the pane clamped it", async () => {
    const user = userEvent.setup();
    const release = Promise.withResolvers<void>();
    const { application, requests } = renderChat(`/portal/chat?scope=workspace&projectId=${projectId}`, {
      conversation: (url) => url.searchParams.get("scope") === "project"
        // Held, and re-served as a fresh response on every project read.
        ? release.promise.then(() => new Response(JSON.stringify(conversationBody({ scope: "project", projectId, messages: [message(2, { content: "project history" })] }))))
        : new Response(JSON.stringify(conversationBody({ scope: "workspace", messages: [message(1, { content: "workspace history" })] }))),
    });
    await openChat();
    await screen.findByText("workspace history");

    // Model Chromium: a pane whose transcript is absent (the scope is still
    // loading) clamps scrollTop to 0, while a pane with the transcript accepts
    // and keeps what it is given.
    const node = scrollNode();
    let pane = 0;
    Object.defineProperty(node, "scrollTop", {
      configurable: true,
      get: () => (node.querySelector("#chat-messages") === null ? 0 : pane),
      set: (value: number) => { pane = node.querySelector("#chat-messages") === null ? 0 : value; },
    });
    fireEvent.scroll(node, { target: { scrollTop: 40 } });
    expect(node.scrollTop).toBe(40);

    // The project scope arrives empty (its read is held): its pane clamps, and
    // the workspace pane's offset must not be captured from that clamped pane.
    await user.selectOptions(scopeSelect(), "project");
    await screen.findByText(`Project conversation — ${projectId} · pending`);
    expect(document.querySelector("#chat-messages")).toBeNull();
    await user.selectOptions(scopeSelect(), "workspace");
    await screen.findByText("workspace history");
    expect(node.scrollTop).toBe(40);

    // A scope that left while its pane was clamped still owes its offset: the
    // restore is re-asserted once the pane can hold it again.
    release.resolve();
    await user.selectOptions(scopeSelect(), "project");
    await screen.findByText("project history");
    fireEvent.scroll(node, { target: { scrollTop: 120 } });
    await user.selectOptions(scopeSelect(), "workspace");
    await waitFor(() => expect(requests.filter((request) => request.path.includes("scope=workspace")).length).toBeGreaterThan(1));
    expect(node.scrollTop).toBe(40);
    await act(async () => {
      await application.queryClient.invalidateQueries({ queryKey: ["chat"] });
    });
    expect(node.scrollTop).toBe(40);
  });

  it("keeps a scope's offset when the browser pins the pane under a replaced transcript", async () => {
    const user = userEvent.setup();
    renderChat(`/portal/chat?scope=workspace&projectId=${projectId}`, {
      conversation: (url) => new Response(JSON.stringify(url.searchParams.get("scope") === "project"
        ? conversationBody({ scope: "project", projectId, messages: [message(2, { content: "project history" })] })
        : conversationBody({ scope: "workspace", messages: [message(1, { content: "workspace history" })] }))),
    });
    await openChat();
    await screen.findByText("workspace history");

    // Model Chromium: a pane holds what it is given while its own transcript is
    // rendered, and is pinned at its own maximum when that transcript is
    // replaced by one the pane cannot hold. That pinning raises a scroll event
    // exactly like a reader's own move.
    const node = scrollNode();
    let pane = 0;
    let replaced = false;
    Object.defineProperty(node, "scrollTop", {
      configurable: true,
      get: () => (replaced ? 0 : pane),
      set: (value: number) => { pane = replaced ? 0 : value; },
    });
    Object.defineProperty(node, "scrollHeight", { configurable: true, get: () => (replaced ? 96 : 320) });
    Object.defineProperty(node, "clientHeight", { configurable: true, get: () => 160 });
    setScroll(40);
    expect(node.scrollTop).toBe(40);

    // The browser pins the pane at its own maximum when the transcript is
    // replaced under it, and that pinning raises a scroll event exactly like a
    // reader's own move.
    replaced = true;
    fireEvent.scroll(node, { target: { scrollTop: 0 } });
    replaced = false;

    // The pinning was not a reader's move: the offset stays this scope's, and a
    // later commit re-asserts it because the pane still owes it.
    fireEvent.change(contentField(), { target: { value: "a draft that forces a commit" } });
    expect(node.scrollTop).toBe(40);

    // The scope carries the same offset across a switch, exactly as the
    // round-trip walk does.
    await user.selectOptions(scopeSelect(), "project");
    await screen.findByText("project history");
    await user.selectOptions(scopeSelect(), "workspace");
    await screen.findByText("workspace history");
    expect(node.scrollTop).toBe(40);
  });

  it("still records a reader's own move inside a pane too short for the scope's offset", async () => {
    const user = userEvent.setup();
    renderChat(`/portal/chat?scope=workspace&projectId=${projectId}`, {
      conversation: (url) => new Response(JSON.stringify(url.searchParams.get("scope") === "project"
        ? conversationBody({ scope: "project", projectId, messages: [message(2, { content: "project history" })] })
        : conversationBody({ scope: "workspace", messages: [message(1, { content: "workspace history" })] }))),
    });
    await openChat();
    await screen.findByText("workspace history");

    const node = scrollNode();
    let pane = 0;
    let content = 320;
    Object.defineProperty(node, "scrollTop", {
      configurable: true,
      get: () => pane,
      set: (value: number) => { pane = Math.min(value, Math.max(0, content - 160)); },
    });
    Object.defineProperty(node, "scrollHeight", { configurable: true, get: () => content });
    Object.defineProperty(node, "clientHeight", { configurable: true, get: () => 160 });
    setScroll(120);
    expect(node.scrollTop).toBe(120);

    // The transcript shrinks so the pane can no longer hold 120 px. A move the
    // reader makes inside that pane sits below the pane's maximum, so it is
    // still the reader's own position — never read as the browser's pinning.
    content = 200;
    fireEvent.scroll(node, { target: { scrollTop: 10 } });
    await user.selectOptions(scopeSelect(), "project");
    await screen.findByText("project history");
    await user.selectOptions(scopeSelect(), "workspace");
    await screen.findByText("workspace history");
    expect(node.scrollTop).toBe(10);
  });

  it("keeps the offset a scope was left with when the pane's own positioning was never reported as a scroll", async () => {
    const user = userEvent.setup();
    renderChat(`/portal/chat?scope=workspace&projectId=${projectId}`, {
      conversation: (url) => new Response(JSON.stringify(url.searchParams.get("scope") === "project"
        ? conversationBody({ scope: "project", projectId, messages: [message(2, { content: "project history" })] })
        : conversationBody({ scope: "workspace", messages: [message(1, { content: "workspace history" })] }))),
    });
    await openChat();
    await screen.findByText("workspace history");

    // Chromium coalesces a pane's scroll events: a pane positioned and left in
    // the same frame is never reported as a scroll at all, so the offset this
    // scope was left with is only observable while its own transcript is still
    // on the pane — at the moment the switch is requested.
    const node = scrollNode();
    node.scrollTop = 120;
    expect(node.scrollTop).toBe(120);

    await user.selectOptions(scopeSelect(), "project");
    await screen.findByText("project history");
    node.scrollTop = 60;
    expect(node.scrollTop).toBe(60);

    await user.selectOptions(scopeSelect(), "workspace");
    await screen.findByText("workspace history");
    expect(node.scrollTop).toBe(120);

    await user.selectOptions(scopeSelect(), "project");
    expect(node.scrollTop).toBe(60);
  });

  it("keeps the composer focused with its caret selection across a background refresh", async () => {
    const { application } = renderChat(`/portal/chat?scope=workspace&projectId=${projectId}`, {
      conversation: () => new Response(JSON.stringify(conversationBody({ scope: "workspace", messages: [message(1, { content: "history" })] }))),
    });
    await openChat();
    await screen.findByText("history");
    fireEvent.change(contentField(), { target: { value: "keep editing during refresh" } });
    contentField().focus();
    contentField().setSelectionRange(5, 12);
    await act(async () => {
      await application.queryClient.refetchQueries({ queryKey: ["chat"] });
    });
    expect(contentField()).toHaveFocus();
    expect(contentField()).toHaveValue("keep editing during refresh");
    expect([contentField().selectionStart, contentField().selectionEnd]).toEqual([5, 12]);
  });
});

describe("receipts", () => {
  it("renders a pending command as awaiting acknowledgement and applied only from the served outcome", async () => {
    renderChat(`/portal/chat?scope=project&projectId=${projectId}`, {
      conversation: () => new Response(JSON.stringify(conversationBody({
        scope: "project",
        projectId,
        messages: [
          message(1, { intent: "guidance", content: "keep it small", command: command({ state: "pending", kind: "guidance" }) }),
          message(2, { role: "assistant", intent: "decision", content: "staged", command: command({ id: 2, state: "acknowledged", kind: "decision", outcome: "applied" }) }),
        ],
      }))),
    });
    await openChat();
    await screen.findByText("keep it small");
    const pending = document.querySelector(`[data-receipt-for="${messageId(1)}"]`);
    expect(pending).not.toBeNull();
    expect(pending).toHaveTextContent("Saved · awaiting agent acknowledgement");
    const applied = document.querySelector(`[data-receipt-for="${messageId(2)}"]`);
    expect(applied).toHaveAttribute("data-outcome", "applied");
    expect(applied).toHaveTextContent("Applied");
  });

  it("never refreshes a retained page's receipt against a page that does not carry it", async () => {
    const user = userEvent.setup();
    let read = 0;
    renderChat(`/portal/chat?scope=project&projectId=${projectId}`, {
      conversation: (url) => {
        if (url.searchParams.get("before") === "cursor-1") {
          return new Response(JSON.stringify(conversationBody({ scope: "project", projectId, messages: [message(1, { content: "old command", command: command({ state: "acknowledged", outcome: "applied" }) })], nextBefore: null })));
        }
        read += 1;
        return new Response(JSON.stringify(read === 1
          ? conversationBody({ scope: "project", projectId, messages: [message(2, { content: "newest" })], nextBefore: "cursor-1" })
          : conversationBody({ scope: "project", projectId, messages: [message(3, { content: "refreshed newest" })], nextBefore: "cursor-1" })));
      },
    });
    await openChat();
    await screen.findByText("newest");
    await user.click(screen.getByRole("button", { name: "Load older" }));
    await screen.findByText("old command");
    // The message arrived with an older page, so its receipt says exactly that
    // instead of repeating an outcome the current page did not refresh.
    const retained = document.querySelector(`[data-receipt-for="${messageId(1)}"]`);
    expect(retained).toHaveAttribute("data-outcome", "retained");
    expect(retained).toHaveTextContent("Acknowledgement not refreshed with this page");
    expect(retained).not.toHaveTextContent("Applied");

    await user.click(screen.getByRole("button", { name: "Refresh" }));
    await screen.findByText("refreshed newest");
    expect(document.querySelector(`[data-receipt-for="${messageId(1)}"]`)).toHaveTextContent("Acknowledgement not refreshed with this page");
  });
});

describe("history", () => {
  it("retains loaded pages across a refresh, dedupes ids and names an unloaded boundary", async () => {
    const user = userEvent.setup();
    let read = 0;
    const older: Record<string, ServedMessage[]> = {
      "cursor-1": [message(1, { insertedAt: "2026-11-16T09:00:00.000000Z" }), message(2, { insertedAt: "2026-11-16T09:01:00.000000Z" })],
      "cursor-2": [message(2, { insertedAt: "2026-11-16T09:01:00.000000Z" })],
      "cursor-2-resigned": [message(2, { insertedAt: "2026-11-16T09:01:00.000000Z" })],
    };
    renderChat(`/portal/chat?scope=project&projectId=${projectId}`, {
      conversation: (url) => {
        const before = url.searchParams.get("before");
        if (before !== null) {
          const page = older[before] ?? [];
          return new Response(JSON.stringify(conversationBody({ scope: "project", projectId, messages: page, nextBefore: null })));
        }
        read += 1;
        const pages = [
          conversationBody({ scope: "project", projectId, messages: [message(3, { insertedAt: "2026-11-16T09:02:00.000000Z" }), message(4, { insertedAt: "2026-11-16T09:03:00.000000Z" })], nextBefore: "cursor-1" }),
          conversationBody({ scope: "project", projectId, messages: [message(5, { insertedAt: "2026-11-16T09:04:00.000000Z" }), message(6, { insertedAt: "2026-11-16T09:05:00.000000Z" })], nextBefore: "cursor-2" }),
          // The same boundary, re-served by a later read: the served cursor is
          // re-signed per response, so its bytes differ while the boundary
          // message the note is judged by does not.
          conversationBody({ scope: "project", projectId, messages: [message(5, { insertedAt: "2026-11-16T09:04:00.000000Z" }), message(6, { insertedAt: "2026-11-16T09:05:00.000000Z" })], nextBefore: "cursor-2-resigned" }),
        ];
        return new Response(JSON.stringify(pages[Math.min(read, pages.length) - 1]));
      },
    });
    await openChat();
    await screen.findByText("message 3");
    expect(messageIds()).toHaveLength(2);

    await user.click(screen.getByRole("button", { name: "Load older" }));
    await screen.findByText("message 1");
    expect(messageIds()).toEqual([messageId(1), messageId(2), messageId(3), messageId(4)]);
    expect(screen.queryByRole("button", { name: "Load older" })).not.toBeInTheDocument();

    // A refresh with a moved boundary: the retained page shares no id with the
    // refreshed page, so the surface names the region it never loaded.
    await user.click(screen.getByRole("button", { name: "Refresh" }));
    await screen.findByText("message 5");
    const gap = document.querySelector("#chat-history-gap");
    expect(gap).not.toBeNull();
    expect(gap).toHaveAttribute("data-boundary-current", messageId(5));
    expect(gap).toHaveAttribute("data-boundary-retained", messageId(2));
    expect(messageIds()).toEqual([messageId(1), messageId(2), messageId(5), messageId(6)]);

    // Loading older from the refreshed boundary closes the gap and keeps the
    // already loaded history deduped.
    await user.click(screen.getByRole("button", { name: "Load older" }));
    await waitFor(() => expect(document.querySelector("#chat-history-gap")).toBeNull());
    expect(messageIds()).toEqual([messageId(1), messageId(2), messageId(5), messageId(6)]);

    // A later refresh on the same boundary reports no gap either.
    await user.click(screen.getByRole("button", { name: "Refresh" }));
    await screen.findByText("message 5");
    expect(document.querySelector("#chat-history-gap")).toBeNull();
    expect(new Set(messageIds())).toEqual(new Set([messageId(1), messageId(2), messageId(5), messageId(6)]));
  });

  it("keeps naming the region a moved boundary skipped until a load reaches the retained chain", async () => {
    const user = userEvent.setup();
    let read = 0;
    const at = (index: number) => `2026-11-16T09:00:${String(index).padStart(2, "0")}.000000Z`;
    const older: Record<string, ServedMessage[]> = {
      "cursor-9": [message(7, { insertedAt: at(7) }), message(8, { insertedAt: at(8) })],
      "cursor-13": [message(11, { insertedAt: at(11) }), message(12, { insertedAt: at(12) })],
      "cursor-11": [message(9, { insertedAt: at(9) }), message(10, { insertedAt: at(10) })],
    };
    renderChat(`/portal/chat?scope=project&projectId=${projectId}`, {
      conversation: (url) => {
        const before = url.searchParams.get("before");
        if (before !== null) {
          const nextBefore = before === "cursor-13" ? "cursor-11" : before === "cursor-11" ? "cursor-9" : "cursor-7";
          return new Response(JSON.stringify(conversationBody({ scope: "project", projectId, messages: older[before] ?? [], nextBefore })));
        }
        read += 1;
        return new Response(JSON.stringify(read === 1
          ? conversationBody({ scope: "project", projectId, messages: [message(9, { insertedAt: at(9) }), message(10, { insertedAt: at(10) })], nextBefore: "cursor-9" })
          : conversationBody({ scope: "project", projectId, messages: [message(13, { insertedAt: at(13) }), message(14, { insertedAt: at(14) })], nextBefore: "cursor-13" })));
      },
    });
    await openChat();
    await screen.findByText("message 9");
    await user.click(screen.getByRole("button", { name: "Load older" }));
    await screen.findByText("message 7");
    expect(document.querySelector("#chat-history-gap")).toBeNull();
    expect(messageIds()).toEqual([7, 8, 9, 10].map(messageId));

    // A re-read ends at a later boundary: the retained page was fetched from the
    // boundary the re-read no longer carries, so the region between them is named.
    await user.click(screen.getByRole("button", { name: "Refresh" }));
    await screen.findByText("message 13");
    const moved = document.querySelector("#chat-history-gap");
    expect(moved).not.toBeNull();
    expect(moved).toHaveAttribute("data-boundary-current", messageId(13));
    expect(moved).toHaveAttribute("data-boundary-retained", messageId(8));
    expect(messageIds()).toEqual([7, 8, 13, 14].map(messageId));

    // A load that only reaches the refreshed boundary does not connect the
    // chains: messages 9 and 10 are still unloaded, so the surface keeps saying
    // so — and keeps the retained page the note names.
    await user.click(screen.getByRole("button", { name: "Load older" }));
    await screen.findByText("message 11");
    const still = document.querySelector("#chat-history-gap");
    expect(still).not.toBeNull();
    expect(still).toHaveAttribute("data-boundary-current", messageId(11));
    expect(still).toHaveAttribute("data-boundary-retained", messageId(8));
    expect(messageIds()).toEqual([7, 8, 11, 12, 13, 14].map(messageId));

    // The reader's next load fills the skipped region; the chains now touch, so
    // the note goes away and the messages it named render again.
    await user.click(screen.getByRole("button", { name: "Load older" }));
    await screen.findByText("message 9");
    await waitFor(() => expect(document.querySelector("#chat-history-gap")).toBeNull());
    expect(messageIds()).toEqual([7, 8, 9, 10, 11, 12, 13, 14].map(messageId));
  });

  it("keeps the chain a refresh re-signs at the same boundary and restarts it at a moved one", async () => {
    const user = userEvent.setup();
    let read = 0;
    const requested: string[] = [];
    const at = (index: number) => `2026-11-16T09:00:${String(index).padStart(2, "0")}.000000Z`;
    const older: Record<string, ServedMessage[]> = {
      "cursor-5": [message(3, { insertedAt: at(3) }), message(4, { insertedAt: at(4) })],
      "cursor-3": [message(1, { insertedAt: at(1) }), message(2, { insertedAt: at(2) })],
      "cursor-8": [message(5, { insertedAt: at(5) }), message(6, { insertedAt: at(6) }), message(7, { insertedAt: at(7) })],
    };
    renderChat(`/portal/chat?scope=project&projectId=${projectId}`, {
      conversation: (url) => {
        const before = url.searchParams.get("before");
        if (before !== null) {
          requested.push(before);
          const nextBefore = before === "cursor-5" ? "cursor-3" : before === "cursor-3" ? "cursor-1" : null;
          return new Response(JSON.stringify(conversationBody({ scope: "project", projectId, messages: older[before] ?? [], nextBefore })));
        }
        read += 1;
        const pages = [
          conversationBody({ scope: "project", projectId, messages: [message(5, { insertedAt: at(5) }), message(6, { insertedAt: at(6) }), message(7, { insertedAt: at(7) }), message(8, { insertedAt: at(8) })], nextBefore: "cursor-5" }),
          // The same boundary, re-served by a later read with a cursor the
          // server signed afresh: the token bytes differ while the boundary
          // message the surface's chain hangs on does not.
          conversationBody({ scope: "project", projectId, messages: [message(5, { insertedAt: at(5) }), message(6, { insertedAt: at(6) }), message(7, { insertedAt: at(7) }), message(8, { insertedAt: at(8) }), message(9, { insertedAt: at(9) })], nextBefore: "cursor-5-resigned" }),
          // A moved boundary: the served page no longer ends at message 5.
          conversationBody({ scope: "project", projectId, messages: [message(8, { insertedAt: at(8) }), message(9, { insertedAt: at(9) }), message(10, { insertedAt: at(10) })], nextBefore: "cursor-8" }),
        ];
        return new Response(JSON.stringify(pages[Math.min(read, pages.length) - 1]));
      },
    });
    await openChat();
    await screen.findByText("message 5");

    // Load older walks the chain down from the served boundary at message 5.
    await user.click(screen.getByRole("button", { name: "Load older" }));
    await screen.findByText("message 3");
    expect(requested).toEqual(["cursor-5"]);

    // The refresh re-signs the cursor for the same boundary position, so the
    // chain pointer stays where the reader's loads reached: the next press
    // continues below message 3 instead of re-reading the served boundary.
    await user.click(screen.getByRole("button", { name: "Refresh" }));
    await screen.findByText("message 9");
    await user.click(screen.getByRole("button", { name: "Load older" }));
    await waitFor(() => expect(requested.length).toBe(2));
    expect(requested).toEqual(["cursor-5", "cursor-3"]);
    await screen.findByText("message 1");

    // A refresh that moves the boundary restarts the chain from the boundary
    // the served page now ends at; the region the retained chain does not
    // reach stays named until the reader's own load fills it.
    await user.click(screen.getByRole("button", { name: "Refresh" }));
    await screen.findByText("message 10");
    const gap = document.querySelector("#chat-history-gap");
    expect(gap).not.toBeNull();
    expect(gap).toHaveAttribute("data-boundary-current", messageId(8));
    expect(gap).toHaveAttribute("data-boundary-retained", messageId(4));
    await user.click(screen.getByRole("button", { name: "Load older" }));
    await screen.findByText("message 5");
    await waitFor(() => expect(document.querySelector("#chat-history-gap")).toBeNull());
    expect(requested).toEqual(["cursor-5", "cursor-3", "cursor-8"]);
    expect(messageIds()).toEqual([1, 2, 3, 4, 5, 6, 7, 8, 9, 10].map(messageId));
  });

  it("renews the served cursor at an unchanged boundary only while the reader has not paged past it", async () => {
    const user = userEvent.setup();
    let read = 0;
    const requested: string[] = [];
    const held = Promise.withResolvers<void>();
    const at = (index: number) => `2026-11-16T09:00:${String(index).padStart(2, "0")}.000000Z`;
    const older: Record<string, ServedMessage[]> = {
      "cursor-5-resigned": [message(3, { insertedAt: at(3) }), message(4, { insertedAt: at(4) })],
      "cursor-5-resigned-2": [message(3, { insertedAt: at(3) }), message(4, { insertedAt: at(4) })],
      "cursor-3": [message(1, { insertedAt: at(1) }), message(2, { insertedAt: at(2) })],
    };
    renderChat(`/portal/chat?scope=project&projectId=${projectId}`, {
      conversation: (url) => {
        const before = url.searchParams.get("before");
        if (before !== null) {
          requested.push(before);
          if (before === "cursor-5-resigned") {
            // The press is held in flight: the re-serve below renews the
            // surface's cursor while this request still carries its own token.
            return held.promise.then(() => new Response(JSON.stringify(conversationBody({ scope: "project", projectId, messages: older[before] ?? [], nextBefore: "cursor-3" }))));
          }
          const nextBefore = before === "cursor-3" ? null : "cursor-3";
          return new Response(JSON.stringify(conversationBody({ scope: "project", projectId, messages: older[before] ?? [], nextBefore })));
        }
        read += 1;
        const served = [message(5, { insertedAt: at(5) }), message(6, { insertedAt: at(6) }), message(7, { insertedAt: at(7) }), message(8, { insertedAt: at(8) }), message(9, { insertedAt: at(9) })];
        const pages = [
          conversationBody({ scope: "project", projectId, messages: served.slice(0, 4), nextBefore: "cursor-5" }),
          // The same boundary re-served with a cursor the server signed
          // afresh, while the reader has not paged past it.
          conversationBody({ scope: "project", projectId, messages: served, nextBefore: "cursor-5-resigned" }),
          // A further re-serve at that boundary, now while the press is still
          // in flight with the previous token.
          conversationBody({ scope: "project", projectId, messages: [...served, message(10, { insertedAt: at(10) })], nextBefore: "cursor-5-resigned-2" }),
          // A re-serve after the pressed page landed: the reader has paged
          // past the boundary, so the older position cannot be re-signed.
          conversationBody({ scope: "project", projectId, messages: [...served, message(10, { insertedAt: at(10) }), message(11, { insertedAt: at(11) })], nextBefore: "cursor-5-resigned-3" }),
        ];
        return new Response(JSON.stringify(pages[Math.min(read, pages.length) - 1]));
      },
    });
    await openChat();
    await screen.findByText("message 5");

    // The refresh re-signs the boundary under the reader's own cursor, so the
    // next press loads through the re-signed token instead of the aging one —
    // and is held in flight from here on.
    await user.click(screen.getByRole("button", { name: "Refresh" }));
    await screen.findByText("message 9");
    await user.click(screen.getByRole("button", { name: "Load older" }));
    await waitFor(() => expect(requested.length).toBe(1));
    expect(requested).toEqual(["cursor-5-resigned"]);

    // Another re-serve at the same boundary renews the surface's cursor
    // underneath the in-flight press — a press made now consumes the renewed
    // token, so the renewal is live under the held one — while the held press
    // stays neither cancelled nor rebound to the boundary that arrived
    // afterwards.
    await user.click(screen.getByRole("button", { name: "Refresh" }));
    await screen.findByText("message 10");
    await user.click(screen.getByRole("button", { name: "Load older" }));
    await waitFor(() => expect(requested.length).toBe(2));
    expect(requested).toEqual(["cursor-5-resigned", "cursor-5-resigned-2"]);
    await screen.findByText("message 3");
    held.resolve();
    // The held press's page belongs to the boundary its own cursor was minted
    // from — the one it captured then — so it lands deduped and the chain
    // keeps the continuation the renewed press loaded.
    await user.click(screen.getByRole("button", { name: "Refresh" }));
    await screen.findByText("message 11");
    expect(messageIds()).toEqual([3, 4, 5, 6, 7, 8, 9, 10, 11].map(messageId));

    // The pressed page continues the chain below the served boundary: the
    // reader has paged past the boundary now, so a further re-serve cannot
    // re-sign that older position and the chain keeps its own cursor.
    await user.click(screen.getByRole("button", { name: "Load older" }));
    await waitFor(() => expect(requested.length).toBe(3));
    expect(requested).toEqual(["cursor-5-resigned", "cursor-5-resigned-2", "cursor-3"]);
    await screen.findByText("message 1");
    expect(messageIds()).toEqual([1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11].map(messageId));
  });

  it("restarts the chain from the served page when a cursor the reader has paged past is refused", async () => {
    const user = userEvent.setup();
    let read = 0;
    const requested: string[] = [];
    const at = (index: number) => `2026-11-16T09:00:${String(index).padStart(2, "0")}.000000Z`;
    const older: Record<string, ServedMessage[]> = {
      "cursor-9": [message(7, { insertedAt: at(7) }), message(8, { insertedAt: at(8) })],
      "cursor-9-resigned-2": [message(7, { insertedAt: at(7) }), message(8, { insertedAt: at(8) })],
      "cursor-7-resigned": [message(5, { insertedAt: at(5) }), message(6, { insertedAt: at(6) })],
    };
    renderChat(`/portal/chat?scope=project&projectId=${projectId}`, {
      conversation: (url) => {
        const before = url.searchParams.get("before");
        if (before !== null) {
          requested.push(before);
          if (before === "cursor-7") {
            // The server's own refusal for a cursor past its `max_age`.
            return new Response(JSON.stringify({ error: { code: "invalid_request", message: "request payload is invalid" } }), { status: 400 });
          }
          const nextBefore = before === "cursor-9" ? "cursor-7" : before === "cursor-9-resigned-2" ? "cursor-7-resigned" : null;
          return new Response(JSON.stringify(conversationBody({ scope: "project", projectId, messages: older[before] ?? [], nextBefore })));
        }
        read += 1;
        const served = [message(9, { insertedAt: at(9) }), message(10, { insertedAt: at(10) })];
        const pages = [
          conversationBody({ scope: "project", projectId, messages: served, nextBefore: "cursor-9" }),
          // The same boundary re-served while the reader has paged past it:
          // the cursor keeps its own position.
          conversationBody({ scope: "project", projectId, messages: [...served, message(11, { insertedAt: at(11) })], nextBefore: "cursor-9-resigned" }),
          // The re-read the refusal triggers re-signs that boundary.
          conversationBody({ scope: "project", projectId, messages: [...served, message(11, { insertedAt: at(11) }), message(12, { insertedAt: at(12) })], nextBefore: "cursor-9-resigned-2" }),
        ];
        return new Response(JSON.stringify(pages[Math.min(read, pages.length) - 1]));
      },
    });
    await openChat();
    await screen.findByText("message 9");

    // Load older walks below the served boundary: the chain pointer now names
    // message 7's position, which the served page cannot re-sign.
    await user.click(screen.getByRole("button", { name: "Load older" }));
    await screen.findByText("message 7");
    expect(requested).toEqual(["cursor-9"]);

    await user.click(screen.getByRole("button", { name: "Refresh" }));
    await screen.findByText("message 11");

    // The press is refused — the token aged past the server's `max_age` — and
    // the refusal stays the composer's notice.
    await user.click(screen.getByRole("button", { name: "Load older" }));
    await waitFor(() => expect(requested).toEqual(["cursor-9", "cursor-7"]));
    await waitFor(() => expect(statusRegion()).toHaveTextContent("The server returned an unexpected response."));

    // The refusal restarts the chain from the served page and re-reads it, so
    // the boundary is re-signed and the next press walks below it again.
    await screen.findByText("message 12");
    await user.click(screen.getByRole("button", { name: "Load older" }));
    await waitFor(() => expect(requested).toEqual(["cursor-9", "cursor-7", "cursor-9-resigned-2"]));
    // The re-walked page lands deduped — its messages already render — and
    // continues the chain below the retained page.
    await waitFor(() => expect(screen.getByRole("button", { name: "Load older" })).toHaveAttribute("aria-disabled", "false"));
    expect(messageIds()).toEqual([7, 8, 9, 10, 11, 12].map(messageId));
    await user.click(screen.getByRole("button", { name: "Load older" }));
    await waitFor(() => expect(requested).toEqual(["cursor-9", "cursor-7", "cursor-9-resigned-2", "cursor-7-resigned"]));
    await screen.findByText("message 5");
    expect(messageIds()).toEqual([5, 6, 7, 8, 9, 10, 11, 12].map(messageId));
  });

  it("leaves the chain alone when the refused press no longer owns it", async () => {
    const user = userEvent.setup();
    const requested: string[] = [];
    let reads = 0;
    const held = Promise.withResolvers<void>();
    const at = (index: number) => `2026-11-16T09:00:${String(index).padStart(2, "0")}.000000Z`;
    const older: Record<string, ServedMessage[]> = {
      "cursor-5-resigned": [message(3, { insertedAt: at(3) }), message(4, { insertedAt: at(4) })],
      "cursor-3": [message(1, { insertedAt: at(1) }), message(2, { insertedAt: at(2) })],
    };
    renderChat(`/portal/chat?scope=project&projectId=${projectId}`, {
      conversation: (url) => {
        const before = url.searchParams.get("before");
        if (before !== null) {
          requested.push(before);
          if (before === "cursor-5") {
            // The first press carries the aging token and is held in flight:
            // the server refuses it only once the later press's page has
            // already moved the chain.
            return held.promise.then(() => new Response(JSON.stringify({ error: { code: "invalid_request", message: "request payload is invalid" } }), { status: 400 }));
          }
          return new Response(JSON.stringify(conversationBody({
            scope: "project",
            projectId,
            messages: older[before] ?? [],
            nextBefore: before === "cursor-5-resigned" ? "cursor-3" : null,
          })));
        }
        reads += 1;
        const served = [message(5, { insertedAt: at(5) }), message(6, { insertedAt: at(6) }), message(7, { insertedAt: at(7) }), message(8, { insertedAt: at(8) })];
        return new Response(JSON.stringify(reads === 1
          ? conversationBody({ scope: "project", projectId, messages: served, nextBefore: "cursor-5" })
          // The same boundary re-served with a token the server signed afresh,
          // while the reader's first press is still in flight with the aging
          // one.
          : conversationBody({ scope: "project", projectId, messages: [...served, message(9, { insertedAt: at(9) })], nextBefore: "cursor-5-resigned" })));
      },
    });
    await openChat();
    await screen.findByText("message 5");

    // The aging token's press is held in flight; the re-serve renews the
    // surface's cursor, the reader presses again through the renewed token,
    // and that page lands first, moving the chain below the served boundary.
    await user.click(screen.getByRole("button", { name: "Load older" }));
    await waitFor(() => expect(requested).toEqual(["cursor-5"]));
    await user.click(screen.getByRole("button", { name: "Refresh" }));
    await screen.findByText("message 9");
    await user.click(screen.getByRole("button", { name: "Load older" }));
    await screen.findByText("message 3");
    expect(requested).toEqual(["cursor-5", "cursor-5-resigned"]);

    // The refused press no longer owns the chain: its refusal stays the
    // notice — no rewind to the served boundary and no re-read of the scope.
    held.resolve();
    await waitFor(() => expect(statusRegion()).toHaveTextContent("The server returned an unexpected response."));
    expect(reads).toBe(2);

    // The next press continues below the page that landed, not from the
    // served boundary the refused press predated.
    await user.click(screen.getByRole("button", { name: "Load older" }));
    await waitFor(() => expect(requested).toEqual(["cursor-5", "cursor-5-resigned", "cursor-3"]));
    await screen.findByText("message 1");
    expect(messageIds()).toEqual([1, 2, 3, 4, 5, 6, 7, 8, 9].map(messageId));
  });

  it("keeps the page a re-serve overtook bound to the boundary its cursor was minted from", async () => {
    const user = userEvent.setup();
    let read = 0;
    const at = (index: number) => `2026-11-16T09:00:${String(index).padStart(2, "0")}.000000Z`;
    const held = Promise.withResolvers<void>();
    const older: Record<string, ServedMessage[]> = {
      "cursor-13": [message(11, { insertedAt: at(11) }), message(12, { insertedAt: at(12) })],
      "cursor-11": [message(9, { insertedAt: at(9) }), message(10, { insertedAt: at(10) })],
    };
    renderChat(`/portal/chat?scope=project&projectId=${projectId}`, {
      conversation: (url) => {
        const before = url.searchParams.get("before");
        if (before === "cursor-9") {
          return held.promise.then(() => new Response(JSON.stringify(conversationBody({ scope: "project", projectId, messages: [message(7, { insertedAt: at(7) }), message(8, { insertedAt: at(8) })], nextBefore: null }))));
        }
        if (before !== null) {
          return new Response(JSON.stringify(conversationBody({ scope: "project", projectId, messages: older[before] ?? [], nextBefore: before === "cursor-13" ? "cursor-11" : null })));
        }
        read += 1;
        return new Response(JSON.stringify(read === 1
          ? conversationBody({ scope: "project", projectId, messages: [message(9, { insertedAt: at(9) }), message(10, { insertedAt: at(10) })], nextBefore: "cursor-9" })
          : conversationBody({ scope: "project", projectId, messages: [message(13, { insertedAt: at(13) }), message(14, { insertedAt: at(14) })], nextBefore: "cursor-13" })));
      },
    });
    await openChat();
    await screen.findByText("message 9");

    // The press captures cursor-9, minted relative to message 9; the re-serve
    // lands before the page does and moves the surface to a later boundary.
    await user.click(screen.getByRole("button", { name: "Load older" }));
    await user.click(screen.getByRole("button", { name: "Refresh" }));
    await screen.findByText("message 13");
    held.resolve();
    await screen.findByText("message 7");

    // The page was fetched from message 9's chain, not from the re-served
    // boundary: the region between the two is unloaded and stays named.
    const gap = document.querySelector("#chat-history-gap");
    expect(gap).not.toBeNull();
    expect(gap).toHaveAttribute("data-boundary-current", messageId(13));
    expect(gap).toHaveAttribute("data-boundary-retained", messageId(8));
    expect(messageIds()).toEqual([7, 8, 13, 14].map(messageId));

    // The reader's own loads fill it from the served boundary.
    await user.click(screen.getByRole("button", { name: "Load older" }));
    await screen.findByText("message 11");
    expect(document.querySelector("#chat-history-gap")).toHaveAttribute("data-boundary-current", messageId(11));
    await user.click(screen.getByRole("button", { name: "Load older" }));
    await screen.findByText("message 9");
    await waitFor(() => expect(document.querySelector("#chat-history-gap")).toBeNull());
    expect(messageIds()).toEqual([7, 8, 9, 10, 11, 12, 13, 14].map(messageId));
  });

  it("keeps two in-flight loads each on the boundary its own cursor was minted from", async () => {
    const user = userEvent.setup();
    let read = 0;
    const at = (index: number) => `2026-11-16T09:00:${String(index).padStart(2, "0")}.000000Z`;
    const held = Promise.withResolvers<void>();
    renderChat(`/portal/chat?scope=project&projectId=${projectId}`, {
      conversation: (url) => {
        const before = url.searchParams.get("before");
        if (before === "cursor-9") {
          return held.promise.then(() => new Response(JSON.stringify(conversationBody({ scope: "project", projectId, messages: [message(7, { insertedAt: at(7) }), message(8, { insertedAt: at(8) })], nextBefore: null }))));
        }
        if (before === "cursor-13") {
          return new Response(JSON.stringify(conversationBody({ scope: "project", projectId, messages: [message(11, { insertedAt: at(11) }), message(12, { insertedAt: at(12) })], nextBefore: "cursor-11" })));
        }
        if (before === "cursor-11") {
          return new Response(JSON.stringify(conversationBody({ scope: "project", projectId, messages: [message(9, { insertedAt: at(9) }), message(10, { insertedAt: at(10) })], nextBefore: null })));
        }
        read += 1;
        return new Response(JSON.stringify(read === 1
          ? conversationBody({ scope: "project", projectId, messages: [message(9, { insertedAt: at(9) }), message(10, { insertedAt: at(10) })], nextBefore: "cursor-9" })
          : conversationBody({ scope: "project", projectId, messages: [message(13, { insertedAt: at(13) }), message(14, { insertedAt: at(14) })], nextBefore: "cursor-13" })));
      },
    });
    await openChat();
    await screen.findByText("message 9");

    // Press one leaves through cursor-9 and is held; the re-serve re-keys the
    // read; press two then leaves through cursor-13 and lands first.
    await user.click(screen.getByRole("button", { name: "Load older" }));
    await user.click(screen.getByRole("button", { name: "Refresh" }));
    await screen.findByText("message 13");
    await user.click(screen.getByRole("button", { name: "Load older" }));
    await screen.findByText("message 11");
    held.resolve();
    await screen.findByText("message 7");

    // Press two chains from the re-served boundary; press one's page belongs to
    // the chain its own cursor was minted from, so the region it skips stays
    // named instead of being excused by the boundary the press never saw.
    const gap = document.querySelector("#chat-history-gap");
    expect(gap).not.toBeNull();
    expect(gap).toHaveAttribute("data-boundary-current", messageId(11));
    expect(gap).toHaveAttribute("data-boundary-retained", messageId(8));
    expect(messageIds()).toEqual([7, 8, 11, 12, 13, 14].map(messageId));

    // The continuation still chains from press two's page, not from the page
    // its press overtook: Load older is still offered, and the next press fills
    // the region between the two chains and closes the gap.
    await user.click(screen.getByRole("button", { name: "Load older" }));
    await screen.findByText("message 9");
    await waitFor(() => expect(document.querySelector("#chat-history-gap")).toBeNull());
    expect(messageIds()).toEqual([7, 8, 9, 10, 11, 12, 13, 14].map(messageId));
  });

  it("surfaces a failed older page and leaves the button ready to retry", async () => {
    const user = userEvent.setup();
    let olderReads = 0;
    renderChat(`/portal/chat?scope=project&projectId=${projectId}`, {
      conversation: (url) => {
        const before = url.searchParams.get("before");
        if (before === "cursor-9") {
          olderReads += 1;
          return olderReads === 1
            ? new Response("{}", { status: 409 })
            : new Response(JSON.stringify(conversationBody({ scope: "project", projectId, messages: [message(7), message(8)], nextBefore: null })));
        }
        return new Response(JSON.stringify(conversationBody({ scope: "project", projectId, messages: [message(9), message(10)], nextBefore: "cursor-9" })));
      },
    });
    await openChat();
    await screen.findByText("message 9");

    // The refusal is the reader's to see and retry: it is named on the status
    // line, the surface keeps the pages it has, and the button is ready again.
    await user.click(screen.getByRole("button", { name: "Load older" }));
    await waitFor(() => expect(statusRegion().textContent).toBe("The workspace changed. Refresh to load its current state."));
    expect(messageIds()).toEqual([9, 10].map(messageId));
    expect(screen.getByRole("button", { name: "Load older" })).toHaveAttribute("aria-disabled", "false");

    await user.click(screen.getByRole("button", { name: "Load older" }));
    await screen.findByText("message 7");
    expect(messageIds()).toEqual([7, 8, 9, 10].map(messageId));
  });

  it("keeps a page a scope switch overtook in the scope that pressed Load older", async () => {
    const user = userEvent.setup();
    const held = Promise.withResolvers<void>();
    renderChat(`/portal/chat?scope=project&projectId=${projectId}`, {
      conversation: (url) => {
        const before = url.searchParams.get("before");
        if (before === "cursor-9") {
          return held.promise.then(() => new Response(JSON.stringify(conversationBody({ scope: "project", projectId, messages: [message(7), message(8)], nextBefore: null }))));
        }
        return url.searchParams.get("scope") === "project"
          ? new Response(JSON.stringify(conversationBody({ scope: "project", projectId, messages: [message(9), message(10)], nextBefore: "cursor-9" })))
          : new Response(JSON.stringify(conversationBody({ scope: "workspace", messages: [message(1, { content: "workspace history" })] })));
      },
    });
    await openChat();
    await screen.findByText("message 9");

    // The press belongs to the project scope; the reader leaves for the
    // workspace before the page arrives.
    await user.click(screen.getByRole("button", { name: "Load older" }));
    await user.selectOptions(scopeSelect(), "workspace");
    await screen.findByText("workspace history");
    held.resolve();

    // The late page lands in the scope that pressed it, never in the shown one.
    await user.selectOptions(scopeSelect(), "project");
    await screen.findByText("message 7");
    expect(messageIds()).toEqual([7, 8, 9, 10].map(messageId));
    await user.selectOptions(scopeSelect(), "workspace");
    await screen.findByText("workspace history");
    expect(messageIds()).toEqual([messageId(1)]);
  });

  it("keeps the loaded conversation visible as stale when a read fails", async () => {
    const user = userEvent.setup();
    let fail = false;
    renderChat(`/portal/chat?scope=project&projectId=${projectId}`, {
      conversation: () => (fail
        ? new Response("{}", { status: 403 })
        : new Response(JSON.stringify(conversationBody({ scope: "project", projectId, messages: [message(1, { content: "loaded history" })] })))),
    });
    await openChat();
    await screen.findByText("loaded history");
    fail = true;
    await user.click(screen.getByRole("button", { name: "Refresh" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("You do not have access to this workspace.");
    expect(screen.getByText("loaded history")).toBeInTheDocument();
    fail = false;
    await user.click(screen.getByRole("button", { name: "Try again" }));
    await waitFor(() => expect(screen.queryByRole("alert")).not.toBeInTheDocument());
    expect(screen.getByText("loaded history")).toBeInTheDocument();
  });
});

describe("run scope", () => {
  function runDetail(options: { waiting?: boolean; state?: string; failure?: Record<string, unknown> | null; evidence?: boolean } = {}) {
    const detail = readWorkDetail({
      id: workItemId,
      projectId,
      title: "Subject work item",
      key: "ONE-1",
      execution: {
        run_id: runId,
        generation: 3,
        state: options.state ?? (options.waiting === true ? "waiting_for_input" : "running"),
        runtime_id: runtimeId,
        can_guide: options.waiting !== true,
        can_pause: options.waiting !== true,
        can_resume: false,
        failure: options.failure ?? null,
        ...(options.waiting === true ? {
          waiting: {
            run_id: runId,
            generation: 3,
            transition_id: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb",
            question: "Which strategy should be used?",
            decision: {
              context: "Two strategies are valid.",
              options: [
                { id: "staged", label: "Stage the artifact", consequence: "Keeps rollback available." },
                { id: "defer", label: "Defer delivery", consequence: "Stays unpublished." },
              ],
              recommended_option_id: "staged",
            },
            payload: {},
            recorded_at: "2026-11-16T11:00:00.000000Z",
          },
        } : {}),
      },
    });
    // The chat read serves its semantic page on the projection's own `timeline`
    // member while `raw.timeline` stays blanked.
    return {
      ...detail,
      timeline: options.evidence === true ? [{
        source: "event",
        run_id: runId,
        generation: 3,
        recorded_at: "2026-11-16T09:58:00.000000Z",
        data: { event_id: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", sequence: 1, kind: "progress", payload: { message: "Two files changed." }, occurred_at: "2026-11-16T09:58:00.000000Z" },
      }] : [],
    };
  }

  const runHeaders = { runId: runId, projectId };

  it("names the served run, the guidance delivery and the unsupported pause, and offers no pause control", async () => {
    renderChat(`/portal/chat?scope=run&runId=${runHeaders.runId}&projectId=${runHeaders.projectId}`, {
      conversation: () => new Response(JSON.stringify(conversationBody({ scope: "run", projectId, runId, runs: [runDetail()] }))),
      workspace: () => workspaceBody({ runtimes: [runtimeRow({ guidance: "next_turn", pause: "unsupported" })] }),
    });
    await openChat();
    await screen.findByText("Run conversation — ONE-1 · Attempt 3");
    expect(document.querySelector(".chat-run-context")).toHaveTextContent("ONE-1 · Subject work item · Attempt 3 · Running");
    await waitFor(() => expect(document.querySelector("#chat-guidance-delivery")).toHaveTextContent("Guidance is delivered on the worker's next turn"));
    expect(document.querySelector("#chat-pause-support")).toHaveTextContent("stop the run from the work detail");
    expect(document.querySelector("#chat-intent option[value=\"pause\"]")).toBeNull();
    expect(document.querySelector("#chat-intent option[value=\"guidance\"]")).not.toBeNull();
    expect(screen.getByLabelText("Run")).toHaveValue(runId);
    // Cancel and retry stay on the work detail, which is linked from the run context.
    expect(screen.getByRole("link", { name: "Open work detail" })).toHaveAttribute("href", `/portal/work/${workItemId}`);
  });

  it("renders a legacy runtime's guidance boundary and keeps pause available", async () => {
    renderChat(`/portal/chat?scope=run&runId=${runId}&projectId=${projectId}`, {
      conversation: () => new Response(JSON.stringify(conversationBody({ scope: "run", projectId, runId, runs: [runDetail()] }))),
      workspace: () => workspaceBody({ runtimes: [] }),
    });
    await openChat();
    await screen.findByText("Run conversation — ONE-1 · Attempt 3");
    expect(document.querySelector("#chat-guidance-delivery")).toHaveTextContent("Guidance is delivered at the worker's next safe execution boundary");
    expect(document.querySelector("#chat-pause-support")).toBeNull();
    expect(document.querySelector("#chat-intent option[value=\"pause\"]")).not.toBeNull();
  });

  it("renders the served decision packet and sends its option and transition from that snapshot", async () => {
    const user = userEvent.setup();
    const bodies: Record<string, unknown>[] = [];
    let posted = false;
    renderChat(`/portal/chat?scope=run&runId=${runId}&projectId=${projectId}`, {
      conversation: () => new Response(JSON.stringify(conversationBody({
        scope: "run",
        projectId,
        runId,
        runs: [runDetail({ waiting: true })],
        messages: posted ? [message(9, { intent: "decision", content: "Stage the artifact" })] : [],
      }))),
      workspace: () => workspaceBody({ execution: { run_id: runId, generation: 3, state: "waiting_for_input", runtime_id: runtimeId, can_guide: false, can_pause: false }, runtimes: [runtimeRow({ guidance: "native_steer", pause: "unsupported" })] }),
      message: (body) => {
        bodies.push(body);
        posted = true;
        return new Response(JSON.stringify({
          message: message(9),
          reply: message(10, { role: "assistant" }),
          work_item_id: workItemId,
          command: command({ id: 3, kind: "decision", state: "pending" }),
        }), { status: 201 });
      },
    });
    await openChat();
    await screen.findByText("Run conversation — ONE-1 · Attempt 3");
    await screen.findByRole("heading", { name: "Which strategy should be used?" });
    const packet = document.querySelector("#chat-decision");
    expect(packet).not.toBeNull();
    expect(packet).toHaveTextContent("Which strategy should be used?");
    expect(packet).toHaveTextContent("Two strategies are valid.");
    const staged = document.querySelector("[data-chat-decision=\"staged\"]");
    expect(staged).toHaveTextContent("Stage the artifact");
    expect(staged).toHaveTextContent("Keeps rollback available.");
    expect(staged).toHaveTextContent("Recommended");
    expect(document.querySelector("[data-chat-decision=\"defer\"]")).not.toHaveTextContent("Recommended");
    await waitFor(() => expect(document.querySelector("#chat-guidance-delivery")).toHaveTextContent("Guidance is native steering for this runtime and is not dispatched from this conversation"));

    await user.click(staged as HTMLElement);
    await screen.findByText("Saved · awaiting agent acknowledgement");
    // The scoped re-read is what renders the durable message: a run-scope send
    // invalidates the query by the surface's own identity, not the request's.
    await waitFor(() => expect(document.querySelector(`[data-message-id="${messageId(9)}"]`)).not.toBeNull());
    expect(bodies).toHaveLength(1);
    expect(bodies[0]).toMatchObject({
      scope: "run",
      run_id: runId,
      intent: "decision",
      content: "Stage the artifact",
      option_id: "staged",
      waiting_transition_id: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb",
      work_item_id: workItemId,
      generation: 3,
    });
  });

  it("renders the served failure text for a failed run, bounded and inert", async () => {
    renderChat(`/portal/chat?scope=run&runId=${runId}&projectId=${projectId}`, {
      conversation: () => new Response(JSON.stringify(conversationBody({ scope: "run", projectId, runId, runs: [runDetail({ state: "failed", failure: { message: "x".repeat(2_500) } })] }))),
      workspace: () => workspaceBody({ execution: { run_id: runId, generation: 3, state: "failed", runtime_id: runtimeId }, runtimes: [] }),
    });
    await openChat();
    await screen.findByText("Run conversation — ONE-1 · Attempt 3");
    const failure = document.querySelector("#chat-failure");
    expect(failure).not.toBeNull();
    expect(failure).toHaveAttribute("data-failure-state", "failed");
    expect(failure?.textContent).toHaveLength(2_001);
    expect(failure?.textContent?.endsWith("…")).toBe(true);
  });

  it("renders the read's semantic evidence oldest first and never a raw entry", async () => {
    renderChat(`/portal/chat?scope=run&runId=${runId}&projectId=${projectId}`, {
      conversation: () => new Response(JSON.stringify(conversationBody({ scope: "run", projectId, runId, runs: [runDetail({ evidence: true })] }))),
      workspace: () => workspaceBody(),
    });
    await openChat();
    await screen.findByText("Two files changed.");
    const rows = document.querySelectorAll("#chat-execution-evidence [data-evidence-source]");
    expect(rows).toHaveLength(1);
    expect(rows[0]).toHaveAttribute("data-evidence-source", "event");
    expect(rows[0]).toHaveTextContent("progress");
    expect(rows[0]).toHaveTextContent("Two files changed.");
  });
});

describe("rendering safety", () => {
  it("renders message content as inert text and links only http(s) values", async () => {
    renderChat(`/portal/chat?scope=run&runId=${runId}&projectId=${projectId}`, {
      conversation: () => new Response(JSON.stringify(conversationBody({
        scope: "run",
        projectId,
        runId,
        messages: [message(1, { content: "One finding: <img src=x onerror=\"window.chatUnsafe=true\"> and the rollback note." })],
        runs: [readWorkDetail({
          id: workItemId,
          projectId,
          title: "Subject work item",
          key: "ONE-1",
          execution: { run_id: runId, generation: 3, state: "running", runtime_id: runtimeId },
          timeline: [],
        })],
      }))),
      workspace: () => workspaceBody(),
    });
    await openChat();
    await screen.findByText(/One finding:/);
    expect(document.querySelectorAll("#chat-messages img, #chat-messages script")).toHaveLength(0);
    expect(document.querySelectorAll("#chat-scope-context img, #chat-scope-context script")).toHaveLength(0);
    expect(document.querySelector("#chat-messages img")).toBeNull();
    expect(screen.getByText(/<img src=x onerror=/)).toBeInTheDocument();
  });

  it("renders a javascript pull-request value as text and an https value as a link", async () => {
    const detail = readWorkDetail({
      id: workItemId,
      projectId,
      title: "Subject work item",
      key: "ONE-1",
      execution: { run_id: runId, generation: 3, state: "running", runtime_id: runtimeId },
      delivery: {
        pull_request: { value: "javascript:alert(1)", source: "manual", generation: 1, url: "javascript:alert(1)", status: "open" },
        ci: { value: "passed", source: "provider", generation: 1, url: "https://example.test/build/42", status: "passed" },
      },
    });
    // The fixture's work item carries the delivery url; the served top-level
    // pull_request_url is the value the run context renders.
    const workItem = { ...(asRecord(detail["work_item"]) ?? {}), pull_request_url: "javascript:alert(1)" };
    renderChat(`/portal/chat?scope=run&runId=${runId}&projectId=${projectId}`, {
      conversation: () => new Response(JSON.stringify(conversationBody({
        scope: "run",
        projectId,
        runId,
        runs: [{ ...detail, work_item: workItem }],
      }))),
      workspace: () => workspaceBody(),
    });
    await openChat();
    await screen.findByText("javascript:alert(1)");
    expect(document.querySelector("a[href^=\"javascript:\"]")).toBeNull();
    expect(document.querySelector("a[href=\"https://example.test/build/42\"]")).not.toBeNull();
  });
});
