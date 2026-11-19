// Goal 0007h — Goal attention workspace acceptance.
//
// Real surfaces only: the released React shell against a running Phoenix
// control plane, the accepted Goal API, and a real PostgreSQL database. No
// response is substituted and no capability is faked; refusals and conflicts
// are produced by the server.
import fs from "node:fs/promises";
import { allowBrowserError, expect, test } from "./fixtures.mjs";

const operatorToken = process.env.SYMMETRY_OPERATOR_TOKEN || "development-operator-token";
// A second release instance started with Goal rollout off. Without it the
// rollout-off case is explicitly skipped, never passed on the primary instance.
const goalsOffUrl = process.env.SYMMETRY_GOALS_OFF_URL;
// The render gate exports SYMMETRY_RENDER_GATE_REQUIRED=1 and starts that
// instance: a gate run missing its URL is half-configured and fails collection
// instead of skipping green, while the local default stays the named skip.
if (process.env.SYMMETRY_RENDER_GATE_REQUIRED === "1" && !goalsOffUrl) {
  throw new Error(
    "SYMMETRY_GOALS_OFF_URL must be set: SYMMETRY_RENDER_GATE_REQUIRED=1 makes the render gate " +
      "fail collection instead of skipping the rollout-off case",
  );
}

async function login(page, path = "/portal/attention", base = "") {
  await page.goto(`${base}/portal/login?return_to=${encodeURIComponent(path)}`);
  await page.getByLabel("Operator token").fill(operatorToken);
  await page.getByRole("button", { name: "Open workspace" }).click();
  await expect(page.getByRole("button", { name: "Refresh", exact: true })).toBeVisible();
}

async function api(page, method, path, body) {
  return page.evaluate(async ({ method, path, body }) => {
    const csrf = document.querySelector('meta[name="csrf-token"]')?.content;
    const response = await fetch(path, {
      method,
      credentials: "same-origin",
      headers: { "content-type": "application/json", "x-csrf-token": csrf },
      body: body === undefined ? undefined : JSON.stringify(body)
    });
    return { status: response.status, body: await response.json().catch(() => null) };
  }, { method, path, body });
}

const acceptance = () => ({
  schema_version: "symmetry.acceptance.v1",
  description: "Operator verifies the goal workspace boundary.",
  predicates: [{ id: "operator", kind: "operator_acceptance" }]
});

const authorityPolicy = () => ({
  operator_required_for_scope_change: true,
  operator_required_for_completion: true,
  publication_allowed: false,
  allowed_actions: []
});

const executionPolicy = () => ({
  automatic_execution: false,
  max_parallel_tasks: 1,
  max_task_admissions: 4,
  max_run_attempts_per_task: 2,
  budget_limit_microusd: null,
  per_run_cost_limit_microusd: null,
  budget_mode: "soft",
  hard_cost_limit_required: false,
  allowed_runtime_ids: [],
  allowed_model_profiles: [],
  final_acceptance: "operator",
  allowed_actions: [],
  allowed_resource_ids: []
});

const contextManifest = () => ({ byte_budget: 32768, required_source_kinds: ["repository"], include_advisory_recall: false });

async function createProject(page, suffix, { blocked = false } = {}) {
  const key = `H${Date.now().toString(36).slice(-5)}${suffix}`.toUpperCase().slice(0, 8);
  const response = await api(page, "POST", "/portal/api/projects", {
    key,
    name: `Goal workspace ${key}`,
    description: "Real Phoenix fixture",
    default_agent_profile: "codex",
    default_workspace: "primary"
  });
  expect(response.status).toBe(201);
  const projectId = response.body.project.id;
  if (blocked) {
    const work = await api(page, "POST", `/portal/api/projects/${projectId}/work-items`, {
      title: `Blocked work ${key}`,
      status: "ready",
      blocked: true,
      blocker: "Operator review required"
    });
    expect(work.status).toBe(201);
  }
  return { id: projectId, key, name: `Goal workspace ${key}` };
}

async function attachRepository(page, projectId) {
  const response = await api(page, "POST", `/portal/api/projects/${projectId}/resources`, {
    kind: "repository",
    name: "Goal workspace repository",
    provider: "manual",
    external_ref: `goal-workspace-${Date.now()}`,
    status: "healthy",
    sync_status: "synced"
  });
  expect(response.status).toBe(201);
  return response.body.resource.id;
}

