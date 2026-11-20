import { describe, expect, it } from "vitest";
import type { ChatConversationMessage, ChatConversationRun } from "../../services/api";
import {
  applicableChatIntents,
  chatDecisionPacket,
  chatEvidenceRows,
  chatFailureView,
  chatHistoryView,
  chatIntentLabel,
  chatReceiptView,
  chatScopeContext,
  chatSendStatusText,
  guidanceDeliveryLabel,
  pauseUnsupported,
  servedLink,
  type ChatAdapterRecord,
  type ChatRunSignals,
} from "./presentation";
import type { ChatHistoryPage } from "./scopes";

const message = (id: string, options: { content?: string; insertedAt?: string; command?: ChatConversationMessage["command"] } = {}): ChatConversationMessage => ({
  id,
  role: "human",
  intent: "discuss",
  content: options.content ?? id,
  inserted_at: options.insertedAt ?? "2026-11-16T09:00:00.000000Z",
  work_item_id: null,
  command: options.command ?? null,
  metadata: null,
});

const emptyDelivery = () => ({ value: null, source: "none" as const, generation: null, url: null, status: null });

const run = (timeline: ChatConversationRun["timeline"]): ChatConversationRun => ({
  work_item: { id: "22222222-2222-4222-8222-222222222222", key: "ONE-1", title: "Subject work item", status: "running", blocked: false, blocker: null },
  outcome: {
    goal: "Subject work item",
    phase: "running",
    owner: { type: "agent", name: null, agent_profile: null },
    summary: null,
    findings: [],
    changed_artifacts: [],
    tests: [],
    blocker: null,
    pull_request_url: null,
    ci_status: null,
    review_status: null,
    delivery: { pull_request: emptyDelivery(), ci: emptyDelivery(), review: emptyDelivery() },
    result: null,
    failure: null,
  },
  timeline,
  raw: { task: null, timeline: [], next_before: null },
});

const page = (messages: readonly ChatConversationMessage[], options: { fetchBefore?: string | null; fetchBeforeId?: string | null; nextBefore?: string | null } = {}): ChatHistoryPage => ({
  messages,
  fetchBefore: options.fetchBefore ?? null,
  fetchBeforeId: options.fetchBeforeId ?? null,
  nextBefore: options.nextBefore ?? null,
});

const adapter = (operations: Partial<NonNullable<ChatAdapterRecord["operations"]>>): ChatAdapterRecord => ({
  kind: "pi",
  native_version: "1.5.2",
  implementation_version: "pi-rpc-v1",
  protocol_version: 1,
  operations: {
    start: true, events: true, cancel: true, resume: false, handoff: false,
    guidance: "unsupported", pause: "unsupported", approval_response: false,
    usage: "unknown", hard_cost_limit: false,
    ...operations,
  },
});

const signals = (overrides: Partial<ChatRunSignals>): ChatRunSignals => ({
  canGuide: false, canPause: false, canResume: false, hasDecision: false, pauseUnsupported: false,
  ...overrides,
});

describe("intent vocabulary", () => {
  it("offers the server's intents filtered by scope and served state", () => {
    expect(applicableChatIntents({ scope: "workspace", run: null, hasProjects: true })).toEqual(["discuss", "status", "start_work"]);
    expect(applicableChatIntents({ scope: "workspace", run: null, hasProjects: false })).toEqual(["discuss", "status"]);
    expect(applicableChatIntents({ scope: "project", run: null, hasProjects: false })).toEqual(["discuss", "status", "start_work"]);
    expect(applicableChatIntents({ scope: "run", run: null, hasProjects: true })).toEqual(["discuss", "status"]);
    expect(applicableChatIntents({ scope: "run", run: signals({ canGuide: true, canPause: true, canResume: true, hasDecision: true }), hasProjects: true }))
      .toEqual(["discuss", "status", "guidance", "pause", "resume", "decision"]);
  });

  it("never offers pause when the runtime's adapter cannot pause", () => {
    expect(applicableChatIntents({ scope: "run", run: signals({ canPause: true, pauseUnsupported: true }), hasProjects: true })).toEqual(["discuss", "status"]);
  });

  it("labels a known intent and renders an unknown served intent verbatim", () => {
    expect(chatIntentLabel("start_work")).toBe("Start work");
    expect(chatIntentLabel("install")).toBe("install");
  });
});

