import { screen, waitFor } from "@testing-library/react";
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

const rendered: Array<{ unmount: () => void }> = [];
afterEach(() => { for (const entry of rendered) entry.unmount(); rendered.length = 0; localStorage.clear(); });

function render(path: string, fetcher: Parameters<typeof renderRoute>[1]) {
  const result = renderRoute(path, fetcher);
  rendered.push(result);
  return result;
}

function workspaceResponse(selected: string) {
  return { selected_project_id: selected, projects: projects.map((project) => ({ ...project, work_items: [] })) };
}

describe("attention entry point", () => {
  it("lists Goal attention and the project blocked work together", async () => {
    const { fetcher } = recordingFetcher((path) => {
      const url = new URL(path, "http://localhost");
      if (url.pathname === "/portal/api/attention") {
        return jsonResponse(readAttention({
          selectedProjectId: projectId,
          goals: [{ goalId, projectId, title: "Deliver the attention entry point", state: "active", blockerReasons: ["waiting_dependency"], attentionReasons: ["awaiting_validation"], activeTasks: 1, nonterminalTasks: 1 }],
          blocked: [{ projectId, projectName: "Project One", workItemId: "44444444-4444-4444-8444-444444444444", key: "ONE-7", title: "Operator review required", blocker: "Needs architecture decision" }],
        }));
      }
      if (url.pathname === "/portal/api/session") return jsonResponse({ csrf_token: "session-token" });
      return jsonResponse(workspaceResponse(projectId));
    });

    render("/portal/attention", fetcher);
    expect(await screen.findByRole("link", { name: "Deliver the attention entry point" })).toHaveAttribute("href", `/portal/goals/${goalId}`);
    expect(screen.getByText("waiting_dependency")).toBeVisible();
    expect(screen.getByText("Settlement attention: awaiting_validation")).toBeVisible();
    expect(await screen.findByText("Operator review required")).toBeVisible();
    expect(screen.getByText("Needs architecture decision")).toBeVisible();
    expect(screen.getByText("Showing Project One.")).toBeVisible();
  });

  it("keeps the project blocked work and states the Goal feed is unavailable when admission is disabled", async () => {
    const { fetcher } = recordingFetcher((path) => {
      const url = new URL(path, "http://localhost");
      if (url.pathname === "/portal/api/attention") {
        return jsonResponse(readAttention({
          selectedProjectId: projectId,
          goals: [],
          blocked: [{ projectId, projectName: "Project One", workItemId: "44444444-4444-4444-8444-444444444444", title: "Operator review required", blocker: "Operator review required" }],
          goalUnavailable: { code: "goal_admission_disabled", message: "goal admission is disabled" },
        }));
      }
      if (url.pathname === "/portal/api/session") return jsonResponse({ csrf_token: "session-token" });
      return jsonResponse(workspaceResponse(projectId));
    });

    render("/portal/attention", fetcher);
    expect(await screen.findByRole("heading", { name: "Goal attention is unavailable on this server" })).toBeVisible();
    expect(screen.getByText(/goal admission is disabled/)).toBeVisible();
    expect(screen.getByText("goal_admission_disabled")).toBeVisible();
    // The pre-existing project blockers stay rendered and actionable.
    expect(screen.getByRole("heading", { name: "Operator review required" })).toBeVisible();
  });

  it("loads the Goal feed's older page with the served cursor and never repeats an entry", async () => {
    const olderGoalId = "55555555-5555-4555-8555-555555555555";
    const { fetcher, calls } = recordingFetcher((path) => {
      const url = new URL(path, "http://localhost");
      if (url.pathname === "/portal/api/attention") {
        const cursor = url.searchParams.get("cursor");
        return jsonResponse(cursor === "cursor-one"
          ? readAttention({ selectedProjectId: projectId, goals: [{ goalId: olderGoalId, projectId, title: "Older goal attention" }], nextCursor: null })
          : readAttention({ selectedProjectId: projectId, goals: [{ goalId, projectId, title: "Newest goal attention" }], nextCursor: "cursor-one" }));
      }
      if (url.pathname === "/portal/api/session") return jsonResponse({ csrf_token: "session-token" });
      return jsonResponse(workspaceResponse(projectId));
    });

    const user = userEvent.setup();
    render("/portal/attention", fetcher);
    expect(await screen.findByRole("link", { name: "Newest goal attention" })).toBeVisible();
    await user.click(screen.getByRole("button", { name: "Load more Goal attention" }));
    expect(await screen.findByRole("link", { name: "Older goal attention" })).toBeVisible();
    expect(screen.getByRole("link", { name: "Newest goal attention" })).toBeVisible();
    expect(calls.some((call) => call.path.includes("cursor=cursor-one"))).toBe(true);
  });

  it("offers goal creation only against a selected project and keeps the server's refusal", async () => {
    const { fetcher, calls } = recordingFetcher((path, init) => {
      const url = new URL(path, "http://localhost");
      if (url.pathname === "/portal/api/attention") {
        return jsonResponse(readAttention({ selectedProjectId: projectId, blocked: [] }));
      }
      if (url.pathname === "/portal/api/session") return jsonResponse({ csrf_token: "session-token" });
      if (url.pathname === `/portal/api/projects/${projectId}/goals`) {
        expect(init?.headers).toMatchObject({ "x-csrf-token": "session-token" });
        const rawBody = typeof init?.body === "string" ? init.body : "{}";
        const body = JSON.parse(rawBody) as { schema_version: string; initial_revision: { objective: string } };
        expect(body.schema_version).toBe("symmetry.goal_create.v1");
        expect(body.initial_revision.objective).toBe("Expose the attention entry point");
        return jsonResponse({ error: { code: "invalid_contract", message: "goal contract is invalid" } }, 422);
      }
      return jsonResponse(workspaceResponse(projectId));
    });

    const user = userEvent.setup();
    render("/portal/attention", fetcher);
    await screen.findByRole("heading", { name: "Attention" });
    await user.click(await screen.findByRole("button", { name: /New goal/ }));
    await user.type(screen.getByLabelText("Title"), "Goal from the attention page");
    await user.type(screen.getByLabelText("Objective"), "Expose the attention entry point");
    await user.click(screen.getByRole("button", { name: "Create goal" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("goal contract is invalid (invalid_contract)");
    expect(calls.some((call) => call.path.includes("/portal/api/projects/") && call.path.endsWith("/goals"))).toBe(true);
  });
});

describe("goal detail surface", () => {
  it("renders the objective, accepted progress, budget truth and the read-only dependency projection", async () => {
    const acceptedSubject = { resource_id: "88888888-8888-4888-8888-888888888888", commit: "a".repeat(40), tree_digest: `sha256:${"b".repeat(64)}` };
    const { fetcher } = recordingFetcher((path) => {
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
            { id: "44444444-4444-4444-8444-444444444444", number: 1, title: "Merge the attention feed", status: "in_progress", "required?": true, "integration?": false, "accepted?": true, accepted_outcome: { id: "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee", disposition: "accepted", subject_hash: `sha256:${"c".repeat(64)}`, evidence_ids: ["99999999-9999-4999-8999-999999999999"], candidate_subject: acceptedSubject }, execution: { state: "completed" }, blockers: [] },
            { id: "55555555-5555-4555-8555-555555555555", number: 2, title: "Render the Goal detail surface", status: "queued", "required?": true, "integration?": true, "accepted?": false, accepted_outcome: null, execution: null, blockers: ["waiting_dependency"] },
          ],
          acceptedOutcomes: [{ id: "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee", work_item_id: "44444444-4444-4444-8444-444444444444", disposition: "accepted", subject_hash: `sha256:${"c".repeat(64)}`, evidence_ids: ["99999999-9999-4999-8999-999999999999"], candidate_subject: acceptedSubject, inserted_at: "2026-11-16T09:50:00Z" }],
          blockers: [{ reason: "waiting_dependency", count: 1, ids: ["55555555-5555-4555-8555-555555555555"], details: [] }],
          settlementAttention: [{ settlement: "awaiting_validation", task_id: "66666666-6666-4666-8666-666666666666", goal_revision: 2, proposed_next_action: "Run the bound validation profile", proposed_next_action_status: "proposal_only" }],
          graph: {
            nodes: [
              { id: "44444444-4444-4444-8444-444444444444", number: 1, title: "Merge the attention feed", "required?": true, board_status: "in_progress", "accepted?": true, blockers: [] },
              { id: "55555555-5555-4555-8555-555555555555", number: 2, title: "Render the Goal detail surface", "required?": true, board_status: "queued", "accepted?": false, blockers: ["waiting_dependency"] },
            ],
            edges: [{ id: "aaaaaaa1-1111-4111-8111-aaaaaaaaaaa1", work_item_id: "55555555-5555-4555-8555-555555555555", depends_on_id: "44444444-4444-4444-8444-444444444444", from: "55555555-5555-4555-8555-555555555555", to: "44444444-4444-4444-8444-444444444444", satisfied: false, blocker: "waiting_dependency", revision: 1 }],
          },
          accounting: { held_microusd: "250000", unknown_reservation_count: 1, settled_reservation_count: 1, released_reservation_count: 0, usage_records: 2, usage_leaf_count: 2, known_usage_cost_microusd: "120000", usage_cost_microusd: null, usage_cost_unknown: true, unknown_usage_count: 1 },
          externalWaits: [{ id: "12121212-1212-4212-8212-121212121212", state: "waiting", reason: null, work_item_id: "55555555-5555-4555-8555-555555555555", resource_id: "88888888-8888-4888-8888-888888888888", external_ref: "wxxb789/symmetry", next_check_at: "2026-11-16T11:00:00Z", check_seq: 3, inserted_at: "2026-11-16T09:55:00Z", updated_at: "2026-11-16T10:00:00Z" }],
        }));
      }
      if (url.pathname === `/portal/api/goals/${goalId}/events`) {
        const after = Number(url.searchParams.get("after") ?? "0");
        return jsonResponse(after === 0
          ? { entries: [{ id: "event-1", sequence: 1, kind: "goal_created", actor_ref: "portal_operator", revision: 1, payload: {}, response: {}, inserted_at: "2026-11-16T09:00:00Z" }], next_after: 1 }
          : { entries: [{ id: "event-2", sequence: 2, kind: "amend", actor_ref: "portal_operator", revision: 2, payload: {}, response: {}, inserted_at: "2026-11-16T09:30:00Z" }], next_after: null });
      }
      if (url.pathname === "/portal/api/session") return jsonResponse({ csrf_token: "session-token" });
      return jsonResponse(workspaceResponse(projectId));
    });

    render(`/portal/goals/${goalId}`, fetcher);
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
    // Read-only projection: edges render, no dependency editing affordance exists.
    expect(screen.getByRole("list", { name: "Dependency edges" })).toBeVisible();
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
    const { fetcher, calls } = recordingFetcher((path, init) => {
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
      if (url.pathname === "/portal/api/session") return jsonResponse({ csrf_token: "session-token" });
      return jsonResponse(workspaceResponse(projectId));
    });

    const user = userEvent.setup();
    render(`/portal/goals/${goalId}`, fetcher);
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

  it("carries a fresh mutation identity per logical action attempt and reports the server receipt", async () => {
    const identities: string[] = [];
    const { fetcher } = recordingFetcher((path, init) => {
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
      if (url.pathname === "/portal/api/session") return jsonResponse({ csrf_token: "session-token" });
      return jsonResponse(workspaceResponse(projectId));
    });

    const user = userEvent.setup();
    render(`/portal/goals/${goalId}`, fetcher);
    await screen.findByRole("heading", { level: 1, name: "Replay goal" });
    await user.click(screen.getByRole("button", { name: "Pause" }));
    await user.type(screen.getByLabelText("Reason"), "Operator pause");
    await user.click(screen.getByRole("button", { name: "Send Pause" }));
    expect(await screen.findByText("Pause accepted by the server.")).toBeVisible();
    await user.click(screen.getByRole("button", { name: "Pause" }));
    await user.type(screen.getByLabelText("Reason"), "Operator pause again");
    await user.click(screen.getByRole("button", { name: "Send Pause" }));
    await waitFor(() => expect(identities.length).toBe(2));
    // Each logical action attempt carries a fresh identity; the server owns replay.
    expect(identities[0]).not.toBe(identities[1]);
  });

  it("offers only the served actions and shows the achieve command with the accepted subject when the server allows it", async () => {
    const acceptedSubject = { resource_id: "88888888-8888-4888-8888-888888888888", commit: "a".repeat(40), tree_digest: `sha256:${"b".repeat(64)}` };
    let achievePayload: Record<string, unknown> | null = null;
    const { fetcher } = recordingFetcher((path, init) => {
      const url = new URL(path, "http://localhost");
      if (url.pathname === `/portal/api/goals/${goalId}`) {
        return jsonResponse(readGoalProjection({
          id: goalId,
          projectId,
          projectName: "Project One",
          title: "Ready goal",
          state: "active",
          allowedActions: ["achieve", "pause", "amend", "cancel"],
          workItems: [{ id: "44444444-4444-4444-8444-444444444444", number: 1, title: "Implementation", status: "done", "required?": true, "integration?": true, "accepted?": true, accepted_outcome: { id: "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee", disposition: "accepted", evidence_ids: ["99999999-9999-4999-8999-999999999999"], candidate_subject: acceptedSubject }, execution: null, blockers: [] }],
          acceptedOutcomes: [{ id: "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee", work_item_id: "44444444-4444-4444-8444-444444444444", disposition: "accepted", evidence_ids: ["99999999-9999-4999-8999-999999999999"], candidate_subject: acceptedSubject }],
        }));
      }
      if (url.pathname === `/portal/api/goals/${goalId}/commands`) {
        const rawBody = typeof init?.body === "string" ? init.body : "{}";
        const envelope = JSON.parse(rawBody) as { payload?: Record<string, unknown> };
        achievePayload = envelope.payload ?? {};
        return jsonResponse({ goal_id: goalId, mutation_id: "m", event: { id: "m", sequence: 3, kind: "achieve", revision: 1 }, response: { state: "achieved" } });
      }
      if (url.pathname === `/portal/api/goals/${goalId}/events`) return jsonResponse({ entries: [], next_after: null });
      if (url.pathname === "/portal/api/session") return jsonResponse({ csrf_token: "session-token" });
      return jsonResponse(workspaceResponse(projectId));
    });

    const user = userEvent.setup();
    render(`/portal/goals/${goalId}`, fetcher);
    await screen.findByRole("heading", { level: 1, name: "Ready goal" });
    await user.click(screen.getByRole("button", { name: "Achieve" }));
    await user.click(screen.getByRole("button", { name: "Send Achieve" }));
    await waitFor(() => expect(achievePayload).not.toBeNull());
    if (achievePayload === null) throw new Error("the achieve command was not sent");
    const payload = achievePayload;
    expect(payload["subject"]).toEqual(acceptedSubject);
    expect(payload["evidence_ids"]).toEqual(["99999999-9999-4999-8999-999999999999"]);
    expect(payload["integration_work_item_id"]).toBe("44444444-4444-4444-8444-444444444444");
    expect(payload["decision_id"]).toBeNull();
  });

  it("renders the served refusal when the server does not allow an action", async () => {
    const { fetcher } = recordingFetcher((path: string) => {
      const url = new URL(path, "http://localhost");
      if (url.pathname === `/portal/api/goals/${goalId}`) {
        return jsonResponse(readGoalProjection({ id: goalId, projectId, projectName: "Project One", title: "Draft goal", state: "draft", allowedActions: ["request_plan", "amend", "cancel"] }));
      }
      if (url.pathname === `/portal/api/goals/${goalId}/events`) return jsonResponse({ entries: [], next_after: null });
      if (url.pathname === "/portal/api/session") return jsonResponse({ csrf_token: "session-token" });
      return jsonResponse(workspaceResponse(projectId));
    });

    render(`/portal/goals/${goalId}`, fetcher);
    await screen.findByRole("heading", { level: 1, name: "Draft goal" });
    expect(screen.getByRole("button", { name: "Request a plan" })).toBeVisible();
    expect(screen.queryByRole("button", { name: "Activate" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Achieve" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Pause" })).not.toBeInTheDocument();
  });

  it("recovers a refused command transport with a retryable error and never claims success", async () => {
    const { fetcher } = recordingFetcher((path: string, init?: RequestInit) => {
      const url = new URL(path, "http://localhost");
      if (url.pathname === `/portal/api/goals/${goalId}`) {
        return jsonResponse(readGoalProjection({ id: goalId, projectId, projectName: "Project One", title: "Offline goal", state: "active", allowedActions: ["pause", "amend", "cancel"] }));
      }
      if (url.pathname === `/portal/api/goals/${goalId}/commands`) {
        void init;
        throw new TypeError("network down");
      }
      if (url.pathname === `/portal/api/goals/${goalId}/events`) return jsonResponse({ entries: [], next_after: null });
      if (url.pathname === "/portal/api/session") return jsonResponse({ csrf_token: "session-token" });
      return jsonResponse(workspaceResponse(projectId));
    });

    const user = userEvent.setup();
    render(`/portal/goals/${goalId}`, fetcher);
    await screen.findByRole("heading", { level: 1, name: "Offline goal" });
    await user.click(screen.getByRole("button", { name: "Pause" }));
    await user.type(screen.getByLabelText("Reason"), "Operator pause");
    await user.click(screen.getByRole("button", { name: "Send Pause" }));
    expect(await screen.findByText(/Cannot reach the server/)).toBeVisible();
    expect(screen.queryByText(/accepted by the server/)).not.toBeInTheDocument();
  });
});
