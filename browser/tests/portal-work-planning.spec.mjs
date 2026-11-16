// Goal 0007c — safe project and work planning acceptance.
//
// Real authenticated fixtures and real mutations: projects and work items are
// seeded through the Portal API with the session CSRF token, and every write
// under test goes through the running control plane's own endpoints. Network
// interception appears only to inject the races the contract names, and it
// always replays or releases a real request:
//   1. holding a genuinely captured mutation response to prove a late write
//      never lands on the project the operator switched to;
//   2. aborting one real request at the transport to exercise the explicit
//      retry — the retry itself reaches the server, and both recorded bodies
//      are asserted equal;
//   3. two independent browser contexts for the optimistic-lock conflicts.
// The two unwritable item kinds (Goal-managed, external-tracker-bound) are
// prepared by scripts/ci/portal-planning-fixture.exs through Control's own
// changesets; every browser interaction with them is still real HTTP.
import { allowBrowserError, assertNoBrowserErrors, expect, monitorBrowserErrors, test } from "./fixtures.mjs";
import {
  api,
  attachResource,
  capture,
  createProject,
  createWorkItem,
  login,
  snapshotMetrics,
  uniqueKey,
  workspace
} from "./readable-fixtures.mjs";
import { applyGoalManaged, applyProviderOwned } from "./planning-fixtures.mjs";

// The suite runs only inside browser/scripts/run-planning-tests.ps1, which sets
// this marker plus the portal URL and the per-run operator token; a collection
// without them skips instead of half-running, and the harness turns a skipped
// summary into a failure.
const harnessEnvironment = ["SYMMETRY_PLANNING_HARNESS", "SYMMETRY_PORTAL_URL", "SYMMETRY_OPERATOR_TOKEN"];
const presentEnvironment = harnessEnvironment.filter((name) => Boolean(process.env[name]));
test.skip(presentEnvironment.length > 0 && presentEnvironment.length < harnessEnvironment.length,
  `${harnessEnvironment.join(", ")} must be set together`);
test.skip(!process.env.SYMMETRY_PLANNING_HARNESS,
  "run browser/scripts/run-planning-tests.ps1: the planning suite needs its per-run server, token and database");

// This suite's acceptance artifacts are its own `capture` screenshots and
// observation JSON. The on-failure trace flushes unreliably on the Windows host
// this gate runs on (`ENOENT .../traces/*.trace` at context close, which
// Playwright reports as the test's own error), so the trace is off here; the
// still-on "only-on-failure" screenshot, error context and video carry failure
// diagnosis.
test.use({ trace: "off" });

const ui = {
  newProject: "New project",
  projectSettings: "Project settings",
  restoreProject: "Restore project",
  newWorkItem: "New work item",
  editWorkItem: "Edit work item",
  create: "Create",
  save: "Save",
  cancel: "Cancel",
  tryAgain: "Try again",
  refresh: "Refresh",
  closeDetail: "Close work detail",
  backToProject: /^Back to projects?$/,
  board: "Work board",
  project: "Project"
};

// The pages render more than one role=status region (the notice line and the
// sr-only refresh announcer), so every notice assertion is scoped by its text.
const statusNotice = (scope, text) => scope.locator('p[role="status"]', { hasText: text });
const alertNotice = (scope, text) => scope.locator('p[role="alert"]', { hasText: text });

const projectUrl = (id, params = {}) => {
  const query = new URLSearchParams(params).toString();
  return `/portal/projects/${id}${query ? `?${query}` : ""}`;
};
const workUrl = (id, params = {}) => {
  const query = new URLSearchParams(params).toString();
  return `/portal/work/${id}${query ? `?${query}` : ""}`;
};

const detailSurface = (page, isMobile, title) =>
  isMobile ? page.locator("#main-content") : page.getByRole("dialog", { name: title });

async function seedProject(page, { prefix, items = [], resources = [] }) {
  const key = uniqueKey(prefix);
  const project = await createProject(page, { key, name: `Planning ${key}`, description: `${prefix} planning fixture` });
  const attached = [];
  for (const resource of resources) attached.push(await attachResource(page, project.id, resource));
  const created = new Map();
  for (const item of items) created.set(item.title, await createWorkItem(page, project.id, item));
  return { ...project, resources: attached, items: created };
}

async function storedProject(page, projectId) {
  const response = await workspace(page, projectId);
  expect(response.status, JSON.stringify(response.body)).toBe(200);
  return response.body.projects.find((candidate) => candidate.id === projectId);
}

async function storedItem(page, projectId, title) {
  const project = await storedProject(page, projectId);
  expect(project, `project ${projectId} in the workspace read`).toBeTruthy();
  const item = project.work_items.find((candidate) => candidate.title === title);
  expect(item, `work item "${title}" in the workspace read`).toBeTruthy();
  return item;
}