describe("scope context", () => {
  const url = { urlScope: "workspace" as const, urlProjectId: null, urlRunId: null };

  it("names the URL's request with a pending marker until the read answers", () => {
    expect(chatScopeContext({ ...url, served: null, pending: true, project: null, run: null })).toBe("Workspace conversation · pending");
    expect(chatScopeContext({ ...url, urlScope: "project", urlProjectId: "p1", served: null, pending: true, project: null, run: null })).toBe("Project conversation — p1 · pending");
    expect(chatScopeContext({ ...url, urlScope: "run", urlRunId: "r1", served: null, pending: true, project: null, run: null })).toBe("Run conversation — r1 · pending");
    expect(chatScopeContext({ ...url, urlScope: "run", urlRunId: null, served: null, pending: false, project: null, run: null })).toBe("Run conversation — no run selected");
  });

  it("names the server's answer once it arrives, never the URL's claim", () => {
    expect(chatScopeContext({ ...url, served: { scope: "workspace", projectId: null, runId: null }, pending: false, project: null, run: null })).toBe("Workspace conversation");
    expect(chatScopeContext({
      ...url,
      served: { scope: "project", projectId: "p1", runId: null },
      pending: false,
      project: { name: "Project One", key: "ONE" },
      run: null,
    })).toBe("Project conversation — Project One (ONE)");
    expect(chatScopeContext({
      ...url,
      urlScope: "run",
      urlRunId: "r1",
      served: { scope: "run", projectId: "p1", runId: "r1" },
      pending: false,
      project: null,
      run: { workItemKey: "ONE-9", generation: 3 },
    })).toBe("Run conversation — ONE-9 · Attempt 3");
    expect(chatScopeContext({ ...url, served: { scope: "project", projectId: "p1", runId: null }, pending: false, project: null, run: null }))
      .toBe("Project conversation — p1");
  });
});

describe("delivery vocabulary", () => {
  it("names guidance delivery from the adapter's own operation value", () => {
    expect(guidanceDeliveryLabel(adapter({ guidance: "native_steer" }))).toBe("Guidance is native steering for this runtime and is not dispatched from this conversation");
    expect(guidanceDeliveryLabel(adapter({ guidance: "next_turn" }))).toBe("Guidance is delivered on the worker's next turn");
    expect(guidanceDeliveryLabel(adapter({ guidance: "unsupported" }))).toBe("Guidance is unsupported by this runtime's adapter");
  });

  it("names the legacy boundary for a runtime without an adapter record", () => {
    expect(guidanceDeliveryLabel(undefined)).toBe("Guidance is delivered at the worker's next safe execution boundary");
    expect(guidanceDeliveryLabel(null)).toBe("Guidance is delivered at the worker's next safe execution boundary");
  });

  it("keeps an adapter record without a guidance value honest instead of claiming a path", () => {
    // A partial record the generated type does not describe; the renderer must
    // stay honest about an operation its record does not advertise.
    const partial = {
      kind: "pi",
      native_version: "1.5.2",
      implementation_version: "pi-rpc-v1",
      protocol_version: 1,
      operations: { start: true, events: true, cancel: true, resume: false, handoff: false, pause: "unsupported", approval_response: false, usage: "unknown", hard_cost_limit: false },
    } as unknown as ChatAdapterRecord;
    expect(guidanceDeliveryLabel(partial)).toBe("Guidance delivery is not advertised by this runtime's adapter record");
  });

  it("reports pause as unsupported only from the adapter's own value", () => {
    expect(pauseUnsupported(adapter({ pause: "unsupported" }))).toBe(true);
    expect(pauseUnsupported(adapter({ pause: "safe_boundary" }))).toBe(false);
    expect(pauseUnsupported(undefined)).toBe(false);
  });

  it("treats a generic adapter as the legacy supervisory registration it is", () => {
    // A generic registration advertises an adapter record whose guidance and
    // pause are "unsupported" — that describes native steering and native
    // pause. Its supervisory protocol delivers guidance at a safe boundary and
    // pauses at one, which is what the server's own can_guide/can_pause and
    // reply report, so neither is labelled unsupported here.
    const generic: ChatAdapterRecord = { ...adapter({}), kind: "generic" };
    expect(guidanceDeliveryLabel(generic)).toBe("Guidance is delivered at the worker's next safe execution boundary");
    expect(pauseUnsupported(generic)).toBe(false);
  });
});

