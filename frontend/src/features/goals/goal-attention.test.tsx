import { act, fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it } from "vitest";
import { jsonResponse, recordingFetcher, renderRoute } from "../../app/test-harness";
import { readAttention, readGoalProjection } from "../../services/api.fixtures";

const projectId = "11111111-1111-4111-8111-111111111111";
const secondProjectId = "22222222-2222-4222-8222-222222222222";
const goalId = "33333333-3333-4333-8333-333333333333";

const projects = [
  { id: projectId, key: "ONE", name: "Project One", description: null, status: "active", work_items: [] },
  { id: secondProjectId, key: "TWO", name: "Project Two", description: null, status: "active", work_items: [] },
];

afterEach(() => { localStorage.clear(); });

// The routes every case in this file shares: the session stub and the workspace
// read for the first project. Each case's handler names only the routes it
// exercises; anything it leaves unanswered is answered here.
function portalFetcher(handler: (path: string, init?: RequestInit) => Response | undefined) {
  return recordingFetcher((path, init) => {
    const answered = handler(path, init);
    if (answered !== undefined) return answered;
    const url = new URL(path, "http://localhost");
    if (url.pathname === "/portal/api/session") return jsonResponse({ csrf_token: "session-token" });
    return jsonResponse(workspaceResponse(projectId));
  });
}

function workspaceResponse(selected: string) {
  return { selected_project_id: selected, projects };
}