async function projectIdOf(page, name) {
  return page.evaluate(async (projectName) => {
    const response = await fetch("/portal/api/workspace", { credentials: "same-origin", headers: { Accept: "application/json" } });
    const body = await response.json();
    return body.projects.find((project) => project.name === projectName)?.id ?? "";
  }, name);
}

async function closeDetail(page, isMobile) {
  if (isMobile) await page.getByRole("link", { name: ui.backToProject }).click();
  else await page.getByRole("button", { name: ui.closeDetail }).click();
}

const boardCard = (page, title) => page.getByRole("link", { name: title });
const columnOf = (page, title) => page.locator("[data-board-card]", { has: boardCard(page, title) }).getAttribute("data-board-column");
const focusedLabel = (page) => page.evaluate(() => document.activeElement?.getAttribute("aria-label") ?? "");
const readyColumnTitles = async (page, projectId) =>
  (await storedProject(page, projectId)).work_items
    .filter((item) => item.status === "ready")
    .sort((left, right) => left.position - right.position)
    .map((item) => item.title);

async function moveByKeyboard(page, title, key) {
  const card = boardCard(page, title);
  await card.focus();
  await page.keyboard.press(`Shift+${key}`);
}

test.describe("planning project mutations", () => {
  test("creates, edits, archives and restores a project on real data", async ({ page, isMobile }, testInfo) => {
    await login(page);
    await page.goto("/portal/projects");
    const project = { key: uniqueKey("PC"), name: `Planning created ${uniqueKey("P")}` };

    await page.getByRole("button", { name: ui.newProject }).click();
    const dialog = page.getByRole("dialog", { name: "New project" });
    await dialog.getByLabel("Name").fill(project.name);
    await dialog.getByLabel("Key").fill(project.key);
    await dialog.getByLabel("Default agent").fill("default");
    await dialog.getByLabel("Workspace").fill("primary");
    await dialog.getByLabel("Description").fill("Created through the planning surface");
    await dialog.getByRole("button", { name: ui.create }).click();
    await expect(statusNotice(page, "Project created.")).toBeVisible();
    const row = page.locator(".project-card", { hasText: project.name });
    await expect(row).toBeVisible();
    const projectId = await projectIdOf(page, project.name);
    expect(projectId).not.toBe("");
    const created = await storedProject(page, projectId);
    expect(created.name).toBe(project.name);
    expect(created.description).toBe("Created through the planning surface");
    expect(created.default_workspace).toBe("primary");

    await row.click();
    await expect(page.getByRole("heading", { level: 1, name: project.name })).toBeVisible();
    await capture(page, testInfo, "planning-project-created", { surface: "project", url: page.url(), project: project.name });

    const renamed = `${project.name} renamed`;
    await page.getByRole("button", { name: ui.projectSettings }).click();
    const settings = page.getByRole("dialog", { name: "Project settings" });
    await settings.getByLabel("Name").fill(renamed);
    await settings.getByRole("button", { name: ui.save }).click();
    await expect(statusNotice(page, "Project updated.")).toBeVisible();
    await expect(page.getByRole("heading", { level: 1, name: renamed })).toBeVisible();
    expect((await storedProject(page, projectId)).name).toBe(renamed);

    await page.getByRole("button", { name: ui.projectSettings }).click();
    const archive = page.getByRole("dialog", { name: "Project settings" });
    await archive.getByLabel("Status").selectOption("archived");
    await archive.getByRole("button", { name: ui.save }).click();
    await expect(statusNotice(page, "Project archived.")).toBeVisible();
    await expect(page.getByText(/This project is archived/)).toBeVisible();
    await expect(page.getByRole("button", { name: ui.newWorkItem })).toHaveCount(0);
    await expect(page.getByRole("button", { name: ui.projectSettings })).toHaveCount(0);
    expect((await storedProject(page, projectId)).status).toBe("archived");
    await capture(page, testInfo, "planning-project-archived", { surface: "project archived", url: page.url() });

    await page.getByRole("button", { name: ui.restoreProject }).click();
    await expect(statusNotice(page, "Project restored.")).toBeVisible();
    await expect(page.getByRole("button", { name: ui.newWorkItem })).toBeVisible();
    await expect(page.getByRole("button", { name: ui.projectSettings })).toBeVisible();
    expect((await storedProject(page, projectId)).status).toBe("active");
    await capture(page, testInfo, "planning-project-restored", { surface: "project restored", url: page.url() });
    const snapshot = await snapshotMetrics(page, projectId);
    expect(snapshot.status).toBe(200);
  });

  test("refuses a duplicate project key with the server's field error and keeps the draft", async ({ page }) => {
    await login(page);
    // The duplicate key is the point of the case: the 422 is expected noise.
    allowBrowserError(page, /status of 422/);
    const existing = await seedProject(page, { prefix: "PK" });
    await page.goto("/portal/projects");
    await page.getByRole("button", { name: ui.newProject }).click();
    const dialog = page.getByRole("dialog", { name: "New project" });
    await dialog.getByLabel("Name").fill(`Planning duplicate ${uniqueKey("D")}`);
    await dialog.getByLabel("Key").fill(existing.key);
    await dialog.getByRole("button", { name: ui.create }).click();
    await expect(dialog.getByText("has already been taken")).toBeVisible();
    await expect(dialog).toBeVisible();
    await expect(dialog.getByLabel("Key")).toHaveValue(existing.key);
    const body = await page.evaluate(async () => {
      const response = await fetch("/portal/api/workspace", { credentials: "same-origin", headers: { Accept: "application/json" } });
      return response.json();
    });
    expect(body.projects.filter((project) => project.key === existing.key)).toHaveLength(1);
  });

  test("submits a project and a work item once on a double submit", async ({ page }) => {
    await login(page);
    await page.goto("/portal/projects");
    const projectRequests = [];
    const itemRequests = [];
    page.on("request", (request) => {
      const path = new URL(request.url()).pathname;
      if (request.method() === "POST" && path === "/portal/api/projects") projectRequests.push(path);
      if (request.method() === "POST" && /^\/portal\/api\/projects\/[^/]+\/work-items$/.test(path)) itemRequests.push(path);
    });
    const project = { key: uniqueKey("PB"), name: `Planning double ${uniqueKey("B")}` };
    await page.getByRole("button", { name: ui.newProject }).click();
    const dialog = page.getByRole("dialog", { name: "New project" });
    await dialog.getByLabel("Name").fill(project.name);
    await dialog.getByLabel("Key").fill(project.key);
    // Two submits in one task: the second is the same-tick duplicate a queued
    // submit event would deliver, and the single-flight guard must stop it.
    await dialog.locator("form").evaluate((form) => { form.requestSubmit(); form.requestSubmit(); });
    await expect(statusNotice(page, "Project created.")).toBeVisible();
    expect(projectRequests).toHaveLength(1);

    const row = page.locator(".project-card", { hasText: project.name });
    await row.click();
    await expect(page.getByRole("heading", { level: 1, name: project.name })).toBeVisible();
    await page.getByRole("button", { name: ui.newWorkItem }).click();
    const itemDialog = page.getByRole("dialog", { name: "New work item" });
    await itemDialog.getByLabel("Title").fill(`Double submitted ${project.key}`);
    await itemDialog.locator("form").evaluate((form) => { form.requestSubmit(); form.requestSubmit(); });
    await expect(statusNotice(page, "Work item created.")).toBeVisible();
    expect(itemRequests).toHaveLength(1);

    const projectId = await projectIdOf(page, project.name);
    const stored = await storedProject(page, projectId);
    expect(stored.work_items.filter((item) => item.title === `Double submitted ${project.key}`)).toHaveLength(1);
  });

  test("re-syncs to the newest project after a stale update in two contexts", async ({ browser, baseURL }) => {
    const first = await browser.newContext({ baseURL });
    const second = await browser.newContext({ baseURL });
    const authoring = await first.newPage();
    const competing = await second.newPage();
    monitorBrowserErrors(authoring);
    monitorBrowserErrors(competing);
    allowBrowserError(competing, /status of 409/);
    try {
      await login(authoring);
      const project = await seedProject(authoring, { prefix: "PS" });
      await login(competing, projectUrl(project.id));
      // The authoring page lands on Attention, which carries no project
      // settings control; both writers must be on the project surface.
      await authoring.goto(projectUrl(project.id));

      await authoring.getByRole("button", { name: ui.projectSettings }).click();
      await competing.getByRole("button", { name: ui.projectSettings }).click();
      const authoringDialog = authoring.getByRole("dialog", { name: "Project settings" });
      const competingDialog = competing.getByRole("dialog", { name: "Project settings" });
      await authoringDialog.getByLabel("Name").fill(`${project.name} current`);
      await competingDialog.getByLabel("Name").fill(`${project.name} stale`);

      await authoringDialog.getByRole("button", { name: ui.save }).click();
      await expect(statusNotice(authoring, "Project updated.")).toBeVisible();
      await competingDialog.getByRole("button", { name: ui.save }).click();
      await expect(alertNotice(competing, "resource changed since it was loaded (stale)")).toBeVisible();
      await expect(competingDialog).toHaveCount(0);
      await expect(competing.getByRole("heading", { level: 1, name: `${project.name} current` })).toBeVisible();
      expect((await storedProject(authoring, project.id)).name).toBe(`${project.name} current`);
    } finally {
      try {
        assertNoBrowserErrors(authoring, competing);
      } finally {
        await first.close();
        await second.close();
      }
    }
  });
});