describe("receipts", () => {
  it("renders no receipt without a command", () => {
    expect(chatReceiptView(null, true)).toBeNull();
  });

  it("renders a non-terminal command as awaiting acknowledgement, never applied", () => {
    const view = chatReceiptView({ kind: "guidance", state: "pending", acknowledgement_outcome: null, acknowledgement_id: null }, true);
    expect(view).toEqual({ outcome: "queued", headline: "Saved · awaiting agent acknowledgement" });
  });

  it("renders applied only from the served acknowledgement outcome", () => {
    expect(chatReceiptView({ kind: "guidance", state: "acknowledged", acknowledgement_outcome: "applied", acknowledgement_id: "ack-1" }, true)?.headline).toBe("Applied");
  });

  it("renders a cancel the control plane applied itself with the plane's own outcome", () => {
    expect(chatReceiptView({ kind: "cancel", state: "applied", acknowledgement_outcome: null, acknowledgement_id: null }, true)).toEqual({ outcome: "control_applied", headline: "Applied by the control plane" });
  });

  it("renders a supervisory command the plane recorded applied without the runtime's acknowledgement as just that", () => {
    expect(chatReceiptView({ kind: "pause", state: "applied", acknowledgement_outcome: null, acknowledgement_id: null }, true)).toEqual({ outcome: "applied_unacknowledged", headline: "Applied by the runtime; acknowledgement not recorded" });
  });

  it("keeps the re-queued retry without a runtime acknowledgement queued for the runtime", () => {
    expect(chatReceiptView({ kind: "retry", state: "applied", acknowledgement_outcome: null, acknowledgement_id: null }, true)).toEqual({ outcome: "deferred", headline: "Queued for the runtime" });
  });

  it("attributes a pre-dispatch rejection to the control plane, never the runtime", () => {
    expect(chatReceiptView({ kind: "guidance", state: "acknowledged", acknowledgement_outcome: "rejected", acknowledgement_id: null }, true)).toEqual({ outcome: "control_rejected", headline: "Rejected by the control plane" });
    expect(chatReceiptView({ kind: "guidance", state: "acknowledged", acknowledgement_outcome: "rejected", acknowledgement_id: "ack-2" }, true)).toEqual({ outcome: "rejected", headline: "Rejected by the runtime" });
  });

  it("never claims a refreshed outcome for a message only a retained page carries", () => {
    const view = chatReceiptView({ kind: "guidance", state: "acknowledged", acknowledgement_outcome: "applied", acknowledgement_id: "ack-3" }, false);
    expect(view).toEqual({ outcome: "retained", headline: "Acknowledgement not refreshed with this page" });
  });
});

describe("send status text", () => {
  it("renders exactly the contract's observables", () => {
    expect(chatSendStatusText({ kind: "idle" })).toBe("Messages are saved to this context.");
    expect(chatSendStatusText({ kind: "sending" })).toBe("Sending…");
    expect(chatSendStatusText({ kind: "saved-question" })).toBe("Saved to this context.");
    expect(chatSendStatusText({ kind: "saved-command" })).toBe("Saved · awaiting agent acknowledgement");
    expect(chatSendStatusText({ kind: "refused", message: "The action id was already used." })).toBe("Refused: The action id was already used.");
    expect(chatSendStatusText({ kind: "unconfirmed" })).toBe("Send not confirmed. The draft is kept; retry reuses the same action identity.");
    expect(chatSendStatusText({ kind: "notice", message: "Guidance must be 32,768 UTF-8 bytes or fewer." })).toBe("Guidance must be 32,768 UTF-8 bytes or fewer.");
  });
});

describe("served links", () => {
  it("admits only http(s) values and keeps every value's text", () => {
    expect(servedLink("https://example.test/pull/1")).toEqual({ text: "https://example.test/pull/1", href: "https://example.test/pull/1" });
    expect(servedLink("http://example.test/pull/1")).toEqual({ text: "http://example.test/pull/1", href: "http://example.test/pull/1" });
    expect(servedLink("javascript:alert(1)")).toEqual({ text: "javascript:alert(1)", href: null });
    expect(servedLink("data:text/html,<b>x</b>")).toEqual({ text: "data:text/html,<b>x</b>", href: null });
    expect(servedLink("/portal/chat")).toEqual({ text: "/portal/chat", href: null });
    expect(servedLink("")).toBeNull();
    expect(servedLink(null)).toBeNull();
  });
});

