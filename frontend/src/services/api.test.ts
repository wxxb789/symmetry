import { describe, expect, it, vi } from "vitest";
import { createApi, Forbidden, Network, readRetry, ServerRefusal, UnexpectedResponse, Validation, type ActionId, type Api, type ExecutionGeneration } from "./api";
import { readWorkDetail } from "./api.fixtures";

const workspace = {
  selected_project_id: null,
  projects: [{ id: "11111111-1111-4111-8111-111111111111", key: "DEMO", name: "Demo", description: null, status: "active", work_items: [] }],
  health: { status: "unknown" },
};

const workItemId = "44444444-4444-4444-8444-444444444444";
const workDetail = readWorkDetail({
  id: workItemId,
  projectId: "11111111-1111-4111-8111-111111111111",
  title: "Ship the read surface",
  key: "DEMO-1",
  timeline: [{
    source: "transition",
    run_id: "55555555-5555-4555-8555-555555555555",
    generation: 1,
    recorded_at: "2026-01-01T00:00:00Z",
    data: { transition_id: "66666666-6666-4666-8666-666666666666", state: "running", payload: {} },
  }],
  nextBefore: "cursor-token",
});

describe("read boundary", () => {
  it("decodes real workspace fields and sends URL selection without another attempt", async () => {
    const fetcher = vi.fn<typeof fetch>().mockResolvedValue(new Response(JSON.stringify(workspace)));
    const api = createApi(fetcher);
    const result = await api.workspace("project /?", new AbortController().signal);
    expect(result.projects[0]?.name).toBe("Demo");
    expect(fetcher).toHaveBeenCalledTimes(1);
    expect(fetcher.mock.calls[0]?.[0]).toBe("/portal/api/workspace?project_id=project+%2F%3F");
  });

  it("rejects malformed success payloads instead of rendering trusted-looking records", async () => {
    const api = createApi(vi.fn<typeof fetch>().mockResolvedValue(new Response('{"projects":[]}')));
    await expect(api.workspace(null, new AbortController().signal)).rejects.toBeInstanceOf(UnexpectedResponse);
  });

  it("rejects invalid UTF-8 bytes in a 200 workspace response instead of accepting U+FFFD", async () => {
    const body = new Uint8Array([
      ...new TextEncoder().encode('{"selected_project_id":null,"projects":[{"id":"11111111-1111-4111-8111-111111111111","key":"DEMO","name":"Demo'),
      0xff,
      ...new TextEncoder().encode('","description":null,"status":"active","work_items":[]}],"health":{"status":"unknown"}}'),
    ]);
    const api = createApi(vi.fn<typeof fetch>().mockResolvedValue(new Response(body)));
    const failure = await api.workspace(null, new AbortController().signal).then(() => null, (error: unknown) => error);
    expect(failure).toBeInstanceOf(UnexpectedResponse);
    expect(failure).not.toBeInstanceOf(Network);
  });

  it("keeps a valid non-ASCII project name byte-exact through decoding", async () => {
    const name = "プロジェクト Ω—λ";
    const body = new Uint8Array([
      ...new TextEncoder().encode('{"selected_project_id":null,"projects":[{"id":"11111111-1111-4111-8111-111111111111","key":"DEMO","name":"'),
      ...new TextEncoder().encode(name),
      ...new TextEncoder().encode('","description":null,"status":"active","work_items":[]}],"health":{"status":"unknown"}}'),
    ]);
    const api = createApi(vi.fn<typeof fetch>().mockResolvedValue(new Response(body)));
    const result = await api.workspace(null, new AbortController().signal);
    expect(result.projects[0]?.name).toBe(name);
  });

  it("applies the same strict UTF-8 rejection to the session bootstrap path", async () => {
    const body = new Uint8Array([...new TextEncoder().encode('{"csrf_token":"to'), 0xff, ...new TextEncoder().encode('ken"}')]);
    const api = createApi(vi.fn<typeof fetch>().mockResolvedValue(new Response(body)));
    const failure = await api.session(new AbortController().signal).then(() => null, (error: unknown) => error);
    expect(failure).toBeInstanceOf(UnexpectedResponse);
    expect(failure).not.toBeInstanceOf(Network);
  });

  it("keeps workspace reads usable when existing resource metadata contains fractional numbers", async () => {
    const body = JSON.stringify({
      ...workspace,
      projects: workspace.projects.map((project) => ({
        ...project,
        resources: [{ metadata: { ratio: 0.5 } }],
      })),
    });
    const api = createApi(vi.fn<typeof fetch>().mockResolvedValue(new Response(body)));
    const result = await api.workspace(null, new AbortController().signal);
    expect(result.projects[0]?.name).toBe("Demo");
  });

  it.each([
    { status: 403, error: Forbidden },
    { status: 422, error: Validation },
  ])("returns a typed non-transient error for HTTP $status", async ({ status, error }) => {
    const api = createApi(vi.fn<typeof fetch>().mockResolvedValue(new Response("{}", { status })));
    await expect(api.workspace(null, new AbortController().signal)).rejects.toBeInstanceOf(error);
  });

  it("leaves network retry to the query owner", async () => {
    const fetcher = vi.fn<typeof fetch>().mockRejectedValue(new TypeError("offline"));
    const api = createApi(fetcher);
    await expect(api.workspace(null, new AbortController().signal)).rejects.toBeInstanceOf(Network);
    expect(fetcher).toHaveBeenCalledTimes(1);
  });

  it("surfaces a failed GET's status when its body cannot be read", async () => {
    const body = new ReadableStream({ start(controller) { controller.error(new Error("body read failed")); } });
    const fetcher = vi.fn<typeof fetch>().mockResolvedValue(new Response(body, { status: 404 }));
    const failure = await createApi(fetcher)
      .workDetail(workItemId, new AbortController().signal)
      .then(() => null, (error: unknown) => error);
    expect(failure).toBeInstanceOf(UnexpectedResponse);
    expect(failure).toMatchObject({ status: 404, transient: false });
    expect(failure).not.toBeInstanceOf(Network);
    expect(readRetry(0, failure)).toBe(false);
  });

  it("marks a server fault retryable so a command keeps its frozen intent", async () => {
    const fetcher = vi.fn<typeof fetch>().mockResolvedValue(new Response("{}", { status: 500 }));
    const api = createApi(fetcher);
    const failure = await api
      .updateWorkItem(workItemId, { version: 1, title: "Fault probe" }, "csrf-token", new AbortController().signal)
      .then(() => null, (error: unknown) => error);
    expect(failure).toBeInstanceOf(UnexpectedResponse);
    expect(failure).toMatchObject({ status: 500, transient: true });
  });

  it("aborts the underlying request when the consuming query is cancelled", async () => {
    let requestSignal: AbortSignal | null | undefined;
    const fetcher = vi.fn<typeof fetch>().mockImplementation((_input, init) => {
      requestSignal = init?.signal;
      const { promise, reject } = Promise.withResolvers<Response>();
      requestSignal?.addEventListener("abort", () => reject(new DOMException("Aborted", "AbortError")));
      return promise;
    });
    const controller = new AbortController();
    const pending = createApi(fetcher).workspace(null, controller.signal);
    await vi.waitFor(() => expect(fetcher).toHaveBeenCalledTimes(1));
    controller.abort();
    await expect(pending).rejects.toBeDefined();
    expect(requestSignal?.aborted).toBe(true);
  });

  it("requests session bootstrap without a browser HTTP cache", async () => {
    const fetcher = vi.fn<typeof fetch>().mockResolvedValue(new Response('{"csrf_token":"fresh-token"}'));
    const session = await createApi(fetcher).session(new AbortController().signal);
    expect(session.csrf_token).toBe("fresh-token");
    expect(fetcher.mock.calls[0]?.[1]?.cache).toBe("no-store");
  });

  it("reads one work item with the served detail envelope and timeline cursor", async () => {
    const fetcher = vi.fn<typeof fetch>().mockResolvedValue(new Response(JSON.stringify(workDetail)));
    const detail = await createApi(fetcher).workDetail(workItemId, new AbortController().signal);
    expect(detail.work_item.id).toBe(workItemId);
    expect(detail.raw.next_before).toBe("cursor-token");
    expect(detail.raw.timeline[0]?.source).toBe("transition");
    expect(fetcher).toHaveBeenCalledTimes(1);
    expect(fetcher.mock.calls[0]?.[0]).toBe(`/portal/api/work-items/${workItemId}`);
  });

  it("keeps older history on the served before cursor with one percent-encoded attempt", async () => {
    const fetcher = vi.fn<typeof fetch>().mockResolvedValue(new Response(JSON.stringify({ items: [], next_before: null })));
    const page = await createApi(fetcher).workTimeline(workItemId, "cursor/+?=&", new AbortController().signal);
    expect(page.next_before).toBeNull();
    expect(fetcher).toHaveBeenCalledTimes(1);
    expect(fetcher.mock.calls[0]?.[0]).toBe(`/portal/api/work-items/${workItemId}/timeline?before=cursor%2F%2B%3F%3D%26`);
  });
});

