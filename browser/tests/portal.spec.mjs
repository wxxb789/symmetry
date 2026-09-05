import { allowBrowserError, assertNoBrowserErrors, expect, monitorBrowserErrors, test } from "./fixtures.mjs";

const operatorToken = process.env.SYMMETRY_OPERATOR_TOKEN || "development-operator-token";

function uniqueProject(prefix = "E2") {
  const suffix = `${Date.now().toString(36)}${Math.random().toString(36).slice(2, 5)}`
    .slice(-6)
    .toUpperCase();
  return {
    key: `${prefix}${suffix}`.slice(0, 8),
    name: `Portal acceptance ${suffix}`
  };
}

async function login(page, path = "/portal") {
  await page.goto("/portal/login");
  await page.getByLabel("Operator token").fill(operatorToken);
  await Promise.all([
    page.waitForURL(/\/portal(?:\?|#|$)/),
    page.getByRole("button", { name: "Open workspace" }).click()
  ]);
  await page.goto(path);
  await expect(page.locator("#refresh-button")).toBeVisible();
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

async function createProject(page, project) {
  await page.locator("#new-project-button").click();
  const dialog = page.locator("#project-dialog");
  await dialog.getByLabel("Name").fill(project.name);
  await dialog.getByLabel("Key").fill(project.key);
  await dialog.getByLabel("Default agent").fill("codex");
  await dialog.getByLabel("Workspace").fill("acceptance");
  await dialog.getByLabel("Description").fill("Feature-level browser acceptance workspace");
  await dialog.getByRole("button", { name: "Create" }).click();
  await expect(page.locator("#toast")).toHaveText("Project created");
  await expect(page.locator("#project-title")).toHaveText(project.name);
  const projectId = new URL(page.url()).searchParams.get("project_id");
  expect(projectId).toBeTruthy();
  return projectId;
}

async function attachResource(page, resource) {
  await page.getByRole("link", { name: "Resources" }).click();
  await page.getByRole("button", { name: "Attach resource" }).click();
  const dialog = page.locator("#resource-dialog");
  await dialog.getByLabel("Type").selectOption(resource.kind);
  await dialog.locator('select[name="status"]').selectOption(resource.status);
  await dialog.locator('select[name="sync_status"]').selectOption(resource.sync_status);
  await dialog.getByLabel("Name").fill(resource.name);
  await dialog.getByLabel("Provider").fill(resource.provider);
  await dialog.getByLabel("Reference", { exact: true }).fill(resource.external_ref);
  if (resource.url) await dialog.getByLabel("URL").fill(resource.url);
  if (resource.status_message) await dialog.getByLabel("Status detail").fill(resource.status_message);
  await dialog.getByRole("button", { name: "Attach" }).click();
  await expect(page.locator("#toast")).toHaveText("Resource attached");
  await expect(page.locator("#resource-table")).toContainText(resource.name);
}

async function createWorkItem(page, item) {
  await page.getByRole("link", { name: "Board" }).click();
  await page.getByRole("button", { name: "New work item" }).click();
  const dialog = page.locator("#work-item-dialog");
  await dialog.getByLabel("Title").fill(item.title);
  await dialog.getByLabel("Description").fill(item.description);
  await dialog.getByLabel("Status").selectOption(item.status);
  await dialog.getByLabel("Priority").selectOption(item.priority);
  await dialog.getByLabel("Owner type").selectOption(item.assignee_type);
  if (item.assignee_name) await dialog.getByLabel("Owner", { exact: true }).fill(item.assignee_name);
  if (item.agent_profile) await dialog.getByLabel("Agent profile").fill(item.agent_profile);
  if (item.workspace) await dialog.getByLabel("Workspace").fill(item.workspace);
  if (item.repository) await dialog.getByLabel("Repository").selectOption({ label: item.repository });
  if (item.branch) await dialog.getByLabel("Branch").fill(item.branch);
  if (item.blocked) {
    await dialog.getByLabel("Blocked").check();
    await dialog.getByLabel("Blocker").fill(item.blocker);
  }
  await dialog.getByRole("button", { name: "Create" }).click();
  await expect(page.locator("#toast")).toHaveText("Work item created");
  await expect(page.locator(".work-card", { hasText: item.title })).toBeVisible();
}

async function selectedProject(page) {
  const response = await portalRequest(page, `/portal/api/workspace${new URL(page.url()).search}`);
  expect(response.status).toBe(200);
  return response.body.projects.find((project) => project.id === response.body.selected_project_id);
}

test.describe("Goal 2 feature acceptance", () => {
  test("project, resource, work-item, filtering, ordering, and persistence", async ({ page }) => {
    const project = uniqueProject();
    await login(page);
    await createProject(page, project);

    await attachResource(page, {
      kind: "repository",
      status: "healthy",
      sync_status: "synced",
      name: "Acceptance repository",
      provider: "GitHub",
      external_ref: "wxxb789/symmetry",
      url: "https://github.com/wxxb789/symmetry",
      status_message: "Repository is available"
    });
    await attachResource(page, {
      kind: "ci",
      status: "degraded",
      sync_status: "stale",
      name: "Acceptance CI",
      provider: "GitHub Actions",
      external_ref: "goal-2",
      url: "https://github.com/wxxb789/symmetry/actions",
      status_message: "Webhook delivery is delayed"
    });

    await expect(page.locator("#health-grid")).toContainText(/degraded/i);
    await expect(page.locator("#health-grid")).toContainText(/attention/i);

    await page.locator(".resource-row", { hasText: "Acceptance CI" }).click();
    const resourceDialog = page.locator("#resource-dialog");
    await resourceDialog.locator('select[name="status"]').selectOption("healthy");
    await resourceDialog.locator('select[name="sync_status"]').selectOption("synced");
    await resourceDialog.getByLabel("Status detail").fill("CI recovered");
    await resourceDialog.getByRole("button", { name: "Update" }).click();
    await expect(page.locator("#toast")).toHaveText("Resource updated");
    await expect(page.locator(".resource-row", { hasText: "Acceptance CI" })).toContainText("CI recovered");
    await page.locator(".resource-row", { hasText: "Acceptance CI" }).click();
    await resourceDialog.getByRole("button", { name: "Detach" }).click();
    await page.locator("#confirm-dialog").getByRole("button", { name: "Detach" }).click();
    await expect(page.locator("#toast")).toHaveText("Resource detached");
    await expect(page.locator(".resource-row", { hasText: "Acceptance CI" })).toHaveCount(0);

    const firstTitle = `Human workflow ${project.key}`;
    const secondTitle = `Agent workflow ${project.key}`;
    await createWorkItem(page, {
      title: firstTitle,
      description: "Human-owned work with a persisted blocker",
      status: "ready",
      priority: "urgent",
      assignee_type: "human",
      assignee_name: "Lina",
      repository: "Acceptance repository",
      branch: `feat/${project.key.toLowerCase()}`,
      blocked: true,
      blocker: "Needs architecture decision"
    });

    const firstCard = page.locator(".work-card", { hasText: firstTitle });
    await firstCard.click();
    await page.getByRole("button", { name: "Edit" }).click();
    const editDialog = page.locator("#work-item-dialog");
    await editDialog.getByLabel("Pull request URL").fill("https://github.com/wxxb789/symmetry/pull/42");
    await editDialog.getByLabel("CI").selectOption("passed");
    await editDialog.locator('select[name="review_status"]').selectOption("required");
    await editDialog.getByRole("button", { name: "Save" }).click();
    await expect(page.locator("#toast")).toHaveText("Work item updated");
    await expect(page.locator("#detail-content")).toContainText("Passed");
    await expect(page.locator("#detail-content")).toContainText("Required");
    await page.getByRole("button", { name: "Close details" }).click();
    await createWorkItem(page, {
      title: secondTitle,
      description: "Agent-owned work ready for execution",
      status: "ready",
      priority: "high",
      assignee_type: "agent",
      agent_profile: "codex",
      workspace: "acceptance",
      repository: "Acceptance repository"
    });

    const secondCard = page.locator(".work-card", { hasText: secondTitle });
    await secondCard.getByTitle("Move up").click();
    await page.reload();
    await expect(page.locator("#project-title")).toHaveText(project.name);
    await expect(page.locator("[data-status='ready'] .work-card h3")).toHaveText([secondTitle, firstTitle]);

    await page.locator("#work-search").fill("Human workflow");
    await expect(page.locator(".work-card", { hasText: firstTitle })).toBeVisible();
    await expect(page.locator(".work-card", { hasText: secondTitle })).toBeHidden();
    await page.locator("#work-search").fill("");
    await page.getByRole("button", { name: "Attention", exact: true }).click();
    await expect(page.locator(".work-card", { hasText: firstTitle })).toBeVisible();
    await expect(page.locator(".work-card", { hasText: secondTitle })).toBeHidden();
    await page.getByRole("button", { name: "All", exact: true }).click();

    await firstCard.click();
    await page.locator("#detail-status-select").selectOption("review");
    await expect(page.locator("#detail-status-select")).toHaveValue("review");
    await page.locator("#detail-status-select").selectOption("done");
    await expect(page.locator("#detail-status-select")).toHaveValue("done");
    await expect(page.getByRole("button", { name: "Start run" })).toHaveCount(0);
    await page.getByRole("button", { name: "Close details" }).click();

    await secondCard.focus();
    await secondCard.press("Enter");
    await expect(page.locator("#detail-title")).toHaveText(secondTitle);
    await page.locator("#detail-status-select").selectOption("review");
    await expect(page.locator("#detail-status-select")).toHaveValue("review");
    await expect(page.getByRole("button", { name: "Start run" })).toHaveCount(0);
    await page.getByRole("button", { name: "Close details" }).click();
    await expect(secondCard).toBeFocused();

    const persisted = await selectedProject(page);
    expect(persisted.resources).toEqual([
      expect.objectContaining({ name: "Acceptance repository", status: "healthy", sync_status: "synced" })
    ]);
    expect(persisted.work_items.find((item) => item.title === firstTitle)).toEqual(
      expect.objectContaining({
        status: "done",
        priority: "urgent",
        blocked: true,
        blocker: "Needs architecture decision",
        pull_request_url: "https://github.com/wxxb789/symmetry/pull/42",
        ci_status: "passed",
        review_status: "required",
        assignee: expect.objectContaining({ type: "human", name: "Lina" }),
        execution: null
      })
    );
    expect(persisted.work_items.find((item) => item.title === secondTitle)).toEqual(
      expect.objectContaining({ status: "review", assignee: expect.objectContaining({ type: "agent" }) })
    );

    const dimensions = await page.evaluate(() => ({
      clientWidth: document.documentElement.clientWidth,
      scrollWidth: document.documentElement.scrollWidth
    }));
    expect(dimensions.scrollWidth).toBeLessThanOrEqual(dimensions.clientWidth);
  });

  test("project archive is read-only and restore keeps workspace state", async ({ page }) => {
    const project = uniqueProject("AR");
    await login(page);
    await createProject(page, project);
    await attachResource(page, {
      kind: "repository",
      status: "healthy",
      sync_status: "synced",
      name: "Retained repository",
      provider: "GitHub",
      external_ref: "example/retained",
      url: "https://example.invalid/example/retained",
      status_message: "Available"
    });
    await createWorkItem(page, {
      title: `Retained work ${project.key}`,
      description: "Must survive project archive and restore",
      status: "backlog",
      priority: "medium",
      assignee_type: "unassigned"
    });

    await page.getByRole("button", { name: "Project settings" }).click();
    const dialog = page.locator("#project-dialog");
    await dialog.getByLabel("Status").selectOption("archived");
    await dialog.getByRole("button", { name: "Save" }).click();
    await expect(page.locator("#project-state")).toHaveText("Archived");
    await expect(page.getByRole("button", { name: "New work item" })).toBeDisabled();
    await page.getByRole("link", { name: "Resources" }).click();
    await expect(page.getByRole("button", { name: "Attach resource" })).toBeDisabled();
    await page.locator(".resource-row", { hasText: "Retained repository" }).click();
    await expect(page.locator("#resource-dialog").getByRole("button", { name: "Update" })).toBeDisabled();
    await expect(page.locator("#delete-resource-button")).toBeHidden();
    await page.locator("#resource-dialog").press("Escape");
    await page.getByRole("link", { name: "Board" }).click();
    await expect(page.locator(".work-card [data-move]")).toHaveCount(0);

    await page.reload();
    await expect(page.locator("#project-state")).toHaveText("Archived");
    await expect(page.locator(".work-card", { hasText: `Retained work ${project.key}` })).toBeVisible();

    await page.getByRole("button", { name: "Project settings" }).click();
    await dialog.getByLabel("Status").selectOption("active");
    await dialog.getByRole("button", { name: "Save" }).click();
    await expect(page.locator("#project-state")).toHaveText("Active");
    await expect(page.getByRole("button", { name: "New work item" })).toBeEnabled();
  });

  test("login, CSRF rejection, and logout enforce the browser session boundary", async ({ page }) => {
    allowBrowserError(page, /status of 401/);
    allowBrowserError(page, /status of 403/);
    await page.goto("/portal/login");
    await page.getByLabel("Operator token").fill("invalid-token");
    await page.getByRole("button", { name: "Open workspace" }).click();
    await expect(page.getByRole("alert")).toContainText("Operator token is invalid");

    await login(page);
    const noCsrfStatus = await page.evaluate(async () => {
      const response = await fetch("/portal/api/projects", {
        method: "POST",
        credentials: "same-origin",
        headers: { "Content-Type": "application/json", Accept: "application/json" },
        body: JSON.stringify({ name: "Rejected", key: "NOPE" })
      });
      return response.status;
    });
    expect(noCsrfStatus).toBe(403);

    await page.getByRole("button", { name: "Sign out" }).click();
    await expect(page).toHaveURL(/\/portal\/login$/);
    await page.goto("/portal");
    await expect(page).toHaveURL(/\/portal\/login$/);
  });

  test("keyboard navigation, dialogs, and drawer remain usable without overflow", async ({ page }) => {
    const project = uniqueProject("KB");
    const title = `Keyboard work ${project.key}`;
    await login(page);
    await createProject(page, project);
    await createWorkItem(page, {
      title,
      description: "Exercise the portal without pointer-only controls.",
      status: "ready",
      priority: "medium",
      assignee_type: "unassigned",
      blocked: true,
      blocker: "Keyboard review required"
    });

    const activityLink = page.getByRole("link", { name: "Activity" });
    await activityLink.focus();
    await activityLink.press("Enter");
    await expect(page.locator('[data-view-panel="activity"]')).toBeVisible();

    const resourcesLink = page.getByRole("link", { name: "Resources" });
    await resourcesLink.focus();
    await resourcesLink.press("Enter");
    const attachButton = page.getByRole("button", { name: "Attach resource" });
    await attachButton.focus();
    await attachButton.press("Enter");
    await expect(page.locator("#resource-dialog")).toBeVisible();
    await page.locator("#resource-dialog").press("Escape");
    await expect(page.locator("#resource-dialog")).not.toBeVisible();

    const boardLink = page.getByRole("link", { name: "Board" });
    await boardLink.focus();
    await boardLink.press("Enter");
    const newItemButton = page.getByRole("button", { name: "New work item" });
    await newItemButton.focus();
    await newItemButton.press("Enter");
    const workItemDialog = page.locator("#work-item-dialog");
    await expect(workItemDialog).toBeVisible();
    const dialogScrollOwners = await workItemDialog.evaluate((dialog) =>
      [dialog, ...dialog.querySelectorAll("*")]
        .filter((element) => {
          const overflowY = getComputedStyle(element).overflowY;
          return ["auto", "scroll"].includes(overflowY) && element.scrollHeight > element.clientHeight + 1;
        })
        .map((element) => element.className)
    );
    expect(dialogScrollOwners).toEqual(["dialog-body"]);
    await workItemDialog.press("Escape");
    await expect(workItemDialog).not.toBeVisible();

    const projectSettings = page.getByRole("button", { name: "Project settings" });
    await projectSettings.focus();
    await projectSettings.press("Enter");
    await expect(page.locator("#project-dialog")).toBeVisible();
    await page.locator("#project-dialog").press("Escape");
    await expect(page.locator("#project-dialog")).not.toBeVisible();

    const card = page.locator(".work-card", { hasText: title });
    await card.focus();
    await card.press("Enter");
    await expect(page.locator("#detail-drawer")).toHaveAttribute("aria-hidden", "false");
    await expect(page.locator("#detail-title")).toHaveText(title);
    const closeDetails = page.getByRole("button", { name: "Close details" });
    await expect(closeDetails).toBeFocused();
    await closeDetails.press("Shift+Tab");
    expect(await page.evaluate(() => document.querySelector("#detail-drawer").contains(document.activeElement))).toBe(true);
    await page.keyboard.press("Tab");
    await expect(closeDetails).toBeFocused();
    await closeDetails.press("Enter");
    await expect(page.locator("#detail-drawer")).toHaveAttribute("aria-hidden", "true");
    await expect(card).toBeFocused();

    const attention = page.locator("#attention-list [data-attention-id]");
    await attention.focus();
    await attention.press("Enter");
    await expect(closeDetails).toBeFocused();
    await closeDetails.press("Enter");
    await expect(attention).toBeFocused();

    const activityTitle = `Keyboard activity ${project.key}`;
    await createWorkItem(page, {
      title: activityTitle,
      description: "Open Activity details from a durable queued run.",
      status: "ready",
      priority: "medium",
      assignee_type: "agent",
      agent_profile: "codex",
      workspace: "acceptance"
    });
    await page.locator(".work-card", { hasText: activityTitle }).click();
    await page.getByRole("button", { name: "Start run" }).click();
    await closeDetails.click();
    await page.getByRole("link", { name: "Activity" }).click();
    const activity = page.locator("[data-work-item-id]", { hasText: activityTitle });
    await expect(activity.locator("time")).toHaveAttribute("datetime", /.+/);
    await expect(activity).toContainText(/Queued for|Elapsed|Duration/);
    await activity.focus();
    await activity.press("Enter");
    await expect(closeDetails).toBeFocused();
    await closeDetails.press("Enter");
    await expect(activity).toBeFocused();

    await activity.press("Enter");
    await page.getByRole("button", { name: "Cancel run" }).click();
    await expect(page.locator("#detail-content")).toContainText("Cancelled");
    await page.getByRole("button", { name: "Edit" }).click();
    const reassignmentDialog = page.locator("#work-item-dialog");
    await reassignmentDialog.getByLabel("Owner type").selectOption("human");
    await reassignmentDialog.getByLabel("Owner", { exact: true }).fill("Lina");
    await reassignmentDialog.getByRole("button", { name: "Save" }).click();
    await expect(page.getByRole("button", { name: "Retry" })).toHaveCount(0);
    await closeDetails.click();
    await expect(page.locator("[data-work-item-id]", { hasText: activityTitle })).toContainText("Execution stopped");

    const dimensions = await page.evaluate(() => ({
      clientWidth: document.documentElement.clientWidth,
      scrollWidth: document.documentElement.scrollWidth
    }));
    expect(dimensions.scrollWidth).toBeLessThanOrEqual(dimensions.clientWidth);
  });
});

test.describe("Goal 2 concurrency", () => {
  test.skip(({ isMobile }) => isMobile, "The stale-write proof uses two desktop contexts.");

  test("a delayed project mutation cannot restore an abandoned UI context", async ({ page }) => {
    const firstProject = uniqueProject("MA");
    const secondProject = uniqueProject("MB");
    await login(page);
    const firstId = await createProject(page, firstProject);
    const secondId = await createProject(page, secondProject);
    await page.locator("#project-switcher").selectOption(firstId);
    await expect(page.locator("#project-title")).toHaveText(firstProject.name);

    let releaseMutation;
    let markMutationStarted;
    const mutationStarted = new Promise((resolve) => { markMutationStarted = resolve; });
    const mutationRelease = new Promise((resolve) => { releaseMutation = resolve; });

    await page.route(`**/portal/api/projects/${firstId}`, async (route) => {
      markMutationStarted();
      await mutationRelease;
      await route.continue();
    }, { times: 1 });

    await page.getByRole("button", { name: "Project settings" }).click();
    const dialog = page.locator("#project-dialog");
    const updatedName = `${firstProject.name} updated`;
    await dialog.getByLabel("Name").fill(updatedName);
    await dialog.getByRole("button", { name: "Save" }).click();
    await mutationStarted;
    await dialog.press("Escape");
    await page.locator("#project-switcher").selectOption(secondId);
    await expect(page.locator("#project-title")).toHaveText(secondProject.name);

    releaseMutation();
    await page.waitForTimeout(300);
    await expect(page.locator("#project-title")).toHaveText(secondProject.name);
    await expect(page.locator("#project-switcher")).toHaveValue(secondId);

    const persisted = await portalRequest(page, `/portal/api/workspace?project_id=${firstId}`);
    expect(persisted.status).toBe(200);
    expect(persisted.body.projects.find((project) => project.id === firstId).name).toBe(updatedName);
  });

  test("two browser contexts reject a stale project update", async ({ browser, baseURL }) => {
    const firstContext = await browser.newContext({ baseURL });
    const secondContext = await browser.newContext({ baseURL });
    const first = await firstContext.newPage();
    const second = await secondContext.newPage();
    const project = uniqueProject("ST");
    monitorBrowserErrors(first);
    monitorBrowserErrors(second);
    allowBrowserError(second, /status of 409/);

    try {
      await login(first);
      const projectId = await createProject(first, project);
      await login(second, `/portal?project_id=${projectId}#board`);

      await first.getByRole("button", { name: "Project settings" }).click();
      await second.getByRole("button", { name: "Project settings" }).click();
      const firstDialog = first.locator("#project-dialog");
      const secondDialog = second.locator("#project-dialog");
      await firstDialog.getByLabel("Name").fill(`${project.name} current`);
      await secondDialog.getByLabel("Name").fill(`${project.name} stale`);

      await firstDialog.getByRole("button", { name: "Save" }).click();
      await expect(first.locator("#project-title")).toHaveText(`${project.name} current`);
      await secondDialog.getByRole("button", { name: "Save" }).click();
      await expect(second.locator("#toast")).toHaveText("This item changed elsewhere. Current data was reloaded.");

      await expect(secondDialog).not.toBeVisible();
      await expect(second.locator("#project-title")).toHaveText(`${project.name} current`);
      const persisted = await portalRequest(first, `/portal/api/workspace?project_id=${projectId}`);
      expect(persisted.status).toBe(200);
      expect(persisted.body.projects.find((candidate) => candidate.id === projectId).name).toBe(`${project.name} current`);
    } finally {
      try {
        assertNoBrowserErrors(first, second);
      } finally {
        await firstContext.close();
        await secondContext.close();
      }
    }
  });

  test("two browser contexts reject a stale work-item update", async ({ browser, baseURL }) => {
    const firstContext = await browser.newContext({ baseURL });
    const secondContext = await browser.newContext({ baseURL });
    const first = await firstContext.newPage();
    const second = await secondContext.newPage();
    const project = uniqueProject("WI");
    const originalTitle = `Shared work ${project.key}`;
    monitorBrowserErrors(first);
    monitorBrowserErrors(second);
    allowBrowserError(second, /status of 409/);

    try {
      await login(first);
      const projectId = await createProject(first, project);
      await createWorkItem(first, {
        title: originalTitle,
        description: "Two operators edit the same work item.",
        status: "ready",
        priority: "medium",
        assignee_type: "unassigned"
      });
      await login(second, `/portal?project_id=${projectId}#board`);

      await first.locator(".work-card", { hasText: originalTitle }).click();
      await second.locator(".work-card", { hasText: originalTitle }).click();
      await first.getByRole("button", { name: "Edit" }).click();
      await second.getByRole("button", { name: "Edit" }).click();
      const firstDialog = first.locator("#work-item-dialog");
      const secondDialog = second.locator("#work-item-dialog");
      const currentTitle = `${originalTitle} current`;
      await firstDialog.getByLabel("Title").fill(currentTitle);
      await secondDialog.getByLabel("Title").fill(`${originalTitle} stale`);

      await firstDialog.getByRole("button", { name: "Save" }).click();
      await expect(first.locator("#detail-title")).toHaveText(currentTitle);
      await secondDialog.getByRole("button", { name: "Save" }).click();
      await expect(second.locator("#toast")).toHaveText("This item changed elsewhere. Current data was reloaded.");
      await expect(secondDialog).not.toBeVisible();
      await expect(second.locator("#detail-title")).toHaveText(currentTitle);

      const persisted = await portalRequest(first, `/portal/api/workspace?project_id=${projectId}`);
      expect(persisted.status).toBe(200);
      expect(persisted.body.projects.find((candidate) => candidate.id === projectId).work_items).toEqual(
        expect.arrayContaining([expect.objectContaining({ title: currentTitle })])
      );
    } finally {
      try {
        assertNoBrowserErrors(first, second);
      } finally {
        await firstContext.close();
        await secondContext.close();
      }
    }
  });

  test("a delayed detail response cannot overwrite a newer drawer selection", async ({ page }) => {
    const project = uniqueProject("RQ");
    await login(page);
    await createProject(page, project);
    const firstTitle = `Slow detail ${project.key}`;
    const secondTitle = `Current detail ${project.key}`;

    for (const title of [firstTitle, secondTitle]) {
      await createWorkItem(page, {
        title,
        description: title,
        status: "ready",
        priority: "medium",
        assignee_type: "unassigned"
      });
    }

    const projectState = await selectedProject(page);
    const firstId = projectState.work_items.find((item) => item.title === firstTitle).id;
    let releaseFirst;
    let markFirstStarted;
    const firstStarted = new Promise((resolve) => { markFirstStarted = resolve; });
    const firstRelease = new Promise((resolve) => { releaseFirst = resolve; });

    await page.route(`**/portal/api/work-items/${firstId}`, async (route) => {
      markFirstStarted();
      await firstRelease;
      await route.continue();
    }, { times: 1 });

    await page.locator(".work-card", { hasText: firstTitle }).click();
    await firstStarted;
    await page.getByRole("button", { name: "Close details" }).click();
    await page.locator(".work-card", { hasText: secondTitle }).click();
    await expect(page.locator("#detail-title")).toHaveText(secondTitle);
    releaseFirst();
    await page.waitForTimeout(250);
    await expect(page.locator("#detail-title")).toHaveText(secondTitle);
  });

  test("background refresh preserves drawer input, focus, and expanded raw history", async ({ page }) => {
    const project = uniqueProject("RF");
    const title = `Refresh-safe detail ${project.key}`;
    await login(page);
    await createProject(page, project);
    await createWorkItem(page, {
      title,
      description: "Keep local drawer interaction state across polling.",
      status: "ready",
      priority: "medium",
      assignee_type: "unassigned"
    });

    const item = (await selectedProject(page)).work_items.find((candidate) => candidate.title === title);
    let detailRequests = 0;

    await page.route(`**/portal/api/work-items/${item.id}/timeline?before=*`, async (route) => {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          items: [{
            source: "event",
            run_id: "00000000-0000-0000-0000-000000000201",
            generation: 1,
            recorded_at: "2026-09-04T08:00:00Z",
            data: { event_id: "00000000-0000-0000-0000-000000000202", kind: "progress", payload: { message: "older-marker" } }
          }],
          next_before: null
        })
      });
    });

    await page.route(`**/portal/api/work-items/${item.id}`, async (route) => {
      const response = await route.fetch();
      const body = await response.json();
      detailRequests += 1;
      body.work_item.execution = {
        task_id: "00000000-0000-0000-0000-000000000203",
        run_id: "00000000-0000-0000-0000-000000000201",
        runtime_id: null,
        generation: 1,
        state: "waiting_for_input",
        waiting: {
          run_id: "00000000-0000-0000-0000-000000000201",
          generation: 1,
          transition_id: "00000000-0000-0000-0000-000000000204",
          question: "Choose a deployment target",
          payload: {},
          recorded_at: "2026-09-04T08:00:00Z"
        },
        latest_command: null,
        result: null,
        failure: null,
        timing: { state_since: "2026-09-04T08:00:00Z", started_at: "2026-09-04T07:59:00Z", finished_at: null },
        can_cancel: true,
        can_retry: false,
        intent_locked: true
      };
      body.raw.timeline = [{
        source: "event",
        run_id: "00000000-0000-0000-0000-000000000201",
        generation: 1,
        recorded_at: "2026-09-04T08:01:00Z",
        data: { event_id: "00000000-0000-0000-0000-000000000205", kind: "waiting_for_input", payload: { question: "Choose a deployment target" } }
      }];
      body.raw.next_before = "older-cursor";
      await route.fulfill({ response, json: body });
    });

    await page.locator(".work-card", { hasText: title }).click();
    const answer = page.getByLabel("Response");
    await answer.fill("staging-blue");
    await page.locator("details.raw-details summary").click();
    await page.getByRole("button", { name: "Load older" }).click();
    await expect(page.locator("#raw-execution-data")).toContainText("older-marker");
    await answer.focus();
    await answer.evaluate((input) => input.setSelectionRange(7, 7));

    await expect.poll(() => detailRequests, { timeout: 8_000 }).toBeGreaterThan(1);
    await expect(answer).toHaveValue("staging-blue");
    await expect(answer).toBeFocused();
    expect(await answer.evaluate((input) => input.selectionStart)).toBe(7);
    await expect(page.locator("details.raw-details")).toHaveAttribute("open", "");
    await expect(page.locator("#raw-execution-data")).toContainText("older-marker");
  });

  test("a delayed workspace response cannot replace the latest project", async ({ page }) => {
    const firstProject = uniqueProject("WA");
    const secondProject = uniqueProject("WB");
    await login(page);
    const firstId = await createProject(page, firstProject);
    const secondId = await createProject(page, secondProject);

    let releaseFirst;
    let markFirstStarted;
    const firstStarted = new Promise((resolve) => { markFirstStarted = resolve; });
    const firstRelease = new Promise((resolve) => { releaseFirst = resolve; });

    await page.route(`**/portal/api/workspace?project_id=${firstId}`, async (route) => {
      markFirstStarted();
      await firstRelease;
      await route.continue();
    }, { times: 1 });

    await page.locator("#project-switcher").selectOption(firstId);
    await firstStarted;
    await page.locator("#project-switcher").selectOption(secondId);
    await expect(page.locator("#project-title")).toHaveText(secondProject.name);
    releaseFirst();
    await page.waitForTimeout(250);
    await expect(page.locator("#project-title")).toHaveText(secondProject.name);
    await expect(page.locator("#project-switcher")).toHaveValue(secondId);
  });
});