/** Sends one accepted Goal command and returns the decoded response. */
async function goalCommand(page, goalId, kind, payload) {
  const read = await api(page, "GET", `/portal/api/goals/${goalId}`);
  expect(read.status).toBe(200);
  const response = await api(page, "POST", `/portal/api/goals/${goalId}/commands`, {
    schema_version: "symmetry.goal_command.v1",
    mutation_id: crypto.randomUUID(),
    expected_version: read.body.version,
    expected_revision: read.body.current_revision,
    kind,
    payload
  });
  return { read, response };
}

/**
 * Creates a draft Goal through the accepted creation command and authors one plan
 * decision with the accepted `request_decision` command, so the surface has a real
 * proposal to approve. The proposal is the operator's own plan document; the
 * surface hashes the same object with the server's canonical encoding.
 */
async function createGoalWithPlan(page, project, repositoryId, title) {
  const created = await api(page, "POST", `/portal/api/projects/${project.id}/goals`, {
    schema_version: "symmetry.goal_create.v1",
    mutation_id: crypto.randomUUID(),
    title,
    initial_revision: {
      objective: `Expose ${title} through the portal goal surface.`,
      non_goals: [],
      acceptance_contract: acceptance(),
      authority_policy: authorityPolicy(),
      execution_policy: executionPolicy(),
      context_manifest: contextManifest(),
      reason: "Initial approved Goal contract"
    }
  });
  if (created.status !== 201) throw new Error(`goal creation failed: ${created.status} ${JSON.stringify(created.body)}`);
  const goalId = created.body.goal.id;

  const proposal = {
    schema_version: "symmetry.plan.v1",
    proposal_id: crypto.randomUUID(),
    goal_id: goalId,
    expected_revision: 1,
    items: [{
      key: "implement",
      title: `Implement ${title}`,
      description: "Bounded goal work",
      required: true,
      integration: true,
      repository_resource_id: repositoryId,
      acceptance: acceptance(),
      depends_on_keys: [],
      model_profile: "codex",
      change_target: null,
      baseline: {
        kind: "subject",
        subject: { resource_id: repositoryId, commit: "a".repeat(40), tree_digest: `sha256:${"b".repeat(64)}` }
      }
    }]
  };

  const decision = await goalCommand(page, goalId, "request_decision", {
    kind: "plan",
    work_item_id: null,
    subject_hash: null,
    proposal
  });
  expect(decision.response.status).toBe(200);
  const decisionId = decision.response.body.response.decision.id;

  return { goalId, proposal, decisionId, proposalJson: JSON.stringify(proposal) };
}

async function capture(page, testInfo, name, observations) {
  await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  const image = testInfo.outputPath(`${name}.png`);
  await page.screenshot({ path: image, fullPage: true });
  await testInfo.attach(name, { path: image, contentType: "image/png" });
  await fs.writeFile(testInfo.outputPath(`${name}.json`), JSON.stringify({
    url: page.url(), viewport: page.viewportSize(), observations
  }, null, 2));
}