describe("work-item receipts", () => {
  it("sends the create draft verbatim and decodes the server's disposition", async () => {
    const fetcher = vi.fn<typeof fetch>().mockResolvedValue(new Response(JSON.stringify({ disposition: "replayed", work_item: { id: workItemId } })));
    const api = createApi(fetcher);
    const draft = { title: "Retry me", creation_id: "11111111-2222-4333-8444-555555555555" };
    const receipt = await api.createWorkItem("11111111-1111-4111-8111-111111111111", draft, "csrf", new AbortController().signal);
    expect(receipt).toEqual({ id: workItemId, disposition: "replayed" });
    const [path, init] = fetcher.mock.calls[0] ?? [];
    expect(path).toBe("/portal/api/projects/11111111-1111-4111-8111-111111111111/work-items");
    expect(init?.method).toBe("POST");
    expect(init?.body).toBe(JSON.stringify(draft));
  });

  it("treats an update receipt without a disposition as not replayed", async () => {
    const fetcher = vi.fn<typeof fetch>().mockResolvedValue(new Response(JSON.stringify({ work_item: { id: workItemId } })));
    const api = createApi(fetcher);
    const receipt = await api.updateWorkItem(workItemId, { version: 2, title: "Edited" }, "csrf", new AbortController().signal);
    expect(receipt).toEqual({ id: workItemId, disposition: "" });
  });
});