describe("decision packet", () => {
  const packet = {
    context: "Two strategies are valid.",
    options: [
      { id: "staged", label: "Stage", consequence: "Keeps rollback." },
      { id: "defer", label: "Defer", consequence: "Stays unpublished." },
    ],
    recommended_option_id: "staged",
  };

  it("extracts the served packet with its recommendation", () => {
    const view = chatDecisionPacket({ transition_id: "t1", generation: 2, question: "Which strategy?", decision: packet, payload: {} });
    expect(view).toEqual({
      context: "Two strategies are valid.",
      question: "Which strategy?",
      transitionId: "t1",
      generation: 2,
      options: [
        { id: "staged", label: "Stage", consequence: "Keeps rollback.", recommended: true },
        { id: "defer", label: "Defer", consequence: "Stays unpublished.", recommended: false },
      ],
    });
  });

  it("reads the packet out of the raw payload when the projection carries none", () => {
    const view = chatDecisionPacket({ transition_id: "t1", generation: 2, question: "Which?", decision: null, payload: { decision: packet } });
    expect(view?.options).toHaveLength(2);
  });

  it("renders no packet without one", () => {
    expect(chatDecisionPacket(null)).toBeNull();
    expect(chatDecisionPacket({ transition_id: "t1", generation: 2, question: null, payload: {} })).toBeNull();
  });
});

describe("failure rendering", () => {
  it("renders the first readable failure string for a failed run", () => {
    expect(chatFailureView("failed", { message: "The runtime stopped." }, null)).toEqual({ state: "failed", text: "The runtime stopped." });
    expect(chatFailureView("cancelled", { summary: "Cancelled by the operator." }, null)?.text).toBe("Cancelled by the operator.");
    expect(chatFailureView("failed", null, { error: "Outcome failure." })?.text).toBe("Outcome failure.");
  });

  it("bounds a long failure at 2,000 characters and marks the cut", () => {
    const view = chatFailureView("failed", { message: "x".repeat(2_500) }, null);
    expect(view?.text).toHaveLength(2_001);
    expect(view?.text.endsWith("…")).toBe(true);
  });

  it("says so when no readable failure text exists, and stays silent while the run is not ended", () => {
    expect(chatFailureView("failed", { signal: "SIGKILL" }, null)).toEqual({ state: "failed", text: "The run ended without a recorded failure message." });
    expect(chatFailureView("running", { message: "x" }, null)).toBeNull();
    expect(chatFailureView(null, null, null)).toBeNull();
  });
});

