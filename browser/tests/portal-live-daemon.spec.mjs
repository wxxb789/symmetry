import { allowBrowserError, expect, test } from "./fixtures.mjs";

const liveEnabled = process.env.SYMMETRY_LIVE_DAEMON === "1";
const operatorToken = process.env.SYMMETRY_OPERATOR_TOKEN || "development-operator-token";
const fixtureSuffix = process.env.SYMMETRY_LIVE_FIXTURE_SUFFIX;
const fixtureBinding = (name) => `goal2-${name}${fixtureSuffix ? `-${fixtureSuffix}` : ""}`;

function uniqueProject() {
  const suffix = `${Date.now().toString(36)}${Math.random().toString(36).slice(2, 5)}`
    .slice(-6)
    .toUpperCase();
  return { key: `LV${suffix}`.slice(0, 8), name: `Live daemon ${suffix}` };
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

async function createProject(page, project) {
  await page.locator("#new-project-button").click();
  const dialog = page.locator("#project-dialog");
  await dialog.getByLabel("Name").fill(project.name);
  await dialog.getByLabel("Key").fill(project.key);
  await dialog.getByLabel("Default agent").fill(fixtureBinding("wait"));
  await dialog.getByLabel("Workspace").fill(fixtureBinding("wait"));
  await dialog.getByLabel("Description").fill("Live daemon acceptance workspace");
  await dialog.getByRole("button", { name: "Create" }).click();
  await expect(page.locator("#project-title")).toHaveText(project.name);
}

async function createAgentWork(page, { title, profile, workspace, description }) {
  await page.getByRole("button", { name: "New work item" }).click();
  const dialog = page.locator("#work-item-dialog");
  await dialog.getByLabel("Title").fill(title);
  await dialog.getByLabel("Description").fill(description);
  await dialog.getByLabel("Status").selectOption("ready");
  await dialog.getByLabel("Owner type").selectOption("agent");
  await dialog.getByLabel("Agent profile").fill(profile);
  await dialog.getByLabel("Workspace").fill(workspace);
  await dialog.getByRole("button", { name: "Create" }).click();
  await expect(page.locator(".work-card", { hasText: title })).toBeVisible();
}

async function attachFreeformResource(page, resource) {
  await page.getByRole("button", { name: "Attach resource" }).click();
  const dialog = page.locator("#resource-dialog");
  await dialog.getByLabel("Type").selectOption(resource.kind);
  await dialog.locator('select[name="status"]').selectOption(resource.status);
  await dialog.locator('select[name="sync_status"]').selectOption(resource.sync_status);
  await dialog.getByLabel("Name").fill(resource.name);
  await dialog.getByLabel("Provider").fill(resource.provider);
  await dialog.getByLabel("Reference", { exact: true }).fill(resource.external_ref);
  await dialog.getByLabel("URL").fill(resource.url);
  await dialog.getByRole("button", { name: "Attach" }).click();
  await expect(page.locator("#resource-table")).toContainText(resource.name);
}

async function openWork(page, title) {
  await page.locator(".work-card", { hasText: title }).click();
  await expect(page.locator("#detail-title")).toHaveText(title);
}

async function detailJSON(page) {
  const workItemId = await page.locator(".work-card", { hasText: await page.locator("#detail-title").textContent() }).getAttribute("data-id");
  return page.evaluate(async (id) => {
    const response = await fetch(`/portal/api/work-items/${id}`, { credentials: "same-origin" });
    return response.json();
  }, workItemId);
}

async function portalRequest(page, path, options = {}) {
  return page.evaluate(
    async ({ path: requestPath, options: requestOptions }) => {
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
      return { status: response.status, body: await response.json().catch(() => ({})) };
    },
    { path, options }
  );
}

async function selectRegisteredReference(dialog, value) {
  const select = dialog.getByLabel("Registered reference");
  await expect(select.locator(`option[value="${value}"]`)).toHaveCount(1);
  await select.evaluate((element, selectedValue) => {
    element.value = selectedValue;
    element.dispatchEvent(new Event("input", { bubbles: true }));
    element.dispatchEvent(new Event("change", { bubbles: true }));
  }, value);
  await expect(select).toHaveValue(value);
}

test.describe("Goal 2 live daemon acceptance", () => {
  test.skip(!liveEnabled, "Set SYMMETRY_LIVE_DAEMON=1 after starting the documented fixture runtimes.");
  test.skip(({ isMobile }) => isMobile, "The live lifecycle is covered once; mobile behavior is covered by deterministic feature tests.");

  test("waiting input and failed retry complete through the portal", async ({ page }) => {
    const project = uniqueProject();
    const waitTitle = `Waiting workflow ${project.key}`;
    const cancelTitle = `Cancel workflow ${project.key}`;
    const retryTitle = `Retry workflow ${project.key}`;

    await login(page);
    await createProject(page, project);

    const workspace = await portalRequest(page, `/portal/api/workspace${new URL(page.url()).search}`);
    expect(workspace.status).toBe(200);
    const waitRuntime = workspace.body.registered_runtimes.find(
      (runtime) => runtime.agent_profile === fixtureBinding("wait") && runtime.workspace === fixtureBinding("wait")
    );
    expect(waitRuntime).toBeTruthy();
    await page.locator("#refresh-button").click();

    await page.getByRole("link", { name: "Resources" }).click();
    await page.getByRole("button", { name: "Attach resource" }).click();
    let resourceDialog = page.locator("#resource-dialog");
    await resourceDialog.getByLabel("Type").selectOption("agent");
    await expect(resourceDialog.getByLabel("Registered reference")).toBeVisible();
    await resourceDialog.getByLabel("Name").fill("Wait agent");
    await selectRegisteredReference(resourceDialog, fixtureBinding("wait"));
    await resourceDialog.getByRole("button", { name: "Attach" }).click();
    await expect(page.locator("#resource-table")).toContainText("Wait agent");

    await page.getByRole("button", { name: "Attach resource" }).click();
    resourceDialog = page.locator("#resource-dialog");
    await resourceDialog.getByLabel("Type").selectOption("runtime");
    await resourceDialog.getByLabel("Name").fill("Wait runtime");
    await selectRegisteredReference(resourceDialog, waitRuntime.runtime_id);
    await resourceDialog.getByRole("button", { name: "Attach" }).click();
    await expect(page.locator("#resource-table")).toContainText("Wait runtime");

    await page.reload();
    await page.getByRole("link", { name: "Resources" }).click();
    await page.locator(".resource-row", { hasText: "Wait agent" }).click();
    await expect(page.locator("#resource-dialog").getByLabel("Registered reference")).toHaveValue(fixtureBinding("wait"));
    await page.locator("#resource-dialog").press("Escape");
    await page.locator(".resource-row", { hasText: "Wait runtime" }).click();
    await expect(page.locator("#resource-dialog").getByLabel("Registered reference")).toHaveValue(waitRuntime.runtime_id);
    await page.locator("#resource-dialog").press("Escape");

    allowBrowserError(page, /status of 422/);
    await page.getByRole("button", { name: "Attach resource" }).click();
    resourceDialog = page.locator("#resource-dialog");
    await resourceDialog.getByLabel("Type").selectOption("runtime");
    await resourceDialog.getByLabel("Name").fill("Forged runtime");
    await resourceDialog.getByLabel("Registered reference").evaluate((element) => {
      element.add(new Option("Forged runtime", "00000000-0000-0000-0000-000000000000", true, true));
      element.dispatchEvent(new Event("change", { bubbles: true }));
    });
    await resourceDialog.getByRole("button", { name: "Attach" }).click();
    await expect(resourceDialog.locator("[data-form-error]")).toContainText("must reference a registered runtime");
    await expect(resourceDialog).toBeVisible();
    await resourceDialog.press("Escape");

    await attachFreeformResource(page, {
      kind: "repository",
      name: "Live repository",
      provider: "GitHub",
      external_ref: "wxxb789/symmetry",
      url: "https://github.com/wxxb789/symmetry",
      status: "healthy",
      sync_status: "synced"
    });
    await attachFreeformResource(page, {
      kind: "ci",
      name: "Live CI",
      provider: "GitHub Actions",
      external_ref: "goal-2-live",
      url: "https://github.com/wxxb789/symmetry/actions",
      status: "healthy",
      sync_status: "synced"
    });
    await page.getByRole("link", { name: "Board" }).click();

    await createAgentWork(page, {
      title: waitTitle,
      profile: fixtureBinding("wait"),
      workspace: fixtureBinding("wait"),
      description: "Request a human decision and resume through the durable input command."
    });
    await openWork(page, waitTitle);
    await page.getByRole("button", { name: "Start run" }).click();
    await expect(page.locator("#detail-content")).toContainText("Agent needs a decision", { timeout: 30_000 });
    await page.getByLabel("Response").fill("continue");
    await page.getByRole("button", { name: "Send" }).click();
    await expect(page.locator("#detail-content")).toContainText("Completed", { timeout: 30_000 });
    await expect(page.locator("details.raw-details")).not.toHaveAttribute("open", "");
    const waited = await detailJSON(page);
    expect(waited.work_item.execution.state).toBe("completed");
    expect(waited.work_item.execution.generation).toBe(1);
    expect(waited.raw.timeline).toEqual(
      expect.arrayContaining([
        expect.objectContaining({ source: "event", data: expect.objectContaining({ kind: "waiting_for_input" }) }),
        expect.objectContaining({ source: "command", data: expect.objectContaining({ kind: "provide_input" }) }),
        expect.objectContaining({ source: "transition", data: expect.objectContaining({ state: "completed" }) })
      ])
    );
    await page.getByRole("button", { name: "Close details" }).click();

    await createAgentWork(page, {
      title: cancelTitle,
      profile: fixtureBinding("cancel"),
      workspace: fixtureBinding("cancel"),
      description: "Enter a real running process and cancel it through a durable command."
    });
    await openWork(page, cancelTitle);
    await page.getByRole("button", { name: "Start run" }).click();
    await expect(page.locator("#detail-content")).toContainText("Running", { timeout: 30_000 });
    await page.getByRole("button", { name: "Cancel run" }).click();
    await expect(page.locator("#detail-content")).toContainText("Cancelled", { timeout: 30_000 });
    await expect(page.getByRole("button", { name: "Retry" })).toBeVisible();
    const cancelled = await detailJSON(page);
    expect(cancelled.work_item.execution.state).toBe("cancelled");
    expect(cancelled.raw.timeline).toEqual(
      expect.arrayContaining([
        expect.objectContaining({ source: "command", data: expect.objectContaining({ kind: "cancel" }) }),
        expect.objectContaining({ source: "transition", data: expect.objectContaining({ state: "cancelled" }) })
      ])
    );
    await page.getByRole("button", { name: "Close details" }).click();

    await createAgentWork(page, {
      title: retryTitle,
      profile: fixtureBinding("retry"),
      workspace: fixtureBinding("retry"),
      description: "Fail once, accept corrected intent, and report delivery evidence on retry."
    });
    await openWork(page, retryTitle);
    await page.getByRole("button", { name: "Start run" }).click();
    await expect(page.locator("#detail-content")).toContainText("Failure", { timeout: 30_000 });
    const failed = await detailJSON(page);
    expect(failed.work_item.execution.state).toBe("failed");
    expect(failed.work_item.execution.generation).toBe(1);

    await page.getByRole("button", { name: "Edit" }).click();
    const editDialog = page.locator("#work-item-dialog");
    await editDialog
      .getByLabel("Description")
      .fill("Corrected intent must be copied into the second durable generation.");
    await editDialog.getByRole("button", { name: "Save" }).click();
    await expect(page.locator("#toast")).toHaveText("Work item updated");
    await page.getByRole("button", { name: "Retry" }).click();

    await expect(page.locator("#detail-content")).toContainText("Completed the requested engineering work", {
      timeout: 30_000
    });
    await expect(page.locator("#detail-content")).toContainText("Verified the requested change");
    await expect(page.locator("#detail-content")).toContainText("README.md");
    await expect(page.locator("#detail-content")).toContainText("fake-agent verification");
    await expect(page.locator("#detail-content")).toContainText("Generation");
    await expect(page.locator("#detail-content")).toContainText("2");
    await expect(page.locator("#detail-content")).not.toContainText("Failure");

    const retried = await detailJSON(page);
    expect(retried.work_item.execution.task_id).toBe(failed.work_item.execution.task_id);
    expect(retried.work_item.execution.state).toBe("completed");
    expect(retried.work_item.execution.generation).toBe(2);
    expect(retried.outcome.goal).toContain("Corrected intent must be copied");
    expect(retried.work_item.delivery.pull_request.source).toBe("agent");
    expect(retried.work_item.delivery.ci).toEqual(expect.objectContaining({ source: "agent", status: "passed" }));
    expect(retried.work_item.delivery.review).toEqual(expect.objectContaining({ source: "agent", status: "required" }));

    const currentEventKinds = retried.raw.timeline
      .filter((entry) => entry.source === "event" && entry.generation === 2)
      .map((entry) => entry.data.kind);
    expect(currentEventKinds).toEqual(
      expect.arrayContaining([
        "progress",
        "finding",
        "artifact",
        "test",
        "pull_request",
        "ci",
        "review",
        "summary"
      ])
    );
    expect(new Set(retried.raw.timeline.map((entry) => entry.generation))).toEqual(new Set([1, 2]));

    await page.locator("#detail-status-select").selectOption("review");
    await expect(page.locator("[data-status='review'] .work-card", { hasText: retryTitle })).toBeVisible();
    await page.locator("#detail-status-select").selectOption("done");
    await expect(page.locator("[data-status='done'] .work-card", { hasText: retryTitle })).toBeVisible();

    await page.getByRole("button", { name: "Close details" }).click();
    await page.getByRole("link", { name: "Activity" }).click();
    await expect(page.locator(".activity-row", { hasText: cancelTitle })).toContainText("Retry available");
    await expect(page.locator(".activity-row", { hasText: retryTitle })).toContainText("Goal 2 retry runtime");
    await expect(page.locator(".activity-row", { hasText: retryTitle })).toContainText("Generation 2");
    await expect(page.locator(".activity-row", { hasText: retryTitle })).toContainText("Done");
  });

  test("raw execution history loads older daemon records on demand", async ({ page }) => {
    const project = uniqueProject();
    const historyTitle = `History workflow ${project.key}`;

    await login(page);
    await createProject(page, project);
    await createAgentWork(page, {
      title: historyTitle,
      profile: fixtureBinding("history"),
      workspace: fixtureBinding("history"),
      description: "Produce enough real execution records to verify raw history pagination."
    });
    await openWork(page, historyTitle);
    await page.getByRole("button", { name: "Start run" }).click();
    await expect(page.locator("#detail-content")).toContainText("Completed the requested engineering work", {
      timeout: 30_000
    });

    const rawDetails = page.locator("details.raw-details");
    await expect(rawDetails).not.toHaveAttribute("open", "");
    await rawDetails.locator("summary").click();
    await expect(page.locator("#load-history-button")).toBeVisible();
    await expect(page.locator("#raw-execution-data")).not.toContainText("debug-001");
    await page.locator("#load-history-button").click();
    await expect(page.locator("#raw-execution-data")).toContainText("debug-001");
    await expect(page.locator("details.raw-details")).toHaveAttribute("open", "");

    const history = await detailJSON(page);
    expect(history.work_item.execution.state).toBe("completed");
    expect(history.raw.next_before).not.toBeNull();
  });
});