test.describe("planning work-item mutations", () => {
  test("creates a work item with the project's resources, edits it and moves status through the board", async ({ page, isMobile }, testInfo) => {
    await login(page);
    const project = await seedProject(page, {
      prefix: "WI",
      // The move target must exist as a rendered column: the board shows the
      // states the served rows actually use.
      items: [{ title: "Working in progress", status: "in_progress", priority: "medium", assignee_type: "unassigned" }],
      resources: [{ kind: "repository", name: "Planning repository", external_ref: "acme/planning" }]
    });
    const title = `Planned work ${project.key}`;
    await page.goto(projectUrl(project.id));
    await page.getByRole("button", { name: ui.newWorkItem }).click();
    const dialog = page.getByRole("dialog", { name: "New work item" });
    await dialog.getByLabel("Title").fill(title);
    await dialog.getByLabel("Description").fill("Created through the planning surface");
    await dialog.getByLabel("Status").selectOption("ready");
    await dialog.getByLabel("Priority").selectOption("urgent");
    await dialog.getByLabel("Owner type").selectOption("human");
    await dialog.getByLabel("Owner", { exact: true }).fill("Lina");
    await dialog.getByLabel("Repository").selectOption({ label: "Planning repository" });
    await dialog.getByLabel("Branch").fill(`feat/${project.key.toLowerCase()}`);
    await dialog.getByLabel("Blocked").check();
    await dialog.getByLabel("Blocker").fill("Needs architecture decision");
    await dialog.getByRole("button", { name: ui.create }).click();
    await expect(statusNotice(page, "Work item created.")).toBeVisible();

    const item = await storedItem(page, project.id, title);
    expect(item).toMatchObject({
      status: "ready",
      priority: "urgent",
      blocked: true,
      blocker: "Needs architecture decision",
      branch: `feat/${project.key.toLowerCase()}`,
      assignee: { type: "human", name: "Lina" }
    });
    expect(item.repository_resource_id).toBe(project.resources[0].id);
    await capture(page, testInfo, "planning-work-item-created", { surface: "list", url: page.url(), title });

    await page.getByRole("link", { name: title }).click();
    const detail = detailSurface(page, isMobile, title);
    await detail.getByRole("button", { name: ui.editWorkItem }).click();
    const editor = page.getByRole("dialog", { name: "Edit work item" });
    const edited = `${title} edited`;
    await editor.getByLabel("Title").fill(edited);
    await editor.getByLabel("Priority").selectOption("high");
    // Owner and manual delivery edits migrated from the retired classic dialog:
    // the same stored values are asserted below.
    await editor.getByLabel("Owner type").selectOption("human");
    await editor.getByLabel("Owner", { exact: true }).fill("Mira");
    await editor.getByLabel("Pull request URL").fill("https://example.invalid/pr/0007c");
    await editor.getByRole("button", { name: ui.save }).click();
    await expect(statusNotice(detail, "Work item updated.")).toBeVisible();
    await expect(detail.getByRole("heading", { level: 1, name: edited })).toBeVisible();
    const editedItem = await storedItem(page, project.id, edited);
    expect(editedItem.priority).toBe("high");
    expect(editedItem.assignee).toMatchObject({ type: "human", name: "Mira" });
    expect(editedItem.pull_request_url).toBe("https://example.invalid/pr/0007c");
    expect(editedItem.delivery.pull_request.value).toBe("https://example.invalid/pr/0007c");
    expect(editedItem.version).toBeGreaterThan(item.version);
    await capture(page, testInfo, "planning-work-item-edited", { surface: "detail", url: page.url(), title: edited });

    await closeDetail(page, isMobile);
    await page.goto(projectUrl(project.id, { view: "board" }));
    await moveByKeyboard(page, edited, "ArrowRight");
    await expect(statusNotice(page, /Moved to/)).toBeVisible();
    await expect.poll(() => columnOf(page, edited)).toBe("in_progress");
    await expect.poll(() => focusedLabel(page)).toBe(edited);
    expect((await storedItem(page, project.id, edited)).status).toBe("in_progress");
    await capture(page, testInfo, "planning-board-moved", { surface: "board", url: page.url(), column: "in_progress" });
  });

  test("refuses a whitespace title with the server's field error and keeps the draft", async ({ page }) => {
    await login(page);
    // The refused write is the point of the case: the 422 is expected noise.
    allowBrowserError(page, /status of 422/);
    const project = await seedProject(page, { prefix: "WV" });
    await page.goto(projectUrl(project.id));
    await page.getByRole("button", { name: ui.newWorkItem }).click();
    const dialog = page.getByRole("dialog", { name: "New work item" });
    await dialog.getByLabel("Title").fill(" ");
    await dialog.getByRole("button", { name: ui.create }).click();
    // The server trims the title, so the required-field message is the one it
    // serves for a whitespace-only title.
    await expect(dialog.getByText("can't be blank")).toBeVisible();
    await expect(dialog.getByText(/validation_failed/)).toBeVisible();
    await expect(dialog.getByLabel("Title")).toHaveValue(" ");
    expect((await storedProject(page, project.id)).work_items).toHaveLength(0);
  });

  test("replays the failed intent verbatim on an explicit retry", async ({ page, isMobile }) => {
    await login(page);
    // The transport abort that produces the retry is expected console noise.
    allowBrowserError(page, /net::ERR_FAILED/);
    const project = await seedProject(page, { prefix: "RT", items: [{ title: "Retry target", status: "ready", priority: "medium", assignee_type: "unassigned" }] });
    const item = project.items.get("Retry target");
    await page.goto(workUrl(item.id, { projectId: project.id }));
    const detail = detailSurface(page, isMobile, "Retry target");
    await detail.getByRole("button", { name: ui.editWorkItem }).click();
    const editor = page.getByRole("dialog", { name: "Edit work item" });
    const renamed = "Retry target renamed";
    await editor.getByLabel("Title").fill(renamed);

    const bodies = [];
    let aborted = false;
    await page.route(`**/portal/api/work-items/${item.id}`, async (route) => {
      if (route.request().method() !== "PATCH") return route.continue();
      bodies.push(route.request().postDataJSON());
      if (!aborted) {
        aborted = true;
        return route.abort("failed");
      }
      return route.continue();
    });
    await editor.getByRole("button", { name: ui.save }).click();
    const retry = editor.getByRole("button", { name: ui.tryAgain });
    await expect(retry).toBeVisible();
    await expect(editor.getByText(/Cannot reach the server/)).toBeVisible();
    await expect(editor.getByLabel("Title")).toHaveValue(renamed);
    await retry.click();
    await expect(statusNotice(detail, "Work item updated.")).toBeVisible();
    expect(bodies).toHaveLength(2);
    expect(bodies[1]).toEqual(bodies[0]);
    expect(bodies[0].version).toBe(item.version);
    const stored = await storedItem(page, project.id, renamed);
    expect(stored.title).toBe(renamed);
    expect(stored.version).toBe(item.version + 1);
  });

  test("keeps an open draft across a background refresh and applies it against the loaded revision", async ({ page, isMobile }) => {
    await login(page);
    const project = await seedProject(page, { prefix: "RF", items: [{ title: "Refresh draft", status: "ready", priority: "medium", assignee_type: "unassigned" }] });
    const item = project.items.get("Refresh draft");
    let detailReads = 0;
    await page.route(`**/portal/api/work-items/${item.id}`, async (route) => {
      if (route.request().method() === "GET") detailReads += 1;
      return route.continue();
    });
    await page.goto(workUrl(item.id, { projectId: project.id }));
    const detail = detailSurface(page, isMobile, "Refresh draft");
    await detail.getByRole("button", { name: ui.editWorkItem }).click();
    const editor = page.getByRole("dialog", { name: "Edit work item" });
    await editor.getByLabel("Branch").fill("feat/draft-survives");
    // The server keeps a blocker reason only for a blocked item.
    await editor.getByLabel("Blocked").check();
    await editor.getByLabel("Blocker").fill("Half-typed note");

    const reads = detailReads;
    // An open editor is modal: the detail's refresh control is aria-hidden to
    // the page, so the refresh the contract names (a background re-read while a
    // draft is open) is dispatched as the DOM click that control runs.
    await page.evaluate(() => {
      const control = Array.from(document.querySelectorAll("button"))
        .find((candidate) => candidate.textContent?.trim() === "Refresh");
      control?.click();
    });
    await expect.poll(() => detailReads).toBeGreaterThan(reads);
    await expect(editor).toBeVisible();
    await expect(editor.getByLabel("Branch")).toHaveValue("feat/draft-survives");
    await expect(editor.getByLabel("Blocker")).toHaveValue("Half-typed note");

    await editor.getByRole("button", { name: ui.save }).click();
    await expect(statusNotice(detail, "Work item updated.")).toBeVisible();
    const stored = await storedItem(page, project.id, "Refresh draft");
    expect(stored.branch).toBe("feat/draft-survives");
    expect(stored.blocked).toBe(true);
    expect(stored.blocker).toBe("Half-typed note");
  });

  test("re-syncs to the newest work item after a stale update in two contexts", async ({ browser, baseURL, isMobile }) => {
    const first = await browser.newContext({ baseURL });
    const second = await browser.newContext({ baseURL });
    const authoring = await first.newPage();
    const competing = await second.newPage();
    monitorBrowserErrors(authoring);
    monitorBrowserErrors(competing);
    allowBrowserError(competing, /status of 409/);
    try {
      await login(authoring);
      const project = await seedProject(authoring, { prefix: "WS", items: [{ title: "Shared work", status: "ready", priority: "medium", assignee_type: "unassigned" }] });
      const item = project.items.get("Shared work");
      // The login helper waits for the project shell's controls, so both pages
      // sign in first and then open the work surface.
      await login(competing);
      await competing.goto(workUrl(item.id, { projectId: project.id }));

      await authoring.goto(workUrl(item.id, { projectId: project.id }));
      // The definition of "the detail surface" follows the drawer breakpoint.
      await detailSurface(authoring, isMobile, "Shared work").getByRole("button", { name: ui.editWorkItem }).click();
      await detailSurface(competing, isMobile, "Shared work").getByRole("button", { name: ui.editWorkItem }).click();
      const authoringEditor = authoring.getByRole("dialog", { name: "Edit work item" });
      const competingEditor = competing.getByRole("dialog", { name: "Edit work item" });
      await authoringEditor.getByLabel("Title").fill("Shared work current");
      await competingEditor.getByLabel("Title").fill("Shared work stale");

      await authoringEditor.getByRole("button", { name: ui.save }).click();
      await expect(statusNotice(authoring, "Work item updated.")).toBeVisible();
      await competingEditor.getByRole("button", { name: ui.save }).click();
      await expect(alertNotice(competing, "resource changed since it was loaded (stale)")).toBeVisible();
      await expect(competingEditor).toHaveCount(0);
      await expect(detailSurface(competing, isMobile, "Shared work current")).toBeVisible();
      expect((await storedItem(authoring, project.id, "Shared work current")).title).toBe("Shared work current");
    } finally {
      try {
        assertNoBrowserErrors(authoring, competing);
      } finally {
        await first.close();
        await second.close();
      }
    }
  });

  test("applies a late board move only to its own project", async ({ page }) => {
    await login(page);
    const first = await seedProject(page, {
      prefix: "XR",
      items: [
        { title: "Held move", status: "backlog", priority: "medium", assignee_type: "unassigned" },
        { title: "Held target", status: "ready", priority: "medium", assignee_type: "unassigned" }
      ]
    });
    const second = await seedProject(page, { prefix: "XS" });
    const item = first.items.get("Held move");

    const gate = Promise.withResolvers();
    const moves = [];
    await page.route(`**/portal/api/work-items/${item.id}/move`, async (route) => {
      moves.push(route.request().postDataJSON());
      await gate.promise;
      return route.continue();
    });

    await page.goto(projectUrl(first.id, { view: "board" }));
    await moveByKeyboard(page, "Held move", "ArrowRight");
    await expect.poll(() => moves.length).toBe(1);
    await page.getByLabel(ui.project).selectOption(second.id);
    await expect(page.getByRole("heading", { level: 1, name: second.name })).toBeVisible();
    gate.resolve();
    await expect.poll(async () => (await storedItem(page, first.id, "Held move")).status).toBe("ready");
    await expect(page.getByRole("heading", { level: 1, name: second.name })).toBeVisible();
    await expect(page.getByRole("alert")).toHaveCount(0);
    await expect(page.getByText(/Moved to/)).toHaveCount(0);

    await page.goto(projectUrl(first.id, { view: "board" }));
    await expect.poll(() => columnOf(page, "Held move")).toBe("ready");
  });

  test("moves and reorders the focused board card with the keyboard", async ({ page }, testInfo) => {
    await login(page);
    const project = await seedProject(page, {
      prefix: "KB",
      items: [
        { title: "Keyboard alpha", status: "backlog", priority: "medium", assignee_type: "unassigned" },
        { title: "Keyboard beta", status: "backlog", priority: "medium", assignee_type: "unassigned" },
        { title: "Keyboard gamma", status: "ready", priority: "medium", assignee_type: "unassigned" }
      ]
    });

    await page.goto(projectUrl(project.id, { view: "board" }));
    await expect(page.getByRole("link", { name: "Keyboard alpha" })).toBeVisible();

    // Column move: appends to the neighbouring column and keeps focus on the card.
    await moveByKeyboard(page, "Keyboard alpha", "ArrowRight");
    await expect(statusNotice(page, /Moved to/)).toBeVisible();
    await expect.poll(() => columnOf(page, "Keyboard alpha")).toBe("ready");
    await expect.poll(() => focusedLabel(page)).toBe("Keyboard alpha");
    await expect.poll(() => readyColumnTitles(page, project.id)).toEqual(["Keyboard gamma", "Keyboard alpha"]);

    // Reorder up: the moved card takes the first position it asks for.
    await moveByKeyboard(page, "Keyboard alpha", "ArrowUp");
    await expect(statusNotice(page, /Moved up/)).toBeVisible();
    await expect.poll(() => readyColumnTitles(page, project.id)).toEqual(["Keyboard alpha", "Keyboard gamma"]);
    await expect.poll(() => focusedLabel(page)).toBe("Keyboard alpha");

    // Reorder down: appending after the last card of the column.
    await moveByKeyboard(page, "Keyboard alpha", "ArrowDown");
    await expect(statusNotice(page, /Moved down/)).toBeVisible();
    await expect.poll(() => readyColumnTitles(page, project.id)).toEqual(["Keyboard gamma", "Keyboard alpha"]);
    await expect.poll(() => focusedLabel(page)).toBe("Keyboard alpha");

    // Unmoved neighbour: at the first position, "up" is a no-op without a request.
    await moveByKeyboard(page, "Keyboard gamma", "ArrowUp");
    await expect.poll(() => readyColumnTitles(page, project.id)).toEqual(["Keyboard gamma", "Keyboard alpha"]);

    await page.reload();
    await expect.poll(() => columnOf(page, "Keyboard alpha")).toBe("ready");
    await expect.poll(() => readyColumnTitles(page, project.id)).toEqual(["Keyboard gamma", "Keyboard alpha"]);
    await capture(page, testInfo, "planning-keyboard-moves", {
      surface: "board",
      url: page.url(),
      ready: await page.evaluate(() => Array.from(document.querySelectorAll("[data-board-column='ready'] .board-card")).map((card) => card.getAttribute("aria-label")))
    });
  });
});