describe("execution evidence", () => {
  it("renders the read's semantic timeline oldest first with each entry's own text", () => {
    const rows = chatEvidenceRows(run([
      {
        source: "transition",
        run_id: "66666666-6666-4666-8666-666666666666",
        generation: 1,
        recorded_at: "2026-11-16T10:00:00.000000Z",
        data: { transition_id: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", state: "running", payload: { from: "queued" } },
      },
      {
        source: "command",
        run_id: "66666666-6666-4666-8666-666666666666",
        generation: 1,
        recorded_at: "2026-11-16T09:59:00.000000Z",
        data: {
          command_id: "cccccccc-0001-4001-8001-000000000001", task_id: "55555555-5555-4555-8555-555555555555",
          kind: "guidance", payload: {}, state: "pending", issued_at: null, applied_at: null,
          acknowledgement_id: null, acknowledgement_outcome: null, acknowledged_at: null,
        },
      },
      {
        source: "event",
        run_id: "66666666-6666-4666-8666-666666666666",
        generation: 1,
        recorded_at: "2026-11-16T09:58:00.000000Z",
        data: {
          event_id: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", sequence: 1, kind: "progress",
          payload: { message: "Two files changed." }, occurred_at: "2026-11-16T09:58:00.000000Z",
        },
      },
    ]));
    // Served newest first, rendered oldest first, and never a raw entry: the
    // command's receipt state comes from the shared derivation.
    expect(rows.map((row) => row.source)).toEqual(["event", "command", "transition"]);
    expect(rows[0]).toMatchObject({ kind: "progress", summary: "Two files changed." });
    expect(rows[1]).toMatchObject({ kind: "guidance", summary: "Awaiting agent acknowledgement" });
    expect(rows[2]).toMatchObject({ kind: "running", summary: "From queued" });
  });

  it("threads the served kind and acknowledgement identity into the derived receipt", () => {
    const rows = chatEvidenceRows(run([
      {
        source: "command",
        run_id: "66666666-6666-4666-8666-666666666666",
        generation: 1,
        recorded_at: "2026-11-16T09:59:30.000000Z",
        data: {
          command_id: "cccccccc-0002-4002-8002-000000000002", task_id: "55555555-5555-4555-8555-555555555555",
          kind: "retry", payload: {}, state: "applied", issued_at: null, applied_at: "2026-11-16T09:59:30.000000Z",
          acknowledgement_id: null, acknowledgement_outcome: null, acknowledged_at: null,
        },
      },
      {
        source: "command",
        run_id: "66666666-6666-4666-8666-666666666666",
        generation: 1,
        recorded_at: "2026-11-16T09:59:00.000000Z",
        data: {
          command_id: "cccccccc-0003-4003-8003-000000000003", task_id: "55555555-5555-4555-8555-555555555555",
          kind: "cancel", payload: {}, state: "acknowledged", issued_at: null, applied_at: null,
          acknowledgement_id: null, acknowledgement_outcome: "rejected", acknowledged_at: "2026-11-16T09:59:00.000000Z",
        },
      },
    ]));
    // The re-queued retry is deferred, not the plane's own application, and a
    // rejection without a runtime acknowledgement id is the plane's own, not
    // the runtime's: both derivations read the served members.
    expect(rows.map((row) => [row.kind, row.summary])).toEqual([
      ["cancel", "Rejected by the control plane"],
      ["retry", "Queued for the runtime"],
    ]);
  });

  it("names an entry's kind and state instead of rendering an empty row", () => {
    const rows = chatEvidenceRows(run([{
      source: "event",
      run_id: "66666666-6666-4666-8666-666666666666",
      generation: 1,
      recorded_at: null,
      data: { event_id: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", sequence: 2, kind: "waiting_for_input", payload: { state: "waiting_for_input" }, occurred_at: null },
    }]));
    expect(rows).toHaveLength(1);
    expect(rows[0]?.source).toBe("event");
    expect(rows[0]?.kind).toBe("waiting_for_input");
    expect(rows[0]?.summary).toBe("waiting_for_input");
  });

  it("renders no rows for a run whose read carries no semantic entries", () => {
    expect(chatEvidenceRows(null)).toEqual([]);
    expect(chatEvidenceRows(run([]))).toEqual([]);
  });
});

describe("history view", () => {
  const first = message("11111111-0001-4001-8001-000000000001", { insertedAt: "2026-11-16T09:00:00.000000Z" });
  const second = message("11111111-0002-4002-8002-000000000002", { insertedAt: "2026-11-16T09:01:00.000000Z" });
  const third = message("11111111-0003-4003-8003-000000000003", { insertedAt: "2026-11-16T09:02:00.000000Z" });
  const fourth = message("11111111-0004-4004-8004-000000000004", { insertedAt: "2026-11-16T09:03:00.000000Z" });

  const seeded = (index: number) => message(`11111111-00${String(index).padStart(2, "0")}-4001-8001-0000000000${String(index).padStart(2, "0")}`, { insertedAt: `2026-11-16T09:${String(index).padStart(2, "0")}:00.000000Z` });

  it("renders the current page alone when no older page is retained", () => {
    const view = chatHistoryView([page([third, fourth], { nextBefore: "cursor-1" })], "cursor-1");
    expect(view.messages.map((item) => item.id)).toEqual([third.id, fourth.id]);
    expect(view.loadCursor).toBe("cursor-1");
    expect(view.gap).toBeNull();
  });

  it("renders the retained older pages before the current page, deduping by id and keeping the refreshed copy", () => {
    const refreshed = { ...fourth, content: "refreshed copy" };
    const view = chatHistoryView([
      page([refreshed], { nextBefore: "cursor-1" }),
      page([third, fourth], { fetchBefore: "cursor-1", fetchBeforeId: fourth.id, nextBefore: "cursor-2" }),
      page([first, second], { fetchBefore: "cursor-2", fetchBeforeId: third.id, nextBefore: null }),
    ], null);
    expect(view.messages.map((item) => item.id)).toEqual([first.id, second.id, third.id, fourth.id]);
    expect(view.messages[3]?.content).toBe("refreshed copy");
    expect(view.currentIds.has(fourth.id)).toBe(true);
    expect(view.gap).toBeNull();
  });

  it("names an unloaded boundary between the current page and a retained chain fetched from a moved boundary", () => {
    const view = chatHistoryView([
      page([third, fourth], { nextBefore: "cursor-2" }),
      page([first, second], { fetchBefore: "cursor-1", fetchBeforeId: second.id, nextBefore: null }),
    ], "cursor-2");
    expect(view.gap).toEqual({ current: third.id, retained: second.id });
    expect(view.messages.map((item) => item.id)).toEqual([first.id, second.id, third.id, fourth.id]);
  });

  it("reports no gap when a retained page was fetched from the current boundary, even with a re-signed cursor, or when the pages overlap", () => {
    const contiguous = chatHistoryView([
      page([third, fourth], { nextBefore: "cursor-1" }),
      page([first, second], { fetchBefore: "cursor-1", fetchBeforeId: third.id, nextBefore: null }),
    ], null);
    expect(contiguous.gap).toBeNull();
    // The same boundary, re-served: the token differs per response, the
    // boundary message's id does not.
    const resigned = chatHistoryView([
      page([third, fourth], { nextBefore: "cursor-1-resigned" }),
      page([first, second], { fetchBefore: "cursor-1", fetchBeforeId: third.id, nextBefore: null }),
    ], "cursor-1-resigned");
    expect(resigned.gap).toBeNull();
    const overlapping = chatHistoryView([
      page([third, fourth], { nextBefore: "cursor-2" }),
      page([second, third], { fetchBefore: "cursor-1", fetchBeforeId: second.id, nextBefore: null }),
    ], "cursor-2");
    expect(overlapping.gap).toBeNull();
  });

  it("reports no gap when a retained older page is present but the served page itself is empty", () => {
    // The empty served page proves nothing about the retained region and its
    // chain has no oldest message to reduce: the guard keeps the reduction
    // total instead of throwing. The state is defensive — current pagination
    // cannot serve an empty page beside a cursor.
    const view = chatHistoryView([
      page([], { nextBefore: "cursor-1" }),
      page([first, second], { fetchBefore: "cursor-1", fetchBeforeId: null, nextBefore: null }),
    ], "cursor-1");
    expect(view.gap).toBeNull();
    expect(view.messages.map((item) => item.id)).toEqual([first.id, second.id]);
  });

  it("reports no gap when the served page ends history, even beside a retained page off its boundary", () => {
    // `nextBefore: null` says the serve reached the oldest history: no cursor
    // continues the chain, so a retained page from a dropped boundary cannot
    // be named as a gap the reader could load. The state is defensive —
    // current pagination cannot drain the serve while a load is retained.
    const view = chatHistoryView([
      page([third, fourth], { nextBefore: null }),
      page([first, second], { fetchBefore: "cursor-dropped", fetchBeforeId: null, nextBefore: null }),
    ], null);
    expect(view.gap).toBeNull();
    expect(view.messages.map((item) => item.id)).toEqual([first.id, second.id, third.id, fourth.id]);
  });

  it("keeps the gap when a later page chains from the moved boundary but the retained chain it left behind is not reached", () => {
    const one = seeded(1);
    const two = seeded(2);
    const three = seeded(3);
    const four = seeded(4);
    const five = seeded(5);
    const six = seeded(6);
    // The refreshed read ends at message five; the page loaded from that
    // boundary reaches message three, but the chain the moved boundary left
    // behind — fetched from message two — is not reached by it, so the region
    // between them (nothing here proves message two loaded) stays named.
    const view = chatHistoryView([
      page([five, six], { nextBefore: "cursor-refreshed" }),
      page([three, four], { fetchBefore: "cursor-refreshed", fetchBeforeId: five.id, nextBefore: "cursor-3" }),
      page([one], { fetchBefore: "cursor-dropped", fetchBeforeId: two.id, nextBefore: null }),
    ], "cursor-3");
    expect(view.gap).toEqual({ current: three.id, retained: one.id });
    expect(view.messages.map((item) => item.id)).toEqual([one.id, three.id, four.id, five.id, six.id]);

    // Loading the skipped region into the chain reaches the older page; only
    // then does the surface stop naming the region, because the two now touch.
    const connected = chatHistoryView([
      page([five, six], { nextBefore: "cursor-refreshed" }),
      page([three, four], { fetchBefore: "cursor-refreshed", fetchBeforeId: five.id, nextBefore: "cursor-3" }),
      page([one, two], { fetchBefore: "cursor-3", fetchBeforeId: three.id, nextBefore: null }),
    ], "cursor-3");
    expect(connected.gap).toBeNull();
    expect(connected.messages.map((item) => item.id)).toEqual([one.id, two.id, three.id, four.id, five.id, six.id]);
  });

  it("keeps naming the next region when the chain reaches only one of several retained pages", () => {
    const one = seeded(1);
    const two = seeded(2);
    const three = seeded(3);
    const four = seeded(4);
    const five = seeded(5);
    const six = seeded(6);
    const seven = seeded(7);
    const eight = seeded(8);
    const nine = seeded(9);
    const ten = seeded(10);
    // The nameable boundary between the chain and the newest page it did not
    // reach is the region Load older fills first; the older region below stays
    // named by the same note once the chain reaches it.
    const beforeReach = chatHistoryView([
      page([nine, ten], { nextBefore: "cursor-chain" }),
      page([seven, eight], { fetchBefore: "cursor-chain", fetchBeforeId: nine.id, nextBefore: "cursor-7" }),
      page([four, five], { fetchBefore: "cursor-dropped-b", fetchBeforeId: six.id, nextBefore: "cursor-4" }),
      page([one, two], { fetchBefore: "cursor-dropped-a", fetchBeforeId: three.id, nextBefore: null }),
    ], "cursor-7");
    expect(beforeReach.gap).toEqual({ current: seven.id, retained: five.id });
    expect(beforeReach.messages.map((item) => item.id)).toEqual([one.id, two.id, four.id, five.id, seven.id, eight.id, nine.id, ten.id]);

    const afterReach = chatHistoryView([
      page([nine, ten], { nextBefore: "cursor-chain" }),
      page([seven, eight], { fetchBefore: "cursor-chain", fetchBeforeId: nine.id, nextBefore: "cursor-7" }),
      page([five, six], { fetchBefore: "cursor-7", fetchBeforeId: seven.id, nextBefore: "cursor-5" }),
      page([four, five], { fetchBefore: "cursor-dropped-b", fetchBeforeId: six.id, nextBefore: "cursor-4" }),
      page([one, two], { fetchBefore: "cursor-dropped-a", fetchBeforeId: three.id, nextBefore: null }),
    ], "cursor-5");
    expect(afterReach.gap).toEqual({ current: four.id, retained: two.id });
    expect(afterReach.messages.map((item) => item.id)).toEqual([one.id, two.id, four.id, five.id, six.id, seven.id, eight.id, nine.id, ten.id]);
  });

  it("names the nearest unloaded region when the farther retained page was retained first", () => {
    const one = seeded(1);
    const two = seeded(2);
    const three = seeded(3);
    const four = seeded(4);
    const five = seeded(5);
    const six = seeded(6);
    const seven = seeded(7);
    const eight = seeded(8);
    // Retention order is farthest-first: the page fetched from message three
    // sits before the page fetched from message six. The note must still name
    // the nearest unloaded region — between the chain's oldest message and the
    // newest point the history did not reach (five) — not the newest message
    // of whichever page happens to sit first in the retained list.
    const view = chatHistoryView([
      page([seven, eight], { nextBefore: "cursor-chain" }),
      page([one, two], { fetchBefore: "cursor-dropped-a", fetchBeforeId: three.id, nextBefore: null }),
      page([four, five], { fetchBefore: "cursor-dropped-b", fetchBeforeId: six.id, nextBefore: "cursor-4" }),
    ], "cursor-chain");
    expect(view.gap).toEqual({ current: seven.id, retained: five.id });
    expect(view.messages.map((item) => item.id)).toEqual([one.id, two.id, four.id, five.id, seven.id, eight.id]);
  });
});