describe("control command boundary", () => {
  const execution = { task_id: "55555555-5555-4555-8555-555555555555", run_id: null, generation: 2, state: "queued" };
  const command = { command_id: "77777777-7777-4777-8777-777777777777", task_id: execution.task_id, kind: "cancel", state: "pending", acknowledgement_outcome: null };
  const actionId = "action-1" as ActionId;
  const generation = 2 as ExecutionGeneration;
  const calls: readonly { name: string; segment: string; answer: Record<string, unknown>; send: (api: Api, csrf: string, signal: AbortSignal) => Promise<unknown> }[] = [
    { name: "start", segment: "run", answer: { disposition: "created", execution }, send: (api, csrf, signal) => api.startWork(workItemId, actionId, csrf, signal) },
    { name: "cancel", segment: "cancel", answer: { disposition: "created", command, execution }, send: (api, csrf, signal) => api.cancelWork(workItemId, generation, actionId, csrf, signal) },
    { name: "retry", segment: "retry", answer: { disposition: "replayed", command, work_item: { id: workItemId }, execution }, send: (api, csrf, signal) => api.retryWork(workItemId, generation, actionId, csrf, signal) },
    { name: "waiting input", segment: "input", answer: { disposition: "created", command, execution }, send: (api, csrf, signal) => api.provideInput(workItemId, { answer: "continue" }, "transition-1", actionId, csrf, signal) },
  ];

  it.each(calls)("sends the $name control with the caller's live CSRF token", async ({ segment, answer, send }) => {
    const fetcher = vi.fn<typeof fetch>().mockResolvedValue(new Response(JSON.stringify(answer), { status: 202 }));
    await send(createApi(fetcher), "rotated-token", new AbortController().signal);
    const [path, init] = fetcher.mock.calls[0] ?? [];
    expect(path).toBe(`/portal/api/work-items/${workItemId}/${segment}`);
    expect(init?.method).toBe("POST");
    expect(init?.headers).toMatchObject({ "x-csrf-token": "rotated-token" });
  });

  it("decodes the server's typed refusal for a refused control command", async () => {
    const refusal = { error: { code: "stale_generation", message: "The attempt has already moved on." } };
    const fetcher = vi.fn<typeof fetch>().mockResolvedValue(new Response(JSON.stringify(refusal), { status: 409 }));
    const failure = await createApi(fetcher)
      .cancelWork(workItemId, generation, actionId, "csrf", new AbortController().signal)
      .then(() => null, (error: unknown) => error);
    expect(failure).toBeInstanceOf(ServerRefusal);
    expect(failure).toMatchObject({ status: 409, code: "stale_generation", message: "The attempt has already moved on." });
  });

  it("keeps an undescribed control refusal a session-protection 403, not a role denial", async () => {
    const fetcher = vi.fn<typeof fetch>().mockResolvedValue(new Response("{}", { status: 403 }));
    const failure = await createApi(fetcher)
      .startWork(workItemId, actionId, "csrf", new AbortController().signal)
      .then(() => null, (error: unknown) => error);
    expect(failure).toBeInstanceOf(Forbidden);
    expect(failure).toMatchObject({ message: "This change was refused. If you signed in elsewhere, refresh the page and try again." });
  });
});

