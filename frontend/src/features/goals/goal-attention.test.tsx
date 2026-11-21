import { act, fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import omittedProposal from "../../../../contracts/fixtures/valid/plan-proposal.integration-omitted.json";
import { jsonResponse, recordingFetcher, renderRoute } from "../../app/test-harness";
import { readAttention, readGoalProjection } from "../../services/api.fixtures";

const projectId = "11111111-1111-4111-8111-111111111111";
const secondProjectId = "22222222-2222-4222-8222-222222222222";
const goalId = "33333333-3333-4333-8333-333333333333";
const secondGoalId = "33333333-3333-4333-8333-333333333334";

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

// The mutation identity the open action form displays: the identity the send
// carries with it.
function displayedIdentity() {
  const text = screen.getByText(/mutation identity [0-9a-f]/).textContent ?? "";
  return (/mutation identity ([0-9a-f-]{36})/.exec(text))?.[1] ?? "";
}

describe("attention entry point", () => {
  it("lists Goal attention and the project blocked work together", async () => {
    const { fetcher } = portalFetcher((path) => {
      const url = new URL(path, "http://localhost");
      if (url.pathname === "/portal/api/attention") {
        return jsonResponse(readAttention({
          selectedProjectId: projectId,
          goals: [
            { goalId, projectId, title: "Deliver the attention entry point", state: "active", blockerReasons: ["waiting_dependency"], attentionReasons: ["awaiting_validation"], activeTasks: 1, nonterminalTasks: 1, readyToAchieve: true },
            { goalId: secondGoalId, projectId, title: "Not yet ready goal", readyToAchieve: false },
          ],
          blocked: [{ projectId, projectName: "Project One", workItemId: "44444444-4444-4444-8444-444444444444", key: "ONE-7", title: "Operator review required", blocker: "Needs architecture decision" }],
        }));
      }
    });

    renderRoute("/portal/attention", fetcher);
    expect(await screen.findByRole("link", { name: "Deliver the attention entry point" })).toHaveAttribute("href", `/portal/goals/${goalId}`);
    expect(screen.getByText("waiting_dependency")).toBeVisible();
    expect(screen.getByText("Settlement attention: awaiting_validation")).toBeVisible();
    // The readiness badge reads the server's own `ready_to_achieve?` member in
    // both polarities: the unready entry never renders it.
    expect(screen.getByText("Ready to achieve on the server")).toBeVisible();
    expect(screen.getAllByText("Ready to achieve on the server")).toHaveLength(1);
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
            { id: "66666666-6666-4666-8666-666666666666", number: 3, title: "Optional polish", status: "queued", "required?": false, integration: false, "accepted?": false, accepted_outcome: null, execution: null, blockers: [] },
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
    // Both served polarities of `required?` and the accepted row's own outcome
    // text render from the served members.
    expect(screen.getByText("optional")).toBeVisible();
    expect(screen.getByText(/^Accepted outcome sha256:c{4}/)).toBeVisible();
    expect(screen.getAllByText(/^Accepted outcome sha256:/)).toHaveLength(1);
    expect(screen.getAllByText(/^No accepted outcome bound to this item/)).toHaveLength(2);
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
    // The served `after` page is ascending from sequence 0, so this button
    // loads newer receipts, not older ones.
    await user.click(screen.getByRole("button", { name: "Load newer receipts" }));
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
  it("re-reads the served versions at submit time so a concurrently advanced goal is not sent stale", async () => {
    // The page settles on version 8; a concurrent operator command then advances
    // the goal to 9 with the revision unchanged. The command must carry the
    // version the server serves at submit, not the settled one, or the server
    // refuses it as stale.
    let advanced = false;
    const commands: { expected_version: number; expected_revision: number }[] = [];
    const { fetcher } = portalFetcher((path, init) => {
      const url = new URL(path, "http://localhost");
      if (url.pathname === `/portal/api/goals/${goalId}`) {
        return jsonResponse(readGoalProjection({ id: goalId, projectId, projectName: "Project One", title: "Concurrently advanced goal", state: "active", currentRevision: 1, version: advanced ? 9 : 8, allowedActions: ["pause", "amend", "cancel"] }));
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
    await screen.findByRole("heading", { level: 1, name: "Concurrently advanced goal" });
    // The settled page still shows version 8: the concurrent advance is not read yet.
    expect(screen.getByText(/· revision 1 · version 8$/)).toBeVisible();
    await user.click(screen.getByRole("button", { name: "Amend" }));
    await user.type(screen.getByRole("textbox", { name: "Objective" }), "Amended while a concurrent operator command advanced the goal");
    await user.type(screen.getByLabelText("Reason"), "Operator amendment");
    // The concurrent operator command advances the goal's version with the revision unchanged.
    advanced = true;
    await user.click(screen.getByRole("button", { name: "Send Amend" }));
    // The command carries the version the server serves now; pre-fix it carries
    // the settled 8 and the server answers the stale conflict instead.
    await waitFor(() => { expect(commands).toHaveLength(1); });
    expect(commands[0]?.expected_version).toBe(9);
    expect(commands[0]?.expected_revision).toBe(1);
    expect(await screen.findByText("Amend accepted by the server.", {}, { timeout: 5_000 })).toBeVisible();
  }, 15_000);

  it("freezes one mutation identity per logical intent until the server answers", async () => {
    const identities: string[] = [];
    const bodies: string[] = [];
    let attempted = 0;
    let servedVersion = 3;
    const { fetcher } = portalFetcher((path, init) => {
      const url = new URL(path, "http://localhost");
      if (url.pathname === `/portal/api/goals/${goalId}`) {
        return jsonResponse(readGoalProjection({ id: goalId, projectId, projectName: "Project One", title: "Replay goal", state: "active", version: servedVersion, allowedActions: ["pause", "amend", "cancel"] }));
      }
      if (url.pathname === `/portal/api/goals/${goalId}/commands`) {
        const rawBody = typeof init?.body === "string" ? init.body : "{}";
        const body = JSON.parse(rawBody) as { mutation_id: string };
        identities.push(body.mutation_id);
        bodies.push(rawBody);
        // The first attempt's acknowledgement is lost at the transport layer:
        // the server may still have applied it, so the intent must replay.
        if (attempted++ === 0) {
          // The applied command advanced the goal before the operator re-sends.
          servedVersion = 4;
          throw new TypeError("network down");
        }
        return jsonResponse({ goal_id: goalId, mutation_id: body.mutation_id, event: { id: body.mutation_id, sequence: 2, kind: "pause", revision: 1 } });
      }
      if (url.pathname === `/portal/api/goals/${goalId}/events`) return jsonResponse({ entries: [], next_after: null });
    });

    const user = userEvent.setup();
    renderRoute(`/portal/goals/${goalId}`, fetcher);
    await screen.findByRole("heading", { level: 1, name: "Replay goal" });
    await user.click(screen.getByRole("button", { name: "Pause" }));
    await user.type(screen.getByLabelText("Reason"), "Operator pause");
    // The identity the form shows is the identity the command carries.
    const shown = displayedIdentity();
    expect(shown).toMatch(/^[0-9a-f-]{36}$/);
    await user.click(screen.getByRole("button", { name: "Send Pause" }));
    expect(await screen.findByText(/Cannot reach the server/)).toBeVisible();
    expect(identities).toEqual([shown]);
    // Re-sending the same payload after the lost acknowledgement reuses the
    // frozen identity, so the server replays instead of applying twice.
    await user.click(screen.getByRole("button", { name: "Pause" }));
    expect(displayedIdentity()).toBe(shown);
    await user.click(screen.getByRole("button", { name: "Send Pause" }));
    // The surface states acceptance only after the served goal settles on the
    // applied state (bounded), so this wait allows for that bound.
    expect(await screen.findByText("Pause accepted by the server.", {}, { timeout: 5_000 })).toBeVisible();
    await waitFor(() => { expect(identities).toEqual([shown, shown]); });
    // The re-send repeated the frozen attempt byte for byte — the identity and
    // both expected versions the first attempt carried, not the version the
    // goal now serves — because the server replays only a byte-equal body and
    // refuses the reused identity as different input otherwise.
    expect(bodies).toHaveLength(2);
    expect(bodies[1]).toBe(bodies[0]);
    const resent = JSON.parse(bodies[1] ?? "{}") as { expected_version?: number };
    expect(resent.expected_version).toBe(3);
    // The settled attempt releases its identity: the next intent mints a new one.
    await user.click(screen.getByRole("button", { name: "Pause" }));
    const renewed = displayedIdentity();
    expect(renewed).not.toBe(shown);
    await user.click(screen.getByRole("button", { name: "Send Pause" }));
    await waitFor(() => { expect(identities).toEqual([shown, shown, renewed]); });
    // The attempt must settle (and release its identity) before the next one;
    // the acceptance notice waits for the served goal, so allow that bound.
    await waitFor(() => expect(screen.getByRole("button", { name: "Pause" })).toBeEnabled(), { timeout: 5_000 });
    // A changed payload is a different logical intent and mints its own identity.
    await user.click(screen.getByRole("button", { name: "Pause" }));
    await user.clear(screen.getByLabelText("Reason"));
    await user.type(screen.getByLabelText("Reason"), "Operator pause with a changed reason");
    expect(displayedIdentity()).not.toBe(renewed);
  }, 15_000);

  it("expires the session when a command answers 401 instead of rendering it as a refusal", async () => {
    const replace = vi.fn();
    const { fetcher } = portalFetcher((path) => {
      const url = new URL(path, "http://localhost");
      if (url.pathname === `/portal/api/goals/${goalId}`) {
        return jsonResponse(readGoalProjection({ id: goalId, projectId, projectName: "Project One", title: "Expired session goal", state: "active", allowedActions: ["pause", "amend", "cancel"] }));
      }
      if (url.pathname === `/portal/api/goals/${goalId}/commands`) {
        // A real expired portal session answers a command with its own JSON 401;
        // the command transport classifies it exactly like every other read.
        return jsonResponse({ error: { code: "unauthenticated", message: "the portal session is not authenticated" } }, 401);
      }
      if (url.pathname === `/portal/api/goals/${goalId}/events`) return jsonResponse({ entries: [], next_after: null });
    });

    const user = userEvent.setup();
    const { application } = renderRoute(`/portal/goals/${goalId}`, fetcher, {
      location: { href: `http://localhost/portal/goals/${goalId}`, replace },
    });
    await screen.findByRole("heading", { level: 1, name: "Expired session goal" });
    await user.click(screen.getByRole("button", { name: "Pause" }));
    await user.type(screen.getByLabelText("Reason"), "Operator pause");
    await user.click(screen.getByRole("button", { name: "Send Pause" }));
    // Session expiry owns this answer: the private cache is cleared and the
    // operator returns to login instead of reading a refusal panel.
    await vi.waitFor(() => { expect(replace).toHaveBeenCalledWith(`/portal/login?return_to=${encodeURIComponent(`/portal/goals/${goalId}`)}`); });
    expect(screen.queryByRole("heading", { name: "The server refused this action" })).not.toBeInTheDocument();
    expect(application.auth.getSnapshot()).toBe(true);
  });

  it("never announces success from an undecodable receipt", async () => {
    const identities: string[] = [];
    const { fetcher } = portalFetcher((path, init) => {
      const url = new URL(path, "http://localhost");
      if (url.pathname === `/portal/api/goals/${goalId}`) {
        return jsonResponse(readGoalProjection({ id: goalId, projectId, projectName: "Project One", title: "Empty receipt goal", state: "active", allowedActions: ["pause", "amend", "cancel"] }));
      }
      // A 200 without the receipt the accepted command answers with is a
      // response the surface cannot read safely, never a success.
      if (url.pathname === `/portal/api/goals/${goalId}/commands`) {
        const rawBody = typeof init?.body === "string" ? init.body : "{}";
        const body = JSON.parse(rawBody) as { mutation_id: string };
        identities.push(body.mutation_id);
        return jsonResponse({});
      }
      if (url.pathname === `/portal/api/goals/${goalId}/events`) return jsonResponse({ entries: [], next_after: null });
    });

    const user = userEvent.setup();
    renderRoute(`/portal/goals/${goalId}`, fetcher);
    await screen.findByRole("heading", { level: 1, name: "Empty receipt goal" });
    await user.click(screen.getByRole("button", { name: "Pause" }));
    await user.type(screen.getByLabelText("Reason"), "Operator pause");
    await user.click(screen.getByRole("button", { name: "Send Pause" }));
    expect(await screen.findByText(/could not be read safely/)).toBeVisible();
    expect(screen.queryByText(/accepted by the server/)).not.toBeInTheDocument();
    // An unreadable answer settles nothing: the surface cannot know whether the
    // server applied the command, so the reopened form presents the same frozen
    // identity and a re-send carries it.
    const first = identities[0] ?? "";
    expect(first).toMatch(/^[0-9a-f-]{36}$/);
    await user.click(screen.getByRole("button", { name: "Pause" }));
    expect(displayedIdentity()).toBe(first);
    await user.click(screen.getByRole("button", { name: "Send Pause" }));
    await waitFor(() => { expect(identities).toEqual([first, first]); });
    expect(screen.queryByText(/accepted by the server/)).not.toBeInTheDocument();
  });

  it("renders a prototype-name served action as an unknown label instead of a function", async () => {
    const { fetcher } = portalFetcher((path) => {
      const url = new URL(path, "http://localhost");
      if (url.pathname === `/portal/api/goals/${goalId}`) {
        return jsonResponse(readGoalProjection({ id: goalId, projectId, projectName: "Project One", title: "Prototype action goal", state: "active", allowedActions: ["constructor", "pause"] }));
      }
      if (url.pathname === `/portal/api/goals/${goalId}/events`) return jsonResponse({ entries: [], next_after: null });
    });

    renderRoute(`/portal/goals/${goalId}`, fetcher);
    await screen.findByRole("heading", { level: 1, name: "Prototype action goal" });
    // The served action list is free-form strings: an own-key lookup renders
    // `constructor` as an unknown label, never Object's own function.
    expect(screen.getByRole("button", { name: "Unknown action: constructor" })).toBeVisible();
    expect(screen.getByRole("button", { name: "Pause" })).toBeVisible();
  });

  it("offers only the served actions and builds the achieve envelope from a server-shaped multi-item plan", async () => {
    // The two accepted outcomes carry distinct subjects and evidence: the
    // server requires every evidence row to belong to the achieve subject
    // (`ensure_evidence_subject!`), so the payload may only carry the
    // integration subject's own evidence.
    const implementationSubject = { resource_id: "88888888-8888-4888-8888-888888888888", commit: "a".repeat(40), tree_digest: `sha256:${"b".repeat(64)}` };
    const integrationSubject = { resource_id: "88888888-8888-4888-8888-888888888888", commit: "c".repeat(40), tree_digest: `sha256:${"d".repeat(64)}` };
    const implementationEvidence = "99999999-9999-4999-8999-999999999999";
    const integrationEvidence = "99999999-9999-4999-8999-999999999998";
    // `undefined`, not `null`: a `= null` initializer would narrow the variable to
    // `null` for the checker and leave everything after the guard unreachable.
    let achievePayload: Record<string, unknown> | undefined;
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
            { id: "44444444-4444-4444-8444-444444444444", number: 1, title: "Implementation", status: "done", "required?": true, integration: false, "accepted?": true, accepted_outcome: { id: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", work_item_id: "44444444-4444-4444-8444-444444444444", disposition: "accepted", subject_hash: `sha256:${"e".repeat(64)}`, evidence_ids: [implementationEvidence], candidate_subject: implementationSubject, inserted_at: "2026-11-16T09:50:00Z" }, execution: null, blockers: [] },
            { id: "55555555-5555-4555-8555-555555555555", number: 2, title: "Integration", status: "done", "required?": true, integration: true, "accepted?": true, accepted_outcome: { id: "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee", work_item_id: "55555555-5555-4555-8555-555555555555", disposition: "accepted", subject_hash: `sha256:${"c".repeat(64)}`, evidence_ids: [integrationEvidence], candidate_subject: integrationSubject, inserted_at: "2026-11-16T09:50:00Z" }, execution: null, blockers: [] },
          ],
          acceptedOutcomes: [
            { id: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", work_item_id: "44444444-4444-4444-8444-444444444444", disposition: "accepted", subject_hash: `sha256:${"e".repeat(64)}`, evidence_ids: [implementationEvidence], candidate_subject: implementationSubject, inserted_at: "2026-11-16T09:50:00Z" },
            { id: "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee", work_item_id: "55555555-5555-4555-8555-555555555555", disposition: "accepted", subject_hash: `sha256:${"c".repeat(64)}`, evidence_ids: [integrationEvidence], candidate_subject: integrationSubject, inserted_at: "2026-11-16T09:51:00Z" },
          ],
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
    await waitFor(() => { expect(achievePayload).toBeDefined(); });
    if (achievePayload === undefined) throw new Error("the achieve command was not sent");
    const payload = achievePayload;
    expect(payload.subject).toEqual(integrationSubject);
    expect(payload.evidence_ids).toEqual([integrationEvidence]);
    expect(payload.integration_work_item_id).toBe("55555555-5555-4555-8555-555555555555");
    expect(payload.decision_id).toBeNull();
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
    const commands: Record<string, unknown>[] = [];
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
    await waitFor(() => { expect(commands.length).toBe(2); });
    const [resolve, admit] = commands as ({ kind?: string; payload?: Record<string, unknown> } | undefined)[];
    if (!resolve || !admit) throw new Error("the approval commands were not sent");
    expect(resolve.kind).toBe("resolve_decision");
    expect(resolve.payload?.decision_id).toBe(decision.id);
    expect(resolve.payload?.expected_decision_version).toBe(3);
    expect(resolve.payload?.option_id).toBe("accept");
    expect(admit.kind).toBe("accept_plan");
    expect(admit.payload?.decision_id).toBe(decision.id);
    // The plan admission carries the server's own digest for this proposal; the
    // vector was computed in this worktree with the portable Elixir toolchain
    // and cross-checked with Python hashlib:
    //   mix run --no-start -e 'IO.puts("sha256:" <> Base.encode16(SymmetryControl.RequestHash.canonical(%{"schema_version" => "symmetry.plan.v1", "items" => []}), case: :lower))'
    // i.e. sha256 over {"items":[],"schema_version":"symmetry.plan.v1"}.
    expect(admit.payload?.proposal_hash).toBe("sha256:e9a1f1719763b645d05e4b1fcc241b7c7fb71c0594876ae16428757a7e222805");
  });

  it("builds the decision resolution from a submit-time read so a concurrently advanced goal is not sent stale", async () => {
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
    // The page settles on goal version 4 and decision version 3; a concurrent
    // operator command then advances the goal to 5 with the revision unchanged.
    // The decision's own drift to 4 is a fixture premise for the re-derivation,
    // not a concurrent effect: the only writer that bumps an open decision
    // without resolving it is the amend supersede, which also advances the
    // revision. The resolution the press applies moves the goal to 6 and the
    // decision to 5.
    let advanced = false;
    let applied = false;
    const servedPair = () => (applied ? { goal: 6, decision: 5 } : advanced ? { goal: 5, decision: 4 } : { goal: 4, decision: 3 });
    const commands: { kind: string; expected_version: number; expected_revision: number; payload: Record<string, unknown> }[] = [];
    const { fetcher } = portalFetcher((path: string, init?: RequestInit) => {
      const url = new URL(path, "http://localhost");
      if (url.pathname === `/portal/api/goals/${goalId}`) {
        const pair = servedPair();
        return jsonResponse(readGoalProjection({
          id: goalId,
          projectId,
          projectName: "Project One",
          title: "Plan approval advance goal",
          state: "draft",
          version: pair.goal,
          currentRevision: 1,
          allowedActions: ["amend", "cancel"],
          decisions: [applied
            ? { ...decision, state: "resolved", "open?": false, version: pair.decision, resolution: { option_id: "accept" } }
            : { ...decision, version: pair.decision }],
        }));
      }
      if (url.pathname === `/portal/api/goals/${goalId}/commands`) {
        const rawBody = typeof init?.body === "string" ? init.body : "{}";
        const envelope = JSON.parse(rawBody) as { kind: string; expected_version: number; expected_revision: number; payload: Record<string, unknown> };
        commands.push(envelope);
        const pair = servedPair();
        // The server validates the envelope pair first (ensure_preconditions!),
        // then the decision's own version (invalid_decision).
        if (envelope.expected_version !== pair.goal || envelope.expected_revision !== 1) {
          return jsonResponse({ error: { code: "stale", message: "resource changed since it was loaded", current_version: pair.goal, current_revision: 1, allowed_actions: ["amend", "cancel"] } }, 409);
        }
        if (envelope.kind === "resolve_decision") {
          if (envelope.payload.expected_decision_version !== pair.decision || envelope.payload.decision_id !== decision.id || envelope.payload.option_id !== "accept") {
            return jsonResponse({ error: { code: "invalid_decision", message: "decision is no longer valid for this Goal" } }, 409);
          }
          applied = true;
          return jsonResponse({ goal_id: goalId, mutation_id: "m", event: { id: "m", sequence: 2, kind: "resolve_decision", revision: 1 } });
        }
        return jsonResponse({ goal_id: goalId, mutation_id: "m", event: { id: "m", sequence: 3, kind: "accept_plan", revision: 1 } });
      }
      if (url.pathname === `/portal/api/goals/${goalId}/events`) return jsonResponse({ entries: [], next_after: null });
    });

    const user = userEvent.setup();
    renderRoute(`/portal/goals/${goalId}`, fetcher);
    await screen.findByRole("heading", { level: 1, name: "Plan approval advance goal" });
    // The settled page still shows version 4 and decision version 3.
    expect(screen.getByText(/· revision 1 · version 4$/)).toBeVisible();
    expect(screen.getByText("Revision 1 · version 3")).toBeVisible();
    // A JSON document contains braces that user-event's keyboard parser reads as
    // key descriptors; set the controlled textarea value directly instead.
    fireEvent.change(screen.getByLabelText("Plan proposal"), { target: { value: JSON.stringify({ schema_version: "symmetry.plan.v1", items: [] }) } });
    // The concurrent operator command advances the goal and the decision before the press.
    advanced = true;
    await user.click(screen.getByRole("button", { name: "Resolve the decision with accept" }));
    await waitFor(() => { expect(commands.map((command) => command.kind)).toEqual(["resolve_decision", "accept_plan"]); });
    const [resolve, admit] = commands;
    if (!resolve || !admit) throw new Error("the approval commands were not sent");
    // The resolution carries the versions the server serves now — the envelope
    // pair and the presented decision's own version. Pre-fix it carries the
    // settled 4/3 pair and the server answers the 409 stale conflict instead.
    expect(resolve.expected_version).toBe(5);
    expect(resolve.expected_revision).toBe(1);
    expect(resolve.payload.decision_id).toBe(decision.id);
    expect(resolve.payload.expected_decision_version).toBe(4);
    expect(resolve.payload.option_id).toBe("accept");
    // The admission follows the applied resolution's own read (the goal moved
    // to 6), exactly as the two-step contract states.
    expect(admit.kind).toBe("accept_plan");
    expect(admit.expected_version).toBe(6);
    // The acceptance notice waits for the served goal to settle (bounded), so
    // this wait allows for that bound.
    expect(await screen.findByText("The server admitted the plan.", {}, { timeout: 5_000 })).toBeVisible();
    expect(screen.queryByRole("heading", { name: "The goal changed since this page loaded" })).not.toBeInTheDocument();
  }, 15_000);

  it("replays the plan approval's decision resolution byte for byte after a lost acknowledgement", async () => {
    const decision = {
      id: "dddddddd-dddd-4ddd-8ddd-dddddddddddd",
      kind: "plan",
      state: "open",
      "open?": true,
      version: 3,
      goal_revision: 1,
      question: "Accept the proposed plan?",
      options: [{ id: "accept", label: "Accept", consequence: "Apply the approved action." }],
      resolution: null,
    };
    let advanced = false;
    let applied = false;
    const resolveBodies: string[] = [];
    let recorded: string | null = null;
    const { fetcher } = portalFetcher((path: string, init?: RequestInit) => {
      const url = new URL(path, "http://localhost");
      if (url.pathname === `/portal/api/goals/${goalId}`) {
        const pair = applied ? { goal: 6, decision: 5 } : advanced ? { goal: 5, decision: 4 } : { goal: 4, decision: 3 };
        return jsonResponse(readGoalProjection({
          id: goalId,
          projectId,
          projectName: "Project One",
          title: "Replay plan approval goal",
          state: "draft",
          version: pair.goal,
          currentRevision: 1,
          allowedActions: ["amend", "cancel"],
          decisions: [applied
            ? { ...decision, state: "resolved", "open?": false, version: pair.decision, resolution: { option_id: "accept" } }
            : { ...decision, version: pair.decision }],
        }));
      }
      if (url.pathname === `/portal/api/goals/${goalId}/commands`) {
        const rawBody = typeof init?.body === "string" ? init.body : "{}";
        const envelope = JSON.parse(rawBody) as { kind: string; expected_version: number; payload: Record<string, unknown> };
        if (envelope.kind === "resolve_decision") {
          resolveBodies.push(rawBody);
          if (recorded === null) {
            // The first attempt reaches the server, applies, and its
            // acknowledgement is lost at the transport layer. The applied
            // resolution moved the goal to 6 and resolved the decision (5).
            recorded = rawBody;
            applied = true;
            throw new TypeError("network down");
          }
          if (rawBody !== recorded) {
            // A body rebuilt from a fresh read is not the recorded request: the
            // server refuses the resolved decision instead of replaying.
            return jsonResponse({ error: { code: "invalid_decision", message: "decision is no longer valid for this Goal" } }, 409);
          }
          return jsonResponse({ goal_id: goalId, mutation_id: "m", event: { id: "m", sequence: 2, kind: "resolve_decision", revision: 1 } });
        }
        return jsonResponse({ goal_id: goalId, mutation_id: "m", event: { id: "m", sequence: 3, kind: "accept_plan", revision: 1 } });
      }
      if (url.pathname === `/portal/api/goals/${goalId}/events`) return jsonResponse({ entries: [], next_after: null });
    });

    const user = userEvent.setup();
    renderRoute(`/portal/goals/${goalId}`, fetcher);
    await screen.findByRole("heading", { level: 1, name: "Replay plan approval goal" });
    // A JSON document contains braces that user-event's keyboard parser reads as
    // key descriptors; set the controlled textarea value directly instead.
    fireEvent.change(screen.getByLabelText("Plan proposal"), { target: { value: JSON.stringify({ schema_version: "symmetry.plan.v1", items: [] }) } });
    advanced = true;
    await user.click(screen.getByRole("button", { name: "Resolve the decision with accept" }));
    expect(await screen.findByText(/Cannot reach the server/)).toBeVisible();
    expect(resolveBodies).toHaveLength(1);
    // The first attempt read at submit: it carries the served 5/4 pair.
    const first = JSON.parse(resolveBodies[0] ?? "{}") as { expected_version: number; payload: Record<string, unknown> };
    expect(first.expected_version).toBe(5);
    expect(first.payload.expected_decision_version).toBe(4);
    // Re-pressing the same intent resends the recorded bytes, so the server
    // replays the applied resolution instead of refusing the resolved decision;
    // a body rebuilt from a fresh read (6/5, resolved) is not the recorded
    // request and would be refused.
    await user.click(screen.getByRole("button", { name: "Resolve the decision with accept" }));
    await waitFor(() => { expect(resolveBodies).toHaveLength(2); });
    expect(resolveBodies[1]).toBe(resolveBodies[0]);
    // The admission follows the applied resolution's read and is acknowledged
    // with the notice, which waits for the served goal to settle (bounded).
    expect(await screen.findByText("The server admitted the plan.", {}, { timeout: 5_000 })).toBeVisible();
  }, 15_000);

  it("replays the plan approval's admission byte for byte when its acknowledgement is lost", async () => {
    const decision = {
      id: "dddddddd-dddd-4ddd-8ddd-dddddddddddd",
      kind: "plan",
      state: "open",
      "open?": true,
      version: 3,
      goal_revision: 1,
      question: "Accept the proposed plan?",
      options: [{ id: "accept", label: "Accept", consequence: "Apply the approved action." }],
      resolution: null,
    };
    // The press's two commands are acknowledged differently: the resolution is
    // answered, and the admission is applied by the server while its
    // acknowledgement is lost in transit. The re-press must resend the recorded
    // admission too — a rebuilt one carries the post-admission versions under a
    // fresh identity, which the server refuses for a command that already
    // applied instead of replaying it.
    let resolutionApplied = false;
    let admissionApplied = false;
    const resolveBodies: string[] = [];
    const admitBodies: string[] = [];
    let recordedResolve: string | null = null;
    let recordedAdmit: string | null = null;
    const { fetcher } = portalFetcher((path: string, init?: RequestInit) => {
      const url = new URL(path, "http://localhost");
      if (url.pathname === `/portal/api/goals/${goalId}`) {
        // The resolution moves the goal to 6 (revision unchanged) and bumps the
        // decision's lock version to 4; the admission moves the goal to 7 with
        // the revision unchanged — only the amendment branch advances
        // `current_revision` (`goals.ex:6337`).
        const pair = admissionApplied ? { goal: 7, revision: 1 } : resolutionApplied ? { goal: 6, revision: 1 } : { goal: 4, revision: 1 };
        return jsonResponse(readGoalProjection({
          id: goalId,
          projectId,
          projectName: "Project One",
          title: "Frozen plan admission goal",
          state: "draft",
          version: pair.goal,
          currentRevision: pair.revision,
          allowedActions: ["amend", "cancel"],
          decisions: [resolutionApplied
            ? { ...decision, state: "resolved", "open?": false, version: 4, resolution: { option_id: "accept" } }
            : { ...decision, version: 3 }],
        }));
      }
      if (url.pathname === `/portal/api/goals/${goalId}/commands`) {
        const rawBody = typeof init?.body === "string" ? init.body : "{}";
        const envelope = JSON.parse(rawBody) as { kind: string };
        if (envelope.kind === "resolve_decision") {
          resolveBodies.push(rawBody);
          if (rawBody !== recordedResolve) {
            if (recordedResolve !== null) {
              // The recorded bytes replay; a resolution rebuilt from a fresh read
              // presents the resolved decision (5) and is refused instead.
              return jsonResponse({ error: { code: "invalid_decision", message: "decision is no longer valid for this Goal" } }, 409);
            }
            recordedResolve = rawBody;
            resolutionApplied = true;
          }
          return jsonResponse({ goal_id: goalId, mutation_id: "m", event: { id: "m", sequence: 2, kind: "resolve_decision", revision: 1 } });
        }
        admitBodies.push(rawBody);
        if (rawBody !== recordedAdmit) {
          if (recordedAdmit !== null) {
            // The admission already applied: the rebuilt identity has no recorded
            // event, so it is not replayed, and the plan validation refuses the
            // already-admitted items with the protocol's own invalid_plan
            // (`protocol.ex:101`, 422).
            return jsonResponse({ error: { code: "invalid_plan", message: "plan does not satisfy Goal contract" } }, 422);
          }
          recordedAdmit = rawBody;
          admissionApplied = true;
          throw new TypeError("network down");
        }
        return jsonResponse({ goal_id: goalId, mutation_id: "m", event: { id: "m", sequence: 3, kind: "accept_plan", revision: 1 } });
      }
      if (url.pathname === `/portal/api/goals/${goalId}/events`) return jsonResponse({ entries: [], next_after: null });
    });

    const user = userEvent.setup();
    renderRoute(`/portal/goals/${goalId}`, fetcher);
    await screen.findByRole("heading", { level: 1, name: "Frozen plan admission goal" });
    // A JSON document contains braces that user-event's keyboard parser reads as
    // key descriptors; set the controlled textarea value directly instead.
    fireEvent.change(screen.getByLabelText("Plan proposal"), { target: { value: JSON.stringify({ schema_version: "symmetry.plan.v1", items: [] }) } });
    await user.click(screen.getByRole("button", { name: "Resolve the decision with accept" }));
    expect(await screen.findByText(/Cannot reach the server/)).toBeVisible();
    expect(resolveBodies).toHaveLength(1);
    expect(admitBodies).toHaveLength(1);
    // The admission of the first press carries the resolution's own read (the
    // goal moved to 6), which is the version the server recorded for it.
    const first = JSON.parse(admitBodies[0] ?? "{}") as { expected_version: number };
    expect(first.expected_version).toBe(6);
    // Re-pressing the same intent resends the recorded admission bytes, so the
    // server replays the applied admission; a body rebuilt from the post-admission
    // read (7/1) is not the recorded request and would be refused.
    await user.click(screen.getByRole("button", { name: "Resolve the decision with accept" }));
    await waitFor(() => { expect(admitBodies).toHaveLength(2); });
    expect(resolveBodies[1]).toBe(resolveBodies[0]);
    expect(admitBodies[1]).toBe(admitBodies[0]);
    // The replay is acknowledged with the notice, which waits for the served goal
    // to settle (bounded).
    expect(await screen.findByText("The server admitted the plan.", {}, { timeout: 5_000 })).toBeVisible();
  }, 15_000);

  it("releases the plan approval attempt when the server answers its resolution", async () => {
    const decision = {
      id: "dddddddd-dddd-4ddd-8ddd-dddddddddddd",
      kind: "plan",
      state: "open",
      "open?": true,
      version: 3,
      goal_revision: 1,
      question: "Accept the proposed plan?",
      options: [{ id: "accept", label: "Accept", consequence: "Apply the approved action." }],
      resolution: null,
    };
    // The first press reads the served 5/3 pair and its resolution is answered
    // 409 stale: a concurrent operator command advances the goal (6) after that
    // read and before the POST. An answered refusal settles the attempt, so the
    // next press must rebuild from a fresh read instead of resending the refused
    // bytes forever.
    let servedVersion = 5;
    let concurrentlyAdvanced = false;
    let decisionResolved = false;
    const resolveBodies: { mutation_id: string; expected_version: number; expected_revision: number; payload: Record<string, unknown> }[] = [];
    const { fetcher } = portalFetcher((path: string, init?: RequestInit) => {
      const url = new URL(path, "http://localhost");
      if (url.pathname === `/portal/api/goals/${goalId}`) {
        return jsonResponse(readGoalProjection({
          id: goalId,
          projectId,
          projectName: "Project One",
          title: "Answered refusal plan goal",
          state: "draft",
          version: servedVersion,
          currentRevision: 1,
          allowedActions: ["amend", "cancel"],
          decisions: [decisionResolved
            ? { ...decision, state: "resolved", "open?": false, version: 4, resolution: { option_id: "accept" } }
            : decision],
        }));
      }
      if (url.pathname === `/portal/api/goals/${goalId}/commands`) {
        const rawBody = typeof init?.body === "string" ? init.body : "{}";
        const envelope = JSON.parse(rawBody) as { kind: string; mutation_id: string; expected_version: number; expected_revision: number; payload: Record<string, unknown> };
        if (envelope.kind === "resolve_decision") {
          resolveBodies.push(envelope);
          // A concurrent operator command advances the goal between the press's
          // own read and this POST, so the first attempt presents the pair the
          // server just moved past — the refusal the submit-time read cannot
          // prevent.
          if (!concurrentlyAdvanced) {
            concurrentlyAdvanced = true;
            servedVersion = 6;
          }
          if (envelope.expected_version !== servedVersion) {
            return jsonResponse({ error: { code: "stale", message: "resource changed since it was loaded", current_version: servedVersion, current_revision: 1, allowed_actions: ["amend", "cancel"] } }, 409);
          }
          decisionResolved = true;
          servedVersion += 1;
          return jsonResponse({ goal_id: goalId, mutation_id: envelope.mutation_id, event: { id: envelope.mutation_id, sequence: 2, kind: "resolve_decision", revision: 1 } });
        }
        if (envelope.expected_version !== servedVersion) {
          return jsonResponse({ error: { code: "stale", message: "resource changed since it was loaded", current_version: servedVersion, current_revision: 1, allowed_actions: ["amend", "cancel"] } }, 409);
        }
        servedVersion += 1;
        return jsonResponse({ goal_id: goalId, mutation_id: envelope.mutation_id, event: { id: envelope.mutation_id, sequence: 3, kind: "accept_plan", revision: 1 } });
      }
      if (url.pathname === `/portal/api/goals/${goalId}/events`) return jsonResponse({ entries: [], next_after: null });
    });

    const user = userEvent.setup();
    renderRoute(`/portal/goals/${goalId}`, fetcher);
    await screen.findByRole("heading", { level: 1, name: "Answered refusal plan goal" });
    // A JSON document contains braces that user-event's keyboard parser reads as
    // key descriptors; set the controlled textarea value directly instead.
    fireEvent.change(screen.getByLabelText("Plan proposal"), { target: { value: JSON.stringify({ schema_version: "symmetry.plan.v1", items: [] }) } });
    await user.click(screen.getByRole("button", { name: "Resolve the decision with accept" }));
    expect(await screen.findByRole("heading", { name: "The goal changed since this page loaded" })).toBeVisible();
    expect(resolveBodies).toHaveLength(1);
    expect(resolveBodies[0]?.expected_version).toBe(5);
    // The released attempt rebuilds: a fresh identity and the pair the server
    // serves now. A kept attempt resends the refused bytes (5) under the first
    // identity, and the server answers the same refusal again.
    await user.click(screen.getByRole("button", { name: "Resolve the decision with accept" }));
    await waitFor(() => { expect(resolveBodies).toHaveLength(2); });
    expect(resolveBodies[1]?.mutation_id).not.toBe(resolveBodies[0]?.mutation_id);
    expect(resolveBodies[1]?.expected_version).toBe(6);
    expect(resolveBodies[1]?.expected_revision).toBe(1);
    expect(resolveBodies[1]?.payload.expected_decision_version).toBe(3);
    // The acceptance notice waits for the served goal to settle (bounded), so
    // this wait allows for that bound.
    expect(await screen.findByText("The server admitted the plan.", {}, { timeout: 5_000 })).toBeVisible();
  }, 15_000);

  it("does not replay a frozen approval for a different proposal", async () => {
    const decision = {
      id: "dddddddd-dddd-4ddd-8ddd-dddddddddddd",
      kind: "plan",
      state: "open",
      "open?": true,
      version: 3,
      goal_revision: 1,
      question: "Accept the proposed plan?",
      options: [{ id: "accept", label: "Accept", consequence: "Apply the approved action." }],
      resolution: null,
    };
    // The first press's resolution is answered by nothing: its response is lost
    // at the transport layer before the server recorded it, which keeps the
    // attempt frozen. Editing the pasted proposal is a different logical intent
    // (a different freeze key), so the next press must mint its own identity and
    // admit the document the operator confirmed — not the frozen attempt's bytes.
    let resolveAttempts = 0;
    let decisionResolved = false;
    let servedVersion = 5;
    const resolveBodies: { mutation_id: string }[] = [];
    const admitBodies: { mutation_id: string; payload: Record<string, unknown> }[] = [];
    const { fetcher } = portalFetcher((path: string, init?: RequestInit) => {
      const url = new URL(path, "http://localhost");
      if (url.pathname === `/portal/api/goals/${goalId}`) {
        return jsonResponse(readGoalProjection({
          id: goalId,
          projectId,
          projectName: "Project One",
          title: "Changed proposal plan goal",
          state: "draft",
          version: servedVersion,
          currentRevision: 1,
          allowedActions: ["amend", "cancel"],
          decisions: [decisionResolved
            ? { ...decision, state: "resolved", "open?": false, version: 4, resolution: { option_id: "accept" } }
            : decision],
        }));
      }
      if (url.pathname === `/portal/api/goals/${goalId}/commands`) {
        const rawBody = typeof init?.body === "string" ? init.body : "{}";
        const envelope = JSON.parse(rawBody) as { kind: string; mutation_id: string; payload: Record<string, unknown> };
        if (envelope.kind === "resolve_decision") {
          resolveBodies.push(envelope);
          // The attempted resolution never applied: the response is lost before
          // the outcome is known, and the fixture does not record the request.
          if (resolveAttempts++ === 0) throw new TypeError("network down");
          decisionResolved = true;
          servedVersion += 1;
          return jsonResponse({ goal_id: goalId, mutation_id: envelope.mutation_id, event: { id: envelope.mutation_id, sequence: 2, kind: "resolve_decision", revision: 1 } });
        }
        admitBodies.push(envelope);
        servedVersion += 1;
        return jsonResponse({ goal_id: goalId, mutation_id: envelope.mutation_id, event: { id: envelope.mutation_id, sequence: 3, kind: "accept_plan", revision: 1 } });
      }
      if (url.pathname === `/portal/api/goals/${goalId}/events`) return jsonResponse({ entries: [], next_after: null });
    });

    const user = userEvent.setup();
    renderRoute(`/portal/goals/${goalId}`, fetcher);
    await screen.findByRole("heading", { level: 1, name: "Changed proposal plan goal" });
    // A JSON document contains braces that user-event's keyboard parser reads as
    // key descriptors; set the controlled textarea value directly instead.
    fireEvent.change(screen.getByLabelText("Plan proposal"), { target: { value: JSON.stringify({ schema_version: "symmetry.plan.v1", items: [] }) } });
    await user.click(screen.getByRole("button", { name: "Resolve the decision with accept" }));
    expect(await screen.findByText(/Cannot reach the server/)).toBeVisible();
    expect(resolveBodies).toHaveLength(1);
    // The operator edits the pasted proposal, which is the changed intent.
    fireEvent.change(screen.getByLabelText("Plan proposal"), { target: { value: JSON.stringify({ schema_version: "symmetry.plan.v1", items: [{ title: "Admit the corrected plan" }] }) } });
    await user.click(screen.getByRole("button", { name: "Resolve the decision with accept" }));
    await waitFor(() => { expect(resolveBodies).toHaveLength(2); });
    // The changed intent mints its own identity; replaying the frozen attempt
    // would resend the first attempt's bytes under the first identity.
    expect(resolveBodies[1]?.mutation_id).not.toBe(resolveBodies[0]?.mutation_id);
    await waitFor(() => { expect(admitBodies).toHaveLength(1); });
    // The admission carries the document the operator confirmed, not the frozen
    // attempt's. The vector is the server's own digest for the defaulted
    // document, computed in this worktree with the portable Elixir toolchain and
    // cross-checked with Python hashlib and Node:
    //   mix run --no-start -e 'IO.puts("sha256:" <> Base.encode16(SymmetryControl.RequestHash.canonical(%{"schema_version" => "symmetry.plan.v1", "items" => [%{"title" => "Admit the corrected plan", "integration" => false, "change_target" => nil}]}), case: :lower))'
    // i.e. sha256 over
    // {"items":[{"change_target":null,"integration":false,"title":"Admit the corrected plan"}],"schema_version":"symmetry.plan.v1"}.
    expect(admitBodies[0]?.payload.proposal_hash).toBe("sha256:749eb3c4bb07d2d9ef09496c0472b11a3e1384b527a5e63ac0b1dfa5e9712afd");
    expect(await screen.findByText("The server admitted the plan.", {}, { timeout: 5_000 })).toBeVisible();
  }, 15_000);

  it("hashes the plan proposal the server validates, with the per-item defaults applied", async () => {
    const decision = { id: "dddddddd-dddd-4ddd-8ddd-dddddddddddd", kind: "plan", state: "open", "open?": true, version: 2, goal_revision: 1, question: "Accept the proposed plan?", options: [{ id: "accept", label: "Accept", consequence: "Apply the approved action." }], resolution: null };
    const commands: Record<string, unknown>[] = [];
    const { fetcher } = portalFetcher((path: string, init?: RequestInit) => {
      const url = new URL(path, "http://localhost");
      if (url.pathname === `/portal/api/goals/${goalId}`) {
        return jsonResponse(readGoalProjection({ id: goalId, projectId, projectName: "Project One", title: "Omitted-default plan goal", state: "draft", allowedActions: ["amend", "cancel"], decisions: [decision] }));
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
    await screen.findByRole("heading", { level: 1, name: "Omitted-default plan goal" });
    // The repo's own valid plan-proposal fixture omits the per-item defaults
    // (`integration`) the server injects before it hashes and validates.
    // A JSON document contains braces that user-event's keyboard parser reads as
    // key descriptors; set the controlled textarea value directly instead.
    fireEvent.change(screen.getByLabelText("Plan proposal"), { target: { value: JSON.stringify(omittedProposal) } });
    await user.click(screen.getByRole("button", { name: "Resolve the decision with accept" }));
    await waitFor(() => { expect(commands.length).toBe(2); });
    const admit = commands[1] as { kind?: string; payload?: Record<string, unknown> } | undefined;
    if (!admit) throw new Error("the plan admission was not sent");
    expect(admit.kind).toBe("accept_plan");
    // The emitted proposal is the defaulted document the digest covers, and the
    // digest is the server's own for that document: computed in this worktree
    // with the portable Elixir toolchain over the same per-item defaults
    // (`Map.put_new(item, "integration", false)`), the omitted-default fixture
    // hashes to sha256:aaae9efd… while its raw bytes hash to sha256:9b04f528….
    const expectedProposal = { ...omittedProposal, items: omittedProposal.items.map((item) => ({ ...item, integration: false })) };
    expect(admit.payload?.proposal).toEqual(expectedProposal);
    expect(admit.payload?.proposal_hash).toBe("sha256:aaae9efde4b51964698a64eac92a5f485213e1b072801d139b0b1a2c597f2519");
  });

  it("escapes C0 controls in the canonical digest exactly as the server's Jason does", async () => {
    const decision = { id: "dddddddd-dddd-4ddd-8ddd-dddddddddddd", kind: "plan", state: "open", "open?": true, version: 2, goal_revision: 1, question: "Accept the proposed plan?", options: [{ id: "accept", label: "Accept", consequence: "Apply the approved action." }], resolution: null };
    const commands: Record<string, unknown>[] = [];
    const { fetcher } = portalFetcher((path: string, init?: RequestInit) => {
      const url = new URL(path, "http://localhost");
      if (url.pathname === `/portal/api/goals/${goalId}`) {
        return jsonResponse(readGoalProjection({ id: goalId, projectId, projectName: "Project One", title: "Control-character plan goal", state: "draft", allowedActions: ["amend", "cancel"], decisions: [decision] }));
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
    await screen.findByRole("heading", { level: 1, name: "Control-character plan goal" });
    // A LongText description admits U+000B and U+001B (the contract bans only
    // U+0000), and the control plane's Jason escapes them with uppercase hex
    // where `JSON.stringify` emits lowercase hex — two texts that hash as
    // different bytes.
    const document = { schema_version: "symmetry.plan.v1", items: [{ description: "before\u000Bafter\u001Bend" }] };
    // A JSON document contains braces that user-event's keyboard parser reads as
    // key descriptors; set the controlled textarea value directly instead.
    fireEvent.change(screen.getByLabelText("Plan proposal"), { target: { value: JSON.stringify(document) } });
    await user.click(screen.getByRole("button", { name: "Resolve the decision with accept" }));
    await waitFor(() => { expect(commands.length).toBe(2); });
    const admit = commands[1] as { kind?: string; payload?: Record<string, unknown> } | undefined;
    if (!admit) throw new Error("the plan admission was not sent");
    expect(admit.kind).toBe("accept_plan");
    // The server's own digest for the defaulted document, computed in this
    // worktree with the portable Elixir toolchain and cross-checked with Python
    // hashlib:
    //   mix run --no-start -e 'IO.puts("sha256:" <> Base.encode16(SymmetryControl.RequestHash.canonical(%{"schema_version" => "symmetry.plan.v1", "items" => [%{"description" => "before\x0Bafter\x1Bend", "integration" => false, "change_target" => nil}]}), case: :lower))'
    // i.e. sha256 over
    // {"items":[{"change_target":null,"description":"before\u000Bafter\u001Bend","integration":false}],"schema_version":"symmetry.plan.v1"}.
    expect(admit.payload?.proposal_hash).toBe("sha256:6a41b487e2e260e048b11ea7e3221f53aa9705fc5a9d943a353e6b8c39d202e5");
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

  it("offers no plan approval for a decision the server no longer serves as open", async () => {
    // The server's `open?` is `state == "open" and goal_revision ==
    // current_revision` (read_model.ex): a decision left open at revision 1
    // while the goal moved to revision 2 still serves `state: "open"`, so a
    // state-based gate would offer the approval control the server refuses.
    const decision = { id: "dddddddd-dddd-4ddd-8ddd-dddddddddddd", goal_revision: 1, kind: "plan", state: "open", "open?": false, version: 5, question: "Accept the proposed plan?", options: [{ id: "accept", label: "Accept", consequence: "Apply the approved action." }], resolution: null };
    const { fetcher } = portalFetcher((path: string) => {
      const url = new URL(path, "http://localhost");
      if (url.pathname === `/portal/api/goals/${goalId}`) {
        return jsonResponse(readGoalProjection({ id: goalId, projectId, projectName: "Project One", title: "Stale plan decision goal", state: "draft", currentRevision: 2, allowedActions: ["amend", "cancel"], decisions: [decision] }));
      }
      if (url.pathname === `/portal/api/goals/${goalId}/events`) return jsonResponse({ entries: [], next_after: null });
    });

    renderRoute(`/portal/goals/${goalId}`, fetcher);
    await screen.findByRole("heading", { level: 1, name: "Stale plan decision goal" });
    expect(screen.queryByRole("button", { name: "Resolve the decision with accept" })).not.toBeInTheDocument();
    // It is not resolved either: the served revision no longer carries it open.
    expect(screen.getByText("This decision is no longer open at the served revision.")).toBeVisible();
    expect(screen.queryByText(/^Resolved:/)).not.toBeInTheDocument();
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
          // stringifying the object. A plain object (not an array) is what a
          // `[object Object]` regression would render.
          workItems: [{ id: "44444444-4444-4444-8444-444444444444", number: 1, title: "Open-member item", status: "in_progress", "required?": true, integration: false, "accepted?": false, accepted_outcome: null, execution: { state: { phase: "running" } }, blockers: [] }],
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