test.describe("unwritable item kinds", () => {
  test("a Goal-managed item offers no writable path and the server refuses legacy writes", async ({ page, isMobile }, testInfo) => {
    await login(page);
    // The legacy refusals below are the point of the case: 403 is expected noise.
    allowBrowserError(page, /status of 403/);
    // The Goal admission contract requires a repository baseline, so the
    // project carries the repository resource the fixture freezes into it.
    const project = await seedProject(page, {
      prefix: "GM",
      resources: [{ kind: "repository", name: "Planning repository", external_ref: "acme/planning" }],
      items: [{ title: "Goal owned", status: "ready", priority: "medium", assignee_type: "unassigned" }]
    });
    const item = project.items.get("Goal owned");
    const fixture = await applyGoalManaged(item.id, project.resources[0].id);
    expect(fixture).toContain("goal-managed");

    await page.goto(projectUrl(project.id, { view: "board" }));
    const card = boardCard(page, "Goal owned");
    await expect(card.getByText("Goal-managed")).toBeVisible();
    const moves = [];
    page.on("request", (request) => {
      if (request.method() === "PATCH" && new URL(request.url()).pathname.endsWith("/move")) moves.push(request.url());
    });
    await moveByKeyboard(page, "Goal owned", "ArrowRight");
    await expect(statusNotice(page, /Goal-managed work has no board movement/)).toBeVisible();
    expect(moves).toHaveLength(0);
    await expect.poll(() => columnOf(page, "Goal owned")).toBe("ready");

    await card.click();
    const detail = detailSurface(page, isMobile, "Goal owned");
    await expect(detail.getByText(/managed by a Goal/)).toBeVisible();
    await expect(detail.getByRole("button", { name: ui.editWorkItem })).toHaveCount(0);

    // The server's authority boundary is real, not only a presentation rule.
    const refusal = await api(page, "PATCH", `/portal/api/work-items/${item.id}`, { version: item.version, title: "Legacy rewrite" });
    expect(refusal.status).toBe(403);
    expect(refusal.body.error.code).toBe("goal_authority_required");
    const moveRefusal = await api(page, "PATCH", `/portal/api/work-items/${item.id}/move`, { version: item.version, status: "review", before_id: null });
    expect(moveRefusal.status).toBe(403);
    expect(moveRefusal.body.error.code).toBe("goal_authority_required");
    expect((await storedItem(page, project.id, "Goal owned")).status).toBe("ready");
    await capture(page, testInfo, "planning-goal-managed-item", { surface: isMobile ? "detail (narrow)" : "detail drawer", url: page.url() });
  });

  test("an external-tracker-owned item keeps its provider-owned fields and column unwritable", async ({ page, isMobile }, testInfo) => {
    await login(page);
    // The provider-owned refusal is the point of the case: 409 is expected noise.
    allowBrowserError(page, /status of 409/);
    const project = await seedProject(page, {
      prefix: "PO",
      resources: [{ kind: "work_tracking", name: "Planning tracker", external_ref: "acme/tracker" }],
      items: [{ title: "Provider owned", description: "Tracked outside", status: "backlog", priority: "low", assignee_type: "unassigned" }]
    });
    const item = project.items.get("Provider owned");
    const fixture = await applyProviderOwned(item.id, project.resources[0].id, "github", `0007c-${Date.now().toString(36)}`);
    expect(fixture).toContain("provider-owned");

    await page.goto(projectUrl(project.id, { view: "board" }));
    const card = boardCard(page, "Provider owned");
    await expect(card.getByText("Provider-owned")).toBeVisible();
    const moves = [];
    page.on("request", (request) => {
      if (request.method() === "PATCH" && new URL(request.url()).pathname.endsWith("/move")) moves.push(request.url());
    });
    await moveByKeyboard(page, "Provider owned", "ArrowRight");
    await expect(statusNotice(page, /owned by the external work tracker/)).toBeVisible();
    expect(moves).toHaveLength(0);
    await expect.poll(() => columnOf(page, "Provider owned")).toBe("backlog");

    await card.click();
    const detail = detailSurface(page, isMobile, "Provider owned");
    await expect(detail.getByText(/external work tracker owns/)).toBeVisible();
    await detail.getByRole("button", { name: ui.editWorkItem }).click();
    const editor = page.getByRole("dialog", { name: "Edit work item" });
    await expect(editor.getByLabel("Title")).toBeDisabled();
    await expect(editor.getByLabel("Description")).toBeDisabled();
    await expect(editor.getByLabel("Priority")).toBeDisabled();
    await expect(editor.getByLabel("Owner type")).toBeDisabled();
    await expect(editor.getByText(/edit them in the tracker/)).toBeVisible();
    await capture(page, testInfo, "planning-provider-owned-editor", { surface: "editor", url: page.url(), disabled: ["Title", "Description", "Priority", "Owner type"] }, { fullPage: false });
    await editor.getByRole("button", { name: ui.cancel }).click();

    const refusal = await api(page, "PATCH", `/portal/api/work-items/${item.id}`, { version: item.version, title: "Legacy rewrite" });
    expect(refusal.status).toBe(409);
    expect(refusal.body.error.code).toBe("provider_owned");
    const stored = await storedItem(page, project.id, "Provider owned");
    expect(stored.title).toBe("Provider owned");
    expect(stored.external.state).toBe("open");
    expect(stored.external.provider_owned_fields).toContain("title");
    await capture(page, testInfo, "planning-provider-owned-item", { surface: "detail", url: page.url() });
  });
});