describe("chat boundary", () => {
  const projectId = "11111111-1111-4111-8111-111111111111";
  const runId = "22222222-2222-4222-8222-222222222222";
  const conversation = {
    scope: "run",
    project_id: projectId,
    run_id: runId,
    messages: [{
      id: "33333333-3333-4333-8333-333333333333",
      role: "human",
      intent: "guidance",
      content: "Prefer the smallest diff.",
      inserted_at: "2026-11-16T10:00:00.000000Z",
      work_item_id: "44444444-4444-4444-8444-444444444444",
      command: {
        command_id: "55555555-5555-4555-8555-555555555555",
        task_id: "66666666-6666-4666-8666-666666666666",
        run_id: runId,
        generation: 1,
        kind: "guidance",
        payload: { message: "Prefer the smallest diff." },
        state: "pending",
        issued_at: "2026-11-16T10:00:00.000000Z",
        applied_at: null,
        acknowledgement_id: null,
        acknowledgement_outcome: null,
        acknowledged_at: null,
      },
      metadata: { source: "durable_command", generation: 1 },
    }],
    runs: [],
    next_before: "cursor-token",
  };

  it("reads a conversation with only the members that scope accepts", async () => {
    const fetcher = vi.fn<typeof fetch>().mockResolvedValue(new Response(JSON.stringify(conversation)));
    const api = createApi(fetcher);
    const result = await api.chatConversation({ scope: "run", projectId, runId, before: "cursor /+?=&" }, new AbortController().signal);
    expect(result.messages[0]?.command?.acknowledgement_outcome).toBeNull();
    expect(result.next_before).toBe("cursor-token");
    expect(fetcher).toHaveBeenCalledTimes(1);
    expect(fetcher.mock.calls[0]?.[0]).toBe(`/portal/api/chat?scope=run&run_id=${runId}&before=cursor+%2F%2B%3F%3D%26`);
  });

  it("never sends a project read a run member or the other way around", async () => {
    const fetcher = vi.fn<typeof fetch>().mockResolvedValue(new Response(JSON.stringify({ ...conversation, scope: "project", run_id: null })));
    const api = createApi(fetcher);
    await api.chatConversation({ scope: "project", projectId, runId }, new AbortController().signal);
    expect(fetcher.mock.calls[0]?.[0]).toBe(`/portal/api/chat?scope=project&project_id=${projectId}`);

    const workspaceFetcher = vi.fn<typeof fetch>().mockResolvedValue(new Response(JSON.stringify({ ...conversation, scope: "workspace", project_id: null, run_id: null })));
    await createApi(workspaceFetcher).chatConversation({ scope: "workspace", projectId, runId }, new AbortController().signal);
    expect(workspaceFetcher.mock.calls[0]?.[0]).toBe("/portal/api/chat?scope=workspace");
  });

  it("rejects a conversation the read schema cannot trust", async () => {
    const fetcher = vi.fn<typeof fetch>().mockResolvedValue(new Response(JSON.stringify({ ...conversation, scope: "elsewhere" })));
    const failure = await createApi(fetcher)
      .chatConversation({ scope: "workspace" }, new AbortController().signal)
      .then(() => null, (error: unknown) => error);
    expect(failure).toBeInstanceOf(UnexpectedResponse);
    expect(fetcher).toHaveBeenCalledTimes(1);
  });

  it("reads the message answer's HTTP status as its disposition", async () => {
    const body = {
      message: { id: "33333333-3333-4333-8333-333333333333", role: "human", intent: "discuss", content: "A question", inserted_at: "2026-11-16T10:00:00.000000Z", work_item_id: null },
      reply: { id: "44444444-4444-4444-8444-444444444444", role: "assistant", intent: "discuss", content: "An answer", inserted_at: "2026-11-16T10:00:01.000000Z", work_item_id: null },
      work_item_id: null,
      command: null,
    };
    const payload = { scope: "workspace", intent: "discuss", content: "A question", action_id: "action-1" } as const;
    const created = await createApi(vi.fn<typeof fetch>().mockResolvedValue(new Response(JSON.stringify(body), { status: 201 })))
      .chatMessage(payload, "csrf", new AbortController().signal);
    expect(created.disposition).toBe("created");
    expect(created.command).toBeNull();
    const replayed = await createApi(vi.fn<typeof fetch>().mockResolvedValue(new Response(JSON.stringify(body), { status: 200 })))
      .chatMessage(payload, "csrf", new AbortController().signal);
    expect(replayed.disposition).toBe("replayed");
    expect(replayed.message.id).toBe("33333333-3333-4333-8333-333333333333");
  });

  it("sends the frozen request verbatim with the caller's CSRF token and never retries", async () => {
    const fetcher = vi.fn<typeof fetch>().mockResolvedValue(new Response(JSON.stringify({
      message: { id: "33333333-3333-4333-8333-333333333333", role: "human", intent: "guidance", content: "Steer", inserted_at: null, work_item_id: null },
      reply: { id: "44444444-4444-4444-8444-444444444444", role: "assistant", intent: "guidance", content: "Saved", inserted_at: null, work_item_id: null },
      work_item_id: "55555555-5555-4555-8555-555555555555",
      command: { command_id: "66666666-6666-4666-8666-666666666666", kind: "guidance", state: "pending", acknowledgement_outcome: null, applied_at: null },
    }), { status: 201 }));
    const payload = { scope: "run" as const, run_id: runId, intent: "guidance", content: "Steer", action_id: "action-1", work_item_id: "55555555-5555-4555-8555-555555555555", generation: 1 };
    const outcome = await createApi(fetcher).chatMessage(payload, "rotated-token", new AbortController().signal);
    expect(outcome.command?.state).toBe("pending");
    expect(outcome.workItemId).toBe("55555555-5555-4555-8555-555555555555");
    const [path, init] = fetcher.mock.calls[0] ?? [];
    expect(path).toBe("/portal/api/chat/messages");
    expect(init?.method).toBe("POST");
    expect(init?.headers).toMatchObject({ "x-csrf-token": "rotated-token" });
    expect(init?.body).toBe(JSON.stringify(payload));
    expect(fetcher).toHaveBeenCalledTimes(1);
  });

  it("carries the server's refusal for a changed replay", async () => {
    const refusal = { error: { code: "idempotency_conflict", message: "This action id was already used with a different request." } };
    const fetcher = vi.fn<typeof fetch>().mockResolvedValue(new Response(JSON.stringify(refusal), { status: 409 }));
    const failure = await createApi(fetcher)
      .chatMessage({ scope: "workspace", intent: "discuss", content: "changed", action_id: "action-1" }, "csrf", new AbortController().signal)
      .then(() => null, (error: unknown) => error);
    expect(failure).toBeInstanceOf(ServerRefusal);
    expect(failure).toMatchObject({ status: 409, code: "idempotency_conflict" });
  });
});