describe("attention entry point", () => {
  it("lists Goal attention and the project blocked work together", async () => {
    const { fetcher } = portalFetcher((path) => {
      const url = new URL(path, "http://localhost");
      if (url.pathname === "/portal/api/attention") {
        return jsonResponse(readAttention({
          selectedProjectId: projectId,
          goals: [{ goalId, projectId, title: "Deliver the attention entry point", state: "active", blockerReasons: ["waiting_dependency"], attentionReasons: ["awaiting_validation"], activeTasks: 1, nonterminalTasks: 1, readyToAchieve: true }],
          blocked: [{ projectId, projectName: "Project One", workItemId: "44444444-4444-4444-8444-444444444444", key: "ONE-7", title: "Operator review required", blocker: "Needs architecture decision" }],
        }));
      }
    });

    renderRoute("/portal/attention", fetcher);
    expect(await screen.findByRole("link", { name: "Deliver the attention entry point" })).toHaveAttribute("href", `/portal/goals/${goalId}`);
    expect(screen.getByText("waiting_dependency")).toBeVisible();
    expect(screen.getByText("Settlement attention: awaiting_validation")).toBeVisible();
    // The readiness badge reads the server's own `ready_to_achieve?` member.
    expect(screen.getByText("Ready to achieve on the server")).toBeVisible();
    expect(await screen.findByText("Operator review required")).toBeVisible();
    expect(screen.getByText("Needs architecture decision")).toBeVisible();
    expect(screen.getByText("Showing Project One.")).toBeVisible();
  });

  it("renders a served Goal-feed refusal statement while keeping the project blocked work", async () => {
    const { fetcher } = portalFetcher((path) => {
      const url = new URL(path, "http://localhost");
      if (url.pathname === "/portal/api/attention") {
        return jsonResponse(readAttention({
          selectedProjectId: projectId,
          goals: [],
          blocked: [{ projectId, projectName: "Project One", workItemId: "44444444-4444-4444-8444-444444444444", title: "Operator review required", blocker: "Operator review required" }],
          goalUnavailable: { code: "goal_admission_disabled", message: "goal admission is disabled" },
        }));
      }
    });

    renderRoute("/portal/attention", fetcher);
    expect(await screen.findByRole("heading", { name: "Goal attention is unavailable on this server" })).toBeVisible();
    expect(screen.getByText(/goal admission is disabled/)).toBeVisible();
    expect(screen.getByText("goal_admission_disabled")).toBeVisible();
    // The pre-existing project blockers stay rendered and actionable.
    expect(screen.getByRole("heading", { name: "Operator review required" })).toBeVisible();
  });

  it("loads the Goal feed's older page with the served cursor and never repeats an entry", async () => {
    const olderGoalId = "55555555-5555-4555-8555-555555555555";
    const { fetcher, calls } = portalFetcher((path) => {
      const url = new URL(path, "http://localhost");
      if (url.pathname === "/portal/api/attention") {
        const cursor = url.searchParams.get("cursor");
        return jsonResponse(cursor === "cursor-one"
          ? readAttention({ selectedProjectId: projectId, goals: [{ goalId: olderGoalId, projectId, title: "Older goal attention" }], nextCursor: null })
          : readAttention({ selectedProjectId: projectId, goals: [{ goalId, projectId, title: "Newest goal attention" }], nextCursor: "cursor-one" }));
      }
    });

    const user = userEvent.setup();
    renderRoute("/portal/attention", fetcher);
    expect(await screen.findByRole("link", { name: "Newest goal attention" })).toBeVisible();
    await user.click(screen.getByRole("button", { name: "Load more Goal attention" }));
    expect(await screen.findByRole("link", { name: "Older goal attention" })).toBeVisible();
    expect(screen.getByRole("link", { name: "Newest goal attention" })).toBeVisible();
    expect(calls.some((call) => call.path.includes("cursor=cursor-one"))).toBe(true);
  });

  it("does not render the feed pages a replaced first-page cursor did not serve", async () => {
    const olderGoalId = "55555555-5555-4555-8555-555555555555";
    const replacedChainGoalId = "66666666-6666-4666-8666-666666666666";
    let firstPageReads = 0;
    const { fetcher } = portalFetcher((path) => {
      const url = new URL(path, "http://localhost");
      if (url.pathname === "/portal/api/attention") {
        const cursor = url.searchParams.get("cursor");
        if (cursor === "cursor-one") {
          return jsonResponse(readAttention({ selectedProjectId: projectId, goals: [{ goalId: olderGoalId, projectId, title: "Older goal attention" }], nextCursor: null }));
        }
        if (cursor === "cursor-two") {
          return jsonResponse(readAttention({ selectedProjectId: projectId, goals: [{ goalId: replacedChainGoalId, projectId, title: "Replaced-chain goal attention" }], nextCursor: null }));
        }
        firstPageReads += 1;
        // The second first-page read ends at another Goal, so its cursor does
        // not name the boundary the reader's page was fetched from.
        return jsonResponse(readAttention({
          selectedProjectId: projectId,
          goals: [{ goalId, projectId, title: "Newest goal attention" }],
          nextCursor: firstPageReads === 1 ? "cursor-one" : "cursor-two",
        }));
      }
    });

    const user = userEvent.setup();
    const route = renderRoute("/portal/attention", fetcher);
    expect(await screen.findByRole("link", { name: "Newest goal attention" })).toBeVisible();
    await user.click(screen.getByRole("button", { name: "Load more Goal attention" }));
    expect(await screen.findByRole("link", { name: "Older goal attention" })).toBeVisible();
    // A realtime invalidation re-reads the feed without the reader's Refresh, so
    // only the served cursor can retire the page that no longer chains under it.
    await act(async () => { await route.application.queryClient.invalidateQueries({ queryKey: ["attention"] }); });
    expect(await screen.findByRole("link", { name: "Newest goal attention" })).toBeVisible();
    expect(screen.queryByRole("link", { name: "Older goal attention" })).toBeNull();
    // The reader's next page is the one the served cursor names.
    await user.click(await screen.findByRole("button", { name: "Load more Goal attention" }));
    expect(await screen.findByRole("link", { name: "Replaced-chain goal attention" })).toBeVisible();
    expect(screen.queryByRole("link", { name: "Older goal attention" })).toBeNull();
  });

  it("offers goal creation only against a selected project and keeps the server's refusal", async () => {
    const { fetcher, calls } = portalFetcher((path, init) => {
      const url = new URL(path, "http://localhost");
      if (url.pathname === "/portal/api/attention") {
        return jsonResponse(readAttention({ selectedProjectId: projectId, blocked: [] }));
      }
      if (url.pathname === `/portal/api/projects/${projectId}/goals`) {
        expect(init?.headers).toMatchObject({ "x-csrf-token": "session-token" });
        const rawBody = typeof init?.body === "string" ? init.body : "{}";
        const body = JSON.parse(rawBody) as { schema_version: string; initial_revision: { objective: string } };
        expect(body.schema_version).toBe("symmetry.goal_create.v1");
        expect(body.initial_revision.objective).toBe("Expose the attention entry point");
        return jsonResponse({ error: { code: "invalid_contract", message: "goal contract is invalid" } }, 422);
      }
    });

    const user = userEvent.setup();
    renderRoute("/portal/attention", fetcher);
    await screen.findByRole("heading", { name: "Attention" });
    await user.click(await screen.findByRole("button", { name: /New goal/ }));
    await user.type(screen.getByLabelText("Title"), "Goal from the attention page");
    await user.type(screen.getByLabelText("Objective"), "Expose the attention entry point");
    await user.click(screen.getByRole("button", { name: "Create goal" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("goal contract is invalid (invalid_contract)");
    expect(calls.some((call) => call.path.includes("/portal/api/projects/") && call.path.endsWith("/goals"))).toBe(true);
  });

  it("offers no goal creation when the served attention read selects no project", async () => {
    const { fetcher } = portalFetcher((path) => {
      const url = new URL(path, "http://localhost");
      if (url.pathname === "/portal/api/attention") {
        return jsonResponse(readAttention({ selectedProjectId: null, blocked: [] }));
      }
    });

    renderRoute("/portal/attention", fetcher);
    await screen.findByRole("heading", { name: "Attention" });
    expect(await screen.findByText("No Goal needs attention")).toBeVisible();
    // The create control is driven by the served project scope; without a
    // selected project it stays disabled and offers no create form.
    expect(screen.getByRole("button", { name: /New goal/ })).toBeDisabled();
    expect(screen.queryByRole("heading", { name: "Create a Goal" })).not.toBeInTheDocument();
  });
});

describe("goal detail surface", () => {
  it("renders the objective, accepted progress, budget truth and the read-only dependency projection", async () => {
    const acceptedSubject = { resource_id: "88888888-8888-4888-8888-888888888888", commit: "a".repeat(40), tree_digest: `sha256:${"b".repeat(64)}` };
    const { fetcher } = portalFetcher((path) => {
      const url = new URL(path, "http://localhost");
      if (url.pathname === `/portal/api/goals/${goalId}`) {
        return jsonResponse(readGoalProjection({
          id: goalId,
          projectId,
          projectKey: "ONE",
          projectName: "Project One",
          title: "Deliver the attention entry point",
          objective: "Merge Goal attention with the project blocked items",
          allowedActions: ["pause", "amend", "cancel"],
          workItems: [
            { id: "44444444-4444-4444-8444-444444444444", number: 1, title: "Merge the attention feed", status: "in_progress", "required?": true, integration: false, "accepted?": true, accepted_outcome: { id: "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee", work_item_id: "44444444-4444-4444-8444-444444444444", disposition: "accepted", subject_hash: `sha256:${"c".repeat(64)}`, evidence_ids: ["99999999-9999-4999-8999-999999999999"], candidate_subject: acceptedSubject, inserted_at: "2026-11-16T09:50:00Z" }, execution: { state: "completed" }, blockers: [] },
            { id: "55555555-5555-4555-8555-555555555555", number: 2, title: "Render the Goal detail surface", status: "queued", "required?": true, integration: true, "accepted?": false, accepted_outcome: null, execution: null, blockers: ["waiting_dependency"] },
          ],
          acceptedOutcomes: [{ id: "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee", work_item_id: "44444444-4444-4444-8444-444444444444", disposition: "accepted", subject_hash: `sha256:${"c".repeat(64)}`, evidence_ids: ["99999999-9999-4999-8999-999999999999"], candidate_subject: acceptedSubject, inserted_at: "2026-11-16T09:50:00Z" }],
          blockers: [{ reason: "waiting_dependency", count: 1, ids: ["55555555-5555-4555-8555-555555555555"], details: [] }],
          settlementAttention: [{ settlement: "awaiting_validation", "required?": true, event_id: "77777777-7777-4777-8777-777777777777", task_id: "66666666-6666-4666-8666-666666666666", goal_revision: 2, reason: null, proposed_next_action: "Run the bound validation profile", proposed_next_action_status: "proposal_only" }],
          graph: {
            nodes: [
              { id: "44444444-4444-4444-8444-444444444444", number: 1, title: "Merge the attention feed", "required?": true, board_status: "in_progress", "accepted?": true, blockers: [] },
              { id: "55555555-5555-4555-8555-555555555555", number: 2, title: "Render the Goal detail surface", "required?": true, board_status: "queued", "accepted?": false, blockers: ["waiting_dependency"] },
            ],
            // The served dependency row has no single-column identity: id is
            // null and satisfaction is the suffixed `satisfied?` member.
            edges: [{ id: null, work_item_id: "55555555-5555-4555-8555-555555555555", depends_on_id: "44444444-4444-4444-8444-444444444444", from: "55555555-5555-4555-8555-555555555555", to: "44444444-4444-4444-8444-444444444444", "satisfied?": false, blocker: "waiting_dependency", revision: 1 }],
            "cycle_detected?": true,
          },
          accounting: { held_microusd: "250000", unknown_reservation_count: 1, settled_reservation_count: 1, released_reservation_count: 0, usage_records: 2, usage_leaf_count: 2, known_usage_cost_microusd: "120000", usage_cost_microusd: null, "usage_cost_unknown?": true, unknown_usage_count: 1 },
          externalWaits: [{ id: "12121212-1212-4212-8212-121212121212", state: "waiting", reason: null, work_item_id: "55555555-5555-4555-8555-555555555555", resource_id: "88888888-8888-4888-8888-888888888888", external_ref: "wxxb789/symmetry", next_check_at: "2026-11-16T11:00:00Z", check_seq: 3, inserted_at: "2026-11-16T09:55:00Z", updated_at: "2026-11-16T10:00:00Z" }],
        }));
      }
      if (url.pathname === `/portal/api/goals/${goalId}/events`) {
        const after = Number(url.searchParams.get("after") ?? "0");
        return jsonResponse(after === 0
          ? { entries: [{ id: "event-1", sequence: 1, kind: "goal_created", actor_ref: "portal_operator", revision: 1, payload: {}, response: {}, inserted_at: "2026-11-16T09:00:00Z" }], next_after: 1 }
          : { entries: [{ id: "event-2", sequence: 2, kind: "amend", actor_ref: "portal_operator", revision: 2, payload: {}, response: {}, inserted_at: "2026-11-16T09:30:00Z" }], next_after: null });
      }
    });

    renderRoute(`/portal/goals/${goalId}`, fetcher);
    expect(await screen.findByRole("heading", { level: 1, name: "Deliver the attention entry point" })).toBeVisible();
    expect(screen.getByText("Merge Goal attention with the project blocked items")).toBeVisible();
    expect(screen.getByText("1 of 2 required items accepted")).toBeVisible();
    expect(screen.getAllByText("waiting_dependency").length).toBeGreaterThan(0);
    // The recommendation is labelled as advisory and separated from the authority controls.
    expect(screen.getByText(/Recommendation \(advisory\): Run the bound validation profile/)).toBeVisible();
    // Budget reservation and consumption stay distinct, and unknown is never zero.
    expect(screen.getByText("$0.2500")).toBeVisible();
    expect(screen.getByText("$0.1200")).toBeVisible();
    expect(screen.getByText("unknown (contains unknown usage)")).toBeVisible();
    // The integration designation renders from the served `integration` member.
    expect(screen.getByText("integration")).toBeVisible();
    // Read-only projection: the served edge renders with its own satisfaction
    // member, the served cycle flag renders its warning, and no dependency
    // editing affordance exists.
    const edges = screen.getByRole("list", { name: "Dependency edges" });
    expect(within(edges).getByText("waiting_dependency")).toBeVisible();
    expect(screen.getByText("The server reports a dependency cycle in the current revision.")).toBeVisible();
    expect(screen.queryByRole("button", { name: /dependency/i })).not.toBeInTheDocument();
    expect(screen.getByText("wxxb789/symmetry")).toBeVisible();
    // The server's action list is rendered as-is; no client rule adds an action.
    expect(screen.getByRole("button", { name: "Pause" })).toBeVisible();
    expect(screen.queryByRole("button", { name: "Achieve" })).not.toBeInTheDocument();
    // Receipts paginate over the served boundary.
    expect(await screen.findByText("goal_created")).toBeVisible();
    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: "Load older receipts" }));
    expect(await screen.findByText("amend")).toBeVisible();
    expect(screen.getByText("goal_created")).toBeVisible();
  });

  it("sends the served expected version and revision, and renders the stale conflict without overwriting", async () => {
    let commandCalls = 0;
    const { fetcher, calls } = portalFetcher((path, init) => {
      const url = new URL(path, "http://localhost");
      if (url.pathname === `/portal/api/goals/${goalId}`) {
        return jsonResponse(readGoalProjection({
          id: goalId,
          projectId,
          projectName: "Project One",
          title: "Stale conflict goal",
          state: "active",
          currentRevision: 2,
          version: 7,
          allowedActions: ["pause", "amend", "cancel"],
        }));
      }
      if (url.pathname === `/portal/api/goals/${goalId}/commands`) {
        commandCalls += 1;
        const rawBody = typeof init?.body === "string" ? init.body : "{}";
        const body = JSON.parse(rawBody) as { mutation_id: string; expected_version: number; expected_revision: number; kind: string; payload: Record<string, unknown> };
        expect(body.expected_version).toBe(7);
        expect(body.expected_revision).toBe(2);
        expect(body.kind).toBe("pause");
        expect(body.payload).toEqual({ reason: "Hold while the operator reviews" });
        expect(body.mutation_id).toMatch(/^[0-9a-f-]{36}$/);
        return jsonResponse({ error: { code: "stale", message: "resource changed since it was loaded", current_version: 9, current_revision: 3, allowed_actions: ["resume", "cancel"] } }, 409);
      }
      if (url.pathname === `/portal/api/goals/${goalId}/events`) return jsonResponse({ entries: [], next_after: null });
    });

    const user = userEvent.setup();
    renderRoute(`/portal/goals/${goalId}`, fetcher);
    await screen.findByRole("heading", { level: 1, name: "Stale conflict goal" });
    await user.click(screen.getByRole("button", { name: "Pause" }));
    await user.type(screen.getByLabelText("Reason"), "Hold while the operator reviews");
    await user.click(screen.getByRole("button", { name: "Send Pause" }));
    expect(await screen.findByRole("heading", { name: "The goal changed since this page loaded" })).toBeVisible();
    expect(screen.getByText(/version 9, revision 3/)).toBeVisible();
    expect(screen.getByText("stale")).toBeVisible();
    expect(commandCalls).toBe(1);
    // The surface requires a reload instead of silently retrying the refused command.
    expect(screen.getByRole("button", { name: "Reload the current state" })).toBeVisible();
    expect(calls.filter((call) => call.path.includes("/commands")).length).toBe(1);
  });

  // The acceptance notice waits for the served goal to settle on the applied
  // state (bounded at 2.5 s), so this case allows that bound; pre-fix bytes
  // never reach the notice and fail on the assertion with the stale conflict.
  it("re-reads the served versions at submit time so a worker-advanced goal is not sent stale", async () => {
    // The page settles on version 8; a worker then advances the goal to 9 with
    // the revision unchanged, exactly like the release's goal-control and
    // wakeup writers. The command must carry the version the server serves at
    // submit, not the settled one, or the server refuses it as stale.
    let advanced = false;
    const commands: Array<{ expected_version: number; expected_revision: number }> = [];
    const { fetcher } = portalFetcher((path, init) => {
      const url = new URL(path, "http://localhost");
      if (url.pathname === `/portal/api/goals/${goalId}`) {
        return jsonResponse(readGoalProjection({ id: goalId, projectId, projectName: "Project One", title: "Worker-advanced goal", state: "active", currentRevision: 1, version: advanced ? 9 : 8, allowedActions: ["pause", "amend", "cancel"] }));
      }
      if (url.pathname === `/portal/api/goals/${goalId}/commands`) {
        const rawBody = typeof init?.body === "string" ? init.body : "{}";
        const envelope = JSON.parse(rawBody) as { expected_version: number; expected_revision: number; kind: string };
        commands.push(envelope);
        if (envelope.expected_version !== 9) {
          return jsonResponse({ error: { code: "stale", message: "resource changed since it was loaded", current_version: 9, current_revision: 1, allowed_actions: ["pause", "amend", "cancel"] } }, 409);
        }
        return jsonResponse({ goal_id: goalId, mutation_id: "m", event: { id: "m", sequence: 4, kind: "amend", revision: 1 } });
      }
      if (url.pathname === `/portal/api/goals/${goalId}/events`) return jsonResponse({ entries: [], next_after: null });
    });

    const user = userEvent.setup();
    renderRoute(`/portal/goals/${goalId}`, fetcher);
    await screen.findByRole("heading", { level: 1, name: "Worker-advanced goal" });
    // The settled page still shows version 8: the worker's advance is not read yet.
    expect(screen.getByText(/· revision 1 · version 8$/)).toBeVisible();
    await user.click(screen.getByRole("button", { name: "Amend" }));
    await user.type(screen.getByRole("textbox", { name: "Objective" }), "Amended while a worker advanced the goal");
    await user.type(screen.getByLabelText("Reason"), "Operator amendment");
    // The worker advances the goal's version with the revision unchanged.
    advanced = true;
    await user.click(screen.getByRole("button", { name: "Send Amend" }));
    // The command carries the version the server serves now; pre-fix it carries
    // the settled 8 and the server answers the stale conflict instead.
    await waitFor(() => expect(commands).toHaveLength(1));
    expect(commands[0]?.expected_version).toBe(9);
    expect(commands[0]?.expected_revision).toBe(1);
    expect(await screen.findByText("Amend accepted by the server.", {}, { timeout: 5_000 })).toBeVisible();
  }, 15_000);

  it("carries a fresh mutation identity per logical action attempt and reports the server receipt", async () => {
    const identities: string[] = [];
    const { fetcher } = portalFetcher((path, init) => {
      const url = new URL(path, "http://localhost");
      if (url.pathname === `/portal/api/goals/${goalId}`) {
        return jsonResponse(readGoalProjection({ id: goalId, projectId, projectName: "Project One", title: "Replay goal", state: "active", allowedActions: ["pause", "amend", "cancel"] }));
      }
      if (url.pathname === `/portal/api/goals/${goalId}/commands`) {
        const rawBody = typeof init?.body === "string" ? init.body : "{}";
        const body = JSON.parse(rawBody) as { mutation_id: string };
        identities.push(body.mutation_id);
        return jsonResponse({ goal_id: goalId, mutation_id: body.mutation_id, event: { id: body.mutation_id, sequence: 2, kind: "pause", revision: 1 } });
      }
      if (url.pathname === `/portal/api/goals/${goalId}/events`) return jsonResponse({ entries: [], next_after: null });
    });

    const user = userEvent.setup();
    renderRoute(`/portal/goals/${goalId}`, fetcher);
    await screen.findByRole("heading", { level: 1, name: "Replay goal" });
    await user.click(screen.getByRole("button", { name: "Pause" }));
    await user.type(screen.getByLabelText("Reason"), "Operator pause");
    await user.click(screen.getByRole("button", { name: "Send Pause" }));
    // The surface states acceptance only after the served goal settles on the
    // applied state (bounded), so this wait allows for that bound.
    expect(await screen.findByText("Pause accepted by the server.", {}, { timeout: 5_000 })).toBeVisible();
    await user.click(screen.getByRole("button", { name: "Pause" }));
    await user.type(screen.getByLabelText("Reason"), "Operator pause again");
    await user.click(screen.getByRole("button", { name: "Send Pause" }));
    await waitFor(() => expect(identities.length).toBe(2));
    // Each logical action attempt carries a fresh identity; the server owns replay.
    expect(identities[0]).not.toBe(identities[1]);
  });

  it("offers only the served actions and builds the achieve envelope from a server-shaped multi-item plan", async () => {
    const acceptedSubject = { resource_id: "88888888-8888-4888-8888-888888888888", commit: "a".repeat(40), tree_digest: `sha256:${"b".repeat(64)}` };
    let achievePayload: Record<string, unknown> | null = null;
    const { fetcher } = portalFetcher((path, init) => {
      const url = new URL(path, "http://localhost");
      if (url.pathname === `/portal/api/goals/${goalId}`) {
        return jsonResponse(readGoalProjection({
          id: goalId,
          projectId,
          projectName: "Project One",
          title: "Ready goal",
          state: "active",
          allowedActions: ["achieve", "pause", "amend", "cancel"],
          // Two required items: the server refuses an achieve without the
          // integration work item id, so the envelope must carry the served
          // `integration` designation.
          workItems: [
            { id: "44444444-4444-4444-8444-444444444444", number: 1, title: "Implementation", status: "done", "required?": true, integration: false, "accepted?": false, accepted_outcome: null, execution: null, blockers: [] },
            { id: "55555555-5555-4555-8555-555555555555", number: 2, title: "Integration", status: "done", "required?": true, integration: true, "accepted?": true, accepted_outcome: { id: "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee", work_item_id: "55555555-5555-4555-8555-555555555555", disposition: "accepted", subject_hash: `sha256:${"c".repeat(64)}`, evidence_ids: ["99999999-9999-4999-8999-999999999999"], candidate_subject: acceptedSubject, inserted_at: "2026-11-16T09:50:00Z" }, execution: null, blockers: [] },
          ],
          acceptedOutcomes: [{ id: "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee", work_item_id: "44444444-4444-4444-8444-444444444444", disposition: "accepted", subject_hash: `sha256:${"c".repeat(64)}`, evidence_ids: ["99999999-9999-4999-8999-999999999999"], candidate_subject: acceptedSubject, inserted_at: "2026-11-16T09:50:00Z" }],
        }));
      }
      if (url.pathname === `/portal/api/goals/${goalId}/commands`) {
        const rawBody = typeof init?.body === "string" ? init.body : "{}";
        const envelope = JSON.parse(rawBody) as { payload?: Record<string, unknown> };
        achievePayload = envelope.payload ?? {};
        return jsonResponse({ goal_id: goalId, mutation_id: "m", event: { id: "m", sequence: 3, kind: "achieve", revision: 1 }, response: { state: "achieved" } });
      }
      if (url.pathname === `/portal/api/goals/${goalId}/events`) return jsonResponse({ entries: [], next_after: null });
    });

    const user = userEvent.setup();
    renderRoute(`/portal/goals/${goalId}`, fetcher);
    await screen.findByRole("heading", { level: 1, name: "Ready goal" });
    await user.click(screen.getByRole("button", { name: "Achieve" }));
    await user.click(screen.getByRole("button", { name: "Send Achieve" }));
    await waitFor(() => expect(achievePayload).not.toBeNull());
    if (achievePayload === null) throw new Error("the achieve command was not sent");
    const payload = achievePayload;
    expect(payload["subject"]).toEqual(acceptedSubject);
    expect(payload["evidence_ids"]).toEqual(["99999999-9999-4999-8999-999999999999"]);
    expect(payload["integration_work_item_id"]).toBe("55555555-5555-4555-8555-555555555555");
    expect(payload["decision_id"]).toBeNull();
  });

  it("renders the served refusal when the server does not allow an action", async () => {
    const { fetcher } = portalFetcher((path: string) => {
      const url = new URL(path, "http://localhost");
      if (url.pathname === `/portal/api/goals/${goalId}`) {
        return jsonResponse(readGoalProjection({ id: goalId, projectId, projectName: "Project One", title: "Draft goal", state: "draft", allowedActions: ["request_plan", "amend", "cancel"] }));
      }
      if (url.pathname === `/portal/api/goals/${goalId}/commands`) {
        return jsonResponse({ error: { code: "forbidden", message: "action is not allowed for this goal" } }, 403);
      }
      if (url.pathname === `/portal/api/goals/${goalId}/events`) return jsonResponse({ entries: [], next_after: null });
    });

    const user = userEvent.setup();
    renderRoute(`/portal/goals/${goalId}`, fetcher);
    await screen.findByRole("heading", { level: 1, name: "Draft goal" });
    expect(screen.getByRole("button", { name: "Request a plan" })).toBeVisible();
    expect(screen.queryByRole("button", { name: "Activate" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Achieve" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Pause" })).not.toBeInTheDocument();
    // A refused amend keeps the server's own message and code on the surface.
    await user.click(screen.getByRole("button", { name: "Amend" }));
    await user.type(screen.getByRole("textbox", { name: "Objective" }), "Amend an action the server forbids");
    await user.type(screen.getByLabelText("Reason"), "Operator amendment");
    await user.click(screen.getByRole("button", { name: "Send Amend" }));
    const refusal = await screen.findByRole("alert");
    expect(refusal).toHaveTextContent("action is not allowed for this goal");
    expect(refusal).toHaveTextContent("forbidden");
  });

  it("recovers a refused command transport with a retryable error and never claims success", async () => {
    const { fetcher } = portalFetcher((path: string) => {
      const url = new URL(path, "http://localhost");
      if (url.pathname === `/portal/api/goals/${goalId}`) {
        return jsonResponse(readGoalProjection({ id: goalId, projectId, projectName: "Project One", title: "Offline goal", state: "active", allowedActions: ["pause", "amend", "cancel"] }));
      }
      if (url.pathname === `/portal/api/goals/${goalId}/commands`) {
        throw new TypeError("network down");
      }
      if (url.pathname === `/portal/api/goals/${goalId}/events`) return jsonResponse({ entries: [], next_after: null });
    });

    const user = userEvent.setup();
    renderRoute(`/portal/goals/${goalId}`, fetcher);
    await screen.findByRole("heading", { level: 1, name: "Offline goal" });
    await user.click(screen.getByRole("button", { name: "Pause" }));
    await user.type(screen.getByLabelText("Reason"), "Operator pause");
    await user.click(screen.getByRole("button", { name: "Send Pause" }));
    expect(await screen.findByText(/Cannot reach the server/)).toBeVisible();
    expect(screen.queryByText(/accepted by the server/)).not.toBeInTheDocument();
  });

  it("renders the empty receipts state when the event read answers without an error", async () => {
    const { fetcher } = portalFetcher((path: string) => {
      const url = new URL(path, "http://localhost");
      if (url.pathname === `/portal/api/goals/${goalId}`) {
        return jsonResponse(readGoalProjection({ id: goalId, projectId, projectName: "Project One", title: "Quiet goal", state: "active", allowedActions: [] }));
      }
      if (url.pathname === `/portal/api/goals/${goalId}/events`) return jsonResponse({ entries: [], next_after: null });
    });

    renderRoute(`/portal/goals/${goalId}`, fetcher);
    await screen.findByRole("heading", { level: 1, name: "Quiet goal" });
    // react-query starts `error` at null, not undefined: a successful empty page
    // must render the empty state, never a failure panel.
    expect(await screen.findByText("No receipts")).toBeVisible();
    expect(screen.queryByRole("heading", { name: /Refresh failed|Unable to load workspace/ })).not.toBeInTheDocument();
  });

  it("renders the plan approval control from a served open plan decision and sends the served option", async () => {
    const decision = {
      id: "dddddddd-dddd-4ddd-8ddd-dddddddddddd",
      kind: "plan",
      state: "open",
      "open?": true,
      version: 3,
      goal_revision: 1,
      question: "Accept the proposed plan?",
      options: [
        { id: "accept", label: "Accept", consequence: "Apply the approved action." },
        { id: "reject", label: "Reject", consequence: "Keep the current approved state." },
      ],
      resolution: null,
    };
    const commands: Array<Record<string, unknown>> = [];
    const { fetcher } = portalFetcher((path: string, init?: RequestInit) => {
      const url = new URL(path, "http://localhost");
      if (url.pathname === `/portal/api/goals/${goalId}`) {
        return jsonResponse(readGoalProjection({ id: goalId, projectId, projectName: "Project One", title: "Plan goal", state: "draft", allowedActions: ["amend", "cancel"], decisions: [decision] }));
      }
      if (url.pathname === `/portal/api/goals/${goalId}/commands`) {
        const rawBody = typeof init?.body === "string" ? init.body : "{}";
        commands.push(JSON.parse(rawBody) as Record<string, unknown>);
        return jsonResponse({ goal_id: goalId, mutation_id: "m", event: { id: "m", sequence: 2, kind: "resolve_decision", revision: 1 } });
      }
      if (url.pathname === `/portal/api/goals/${goalId}/events`) return jsonResponse({ entries: [], next_after: null });
    });

    const user = userEvent.setup();
    renderRoute(`/portal/goals/${goalId}`, fetcher);
    await screen.findByRole("heading", { level: 1, name: "Plan goal" });
    // A JSON document contains braces that user-event's keyboard parser reads as
    // key descriptors; set the controlled textarea value directly instead.
    fireEvent.change(screen.getByLabelText("Plan proposal"), { target: { value: JSON.stringify({ schema_version: "symmetry.plan.v1", items: [] }) } });
    await user.click(screen.getByRole("button", { name: "Resolve the decision with accept" }));
    // The accepted two-step contract: the served decision is resolved with the
    // served option, then the plan admission follows with the re-read versions.
    await waitFor(() => expect(commands.length).toBe(2));
    const [resolve, admit] = commands as Array<{ kind?: string; payload?: Record<string, unknown> } | undefined>;
    if (!resolve || !admit) throw new Error("the approval commands were not sent");
    expect(resolve.kind).toBe("resolve_decision");
    expect(resolve.payload?.["decision_id"]).toBe(decision.id);
    expect(resolve.payload?.["expected_decision_version"]).toBe(3);
    expect(resolve.payload?.["option_id"]).toBe("accept");
    expect(admit.kind).toBe("accept_plan");
    expect(admit.payload?.["decision_id"]).toBe(decision.id);
    // The plan admission carries the server's own digest for this proposal; the
    // vector was computed in this worktree with the portable Elixir toolchain
    // and cross-checked with Python hashlib:
    //   mix run --no-start -e 'IO.puts("sha256:" <> Base.encode16(SymmetryControl.RequestHash.canonical(%{"schema_version" => "symmetry.plan.v1", "items" => []}), case: :lower))'
    // i.e. sha256 over {"items":[],"schema_version":"symmetry.plan.v1"}.
    expect(admit.payload?.["proposal_hash"]).toBe("sha256:e9a1f1719763b645d05e4b1fcc241b7c7fb71c0594876ae16428757a7e222805");
  });

  it("offers no plan approval when the served plan decision carries no acceptance option", async () => {
    const decision = { id: "dddddddd-dddd-4ddd-8ddd-dddddddddddd", goal_revision: 1, kind: "plan", state: "open", "open?": true, version: 3, question: "Accept the proposed plan?", options: [{ id: "reject", label: "Reject", consequence: "Keep the current approved state." }], resolution: null };
    const { fetcher } = portalFetcher((path: string) => {
      const url = new URL(path, "http://localhost");
      if (url.pathname === `/portal/api/goals/${goalId}`) {
        return jsonResponse(readGoalProjection({ id: goalId, projectId, projectName: "Project One", title: "No option goal", state: "draft", allowedActions: ["amend", "cancel"], decisions: [decision] }));
      }
      if (url.pathname === `/portal/api/goals/${goalId}/events`) return jsonResponse({ entries: [], next_after: null });
    });

    renderRoute(`/portal/goals/${goalId}`, fetcher);
    await screen.findByRole("heading", { level: 1, name: "No option goal" });
    expect(screen.queryByRole("button", { name: "Resolve the decision with accept" })).not.toBeInTheDocument();
    expect(screen.getByText("The served plan decision carries no acceptance option; this surface offers no resolution for it.")).toBeVisible();
  });

  it("offers no plan approval for a resolved plan decision", async () => {
    const decision = { id: "dddddddd-dddd-4ddd-8ddd-dddddddddddd", goal_revision: 1, kind: "plan", state: "resolved", "open?": false, version: 4, question: "Accept the proposed plan?", options: [{ id: "accept", label: "Accept", consequence: "Apply the approved action." }], resolution: { option_id: "accept" } };
    const { fetcher } = portalFetcher((path: string) => {
      const url = new URL(path, "http://localhost");
      if (url.pathname === `/portal/api/goals/${goalId}`) {
        return jsonResponse(readGoalProjection({ id: goalId, projectId, projectName: "Project One", title: "Resolved plan goal", state: "draft", allowedActions: ["activate", "amend", "cancel"], decisions: [decision] }));
      }
      if (url.pathname === `/portal/api/goals/${goalId}/events`) return jsonResponse({ entries: [], next_after: null });
    });

    renderRoute(`/portal/goals/${goalId}`, fetcher);
    await screen.findByRole("heading", { level: 1, name: "Resolved plan goal" });
    expect(screen.queryByRole("button", { name: "Resolve the decision with accept" })).not.toBeInTheDocument();
    expect(screen.getByText(/Resolved: \{"option_id":"accept"\}/)).toBeVisible();
  });

  it("renders the explicit absence text for a served non-scalar member", async () => {
    const { fetcher } = portalFetcher((path) => {
      const url = new URL(path, "http://localhost");
      if (url.pathname === `/portal/api/goals/${goalId}`) {
        return jsonResponse(readGoalProjection({
          id: goalId,
          projectId,
          projectName: "Project One",
          title: "Open-member goal",
          state: "active",
          allowedActions: [],
          // The served read keeps open-JSON members as served: a non-scalar
          // `execution.state` must render the explicit absence text instead of
          // stringifying the object.
          workItems: [{ id: "44444444-4444-4444-8444-444444444444", number: 1, title: "Open-member item", status: "in_progress", "required?": true, integration: false, "accepted?": false, accepted_outcome: null, execution: { state: ["running", "queued"] }, blockers: [] }],
        }));
      }
      if (url.pathname === `/portal/api/goals/${goalId}/events`) return jsonResponse({ entries: [], next_after: null });
    });

    renderRoute(`/portal/goals/${goalId}`, fetcher);
    await screen.findByRole("heading", { level: 1, name: "Open-member goal" });
    expect(screen.getByText("Execution: unknown state")).toBeVisible();
    expect(screen.queryByText(/\[object Object\]/)).not.toBeInTheDocument();
  });
});