test.describe("archived project read-only", () => {
  test("an archived project's board offers no create, edit or move path", async ({ page, isMobile }) => {
    await login(page);
    const project = await seedProject(page, {
      prefix: "AR",
      items: [
        { title: "Retained work", status: "backlog", priority: "medium", assignee_type: "unassigned" },
        { title: "Retained target", status: "ready", priority: "medium", assignee_type: "unassigned" }
      ]
    });
    const archived = await api(page, "PATCH", `/portal/api/projects/${project.id}`, { version: (await storedProject(page, project.id)).version, status: "archived" });
    expect(archived.status, JSON.stringify(archived.body)).toBe(200);

    await page.goto(projectUrl(project.id, { view: "board" }));
    // Scoped to the notice: the board hint carries the same sentence stem.
    await expect(statusNotice(page, /This project is archived/)).toBeVisible();
    await expect(page.getByRole("button", { name: ui.newWorkItem })).toHaveCount(0);
    await expect(page.getByRole("button", { name: ui.projectSettings })).toHaveCount(0);
    await expect(page.getByRole("button", { name: ui.restoreProject })).toBeVisible();
    await expect(page.getByRole("link", { name: "Retained work" })).toBeVisible();

    const moves = [];
    page.on("request", (request) => {
      if (request.method() === "PATCH" && new URL(request.url()).pathname.endsWith("/move")) moves.push(request.url());
    });
    await moveByKeyboard(page, "Retained work", "ArrowRight");
    await expect(statusNotice(page, /restore it to move work/)).toBeVisible();
    expect(moves).toHaveLength(0);
    expect((await storedItem(page, project.id, "Retained work")).status).toBe("backlog");

    await page.getByRole("link", { name: "Retained work" }).click();
    const detail = detailSurface(page, isMobile, "Retained work");
    await expect(detail.getByRole("button", { name: ui.editWorkItem })).toHaveCount(0);

    await page.goto(projectUrl(project.id, { view: "board" }));
    await page.getByRole("button", { name: ui.restoreProject }).click();
    await expect(statusNotice(page, "Project restored.")).toBeVisible();
    await expect(page.getByRole("button", { name: ui.newWorkItem })).toBeVisible();
    expect((await storedProject(page, project.id)).status).toBe("active");
  });
});
