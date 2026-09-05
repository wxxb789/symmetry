import { allowBrowserError, expect, test } from "./fixtures.mjs";

const composeEnabled = process.env.SYMMETRY_COMPOSE_DAEMON === "1";
const operatorToken = process.env.SYMMETRY_OPERATOR_TOKEN || "development-operator-token";

function uniqueProject(prefix = "DC") {
  const suffix = `${Date.now().toString(36)}${Math.random().toString(36).slice(2, 5)}`
    .slice(-6)
    .toUpperCase();
  return { key: `${prefix}${suffix}`.slice(0, 8), name: `Compose daemon ${suffix}` };
}

function fixtureDescription(mode, description) {
  return `[symmetry-fake-agent:${mode}]\n${description}`;
}

async function login(page) {
  await page.goto("/portal/login");
  await page.getByLabel("Operator token").fill(operatorToken);
  await Promise.all([
    page.waitForURL(/\/portal(?:\?|#|$)/),
    page.getByRole("button", { name: "Open workspace" }).click()
  ]);
  await expect(page.locator("#refresh-button")).toBeVisible();
}

async function portalRequest(page, path, options = {}) {
  return page.evaluate(async ({ requestPath, requestOptions }) => {
    const csrf = document.querySelector("meta[name='csrf-token']")?.content || "";
    const response = await fetch(requestPath, {
      credentials: "same-origin",
      ...requestOptions,
      headers: {
        Accept: "application/json",
        ...(requestOptions.body ? { "Content-Type": "application/json" } : {}),
        ...(requestOptions.method && requestOptions.method !== "GET" ? { "x-csrf-token": csrf } : {}),
        ...(requestOptions.headers || {})
      }
    });
    return { status: response.status, body: await response.json() };
  }, { requestPath: path, requestOptions: options });
}

async function createProject(page, project, defaults = { agent: "default", workspace: "primary" }) {
  await page.locator("#new-project-button").click();
  const dialog = page.locator("#project-dialog");
  await dialog.getByLabel("Name").fill(project.name);
  await dialog.getByLabel("Key").fill(project.key);
  await dialog.getByLabel("Default agent").fill(defaults.agent);
  await dialog.getByLabel("Workspace").fill(defaults.workspace);
  await dialog.getByLabel("Description").fill("Trusted-local Compose execution acceptance");
  await dialog.getByRole("button", { name: "Create" }).click();
  await expect(page.locator("#project-title")).toHaveText(project.name);
  const projectId = new URL(page.url()).searchParams.get("project_id");
  expect(projectId).toBeTruthy();
  return projectId;
}

async function attachResource(page, resource) {
  await page.getByRole("button", { name: "Attach resource" }).click();
  const dialog = page.locator("#resource-dialog");
  await dialog.getByLabel("Type").selectOption(resource.kind);
  await dialog.locator('select[name="status"]').selectOption(resource.status);
  await dialog.locator('select[name="sync_status"]').selectOption(resource.sync_status);
  await dialog.getByLabel("Name").fill(resource.name);
  await dialog.getByLabel("Provider").fill(resource.provider);
  if (resource.registered_ref) {
    const select = dialog.getByLabel("Registered reference");
    await expect(select.locator(`option[value="${resource.registered_ref}"]`)).toHaveCount(1);
    await select.evaluate((element, value) => {
      element.value = value;
      element.dispatchEvent(new Event("input", { bubbles: true }));
      element.dispatchEvent(new Event("change", { bubbles: true }));
    }, resource.registered_ref);
    await expect(select).toHaveValue(resource.registered_ref);
  } else {
    await dialog.getByLabel("Reference", { exact: true }).fill(resource.external_ref);
  }
  if (resource.url) await dialog.getByLabel("URL").fill(resource.url);
  if (resource.status_message) await dialog.getByLabel("Status detail").fill(resource.status_message);
  await dialog.getByRole("button", { name: "Attach" }).click();
  await expect(page.locator("#resource-table")).toContainText(resource.name);
}

async function createWorkItem(page, item) {
  await page.getByRole("link", { name: "Board" }).click();
  await page.getByRole("button", { name: "New work item" }).click();
  const dialog = page.locator("#work-item-dialog");
  await dialog.getByLabel("Title").fill(item.title);
  await dialog.getByLabel("Description").fill(item.description);
  await dialog.getByLabel("Status").selectOption(item.status || "ready");
  await dialog.getByLabel("Priority").selectOption(item.priority || "medium");
  await dialog.getByLabel("Owner type").selectOption(item.assignee_type);
  if (item.assignee_name) await dialog.getByLabel("Owner", { exact: true }).fill(item.assignee_name);
  if (item.repository) await dialog.getByLabel("Repository").selectOption({ label: item.repository });
  if (item.branch) await dialog.getByLabel("Branch").fill(item.branch);
  if (item.blocked) {
    await dialog.getByLabel("Blocked").check();
    await dialog.getByLabel("Blocker").fill(item.blocker);
  }
  await dialog.getByRole("button", { name: "Create" }).click();
  await expect(page.locator(".work-card", { hasText: item.title })).toBeVisible();
}

async function openWork(page, title) {
  await page.locator(".work-card", { hasText: title }).click();
  await expect(page.locator("#detail-title")).toHaveText(title);
}

async function detailJSON(page, title) {
  const workItemId = await page.locator(".work-card", { hasText: title }).getAttribute("data-id");
  return page.evaluate(async (id) => {
    const response = await fetch(`/portal/api/work-items/${id}`, { credentials: "same-origin" });
    return response.json();
  }, workItemId);
}

test.describe("Goal 2 Compose daemon acceptance", () => {
  test.setTimeout(180_000);
  test.skip(!composeEnabled, "Set SYMMETRY_COMPOSE_DAEMON=1 for the trusted-local Compose stack.");
  test.skip(({ isMobile }) => isMobile, "The container execution path is covered once; mobile UI uses the feature matrix.");

  test("the production Compose stack completes F1-F7 and persists each outcome", async ({ page }) => {
    const project = uniqueProject();
    const auxiliary = uniqueProject("DS");
    const humanTitle = `Human workflow ${project.key}`;
    const waitTitle = `Waiting workflow ${project.key}`;
    const cancelTitle = `Cancel workflow ${project.key}`;
    const retryTitle = `Retry workflow ${project.key}`;

    await login(page);
    const projectId = await createProject(page, project, { agent: "placeholder", workspace: "placeholder" });

    await page.getByRole("button", { name: "Project settings" }).click();
    const projectDialog = page.locator("#project-dialog");
    await projectDialog.getByLabel("Default agent").fill("default");
    await projectDialog.getByLabel("Workspace").fill("primary");
    await projectDialog.getByLabel("Description").fill("F1-F7 production Compose acceptance workspace");
    await projectDialog.getByRole("button", { name: "Save" }).click();

    let runtime;
    await expect
      .poll(async () => {
        const workspace = await portalRequest(page, `/portal/api/workspace?project_id=${projectId}`);
        runtime = workspace.body.registered_runtimes.find(
          (candidate) => candidate.agent_profile === "default" && candidate.workspace === "primary"
        );
        return Boolean(runtime);
      }, { timeout: 30_000 })
      .toBe(true);

    await page.getByRole("link", { name: "Resources" }).click();
    await attachResource(page, {
      kind: "repository",
      status: "healthy",
      sync_status: "synced",
      name: "Compose repository",
      provider: "GitHub",
      external_ref: "wxxb789/symmetry",
      url: "https://github.com/wxxb789/symmetry",
      status_message: "Repository is available"
    });
    await attachResource(page, {
      kind: "ci",
      status: "degraded",
      sync_status: "failed",
      name: "Compose CI",
      provider: "GitHub Actions",
      external_ref: "goal-2-compose",
      url: "https://github.com/wxxb789/symmetry/actions",
      status_message: "CI webhook delivery failed"
    });
    await attachResource(page, {
      kind: "connection",
      status: "offline",
      sync_status: "stale",
      name: "Compose connection",
      provider: "GitHub",
      external_ref: "github-compose",
      url: "https://github.com",
      status_message: "Reconnect provider credentials"
    });
    await attachResource(page, {
      kind: "agent",
      status: "healthy",
      sync_status: "synced",
      name: "Compose agent",
      provider: "Symmetry",
      registered_ref: "default"
    });
    await attachResource(page, {
      kind: "runtime",
      status: "healthy",
      sync_status: "synced",
      name: "Compose runtime",
      provider: "Symmetry",
      registered_ref: runtime.runtime_id
    });

    await expect(page.locator(".health-item", { hasText: "Connections" })).toContainText("Degraded");
    await expect(page.locator(".health-item", { hasText: "Sync" })).toContainText("Attention");
    const connectionRow = page.locator(".resource-row", { hasText: "Compose connection" });
    await expect(connectionRow).toContainText("Reconnect provider credentials");
    await connectionRow.click();
    const resourceDialog = page.locator("#resource-dialog");
    await expect(resourceDialog.locator('select[name="status"]')).toHaveValue("offline");
    await expect(resourceDialog.locator('select[name="sync_status"]')).toHaveValue("stale");
    await expect(resourceDialog.getByRole("button", { name: "Update" })).toBeVisible();
    await resourceDialog.press("Escape");

    await createProject(page, auxiliary);
    await page.locator("#project-switcher").selectOption(projectId);
    await expect(page.locator("#project-title")).toHaveText(project.name);
    await page.getByRole("link", { name: "Resources" }).click();
    await expect(page.locator("#resource-table")).toContainText("Compose repository");
    await expect(page.locator("#resource-table")).toContainText("Compose runtime");

    await createWorkItem(page, {
      title: humanTitle,
      description: "Human-owned work with complete delivery metadata",
      priority: "urgent",
      assignee_type: "human",
      assignee_name: "Lina",
      repository: "Compose repository",
      branch: `feat/${project.key.toLowerCase()}`,
      blocked: true,
      blocker: "Needs architecture decision"
    });
    await createWorkItem(page, {
      title: waitTitle,
      description: fixtureDescription("wait_input", "Request and persist one human decision."),
      priority: "high",
      assignee_type: "agent",
      repository: "Compose repository"
    });

    const humanCard = page.locator(".work-card", { hasText: humanTitle });
    await humanCard.getByTitle("Move down").click();
    await expect(page.locator("[data-status='ready'] .work-card h3")).toHaveText([waitTitle, humanTitle]);
    await page.reload();
    await expect(page.locator("#project-title")).toHaveText(project.name);
    await expect(page.locator("[data-status='ready'] .work-card h3")).toHaveText([waitTitle, humanTitle]);
    await openWork(page, humanTitle);
    await page.getByRole("button", { name: "Edit" }).click();
    const humanDialog = page.locator("#work-item-dialog");
    await humanDialog.getByLabel("Pull request URL").fill("https://github.com/wxxb789/symmetry/pull/42");
    await humanDialog.getByLabel("CI").selectOption("passed");
    await humanDialog.locator('select[name="review_status"]').selectOption("approved");
    await humanDialog.getByRole("button", { name: "Save" }).click();
    await expect(page.locator("#detail-content")).toContainText("Approved");
    await page.getByRole("button", { name: "Close details" }).click();
    await openWork(page, humanTitle);
    await page.locator("#detail-status-select").selectOption("review");
    await expect(page.locator("[data-status='review'] .work-card", { hasText: humanTitle })).toBeVisible();
    await page.getByRole("button", { name: "Close details" }).click();
    await openWork(page, humanTitle);
    await page.locator("#detail-status-select").selectOption("done");
    await expect(page.locator("[data-status='done'] .work-card", { hasText: humanTitle })).toBeVisible();
    const human = await detailJSON(page, humanTitle);
    expect(human.work_item).toEqual(
      expect.objectContaining({
        status: "done",
        priority: "urgent",
        blocked: true,
        pull_request_url: "https://github.com/wxxb789/symmetry/pull/42",
        ci_status: "passed",
        review_status: "approved",
        execution: null
      })
    );
    await page.getByRole("button", { name: "Close details" }).click();

    await openWork(page, waitTitle);
    await page.getByRole("button", { name: "Start run" }).click();
    await expect(page.locator("#detail-content")).toContainText("Agent needs a decision", { timeout: 30_000 });
    const waiting = await detailJSON(page, waitTitle);
    const waitingTransitionId = waiting.work_item.execution.waiting.transition_id;
    expect(waiting.work_item.execution).toEqual(
      expect.objectContaining({ state: "waiting_for_input", generation: 1, runtime_id: runtime.runtime_id })
    );
    const activeWorkspace = await portalRequest(page, `/portal/api/workspace?project_id=${projectId}`);
    const activeEntry = activeWorkspace.body.activity.find((entry) => entry.work_item.title === waitTitle);
    expect(activeEntry.execution).toEqual(
      expect.objectContaining({ state: "waiting_for_input", runtime_id: runtime.runtime_id })
    );
    expect(activeEntry.runtime.runtime_id).toBe(runtime.runtime_id);

    let droppedStatus;
    let replayStatus;
    let firstInputRequest = true;
    allowBrowserError(page, /net::ERR_(?:FAILED|CONNECTION_FAILED)/);
    await page.route("**/portal/api/work-items/*/input", async (route) => {
      const response = await route.fetch();
      if (firstInputRequest) {
        firstInputRequest = false;
        droppedStatus = response.status();
        await route.abort("connectionfailed");
      } else {
        replayStatus = response.status();
        await route.fulfill({ response });
      }
    });
    await page.getByLabel("Response").fill("continue");
    await page.getByRole("button", { name: "Send" }).click();
    await expect.poll(() => droppedStatus).toBe(202);
    await expect(page.getByLabel("Response")).toHaveValue("continue");
    await expect(page.getByRole("button", { name: "Send" })).toBeEnabled();
    await page.getByRole("button", { name: "Send" }).click();
    await expect.poll(() => replayStatus).toBe(200);
    await page.unroute("**/portal/api/work-items/*/input");
    await expect(page.locator("#detail-content")).toContainText("Completed", { timeout: 30_000 });
    const waited = await detailJSON(page, waitTitle);
    expect(waited.work_item.execution).toEqual(
      expect.objectContaining({ state: "completed", generation: 1, runtime_id: runtime.runtime_id })
    );
    expect(waited.raw.timeline).toEqual(
      expect.arrayContaining([
        expect.objectContaining({ source: "event", data: expect.objectContaining({ kind: "waiting_for_input" }) }),
        expect.objectContaining({ source: "command", data: expect.objectContaining({ kind: "provide_input" }) }),
        expect.objectContaining({ source: "transition", data: expect.objectContaining({ state: "completed" }) })
      ])
    );
    expect(
      waited.raw.timeline.filter(
        (entry) => entry.source === "command" && entry.data.kind === "provide_input"
      )
    ).toHaveLength(1);
    allowBrowserError(page, /status of 409/);
    const staleInput = await portalRequest(page, `/portal/api/work-items/${waited.work_item.id}/input`, {
      method: "POST",
      body: JSON.stringify({
        input: { answer: "stale" },
        action_id: `stale-${project.key}`,
        waiting_transition_id: waitingTransitionId
      })
    });
    expect(staleInput.status).toBe(409);
    expect(staleInput.body).toEqual(expect.objectContaining({ error: expect.objectContaining({ code: "state_conflict" }) }));
    await page.locator("#detail-status-select").selectOption("done");
    await expect(page.locator("[data-status='done'] .work-card", { hasText: waitTitle })).toBeVisible();
    await page.getByRole("button", { name: "Close details" }).click();

    await createWorkItem(page, {
      title: cancelTitle,
      description: fixtureDescription("slow", "Enter a real process and cancel it through the portal."),
      assignee_type: "agent",
      repository: "Compose repository"
    });
    await openWork(page, cancelTitle);
    await page.getByRole("button", { name: "Start run" }).click();
    await expect(page.locator("#detail-content")).toContainText("Running", { timeout: 30_000 });
    await page.getByRole("button", { name: "Cancel run" }).click();
    await expect(page.locator("#detail-content")).toContainText("Cancelled", { timeout: 30_000 });
    await expect(page.getByRole("button", { name: "Retry" })).toBeVisible();
    const cancelled = await detailJSON(page, cancelTitle);
    expect(cancelled.work_item.execution).toEqual(expect.objectContaining({ state: "cancelled", generation: 1 }));
    expect(cancelled.raw.timeline).toEqual(
      expect.arrayContaining([
        expect.objectContaining({ source: "command", data: expect.objectContaining({ kind: "cancel" }) }),
        expect.objectContaining({ source: "transition", data: expect.objectContaining({ state: "cancelled" }) })
      ])
    );
    await page.getByRole("button", { name: "Close details" }).click();

    await createWorkItem(page, {
      title: retryTitle,
      description: fixtureDescription("fail_once_then_evidence_success", "Fail once and preserve both generations."),
      assignee_type: "agent",
      repository: "Compose repository"
    });
    await openWork(page, retryTitle);
    await page.getByRole("button", { name: "Start run" }).click();
    await expect(page.locator("#detail-content")).toContainText("Failure", { timeout: 30_000 });
    const failed = await detailJSON(page, retryTitle);
    expect(failed.work_item.execution).toEqual(expect.objectContaining({ state: "failed", generation: 1 }));
    await expect(page.locator(".health-item", { hasText: "Executions" })).toContainText("Fault");

    await page.getByRole("button", { name: "Edit" }).click();
    const retryDialog = page.locator("#work-item-dialog");
    await retryDialog
      .getByLabel("Description")
      .fill(fixtureDescription("fail_once_then_evidence_success", "Corrected intent is used by generation two."));
    await retryDialog.getByRole("button", { name: "Save" }).click();
    await expect(page.locator("#detail-content")).toContainText("Corrected intent is used by generation two.");
    await page.getByRole("button", { name: "Retry" }).click();
    await expect(page.locator("#detail-content")).toContainText("Completed the requested engineering work", {
      timeout: 30_000
    });
    await expect(page.locator("#detail-content")).toContainText("Verified the requested change");
    await expect(page.locator("#detail-content")).toContainText("README.md");
    await expect(page.locator("#detail-content")).toContainText("fake-agent verification");

    const retried = await detailJSON(page, retryTitle);
    expect(retried.work_item.execution.task_id).toBe(failed.work_item.execution.task_id);
    expect(retried.work_item.execution).toEqual(expect.objectContaining({ state: "completed", generation: 2 }));
    expect(retried.work_item.delivery.pull_request).toEqual(expect.objectContaining({ source: "agent" }));
    expect(retried.work_item.delivery.ci).toEqual(expect.objectContaining({ source: "agent", status: "passed" }));
    expect(retried.work_item.delivery.review).toEqual(expect.objectContaining({ source: "agent", status: "required" }));
    expect(new Set(retried.raw.timeline.map((entry) => entry.generation))).toEqual(new Set([1, 2]));
    expect(
      retried.raw.timeline
        .filter((entry) => entry.source === "event" && entry.generation === 2)
        .map((entry) => entry.data.kind)
    ).toEqual(expect.arrayContaining(["progress", "finding", "artifact", "test", "pull_request", "ci", "review", "summary"]));

    await page.getByRole("button", { name: "Edit" }).click();
    const deliveryDialog = page.locator("#work-item-dialog");
    await deliveryDialog.getByLabel("Pull request URL").fill("https://github.com/wxxb789/symmetry/pull/84");
    await deliveryDialog.getByLabel("CI").selectOption("passed");
    await deliveryDialog.locator('select[name="review_status"]').selectOption("approved");
    await deliveryDialog.getByRole("button", { name: "Save" }).click();
    await expect(page.locator("#detail-content")).toContainText("Approved");
    await page.getByRole("button", { name: "Close details" }).click();
    await openWork(page, retryTitle);
    await page.locator("#detail-status-select").selectOption("review");
    await expect(page.locator("[data-status='review'] .work-card", { hasText: retryTitle })).toBeVisible();
    await page.getByRole("button", { name: "Close details" }).click();
    await openWork(page, retryTitle);
    await page.locator("#detail-status-select").selectOption("done");
    await expect(page.locator("[data-status='done'] .work-card", { hasText: retryTitle })).toBeVisible();
    const approved = await detailJSON(page, retryTitle);
    expect(approved.work_item.delivery.pull_request).toEqual(
      expect.objectContaining({ source: "manual", url: "https://github.com/wxxb789/symmetry/pull/84" })
    );
    expect(approved.work_item.delivery.ci).toEqual(expect.objectContaining({ source: "manual", status: "passed" }));
    expect(approved.work_item.delivery.review).toEqual(
      expect.objectContaining({ source: "manual", status: "approved" })
    );
    await page.getByRole("button", { name: "Close details" }).click();

    await page.getByRole("button", { name: "Project settings" }).click();
    await projectDialog.getByLabel("Status").selectOption("archived");
    await projectDialog.getByRole("button", { name: "Save" }).click();
    await expect(page.locator("#project-state")).toHaveText("Archived");
    await openWork(page, retryTitle);
    await expect(page.locator("#detail-status-select")).toBeDisabled();
    await page.getByRole("button", { name: "Close details" }).click();
    await page.getByRole("button", { name: "Project settings" }).click();
    await projectDialog.getByLabel("Status").selectOption("active");
    await projectDialog.getByRole("button", { name: "Save" }).click();
    await expect(page.locator("#project-state")).toHaveText("Active");

    await page.getByRole("link", { name: "Activity" }).click();
    await expect(page.locator(".activity-row", { hasText: waitTitle })).toContainText("default");
    await expect(page.locator(".activity-row", { hasText: waitTitle })).toContainText("Done");
    await expect(page.locator(".activity-row", { hasText: cancelTitle })).toContainText("Retry available");
    await expect(page.locator(".activity-row", { hasText: retryTitle })).toContainText("Generation 2");
    await expect(page.locator(".activity-row", { hasText: retryTitle })).toContainText("Done");

    const persisted = await portalRequest(page, `/portal/api/workspace?project_id=${projectId}`);
    const persistedProject = persisted.body.projects.find((candidate) => candidate.id === projectId);
    expect(persistedProject).toEqual(
      expect.objectContaining({
        name: project.name,
        status: "active",
        default_agent_profile: "default",
        default_workspace: "primary",
        description: "F1-F7 production Compose acceptance workspace"
      })
    );
    expect(persistedProject.resources).toHaveLength(5);
    expect(persistedProject.work_items.find((item) => item.title === humanTitle).execution).toBeNull();
    expect(persistedProject.work_items.find((item) => item.title === waitTitle)).toEqual(
      expect.objectContaining({ status: "done", execution: expect.objectContaining({ state: "completed", generation: 1 }) })
    );
    expect(persistedProject.work_items.find((item) => item.title === cancelTitle).execution).toEqual(
      expect.objectContaining({ state: "cancelled", generation: 1 })
    );
    expect(persistedProject.work_items.find((item) => item.title === retryTitle)).toEqual(
      expect.objectContaining({ status: "done", execution: expect.objectContaining({ state: "completed", generation: 2 }) })
    );
  });
});