test.describe("Goal attention workspace", () => {
  test("attention merges Goal attention with the project blockers from real reads", async ({ page }, testInfo) => {
    await login(page);
    const project = await createProject(page, "A", { blocked: true });

    const empty = await api(page, "GET", `/portal/api/attention?project_id=${project.id}`);
    expect(empty.status).toBe(200);
    // The draft-free feed is empty but the project blockers are served.
    expect(empty.body.project_blocked.entries).toHaveLength(1);
    expect(empty.body.goal_attention.unavailable).toBe(null);

    const repositoryId = await attachRepository(page, project.id);
    const { goalId } = await createGoalWithPlan(page, project, repositoryId, "Attention merge goal");

    // The served document itself carries the Goal entry beside the blockers;
    // the page below renders those two reads, not a substituted response.
    const served = await api(page, "GET", `/portal/api/attention?project_id=${project.id}`);
    expect(served.status).toBe(200);
    expect(served.body.goal_attention.unavailable).toBe(null);
    expect(served.body.goal_attention.entries.map((entry) => entry.goal_id)).toContain(goalId);

    await page.goto(`/portal/attention?projectId=${project.id}`);
    await expect(page.getByRole("heading", { level: 1, name: "Attention", exact: true })).toBeVisible();
    await expect(page.getByRole("link", { name: "Attention merge goal" })).toBeVisible();
    await expect(page.getByRole("heading", { name: `Blocked work ${project.key}`, exact: true })).toBeVisible();
    await expect(page.getByText("Operator review required")).toBeVisible();
    await capture(page, testInfo, "attention-merged", "Goal attention and the project blocked work render on the same attention entry point from real server reads");
  });

  test("plan approval, activation, pause, resume and amendment run through the delegated commands with server versions", async ({ page }, testInfo) => {
    await login(page);
    const project = await createProject(page, "B");
    const repositoryId = await attachRepository(page, project.id);
    const { goalId, proposalJson } = await createGoalWithPlan(page, project, repositoryId, "Lifecycle goal");

    await page.goto(`/portal/goals/${goalId}`);
    await expect(page.getByRole("heading", { level: 1, name: "Lifecycle goal" })).toBeVisible();
    await expect(page.getByText("plan decision")).toBeVisible();
    await expect(page.getByText("Accept the proposed plan?")).toBeVisible();

    const commands = [];
    page.on("response", async (response) => {
      if (response.url().endsWith(`/portal/api/goals/${goalId}/commands`)) {
        commands.push({ status: response.status(), kind: response.request().postDataJSON()?.kind });
      }
    });
    await page.getByLabel("Plan proposal").fill(proposalJson);
    await page.getByRole("button", { name: "Resolve the decision with accept" }).click();
    await expect(page.getByText("The server admitted the plan.")).toBeVisible();
    // Both accepted commands were sent: the decision resolution, then the plan
    // admission carrying the versions the server returned after resolving.
    expect(commands).toEqual([{ status: 200, kind: "resolve_decision" }, { status: 200, kind: "accept_plan" }]);
    await capture(page, testInfo, "goal-plan-approved", "The surface resolved the served plan decision and admitted the plan through the delegated commands");

    const admitted = await api(page, "GET", `/portal/api/goals/${goalId}`);
    expect(admitted.body.work_items).toHaveLength(1);
    expect(admitted.body.allowed_actions).toContain("activate");

    await page.getByRole("button", { name: "Activate" }).click();
    await page.getByRole("button", { name: "Send Activate" }).click();
    await expect(page.getByText("Activate accepted by the server.")).toBeVisible();
    const activated = await api(page, "GET", `/portal/api/goals/${goalId}`);
    expect(activated.body.state).toBe("active");
    await expect(page.locator(".page-heading")).toContainText("version " + activated.body.version);

    // pause -> resume -> amend, each carrying the freshly served version.
    await page.getByRole("button", { name: "Pause" }).click();
    await page.getByLabel("Reason").fill("Operator review pause");
    await page.getByRole("button", { name: "Send Pause" }).click();
    await expect(page.getByText("Pause accepted by the server.")).toBeVisible();

    await page.getByRole("button", { name: "Resume" }).click();
    await page.getByLabel("Reason").fill("Operator review complete");
    await page.getByRole("button", { name: "Send Resume" }).click();
    await expect(page.getByText("Resume accepted by the server.")).toBeVisible();

    await page.getByRole("button", { name: "Amend" }).click();
    await page.getByRole("textbox", { name: "Objective" }).fill("Amended objective from the goal surface");
    await page.getByLabel("Reason").fill("Second approved revision");
    await page.getByRole("button", { name: "Send Amend" }).click();
    await expect(page.getByText("Amend accepted by the server.")).toBeVisible();

    const amended = await api(page, "GET", `/portal/api/goals/${goalId}`);
    expect(amended.body.current_revision).toBe(2);
    expect(amended.body.revision.objective).toBe("Amended objective from the goal surface");
    await page.getByRole("button", { name: "Refresh", exact: true }).click();
    await expect(page.getByText("revision 2 · version " + amended.body.version)).toHaveCount(1);
    await capture(page, testInfo, "goal-lifecycle", "The surface drove approval, activation, pause, resume and amendment through the accepted commands and rendered the server's revision 2");
  });

  test("a stale version is refused with the server's current state and the surface never overwrites", async ({ page }, testInfo) => {
    allowBrowserError(page, /status of 409|status of 422/);
    await login(page);
    const project = await createProject(page, "C");
    const repositoryId = await attachRepository(page, project.id);
    const { goalId } = await createGoalWithPlan(page, project, repositoryId, "Stale conflict goal");

    await page.goto(`/portal/goals/${goalId}`);
    await expect(page.getByRole("heading", { level: 1, name: "Stale conflict goal" })).toBeVisible();
    await page.getByRole("button", { name: "Amend" }).click();
    await page.getByRole("textbox", { name: "Objective" }).fill("Stale amendment attempt");
    await page.getByLabel("Reason").fill("Attempted with a stale page");

    // Another operator moves the goal first: the open decision carries a fresh
    // version the page has not loaded.
    const moved = await goalCommand(page, goalId, "cancel", { reason: "Concurrent operator decision" });
    expect(moved.response.status).toBe(200);

    await page.getByRole("button", { name: "Send Amend" }).click();
    await expect(page.getByRole("heading", { name: "The goal changed since this page loaded" })).toBeVisible();
    await expect(page.getByText(/Server state: version/)).toBeVisible();
    await expect(page.locator("code")).toHaveText("stale");
    await expect(page.getByRole("button", { name: "Reload the current state" })).toBeVisible();
    await capture(page, testInfo, "goal-stale-conflict", "The stale command was refused with the server's current version and revision; nothing was overwritten silently");

    await page.getByRole("button", { name: "Reload the current state" }).click();
    await expect(page.getByText("cancelled", { exact: true }).first()).toBeVisible();
    await expect(page.getByRole("button", { name: "Amend" })).toHaveCount(0);
    await capture(page, testInfo, "goal-after-reload", "Reloading the refused goal renders the server's current cancelled state and no longer offers the refused action");
  });

  test("mutation replay returns the stored receipt for the same mutation identity", async ({ page }) => {
    allowBrowserError(page, /status of 409/);
    await login(page);
    const project = await createProject(page, "D");
    const repositoryId = await attachRepository(page, project.id);
    const { goalId } = await createGoalWithPlan(page, project, repositoryId, "Replay goal");

    const read = await api(page, "GET", `/portal/api/goals/${goalId}`);
    const mutationId = crypto.randomUUID();
    const envelope = {
      schema_version: "symmetry.goal_command.v1",
      mutation_id: mutationId,
      expected_version: read.body.version,
      expected_revision: read.body.current_revision,
      kind: "amend",
      payload: {
        reason: "Replay proof",
        revision_contract: {
          objective: "Replayed amendment objective",
          non_goals: [],
          acceptance_contract: acceptance(),
          authority_policy: authorityPolicy(),
          execution_policy: executionPolicy(),
          context_manifest: contextManifest()
        }
      }
    };

    const first = await api(page, "POST", `/portal/api/goals/${goalId}/commands`, envelope);
    expect(first.status).toBe(200);
    expect(first.body.event.id).toBe(mutationId);

    const replay = await api(page, "POST", `/portal/api/goals/${goalId}/commands`, envelope);
    expect(replay.status).toBe(200);
    expect(replay.body.event.id).toBe(mutationId);
    expect(replay.body.event.sequence).toBe(first.body.event.sequence);

    const afterReplay = await api(page, "GET", `/portal/api/goals/${goalId}`);
    expect(afterReplay.body.current_revision).toBe(2);
    expect(afterReplay.body.version).toBe(first.body.goal.version);

    const conflict = await api(page, "POST", `/portal/api/goals/${goalId}/commands`, { ...envelope, payload: { ...envelope.payload, reason: "Conflicting replay" } });
    expect(conflict.status).toBe(409);
    expect(conflict.body.error.code).toBe("idempotency_conflict");
  });

  test("the dependency projection renders read-only and the receipts paginate over the served boundary", async ({ page }, testInfo) => {
    allowBrowserError(page, /status of 409/);
    await login(page);
    const project = await createProject(page, "E");
    const repositoryId = await attachRepository(page, project.id);
    const { goalId } = await createGoalWithPlan(page, project, repositoryId, "Projection goal");

    // Real commands sent through the accepted API create the audit receipts the
    // surface renders; a replay stays idempotent and a stale mutation conflicts.
    const read = await api(page, "GET", `/portal/api/goals/${goalId}`);
    const message = { kind: "cancel", payload: { reason: "Receipt history" } };
    const fresh = await api(page, "POST", `/portal/api/goals/${goalId}/commands`, {
      schema_version: "symmetry.goal_command.v1",
      mutation_id: crypto.randomUUID(),
      expected_version: read.body.version,
      expected_revision: read.body.current_revision,
      ...message
    });
    expect(fresh.status).toBe(200);
    const replay = await api(page, "POST", `/portal/api/goals/${goalId}/commands`, {
      schema_version: "symmetry.goal_command.v1",
      mutation_id: fresh.body.event.id,
      expected_version: read.body.version,
      expected_revision: read.body.current_revision,
      ...message
    });
    expect(replay.status).toBe(200);
    expect(replay.body.event.id).toBe(fresh.body.event.id);
    const conflict = await api(page, "POST", `/portal/api/goals/${goalId}/commands`, {
      schema_version: "symmetry.goal_command.v1",
      mutation_id: crypto.randomUUID(),
      expected_version: read.body.version,
      expected_revision: read.body.current_revision,
      ...message
    });
    expect(conflict.status).toBe(409);
    const events = await api(page, "GET", `/portal/api/goals/${goalId}/events?after=0`);
    expect(events.status).toBe(200);
    expect(events.body.entries.length).toBeGreaterThanOrEqual(3);

    await page.goto(`/portal/goals/${goalId}`);
    await expect(page.getByRole("heading", { level: 1, name: "Projection goal" })).toBeVisible();
    await expect(page.getByRole("heading", { name: /Dependencies/ })).toBeVisible();
    await expect(page.getByText("Read-only projection")).toBeVisible();
    // No editing affordance exists on the dependency projection.
    await expect(page.getByRole("button", { name: /Add dependency|Remove dependency|Edit dependency/i })).toHaveCount(0);

    // The surface renders the receipts the server recorded, newest page included.
    await page.getByRole("button", { name: "Refresh", exact: true }).click();
    const receipts = page.locator(".receipt-list");
    await expect(receipts).toBeVisible();
    const loaded = await receipts.locator("li").count();
    expect(loaded).toBeGreaterThanOrEqual(events.body.entries.length);
    await capture(page, testInfo, "goal-receipts", `The receipts list rendered ${loaded} of ${events.body.entries.length} real audit events the server recorded`);
  });

  test("the Goal detail read refuses an unauthorized context snapshot and renders the refusal", async ({ page }) => {
    allowBrowserError(page, /status of 404|status of 401/);
    await login(page);
    const project = await createProject(page, "F");
    const repositoryId = await attachRepository(page, project.id);
    const { goalId } = await createGoalWithPlan(page, project, repositoryId, "Refusal goal");

    const refused = await api(page, "GET", `/portal/api/goals/${goalId}/contexts/${crypto.randomUUID()}`);
    expect(refused.status).toBe(404);
    expect(refused.body.error.code).toBe("not_found");

    const unauthenticated = await page.evaluate(async (path) => {
      const response = await fetch(path, { credentials: "omit", headers: { Accept: "application/json" } });
      return { status: response.status, body: await response.json().catch(() => null) };
    }, `/portal/api/goals/${goalId}`);
    expect(unauthenticated.status).toBe(401);
    expect(unauthenticated.body.error.code).toBe("unauthenticated");
  });

  test("the Goal surfaces recover from a disconnected read and refresh the accumulated state", async ({ page }, testInfo) => {
    await login(page);
    const project = await createProject(page, "G");
    const repositoryId = await attachRepository(page, project.id);
    const { goalId, proposalJson } = await createGoalWithPlan(page, project, repositoryId, "Recovery goal");

    await page.goto(`/portal/goals/${goalId}`);
    await expect(page.getByRole("heading", { level: 1, name: "Recovery goal" })).toBeVisible();
    await page.getByLabel("Plan proposal").fill(proposalJson);
    await page.getByRole("button", { name: "Resolve the decision with accept" }).click();
    await expect(page.getByText("The server admitted the plan.")).toBeVisible();

    allowBrowserError(page, /net::ERR_FAILED|Failed to fetch|Failed to load resource/);
    await page.route("**/portal/api/goals/*", (route) => route.abort("failed"));
    await page.reload();
    await expect(page.getByRole("heading", { name: "Unable to load workspace", exact: true }).or(page.getByRole("heading", { name: "This Goal could not be read." }))).toBeVisible();
    await capture(page, testInfo, "goal-disconnected", "A real aborted Goal read renders the retryable error state instead of a fabricated goal");

    await page.unrouteAll({ behavior: "wait" });
    await page.getByRole("button", { name: "Try again", exact: true }).click();
    await expect(page.getByRole("heading", { level: 1, name: "Recovery goal" })).toBeVisible();
    const admitted = await api(page, "GET", `/portal/api/goals/${goalId}`);
    expect(admitted.body.work_items).toHaveLength(1);
    await capture(page, testInfo, "goal-recovered", "Retry re-read the real Goal read and the admitted work item is rendered");
  });

  test("with Goal rollout off a present Goal still serves its real attention entry", async ({ page }, testInfo) => {
    test.skip(!goalsOffUrl, "SYMMETRY_GOALS_OFF_URL is not set: a rollout-off release instance is required, so this case is skipped, not passed.");
    await login(page, "/portal/attention", goalsOffUrl);
    const project = await createProject(page, "J", { blocked: true });

    // Creation is an ordinary accepted command on this instance; the
    // admission refusal below and the read are what this case binds.
    const created = await api(page, "POST", `/portal/api/projects/${project.id}/goals`, {
      schema_version: "symmetry.goal_create.v1",
      mutation_id: crypto.randomUUID(),
      title: "Rollout-off goal",
      initial_revision: {
        objective: "Serve the attention read while Goal admission is disabled.",
        non_goals: [],
        acceptance_contract: acceptance(),
        authority_policy: authorityPolicy(),
        execution_policy: executionPolicy(),
        context_manifest: contextManifest(),
        reason: "Initial approved Goal contract"
      }
    });
    expect(created.status).toBe(201);
    const goalId = created.body.goal.id;

    // Discriminating probe: creation and the read below succeed against a
    // rollout-on server too, so the case binds this instance's admission gate
    // directly. The command is contract-valid (the request_plan payload
    // requires all five members, and `subject.resource_id` must equal
    // `repository_resource_id`), so a 409 here is the admission gate's
    // refusal, never a validator's.
    const probeRepositoryId = crypto.randomUUID();
    const refused = await goalCommand(page, goalId, "request_plan", {
      model_profile: "codex",
      repository_resource_id: probeRepositoryId,
      subject: {
        resource_id: probeRepositoryId,
        commit: "0".repeat(40),
        tree_digest: `sha256:${"0".repeat(64)}`
      },
      session_mode: "fresh",
      requested_session_id: null
    });
    expect(refused.response.status).toBe(409);
    expect(refused.response.body.error.code).toBe("goal_admission_disabled");

    // What the rollout-off server actually serves: the Goal entry beside the
    // project blockers, with no unavailable feed stated.
    const served = await api(page, "GET", `/portal/api/attention?project_id=${project.id}`);
    expect(served.status).toBe(200);
    expect(served.body.goal_attention.unavailable).toBe(null);
    expect(served.body.goal_attention.entries.map((entry) => entry.goal_id)).toContain(goalId);

    await page.goto(`${goalsOffUrl}/portal/attention?projectId=${project.id}`);
    await expect(page.getByRole("heading", { level: 1, name: "Attention", exact: true })).toBeVisible();
    await expect(page.getByRole("link", { name: "Rollout-off goal" })).toBeVisible();
    await expect(page.getByRole("heading", { name: `Blocked work ${project.key}`, exact: true })).toBeVisible();
    await expect(page.getByRole("heading", { name: "Goal attention is unavailable on this server" })).toHaveCount(0);
    await capture(page, testInfo, "attention-rollout-off", "With Goal rollout off the real attention read serves the present Goal beside the project blockers and states no unavailable feed");
  });

  test("the attention list is keyboard reachable and the Goal surface hands focus back", async ({ page }, testInfo) => {
    await login(page);
    const project = await createProject(page, "K");
    const repositoryId = await attachRepository(page, project.id);
    const { goalId } = await createGoalWithPlan(page, project, repositoryId, "Keyboard goal");

    await page.goto(`/portal/attention?projectId=${project.id}`);
    const link = page.getByRole("link", { name: "Keyboard goal" });
    await link.focus();
    await expect(link).toBeFocused();
    await link.press("Enter");
    await expect(page).toHaveURL(new RegExp(`/portal/goals/${goalId}$`));
    // The shell moves focus into the destination content: the Goal heading.
    await expect(page.getByRole("heading", { level: 1, name: "Keyboard goal" })).toBeFocused();
    await page.goBack();
    await expect(page.getByRole("heading", { level: 1, name: "Attention", exact: true })).toBeFocused();
    await capture(page, testInfo, "attention-keyboard-walk", "Keyboard: Enter on the attention Goal link opens the Goal surface and focus lands on its heading; history back returns focus to the attention heading");
  });
});
