import { allowBrowserError, expect, test } from "./fixtures.mjs";

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

async function login(page, path = "/portal/classic") {
  await page.goto("/portal/login?return_to=%2Fportal%2Fclassic");
  await page.getByLabel("Operator token").fill(operatorToken);
  await Promise.all([
    page.waitForURL(/\/portal\/classic(?:\?|#|$)/),
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

// The classic project and work-item planning dialogs are retired in favour of
// the /portal/projects and /portal/work/:workItemId planning surfaces (0007c,
// portal-work-planning.spec.mjs); seeding for the remaining classic checks now
// goes through the authenticated Portal API. Resource attach, update and detach
// stays on the classic resource dialog (0007e).
async function createProject(page, project) {
  const response = await portalRequest(page, "/portal/api/projects", {
    method: "POST",
    body: JSON.stringify({
      name: project.name,
      key: project.key,
      default_agent_profile: "codex",
      default_workspace: "acceptance",
      description: "Feature-level browser acceptance workspace"
    })
  });
  expect(response.status, JSON.stringify(response.body)).toBe(201);
  const projectId = response.body.project.id;
  await page.goto(`/portal/classic?project_id=${projectId}#board`);
  await expect(page.locator("#project-title")).toHaveText(project.name);
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
  await dialog.getByLabel("Provider", { exact: true }).fill(resource.provider);
  await dialog.getByLabel("Reference", { exact: true }).fill(resource.external_ref);
  if (resource.url) await dialog.getByLabel("URL").fill(resource.url);
  if (resource.status_message) await dialog.getByLabel("Status detail").fill(resource.status_message);
  await dialog.getByRole("button", { name: "Attach" }).click();
  await expect(page.locator("#toast")).toHaveText("Resource attached");
  await expect(page.locator("#resource-table")).toContainText(resource.name);
}

async function createWorkItem(page, item) {
  const projectId = new URL(page.url()).searchParams.get("project_id");
  expect(projectId).toBeTruthy();
  const response = await portalRequest(page, `/portal/api/projects/${projectId}/work-items`, {
    method: "POST",
    body: JSON.stringify(item)
  });
  expect(response.status, JSON.stringify(response.body)).toBe(201);
  await page.getByRole("link", { name: "Board" }).click();
  await page.locator("#refresh-button").click();
  await expect(page.locator(".work-card", { hasText: item.title })).toBeVisible();
}

async function selectedProject(page) {
  const response = await portalRequest(page, `/portal/api/workspace${new URL(page.url()).search}`);
  expect(response.status).toBe(200);
  return response.body.projects.find((project) => project.id === response.body.selected_project_id);
}

test.describe("Goal 2 feature acceptance", () => {
  test("classic resource attach, update and detach with truthful health", async ({ page }) => {
    const project = uniqueProject();
    await login(page);
    await createProject(page, project);
    await createWorkItem(page, {
      title: `Human workflow ${project.key}`,
      description: "Human-owned work seeded through the Portal API",
      status: "ready",
      priority: "urgent",
      assignee_type: "human",
      assignee_name: "Lina",
      blocked: true,
      blocker: "Needs architecture decision"
    });

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

    const persisted = await selectedProject(page);
    expect(persisted.resources).toEqual([
      expect.objectContaining({ name: "Acceptance repository", status: "healthy", sync_status: "synced" })
    ]);

    const dimensions = await page.evaluate(() => ({
      clientWidth: document.documentElement.clientWidth,
      scrollWidth: document.documentElement.scrollWidth
    }));
    expect(dimensions.scrollWidth).toBeLessThanOrEqual(dimensions.clientWidth);
  });

  // The classic detail-security check ("detail output encodes untrusted
  // statuses and rejects unsafe links") is retired here: the same
  // malicious-response injection probe now runs against /portal/work/:workItemId
  // in portal-work-views.spec.mjs, where the read surface moved.

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
    await page.goto("/portal/classic");
    await expect(page).toHaveURL(/\/portal\/login\?return_to=%2Fportal%2Fclassic$/);
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
    await expect(page.locator('[data-view-panel="board"]')).toBeVisible();

    // The classic work-card -> drawer keyboard walk is retired here: the list,
    // board and detail keyboard walk with focus return now runs on
    // /portal/projects/:projectId and /portal/work/:workItemId in
    // portal-work-views.spec.mjs. The dialog and control paths below stay.
    const closeDetails = page.getByRole("button", { name: "Close details" });

    const attention = page.locator("#attention-list [data-attention-id]");
    await attention.focus();
    await attention.press("Enter");
    await expect(closeDetails).toBeFocused();
    await closeDetails.press("Enter");
    await expect(attention).toBeFocused();

    // The classic work-detail execution control walk (queued run with a live
    // timer, Cancel run -> Cancelled, Retry availability and retry hidden after
    // reassignment, the Activity row's "Execution stopped") is retired here:
    // the start/cancel/retry flow, its rendered state and its server-side
    // gating now run on /portal/work/:workItemId in
    // portal-execution-control.spec.mjs and, against a live daemon, in
    // portal-live-daemon.spec.mjs. The reassignment edit path itself remains
    // classic-owned editing coverage (goal 0007c) and is not migrated here.

    const dimensions = await page.evaluate(() => ({
      clientWidth: document.documentElement.clientWidth,
      scrollWidth: document.documentElement.scrollWidth
    }));
    expect(dimensions.scrollWidth).toBeLessThanOrEqual(dimensions.clientWidth);
  });
});

test.describe("Goal 2 concurrency", () => {
  test.skip(({ isMobile }) => isMobile, "The stale-write proof uses two desktop contexts.");

  // The classic "a delayed detail response cannot overwrite a newer drawer
  // selection" race is retired here; portal-work-views.spec.mjs holds the real
  // captured detail response and proves the newer selection survives release.

  // The classic "an older history failure cannot notify or mutate a newer
  // drawer" race is retired here. It is covered twice on /portal/work/:workItemId:
  // by the always-on daemon-free synthetic fault-injection guard in
  // portal-work-views.spec.mjs (a generated contract fixture supplies the cursor;
  // it makes no real-history claim) and by the separate mandatory real-cursor
  // case in portal-live-daemon.spec.mjs.

  // The classic "background refresh preserves drawer input, focus, and expanded
  // raw history" case is retired here. Its migratable half — a waiting-input
  // draft, its caret and focus surviving a background refresh — now runs on
  // /portal/work/:workItemId in portal-execution-control.spec.mjs, against the
  // served waiting state rather than a classic drawer mock. The classic
  // raw-execution expander has no equivalent on the new surface: execution
  // history pagination is covered by portal-work-views.spec.mjs (older page,
  // error and retry) and by the real daemon cursor case in
  // portal-live-daemon.spec.mjs, so no assertion was migrated into a new
  // expander that the fixed design does not define. The classic editing path
  // (owner reassignment, dialogs) stays classic-owned under goal 0007c.

  // The classic "a delayed workspace response cannot replace the latest
  // project" race is retired here; portal-work-views.spec.mjs holds a real
  // captured workspace snapshot for the previous project and proves the shell
  // never renders it after the newer project has rendered.
});

// Goal 0007c retires the classic project and work-item planning coverage here:
// create, edit, archive/restore, board order/move, the two stale-update
// conflicts and the delayed-mutation race. The real-API acceptance for those
// lives in portal-work-planning.spec.mjs, run by
// browser/scripts/run-planning-tests.ps1 against the /portal/projects and
// /portal/work/:workItemId planning surfaces.
