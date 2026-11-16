// Goal 0007b — readable work views acceptance.
//
// Real authenticated fixtures only: projects, resources and work items are
// created through the Portal API with the session CSRF token (the read-only
// shell exposes no mutation controls), and every rendered read is decoded from
// the running control plane. Network interception appears in four places only,
// all explicitly separated from real-data acceptance:
//   1. holding a genuinely captured real response to prove late responses are
//      never rendered as another project's data or as newer state;
//   2. aborting real requests to render the genuine error states;
//   3. the migrated malicious-response injection check;
//   4. the daemon-free synthetic fault injection guard for the obsolete
//      older-history request, which uses generated contract fixture shapes and
//      is never counted as real history evidence.
import { readFile } from "node:fs/promises";
import { allowBrowserError, assertNoBrowserErrors, expect, monitorBrowserErrors, test } from "./fixtures.mjs";
import {
  attachResource,
  capture,
  createProject,
  createWorkItem,
  login,
  moveWorkItem,
  operatorToken,
  projectItem,
  recordKeys,
  snapshotMetrics,
  uniqueKey,
  updateWorkItem,
  workspace
} from "./readable-fixtures.mjs";

// Exact accessible names implemented by the frontend contract; they live in one
// place so a wording change is a one-line edit, not a hunt through assertions.
const ui = {
  viewNav: "Work views",
  list: "List",
  board: "Board",
  filter: "Filter work items",
  state: "Work state",
  owner: "Work owner",
  allStates: "All states",
  blockedOnly: "Blocked only",
  savedViews: "Saved views",
  savedViewName: "Saved view name",
  saveView: "Save view",
  deleteSavedView: "Delete saved view",
  workItems: "Work items",
  boardRegion: "Work board",
  noMatching: "No matching work items",
  clearFilters: "Clear filters",
  emptyList: "No work items",
  costUnknown: "Cost unknown",
  ciUnknown: "CI unknown",
  noEvidence: "No accepted evidence available",
  noReportedOutput: "No reported execution output",
  noBlocker: "No blocker",
  noNextAction: "No next action recorded",
  noHistory: "No execution history recorded",
  loadOlder: "Load older history",
  closeDetail: "Close work detail",
  backToProject: /^Back to projects?$/,
  errorHeading: "Unable to load workspace",
  tryAgain: "Try again",
  refresh: "Refresh"
};

const projectUrl = (id, params = {}) => {
  const query = new URLSearchParams(params).toString();
  return `/portal/projects/${id}${query ? `?${query}` : ""}`;
};
const workUrl = (id, params = {}) => {
  const query = new URLSearchParams(params).toString();
  return `/portal/work/${id}${query ? `?${query}` : ""}`;
};

async function seedProject(page, { prefix, items = [], resources = [] }) {
  const key = uniqueKey(prefix);
  const project = await createProject(page, { key, name: `Readable work views ${key}` });
  for (const resource of resources) await attachResource(page, project.id, resource);
  const created = new Map();
  for (const item of items) created.set(item.title, await createWorkItem(page, project.id, item));
  return { ...project, items: created };
}

// Detail is a modal side panel on desktop and a full page at narrow widths.
const detailSurface = (page, isMobile, title) =>
  isMobile ? page.locator("#main-content") : page.getByRole("dialog", { name: title });
// browser.newContext does not inherit project use options, so a fresh signed-out
// context copies only the supported BrowserContextOptions fields from the
// running project (exact viewport, plus mobile/touch/scale/agent semantics).
// No storageState is passed: the context stays signed out.
const deviceContextOptions = (testInfo) => {
  const use = testInfo.project.use ?? {};
  return {
    ...(use.viewport ? { viewport: use.viewport } : {}),
    ...(use.isMobile === undefined ? {} : { isMobile: use.isMobile }),
    ...(use.hasTouch === undefined ? {} : { hasTouch: use.hasTouch }),
    ...(use.deviceScaleFactor === undefined ? {} : { deviceScaleFactor: use.deviceScaleFactor }),
    ...(use.userAgent ? { userAgent: use.userAgent } : {})
  };
};
// Error branches render before the detail title is known, so the dialog is
// addressed by role only.
const detailPanel = (page, isMobile) =>
  isMobile ? page.locator("#main-content") : page.locator('[role="dialog"]');

async function closeDetail(page, isMobile, { escape = false, keys } = {}) {
  if (isMobile) await page.getByRole("link", { name: ui.backToProject }).click();
  else if (escape && keys) await keys.press(page, "Escape");
  else if (escape) await page.keyboard.press("Escape");
  else await page.getByRole("button", { name: ui.closeDetail }).click();
}

// Arrow-key walk: which card/column received focus.
const focusedCard = (page) =>
  page.evaluate(() => {
    const element = document.activeElement;
    return {
      label: element?.getAttribute("aria-label") ?? "",
      column: element?.closest("section")?.querySelector("h2")?.textContent?.trim() ?? ""
    };
  });

test.describe("Readable work views", () => {
  test("list, board, filters and summary render real project work with stable URL state", async ({ page }, testInfo) => {
    await login(page);
    const key = uniqueKey("RV");
    const project = await createProject(page, { key, name: `Readable work views ${key}` });
    const staleCi = await attachResource(page, project.id, {
      kind: "ci",
      status: "degraded",
      sync_status: "stale",
      name: `Stale CI ${key}`,
      provider: "GitHub Actions",
      external_ref: `stale-ci-${key}`,
      status_message: "Webhook delivery is delayed"
    });

    const titles = {
      blocked: `Human decision work ${key}`,
      agent: `Agent ready work ${key}`,
      backlog: `Backlog unassigned ${key}`,
      progress: `In progress work ${key}`,
      review: `Review work ${key}`,
      manualCi: `Manual CI work ${key}`,
      staleCi: `Stale CI work ${key}`
    };
    const fillerTitles = Array.from({ length: 5 }, (_, index) => `Filler work ${index + 1} ${key}`);

    await createWorkItem(page, project.id, {
      title: titles.blocked,
      description: "Real blocked human work",
      status: "ready",
      priority: "urgent",
      assignee_type: "human",
      assignee_name: "Lina",
      blocked: true,
      blocker: "Needs architecture decision"
    });
    await createWorkItem(page, project.id, {
      title: titles.agent,
      description: "Real agent work without delivery evidence",
      status: "ready",
      priority: "high",
      assignee_type: "agent",
      agent_profile: "codex",
      workspace: "acceptance"
    });
    await createWorkItem(page, project.id, {
      title: titles.backlog,
      description: "Real unassigned backlog work",
      status: "backlog",
      priority: "low",
      assignee_type: "unassigned",
      blocked: true,
      blocker: "Waiting for operator triage"
    });
    await createWorkItem(page, project.id, {
      title: titles.progress,
      description: "Real in-progress human work",
      status: "in_progress",
      priority: "medium",
      assignee_type: "human",
      assignee_name: "Mira"
    });
    await createWorkItem(page, project.id, {
      title: titles.review,
      description: "Real work awaiting review",
      status: "review",
      priority: "medium",
      assignee_type: "agent",
      agent_profile: "codex",
      workspace: "acceptance"
    });
    const manualCi = await createWorkItem(page, project.id, {
      title: titles.manualCi,
      description: "Manual CI evidence without a recorded freshness timestamp",
      status: "done",
      priority: "medium",
      assignee_type: "human",
      assignee_name: "Lina"
    });
    const staleCiItem = await createWorkItem(page, project.id, {
      title: titles.staleCi,
      description: "Work linked to a stale CI resource",
      status: "ready",
      priority: "medium",
      assignee_type: "human",
      assignee_name: "Mira",
      ci_resource_id: staleCi.id
    });
    for (const title of fillerTitles) {
      await createWorkItem(page, project.id, {
        title,
        description: "Real work for snapshot density",
        status: "backlog",
        priority: "low",
        assignee_type: "unassigned"
      });
    }
    // Ready-column ordering: three real rows created in an order that matches
    // neither the rendered position order nor the alphabetic order.
    const orderTitles = {
      zulu: `Zulu order work ${key}`,
      mike: `Mike order work ${key}`,
      alpha: `Alpha order work ${key}`
    };
    const orderItems = {};
    for (const name of ["zulu", "alpha", "mike"]) {
      orderItems[name] = await createWorkItem(page, project.id, {
        title: orderTitles[name],
        description: "Ready-column ordering row",
        status: "ready",
        priority: "low",
        assignee_type: "unassigned"
      });
    }
    await moveWorkItem(page, orderItems.mike.id, {
      version: orderItems.mike.version,
      status: "ready",
      beforeId: orderItems.alpha.id
    });
    await updateWorkItem(page, manualCi.id, {
      version: (await projectItem(page, project.id, titles.manualCi)).version,
      pull_request_url: "https://github.com/wxxb789/symmetry/pull/42",
      ci_status: "passed"
    });
    await updateWorkItem(page, staleCiItem.id, {
      version: (await projectItem(page, project.id, titles.staleCi)).version,
      ci_status: "passed"
    });

    const metrics = await snapshotMetrics(page, project.id);
    expect(metrics.status).toBe(200);
    expect(metrics.workItemCount).toBe(15);

    await page.goto(projectUrl(project.id));
    await expect(page.getByRole("heading", { level: 1, name: project.name })).toBeVisible();
    const list = page.getByRole("list", { name: ui.workItems });
    await expect(list.getByRole("link", { name: titles.blocked })).toBeVisible();
    await expect(page.getByText("15 work items · 2 blocked")).toBeVisible();

    const blockedRow = list.getByRole("link", { name: titles.blocked });
    await expect(blockedRow).toContainText("Lina");
    await expect(blockedRow).toContainText("ready");
    await expect(blockedRow).toContainText("Needs architecture decision");
    await expect(blockedRow).toContainText(ui.costUnknown);
    // Row-level truth labels: acceptance is never claimed for a row, and a row
    // without a reported execution result says so instead of staying blank.
    await expect(blockedRow).toContainText("Acceptance not recorded");
    await expect(blockedRow).toContainText("No reported result");
    await expect(list.getByRole("link", { name: titles.agent })).toContainText(ui.ciUnknown);
    await expect(list.getByRole("link", { name: titles.manualCi })).toContainText("CI passed · freshness unknown");
    // Bound-resource staleness is exercised by a real API fixture: this row is
    // bound through ci_resource_id to a resource whose sync_status is "stale".
    await expect(list.getByRole("link", { name: titles.staleCi })).toContainText("CI passed · bound resource stale");
    await expect(page.locator("#main-content")).not.toContainText("$0");

    await page.getByLabel(ui.filter).fill("Human decision");
    await expect(list.getByRole("link", { name: titles.blocked })).toBeVisible();
    await expect(list.getByRole("link", { name: titles.agent })).toHaveCount(0);
    await expect.poll(() => new URL(page.url()).searchParams.get("q")).toBe("Human decision");
    await page.reload();
    await expect(page.getByLabel(ui.filter)).toHaveValue("Human decision");
    await expect(list.getByRole("link", { name: titles.blocked })).toBeVisible();
    await expect(list.getByRole("link", { name: titles.agent })).toHaveCount(0);

    await page.getByLabel(ui.filter).fill("no such work item");
    await expect(page.getByRole("heading", { name: ui.noMatching })).toBeVisible();
    await page.getByRole("button", { name: ui.clearFilters }).click();
    // Clearing removes the button itself under a same-pathname update: the
    // still-present filter input must receive focus, never `body`.
    await expect(page.getByLabel(ui.filter)).toBeFocused();
    await expect(page.getByLabel(ui.filter)).toHaveValue("");
    await expect(list.getByRole("link", { name: titles.agent })).toBeVisible();

    await page.getByLabel(ui.state).selectOption({ label: "done" });
    await expect(list.getByRole("link", { name: titles.manualCi })).toBeVisible();
    await expect(list.getByRole("link", { name: titles.blocked })).toHaveCount(0);
    await page.getByLabel(ui.state).selectOption({ label: ui.allStates });

    // A filter value absent from the snapshot keeps an explicit option instead
    // of silently lying about the active filter.
    await page.goto(projectUrl(project.id, { state: "future_state" }));
    const absentStateFilter = page.getByLabel(ui.state);
    await expect(absentStateFilter).toHaveValue("future_state");
    await expect(absentStateFilter.locator('option[value="future_state"]'))
      .toHaveText("Unknown status: future_state (not in this project)");
    await absentStateFilter.selectOption({ label: ui.allStates });
    await expect(list.getByRole("link", { name: titles.agent })).toBeVisible();

    await page.getByLabel(ui.blockedOnly).check();
    await expect(list.getByRole("link", { name: titles.blocked })).toBeVisible();
    await expect(list.getByRole("link", { name: titles.agent })).toHaveCount(0);
    await expect.poll(() => new URL(page.url()).searchParams.has("blocked")).toBe(true);
    await page.reload();
    await expect(page.getByLabel(ui.blockedOnly)).toBeChecked();
    await expect(list.getByRole("link", { name: titles.blocked })).toBeVisible();
    await expect(list.getByRole("link", { name: titles.agent })).toHaveCount(0);
    await page.getByLabel(ui.blockedOnly).uncheck();

    // Owner filter: pick a real served option, prove the URL carries it, the
    // rows are filtered by owner, and both survive a reload.
    await page.getByLabel(ui.owner).selectOption({ label: "Agent · codex" });
    await expect.poll(() => new URL(page.url()).searchParams.get("owner")).toBe("agent:codex");
    await expect(list.getByRole("link", { name: titles.agent })).toBeVisible();
    await expect(list.getByRole("link", { name: titles.review })).toBeVisible();
    await expect(list.getByRole("link", { name: titles.blocked })).toHaveCount(0);
    await expect(list.getByRole("link", { name: titles.progress })).toHaveCount(0);
    await page.reload();
    await expect(page.getByLabel(ui.owner)).toHaveValue("agent:codex");
    await expect(list.getByRole("link", { name: titles.agent })).toBeVisible();
    await expect(list.getByRole("link", { name: titles.review })).toBeVisible();
    await expect(list.getByRole("link", { name: titles.blocked })).toHaveCount(0);
    await page.getByLabel(ui.owner).selectOption({ label: "All owners" });

    await page.getByLabel(ui.filter).fill("Human decision");
    await page.getByLabel(ui.savedViewName).fill(`Saved ${key}`);
    await page.getByRole("button", { name: ui.saveView }).click();
    await page.getByLabel(ui.filter).fill("");
    await expect(list.getByRole("link", { name: titles.agent })).toBeVisible();
    await page.reload();
    await page.getByLabel(ui.savedViews).selectOption({ label: `Saved ${key}` });
    await expect(page.getByLabel(ui.filter)).toHaveValue("Human decision");
    await expect(list.getByRole("link", { name: titles.blocked })).toBeVisible();
    // Deleting removes the button itself: focus stays on the saved-views control.
    await page.getByRole("button", { name: ui.deleteSavedView }).click();
    await expect(page.getByLabel(ui.savedViews)).toBeFocused();
    await expect(page.getByLabel(ui.savedViews).locator(`option[value="Saved ${key}"]`)).toHaveCount(0);
    await page.getByLabel(ui.filter).fill("");
    await page.getByLabel(ui.state).selectOption({ label: ui.allStates });

    await capture(page, testInfo, "work-list", {
      surface: "list",
      snapshotBytes: metrics.bytes,
      workItemCount: metrics.workItemCount,
      largestObservedSnapshot:
        `Real /portal/api/workspace snapshot for project ${project.key}: ${metrics.bytes} bytes, ` +
        `${metrics.workItemCount} work items. The list keeps the existing snapshot contract; server-side ` +
        "work-item pagination would be a wire-contract addition and is reported as a limitation, not invented.",
      costAvailability: `Rendered "${ui.costUnknown}"; no $0 anywhere in the surface`,
      absentFilterHonesty: "select keeps future_state with option \"Unknown status: future_state (not in this project)\"",
      ciLabels: [ui.ciUnknown, "CI passed · freshness unknown", "CI passed · bound resource stale"],
      ciLimitation:
        "Exercised fixtures only: the manually configured passed-CI row carries no recorded_at, so it renders " +
        "\"CI passed · freshness unknown\" with the non-green unknown tone; the row bound through ci_resource_id to a " +
        "resource whose sync_status is \"stale\" renders \"CI passed · bound resource stale\" with the stale tone; and " +
        "the row without CI evidence renders \"CI unknown\". The provider timestamp-stale branch remains covered by " +
        "the presentation unit test and is not exercised by these real API fixtures; provider ingestion is not used.",
      url: page.url()
    });

    const navigation = page.getByRole("navigation", { name: ui.viewNav });
    await navigation.getByRole("link", { name: ui.board }).click();
    await expect(page).toHaveURL(/view=board/);
    const board = page.getByRole("region", { name: ui.boardRegion });
    await expect(board).toBeVisible();
    for (const column of ["backlog", "ready", "in progress", "review", "done"]) {
      await expect(board.getByRole("heading", { name: column, exact: true })).toBeVisible();
    }
    // The captured column list is exactly what the board renders, in order.
    await expect(board.locator(".board-column h2"))
      .toHaveText(["backlog", "ready", "in progress", "review", "done"]);
    await expect(board.getByRole("link", { name: titles.blocked })).toBeVisible();
    await expect(board.getByRole("link", { name: fillerTitles[0] })).toBeVisible();
    await page.reload();
    await expect(page.getByRole("region", { name: ui.boardRegion })).toBeVisible();
    // Rendered ready-column order must be the served position order, with
    // titles that cannot match position order by accident.
    const servedReady = (await workspace(page, project.id)).body.projects
      .find((entry) => entry.id === project.id).work_items
      .filter((item) => item.status === "ready")
      .sort((left, right) => left.position - right.position)
      .map((item) => item.title);
    const readySection = board.locator(".board-column").filter({
      has: page.getByRole("heading", { name: "ready", exact: true })
    });
    await expect(readySection).toHaveCount(1);
    await expect(readySection.getByRole("link")).toHaveCount(servedReady.length);
    const renderedReady = await readySection
      .getByRole("link")
      .evaluateAll((links) => links.map((link) => link.getAttribute("aria-label")));
    expect(renderedReady).toEqual(servedReady);
    const renderedOrder = renderedReady.filter((title) =>
      [orderTitles.zulu, orderTitles.mike, orderTitles.alpha].includes(title));
    expect(renderedOrder).toEqual([orderTitles.zulu, orderTitles.mike, orderTitles.alpha]);
    expect(renderedOrder).not.toEqual([orderTitles.zulu, orderTitles.mike, orderTitles.alpha].slice().sort());
    expect(renderedOrder).not.toEqual([orderTitles.zulu, orderTitles.alpha, orderTitles.mike]);
    // Every served work item renders as exactly one board card: no column is
    // dropped, no card is duplicated, and nothing is paginated away.
    await expect(board.getByRole("link")).toHaveCount(metrics.workItemCount);
    await capture(page, testInfo, "work-board", {
      surface: "board",
      columns: ["backlog", "ready", "in progress", "review", "done"],
      cards: metrics.workItemCount,
      readyColumnOrder: renderedOrder,
      servedReadyOrder: servedReady,
      url: page.url()
    });

    await navigation.getByRole("link", { name: ui.list }).click();
    await expect(page.getByRole("list", { name: ui.workItems })).toBeVisible();
  });

  test("work detail renders outcome, evidence, blocker, next action and history on real data", async ({ page, isMobile }, testInfo) => {
    await login(page);
    const key = uniqueKey("RD");
    const blockedTitle = `Detail blocked ${key}`;
    const unblockedTitle = `Detail unblocked ${key}`;
    const project = await seedProject(page, {
      prefix: "RD",
      items: [
        {
          title: blockedTitle,
          description: "Real blocked work with no execution",
          status: "ready",
          priority: "high",
          assignee_type: "human",
          assignee_name: "Lina",
          blocked: true,
          blocker: "Operator review required"
        },
        {
          title: unblockedTitle,
          description: "Real unblocked work with no recorded next action",
          status: "backlog",
          priority: "low",
          assignee_type: "unassigned"
        }
      ]
    });
    const blocked = project.items.get(blockedTitle);
    const unblocked = project.items.get(unblockedTitle);

    const requests = [];
    page.on("request", (request) => {
      const path = new URL(request.url()).pathname;
      if (path.startsWith(`/portal/api/work-items/${blocked.id}`)) requests.push(path);
    });

    await page.goto(projectUrl(project.id));
    await page.getByRole("list", { name: ui.workItems }).getByRole("link", { name: blocked.title }).click();

    const detail = detailSurface(page, isMobile, blocked.title);
    await expect(detail.getByRole("heading", { level: 1, name: blocked.title })).toBeVisible();
    // Only the section headings: the drawer also renders a Radix Dialog.Title
    // heading, which is not a detail section.
    const sections = await detail.locator(".detail-section > h2").allTextContents();
    expect(sections.slice(0, 5)).toEqual([
      "Outcome",
      "Latest accepted evidence",
      "Blocker",
      "Next action",
      "Execution history"
    ]);
    await expect(detail.getByText(ui.noEvidence)).toBeVisible();
    await expect(detail.getByText(ui.noReportedOutput)).toBeVisible();
    await expect(detail.getByText("Operator review required")).toBeVisible();
    await expect(detail.getByText(ui.noNextAction)).toBeVisible();
    await expect(detail.getByText(ui.noHistory)).toBeVisible();
    await expect(detail.locator("ol.timeline > li.timeline-entry")).toHaveCount(0);
    // Cost is served as unknown in two consumed places; assert both explicitly.
    await expect(detail.locator(".detail-chips")).toContainText(ui.costUnknown);
    await expect(detail.locator("dl.detail-facts div", { hasText: "Cost" })).toContainText(ui.costUnknown);
    await expect(detail.getByRole("button", { name: ui.loadOlder })).toHaveCount(0);
    await expect(detail.getByText(ui.noBlocker)).toHaveCount(0);
    // The read-only surface never offers an execution control.
    await expect(detail.getByRole("button", { name: "Start run" })).toHaveCount(0);
    expect(requests).toContain(`/portal/api/work-items/${blocked.id}`);
    expect(requests.filter((path) => path.endsWith("/timeline"))).toEqual([]);

    await capture(page, testInfo, "work-detail", {
      surface: "detail",
      presentation: isMobile ? "full page (narrow)" : "desktop side panel",
      sections,
      fields: [ui.noEvidence, ui.noReportedOutput, "Operator review required", ui.noNextAction, ui.costUnknown],
      url: page.url()
    });
    await detail.getByRole("heading", { name: "Execution history" }).scrollIntoViewIfNeeded();
    await capture(page, testInfo, "work-history-empty", {
      surface: "history",
      state: "empty",
      label: ui.noHistory,
      cursorFabricated: false,
      url: page.url()
    });

    await closeDetail(page, isMobile);
    await expect(page).toHaveURL(new RegExp(`/portal/projects/${project.id}(?:\\?|$)`));
    await expect(page.getByRole("heading", { level: 1, name: project.name })).toBeFocused();

    await page.getByRole("list", { name: ui.workItems }).getByRole("link", { name: unblocked.title }).click();
    const unblockedDetail = detailSurface(page, isMobile, unblocked.title);
    await expect(unblockedDetail.getByText(ui.noBlocker)).toBeVisible();
    await expect(unblockedDetail.getByRole("button", { name: "Start run" })).toHaveCount(0);
    await closeDetail(page, isMobile);
  });

  test("empty and error states render on the project and work routes", async ({ page, isMobile }, testInfo) => {
    await login(page);
    const empty = await createProject(page, { key: uniqueKey("RE"), name: `Readable empty ${uniqueKey("E")}` });
    await page.goto(projectUrl(empty.id));
    await expect(page.getByRole("heading", { name: ui.emptyList })).toBeVisible();
    await capture(page, testInfo, "work-list-empty", {
      surface: "list",
      state: "empty",
      project: empty.key,
      url: page.url()
    });

    allowBrowserError(page, /net::ERR_FAILED|Failed to fetch|Failed to load resource/);
    await page.route("**/portal/api/workspace*", (route) => route.abort("failed"));
    await page.reload();
    await expect(page.getByRole("heading", { name: ui.errorHeading })).toBeVisible();
    await capture(page, testInfo, "work-list-error", {
      surface: "list",
      state: "error",
      trigger: "real workspace requests aborted; no fabricated backend response",
      url: page.url()
    });
    await page.unrouteAll({ behavior: "wait" });
    await page.getByRole("button", { name: ui.tryAgain }).click();
    await expect(page.getByRole("heading", { name: ui.emptyList })).toBeVisible();

    const project = await seedProject(page, {
      prefix: "RX",
      items: [{
        title: `Error detail ${uniqueKey("Z")}`,
        description: "Real work whose detail requests fail",
        status: "ready",
        priority: "medium",
        assignee_type: "unassigned"
      }]
    });
    const item = [...project.items.values()][0];
    await page.goto(projectUrl(project.id));
    await page.route(`**/portal/api/work-items/${item.id}`, (route) => route.abort("failed"));
    await page.getByRole("list", { name: ui.workItems }).getByRole("link", { name: item.title }).click();
    const detail = detailPanel(page, isMobile);
    await expect(detail.getByRole("heading", { name: ui.errorHeading })).toBeVisible();
    await capture(page, testInfo, "work-detail-error", {
      surface: "detail",
      state: "error",
      trigger: "real detail requests aborted; retry then loads the real response",
      url: page.url()
    });
    await page.unrouteAll({ behavior: "wait" });
    await detail.getByRole("button", { name: ui.tryAgain }).click();
    await expect(detail.getByRole("heading", { level: 1, name: item.title })).toBeVisible();
    await closeDetail(page, isMobile);
  });

  test("keyboard walk and focus return keep URL state", async ({ page, isMobile }, testInfo) => {
    await login(page);
    const key = uniqueKey("RK");
    const titles = {
      alpha: `Keyboard alpha ${key}`,
      beta: `Keyboard beta ${key}`,
      gamma: `Keyboard gamma ${key}`,
      delta: `Keyboard delta ${key}`
    };
    const project = await seedProject(page, {
      prefix: "RK",
      items: [
        { title: titles.alpha, description: "Keyboard row", status: "backlog", priority: "low", assignee_type: "unassigned" },
        { title: titles.beta, description: "Keyboard row", status: "ready", priority: "low", assignee_type: "unassigned" },
        { title: titles.gamma, description: "Keyboard row", status: "ready", priority: "low", assignee_type: "unassigned" },
        { title: titles.delta, description: "Keyboard row", status: "in_progress", priority: "low", assignee_type: "unassigned" }
      ]
    });

    await page.goto(projectUrl(project.id));
    // URL-backed filters update only the search parameters; every keystroke must
    // keep focus in the input (a search-only update is not a route change).
    // fill() would set the value in one event and hide a focus reset.
    const filter = page.getByLabel(ui.filter);
    await filter.click();
    await filter.pressSequentially("Keyboard", { delay: 25 });
    await expect(filter).toBeFocused();
    await expect(filter).toHaveValue("Keyboard");
    await expect(page).toHaveURL(/[?&]q=Keyboard/);
    const list = page.getByRole("list", { name: ui.workItems });
    await expect(list.getByRole("link", { name: titles.alpha })).toBeVisible();

    const keys = recordKeys();
    await list.getByRole("link", { name: titles.alpha }).focus();
    await keys.press(page, "Enter");
    const detail = detailSurface(page, isMobile, titles.alpha);
    await expect(detail.getByRole("heading", { level: 1, name: titles.alpha })).toBeVisible();
    if (!isMobile) {
      // The desktop drawer is a modal dialog: opening it moves focus inside,
      // Shift+Tab reaches the Close control and wraps within the dialog, and
      // Tab wraps back to it. Narrow widths render a full page with no focus
      // trap, so this branch is desktop-only.
      const closeButton = detail.getByRole("button", { name: ui.closeDetail });
      const focusInDialog = () => page.evaluate(() => {
        const active = document.activeElement;
        return active !== null && active.closest('[role="dialog"]') !== null;
      });
      expect(await focusInDialog(), "opening the drawer moves focus inside it").toBe(true);
      await keys.press(page, "Shift+Tab");
      await expect(closeButton).toBeFocused();
      await keys.press(page, "Shift+Tab");
      expect(await focusInDialog(), "Shift+Tab never leaves the dialog").toBe(true);
      await keys.press(page, "Tab");
      await expect(closeButton).toBeFocused();
    }
    await closeDetail(page, isMobile, { escape: true, keys });
    await expect(page).toHaveURL(/[?&]q=Keyboard/);
    await expect(page.getByRole("heading", { level: 1, name: project.name })).toBeFocused();

    await page.getByRole("navigation", { name: ui.viewNav }).getByRole("link", { name: ui.board }).click();
    const board = page.getByRole("region", { name: ui.boardRegion });
    await board.getByRole("link", { name: titles.beta }).focus();
    await keys.press(page, "ArrowDown");
    await expect.poll(async () => (await focusedCard(page)).label).toBe(titles.gamma);
    expect((await focusedCard(page)).column).toBe("ready");
    await keys.press(page, "ArrowUp");
    await expect.poll(async () => (await focusedCard(page)).label).toBe(titles.beta);
    const columns = (await board.locator("section h2").allTextContents()).map((column) => column.trim());
    const readyIndex = columns.indexOf("ready");
    expect(readyIndex, "the ready column renders on the board").toBeGreaterThanOrEqual(0);
    // The keyboard fixture renders backlog, ready and in progress: ready is
    // never the last column, so the horizontal walk always runs.
    expect(readyIndex, "the ready column is not the last column").toBeLessThan(columns.length - 1);
    await keys.press(page, "ArrowRight");
    await expect.poll(async () => (await focusedCard(page)).column).toBe(columns[readyIndex + 1]);
    await keys.press(page, "ArrowLeft");
    await expect.poll(async () => (await focusedCard(page)).label).toBe(titles.beta);
    await keys.press(page, "Enter");
    const boardDetail = detailSurface(page, isMobile, titles.beta);
    await expect(boardDetail.getByRole("heading", { level: 1, name: titles.beta })).toBeVisible();
    await closeDetail(page, isMobile, { escape: true, keys });
    await expect(page.getByRole("heading", { level: 1, name: project.name })).toBeFocused();
    await capture(page, testInfo, "keyboard-walk", {
      surface: "list/board/detail keyboard",
      keys: keys.actions,
      focusReturn: "project heading after Escape/Back",
      url: page.url()
    });
  });

  // Independent fresh-session case for the work detail URL. It is deliberately
  // separate from the project deep link below so each scenario still executes
  // while the release HTML route for /portal/work/:id is unauthorized: this one
  // stops at the document assertion, the project case must still run.
  test("a fresh signed-out session re-enters the work detail deep link over its project board", async ({ page, browser, baseURL, isMobile }, testInfo) => {
    await login(page);
    const fixture = await seedProject(page, {
      prefix: "RW",
      items: [{
        title: `Work deep link ${uniqueKey("J")}`,
        description: "Fresh-session work detail deep link",
        status: "ready",
        priority: "low",
        assignee_type: "unassigned"
      }]
    });
    const subject = [...fixture.items.values()][0];
    const workDetailLink = workUrl(subject.id, { view: "board" });
    // A new context starts signed out; nothing is faked or signed in first.
    const context = await browser.newContext({ ...deviceContextOptions(testInfo), baseURL });
    const fresh = await context.newPage();
    monitorBrowserErrors(fresh);
    try {
      const document = await fresh.goto(workDetailLink);
      expect(document.status(), "the release must serve the authenticated work detail document").toBe(200);
      await expect(fresh).toHaveURL(/\/portal\/login\?return_to=/);
      expect(new URL(fresh.url()).searchParams.get("return_to")).toBe(workDetailLink);
      await fresh.getByLabel("Operator token").fill(operatorToken);
      await fresh.getByRole("button", { name: "Open workspace" }).click();
      await expect.poll(
        () => new URL(fresh.url()).pathname + new URL(fresh.url()).search,
        { message: "login must return to the requested work detail deep link" }
      ).toBe(workDetailLink);
      // Desktop renders the drawer for every work URL, including one without a
      // projectId; narrow widths render the full page inside main content.
      const freshDetail = detailSurface(fresh, isMobile, subject.title);
      await expect(freshDetail.getByRole("heading", { level: 1, name: subject.title })).toBeVisible();
      if (!isMobile) {
        // Desktop keeps the scoped work URL as a drawer over the project board
        // background for that work item's project.
        await expect(fresh.locator('#main-content [aria-label="Work board"]')).toHaveCount(1);
        await expect(fresh.locator("#main-content")).toContainText(fixture.name);
        await expect(fresh.locator("#main-content")).toContainText(subject.title);
      }
      await fresh.reload();
      await expect(freshDetail.getByRole("heading", { level: 1, name: subject.title })).toBeVisible();
      if (!isMobile) await expect(fresh.locator('#main-content [aria-label="Work board"]')).toHaveCount(1);
      await capture(fresh, testInfo, "deep-link-work-reentry", {
        surface: "fresh signed-out session, work detail deep link",
        workDetailUrl: workDetailLink,
        note: "Login returned to the requested work detail URL and the reload kept the drawer over its project board",
        url: fresh.url()
      });
      // Successful paths still require zero browser errors; a failing primary
      // assertion above keeps its own failure instead of being overwritten here.
      assertNoBrowserErrors(fresh);
    } finally {
      await context.close();
    }
  });

  // Independent fresh-session case for the project board/filter URL, using its
  // own signed-out context and fixture so it runs even when the work detail
  // document route above is unavailable.
  test("a fresh signed-out session re-enters the project board and filter deep link", async ({ page, browser, baseURL }, testInfo) => {
    await login(page);
    const fixture = await seedProject(page, {
      prefix: "RX",
      items: [
        {
          title: `Project deep link keeper ${uniqueKey("K")}`,
          description: "Survives the filter and the reload",
          status: "ready",
          priority: "low",
          assignee_type: "unassigned"
        },
        {
          title: `Project deep link other ${uniqueKey("L")}`,
          description: "Filtered out of the deep link",
          status: "ready",
          priority: "low",
          assignee_type: "unassigned"
        }
      ]
    });
    const [keeper, other] = [...fixture.items.values()];
    const projectLink = `/portal/projects/${fixture.id}?view=board&q=keeper`;
    // A new context starts signed out with the running project's exact viewport
    // and device flags; nothing is faked or signed in first.
    const context = await browser.newContext({ ...deviceContextOptions(testInfo), baseURL });
    const fresh = await context.newPage();
    monitorBrowserErrors(fresh);
    try {
      const document = await fresh.goto(projectLink);
      expect(document.status(), "the release must serve the authenticated project document").toBe(200);
      await expect(fresh).toHaveURL(/\/portal\/login\?return_to=/);
      expect(new URL(fresh.url()).searchParams.get("return_to")).toBe(projectLink);
      await fresh.getByLabel("Operator token").fill(operatorToken);
      await fresh.getByRole("button", { name: "Open workspace" }).click();
      expect(
        new URL(fresh.url()).pathname + new URL(fresh.url()).search,
        "login must return to the requested project view/filter URL"
      ).toBe(projectLink);
      const board = fresh.getByRole("region", { name: ui.boardRegion });
      await expect(board).toBeVisible();
      await expect(board.getByRole("link", { name: keeper.title })).toBeVisible();
      await expect(board.getByRole("link", { name: other.title })).toHaveCount(0);
      await fresh.reload();
      await expect(fresh.getByRole("region", { name: ui.boardRegion })).toBeVisible();
      await expect(fresh.getByRole("link", { name: keeper.title })).toBeVisible();
      await expect(fresh.getByRole("link", { name: other.title })).toHaveCount(0);
      await capture(fresh, testInfo, "deep-link-project-view", {
        surface: "fresh signed-out session, project board and filter deep link",
        projectViewUrl: projectLink,
        note: "Login returned to the requested project view/filter URL, and the reload kept the filtered board",
        url: fresh.url()
      });
      // Successful paths still require zero browser errors; a failing primary
      // assertion above keeps its own failure instead of being overwritten here.
      assertNoBrowserErrors(fresh);
    } finally {
      await context.close();
    }
  });

  test("a work URL claiming another project is canonicalized and never renders foreign work", async ({ page, isMobile }, testInfo) => {
    await login(page);
    const claimed = await seedProject(page, {
      prefix: "RP",
      items: [{
        title: `Claimed project work ${uniqueKey("F")}`,
        description: "Belongs to the project the URL wrongly claims",
        status: "ready",
        priority: "low",
        assignee_type: "unassigned"
      }]
    });
    const served = await seedProject(page, {
      prefix: "RQ",
      items: [{
        title: `Served project work ${uniqueKey("G")}`,
        description: "The project the served work item actually belongs to",
        status: "ready",
        priority: "low",
        assignee_type: "unassigned"
      }]
    });
    const claimedItem = [...claimed.items.values()][0];
    const subject = [...served.items.values()][0];

    await page.goto(projectUrl(served.id));
    await page.getByRole("link", { name: subject.title }).click();
    const detail = detailSurface(page, isMobile, subject.title);
    await expect(detail.getByRole("heading", { level: 1, name: subject.title })).toBeVisible();

    // Re-enter this history entry with a work URL that claims the other
    // project: the served projection wins, the URL is canonicalized, and no
    // work from the claimed project is rendered at any moment.
    await page.evaluate(
      ({ href }) => window.history.replaceState(window.history.state, "", href),
      { href: workUrl(subject.id, { projectId: claimed.id }) }
    );
    await page.goBack();
    await expect(page.getByRole("heading", { level: 1, name: served.name })).toBeVisible();
    await page.goForward();
    await expect(detail.getByRole("heading", { level: 1, name: subject.title })).toBeVisible();
    await expect.poll(() => new URL(page.url()).searchParams.get("projectId")).toBe(served.id);
    await expect(page.getByText(claimedItem.title)).toHaveCount(0);
    await capture(page, testInfo, "work-url-canonicalization", {
      surface: "detail URL canonicalization",
      claimedProjectId: claimed.id,
      servedProjectId: served.id,
      observations: [
        `work URL claiming ${claimed.key} was canonicalized to the served ${served.key}`,
        "the claimed project's work item was never rendered anywhere in the document",
        "the subject detail stayed mounted across canonicalization"
      ],
      url: page.url()
    });
    await closeDetail(page, isMobile, { escape: true });
    await expect(page).toHaveURL(new RegExp(`/portal/projects/${served.id}(?:\\?|$)`));
  });

  // Migrated from the classic detail-security check in portal.spec.mjs. This is
  // a malicious-response injection probe: the real detail response is captured
  // and tampered with in flight. It stays separate from every real-data
  // acceptance above, which never substitutes a payload for a server response.
  test("injected malicious detail values are rendered as text and never as markup or unsafe URLs", async ({ page, isMobile }, testInfo) => {
    await login(page);
    const key = uniqueKey("RI");
    const title = `Injected detail ${key}`;
    const project = await seedProject(page, {
      prefix: "RI",
      items: [{
        title,
        description: "Real work whose response is tampered with in flight",
        status: "ready",
        priority: "medium",
        assignee_type: "unassigned"
      }]
    });
    const item = project.items.get(title);

    const payload = {
      markup: '\"><img src=x onerror=alert(1)>',
      svg: "<svg/onload=alert(1)>",
      scheme: "javascript:alert(1)"
    };
    const injectedFields = [];
    await page.route(`**/portal/api/work-items/${item.id}`, async (route) => {
      const response = await route.fetch();
      const body = await response.json();
      // Schema-valid mutation: every injected value stays inside an existing
      // string field, so the typed boundary accepts the response and the
      // renderer itself must escape it. Replacing whole structures or breaking
      // required fields would make this probe vacuous through a decode error.
      body.work_item.external = {
        provider: "github",
        id: "42",
        url: payload.scheme,
        state: payload.markup,
        available: true,
        assignee: null,
        labels: [],
        updated_at: "2026-11-15T09:45:00.000000Z",
        resource_id: body.work_item.project_id
      };
      injectedFields.push("work_item.external.url", "work_item.external.state");
      body.work_item.delivery.pull_request.status = payload.markup;
      body.work_item.delivery.pull_request.url = payload.scheme;
      body.work_item.delivery.ci.status = payload.svg;
      body.work_item.delivery.ci.url = payload.scheme;
      body.work_item.delivery.review.status = payload.markup;
      body.work_item.delivery.review.url = payload.scheme;
      injectedFields.push(
        "work_item.delivery.pull_request.status",
        "work_item.delivery.pull_request.url",
        "work_item.delivery.ci.status",
        "work_item.delivery.ci.url",
        "work_item.delivery.review.status",
        "work_item.delivery.review.url"
      );
      body.work_item.review_status = payload.svg;
      injectedFields.push("work_item.review_status");
      body.outcome.summary = payload.markup;
      injectedFields.push("outcome.summary");
      body.raw.timeline = [{
        source: "event",
        run_id: "00000000-0000-4000-8000-000000000001",
        generation: 1,
        recorded_at: "2026-11-15T09:45:00.000000Z",
        data: {
          event_id: "00000000-0000-4000-8000-000000000002",
          sequence: 1,
          kind: payload.svg,
          occurred_at: "2026-11-15T09:45:00.000000Z",
          payload: { message: payload.markup, url: payload.scheme }
        }
      }];
      injectedFields.push("raw.timeline[0].data.kind", "raw.timeline[0].data.payload.message", "raw.timeline[0].data.payload.url");
      await route.fulfill({ response, json: body });
    });

    await page.goto(projectUrl(project.id));
    await page.getByRole("list", { name: ui.workItems }).getByRole("link", { name: title }).click();
    // The tampered response must decode and render; an error fallback would
    // make the probe vacuous.
    const detail = detailSurface(page, isMobile, title);
    await expect(detail.getByRole("heading", { level: 1, name: title })).toBeVisible();
    await expect(detail.getByText(ui.noEvidence, { exact: false })).toBeVisible();
    // Each injected payload is rendered, as inert text.
    await expect(detail.getByText(payload.markup, { exact: false }).first()).toBeVisible();
    await expect(detail.getByText(payload.svg, { exact: false }).first()).toBeVisible();
    await expect(detail.locator("dl.delivery-list")).toContainText(`CI ${payload.svg}`);
    await expect(detail.locator("ol.timeline > li.timeline-entry")).toHaveCount(1);
    // Escaped, never parsed as markup.
    const markup = await page.locator("body").innerHTML();
    expect(markup).toContain("&lt;img");
    expect(markup).toContain("&lt;svg/onload");
    expect(markup).not.toContain("<img");
    expect(markup).not.toContain("<svg/onload");
    // No active unsafe URL and no injected element or event handler.
    expect(await page.locator("img").count()).toBe(0);
    await expect(detail.getByRole("link", { name: "Open external record" })).toHaveCount(0);
    await expect(detail.locator("dl.delivery-list").getByRole("link", { name: "Open" })).toHaveCount(0);
    const unsafeUrlAttributes = await page.evaluate(() =>
      [...document.querySelectorAll("[href],[src],[action],[formaction]")]
        .flatMap((element) => ["href", "src", "action", "formaction"].map((name) => element.getAttribute(name)))
        .filter((value) => typeof value === "string" && /^\s*(javascript|data|vbscript):/i.test(value))
    );
    expect(unsafeUrlAttributes).toEqual([]);
    await capture(page, testInfo, "malicious-response-injection", {
      surface: "detail",
      kind: "malicious-response injection (fault isolation, not real-data evidence)",
      realDataEvidence: false,
      decodedAndRendered: "the schema-valid tampered response decoded and rendered; no error fallback was accepted",
      injectedFields,
      renderedPayloadText: [payload.markup, payload.svg],
      checks: [
        "injected markup appears escaped as text and never as an element",
        "no javascript:/data:/vbscript: value in any href, src, action or formaction attribute",
        "no injected img element, event-handler attribute, provider link or delivery link"
      ],
      url: page.url()
    });
    if (isMobile) await page.getByRole("link", { name: ui.backToProject }).click();
    else await page.keyboard.press("Escape");
  });

  // Daemon-free guard for the obsolete older-history request. The backend cannot
  // serve a timeline cursor without a real daemon run, so this case injects a
  // generated, schema-valid contract fixture for the history owner while the
  // current detail stays real data. It is explicitly synthetic fault injection:
  // no capture is taken, the injected cursor is never claimed as real, and the
  // real daemon-backed cursor case runs separately in
  // portal-live-daemon.spec.mjs.
  test("an obsolete older-history failure never mutates a newer real detail (synthetic fault injection)", async ({ page, isMobile }) => {
    await login(page);
    const project = await seedProject(page, {
      prefix: "RG",
      items: [
        {
          title: `Injected history owner ${uniqueKey("H")}`,
          description: "Synthetic fault isolation for the obsolete older-history request",
          status: "ready",
          priority: "low",
          assignee_type: "unassigned"
        },
        {
          title: `Real current detail ${uniqueKey("I")}`,
          description: "Real detail selected while the obsolete request is in flight",
          status: "ready",
          priority: "low",
          assignee_type: "unassigned"
        }
      ]
    });
    const [historyOwner, current] = [...project.items.values()];
    const fixture = JSON.parse(
      await readFile(new URL("../../contracts/fixtures/valid/work-detail-read.paginated.json", import.meta.url), "utf8")
    );
    fixture.work_item.id = historyOwner.id;
    fixture.work_item.key = historyOwner.key;
    fixture.work_item.title = historyOwner.title;
    fixture.work_item.project_id = project.id;
    const fixtureFingerprint = fixture.outcome.goal;
    expect(fixture.raw.next_before).toBeTruthy();
    expect(fixtureFingerprint).toBeTruthy();

    await page.route(`**/portal/api/work-items/${historyOwner.id}`, async (route) => {
      await route.fulfill({ json: fixture });
    });
    const heldStarted = Promise.withResolvers();
    const heldRelease = Promise.withResolvers();
    const heldSettled = Promise.withResolvers();
    await page.route(`**/portal/api/work-items/${historyOwner.id}/timeline?before=*`, async (route) => {
      heldStarted.resolve();
      await heldRelease.promise;
      try {
        await route.fulfill({
          status: 503,
          contentType: "application/json",
          body: JSON.stringify({ error: { code: "provider_failure", message: "Older history failed" } })
        });
      } finally {
        heldSettled.resolve();
      }
    });
    allowBrowserError(page, /status of 503/);

    await page.goto(projectUrl(project.id));
    await page.getByRole("link", { name: historyOwner.title }).click();
    const ownerDetail = detailSurface(page, isMobile, historyOwner.title);
    await expect(ownerDetail.getByRole("heading", { level: 1, name: historyOwner.title })).toBeVisible();
    const ownerHistory = ownerDetail.locator("section.detail-section", {
      has: page.getByRole("heading", { name: "Execution history" })
    });
    await expect(ownerHistory.getByRole("button", { name: "Load older history" })).toBeVisible();
    await ownerHistory.getByRole("button", { name: "Load older history" }).click();
    await heldStarted.promise;

    await closeDetail(page, isMobile, { escape: true });
    await page.getByRole("link", { name: current.title }).click();
    const currentDetail = detailSurface(page, isMobile, current.title);
    await expect(currentDetail.getByRole("heading", { level: 1, name: current.title })).toBeVisible();
    await expect(currentDetail.getByText(current.description)).toBeVisible();
    heldRelease.resolve();
    await heldSettled.promise;
    await expect(currentDetail.getByRole("heading", { level: 1, name: current.title })).toBeVisible();
    await expect(currentDetail.getByText("Earlier history could not be loaded.")).toHaveCount(0);
    await expect(currentDetail.getByText(fixtureFingerprint)).toHaveCount(0);
    // Handler completion only proves the 503 was delivered, not that the page
    // applied it. The real refresh re-renders the current detail from its own
    // 200 response, then the same invariants are asserted again.
    const currentRefresh = page.waitForResponse((response) => {
      const url = new URL(response.url());
      return url.pathname === `/portal/api/work-items/${current.id}` && response.status() === 200;
    });
    await currentDetail.getByRole("button", { name: ui.refresh, exact: true }).click();
    expect((await currentRefresh).status()).toBe(200);
    await expect(currentDetail.getByRole("heading", { level: 1, name: current.title })).toBeVisible();
    await expect(currentDetail.getByText(current.description)).toBeVisible();
    await expect(currentDetail.getByText("Earlier history could not be loaded.")).toHaveCount(0);
    await expect(currentDetail.getByText(fixtureFingerprint)).toHaveCount(0);
    await closeDetail(page, isMobile, { escape: true });
  });

  test.describe("concurrency", () => {
    test.skip(({ isMobile }) => isMobile, "The late-response proof uses a desktop session.");

    test("held real responses never render as another project's data or newer state", async ({ page }, testInfo) => {
      await login(page);
      const held = await seedProject(page, {
        prefix: "RA",
        items: [{
          title: `Held project work ${uniqueKey("A")}`,
          description: "Held older project snapshot",
          status: "backlog",
          priority: "low",
          assignee_type: "unassigned"
        }]
      });
      const current = await seedProject(page, {
        prefix: "RB",
        items: [
          {
            title: `Current project work ${uniqueKey("B")}`,
            description: "Current project snapshot",
            status: "ready",
            priority: "low",
            assignee_type: "unassigned"
          },
          {
            title: `Detail one ${uniqueKey("C")}`,
            description: "First detail target",
            status: "ready",
            priority: "low",
            assignee_type: "unassigned"
          },
          {
            title: `Detail two ${uniqueKey("D")}`,
            description: "Second detail target",
            status: "ready",
            priority: "low",
            assignee_type: "unassigned"
          }
        ]
      });
      const heldItem = [...held.items.values()][0];
      const [currentItem, detailOne, detailTwo] = [...current.items.values()];

      // A genuinely captured real snapshot, held while another project renders,
      // then released late. The captured bytes are never regenerated.
      const heldProjectStarted = Promise.withResolvers();
      const heldProjectRelease = Promise.withResolvers();
      const heldProjectSettled = Promise.withResolvers();
      const heldProjectFulfilled = [];
      await page.route(`**/portal/api/workspace?project_id=${held.id}`, async (route) => {
        const response = await route.fetch();
        const body = await response.json();
        heldProjectStarted.resolve();
        await heldProjectRelease.promise;
        try {
          await route.fulfill({ response, json: body });
          heldProjectFulfilled.push("workspace");
        } catch {
          // The page may have cancelled the held request. Fulfilling only means
          // the response was delivered, never that the page applied it; the
          // post-release re-assertions below prove it was not applied.
        } finally {
          heldProjectSettled.resolve();
        }
      });

      await page.goto(projectUrl(current.id));
      await expect(page.getByRole("heading", { level: 1, name: current.name })).toBeVisible();
      await page.getByLabel("Project", { exact: true }).selectOption(held.id);
      await heldProjectStarted.promise;
      // The picker disables itself while its project directory query is pending
      // (pre-existing shell behavior), so return through the real workspace
      // navigation and the project directory card, with the held real response
      // still unreleased.
      await page.getByRole("navigation", { name: "Workspace navigation" })
        .getByRole("link", { name: "Projects", exact: true }).click();
      const directory = page.getByRole("list", { name: "Projects" });
      const currentCard = directory.getByRole("link", { name: new RegExp(current.name) });
      await expect(currentCard).toBeVisible();
      await currentCard.click();
      await expect(page.getByRole("heading", { level: 1, name: current.name })).toBeVisible();
      heldProjectRelease.resolve();
      await heldProjectSettled.promise;
      await expect(page.getByRole("heading", { level: 1, name: current.name })).toBeVisible();
      await expect(page.getByRole("link", { name: currentItem.title })).toBeVisible();
      await expect(page.getByRole("link", { name: heldItem.title })).toHaveCount(0);
      await expect(page.getByLabel("Project", { exact: true })).toHaveValue(current.id);

      const heldDetailStarted = Promise.withResolvers();
      const heldDetailRelease = Promise.withResolvers();
      const heldDetailSettled = Promise.withResolvers();
      const heldDetailOutcomes = [];
      await page.route(`**/portal/api/work-items/${detailOne.id}`, async (route) => {
        const response = await route.fetch();
        const body = await response.json();
        heldDetailStarted.resolve();
        await heldDetailRelease.promise;
        try {
          await route.fulfill({ response, json: body });
          heldDetailOutcomes.push("delivered");
        } catch {
          // See above: delivery is not application.
          heldDetailOutcomes.push("aborted");
        } finally {
          heldDetailSettled.resolve();
        }
      });

      await page.getByRole("link", { name: detailOne.title }).click();
      await heldDetailStarted.promise;
      // The held detail may still be pending; browser history returns to the
      // project list without depending on the drawer's close control.
      await page.goBack();
      await expect(page.getByRole("heading", { level: 1, name: current.name })).toBeVisible();
      await page.getByRole("link", { name: detailTwo.title }).click();
      await expect(page.getByRole("dialog", { name: detailTwo.title })).toBeVisible();
      heldDetailRelease.resolve();
      await heldDetailSettled.promise;
      await expect(page.getByRole("dialog", { name: detailTwo.title })).toBeVisible();
      await expect(page.getByRole("heading", { level: 1, name: detailOne.title })).toHaveCount(0);
      await page.keyboard.press("Escape");

      const newer = await createWorkItem(page, current.id, {
        title: `Newer state ${uniqueKey("E")}`,
        description: "Created after the page rendered",
        status: "ready",
        priority: "low",
        assignee_type: "unassigned"
      });
      await page.getByRole("button", { name: ui.refresh, exact: true }).click();
      await expect(page.getByRole("link", { name: newer.title })).toBeVisible();
      // Re-assert every held-response negative invariant after the newer state
      // rendered from the real refresh response: delivery never became
      // application for either held response.
      await expect(page.getByRole("heading", { level: 1, name: current.name })).toBeVisible();
      await expect(page.getByRole("link", { name: currentItem.title })).toBeVisible();
      await expect(page.getByRole("link", { name: heldItem.title })).toHaveCount(0);
      await expect(page.getByText(heldItem.title)).toHaveCount(0);
      await expect(page.getByLabel("Project", { exact: true })).toHaveValue(current.id);
      await expect(page.getByRole("heading", { level: 1, name: detailOne.title })).toHaveCount(0);
      await expect(page.getByRole("heading", { level: 1, name: detailTwo.title })).toHaveCount(0);
      await capture(page, testInfo, "late-response-guard", {
        surface: "project switch, detail selection and refresh concurrency",
        heldProjectResponse: `real /portal/api/workspace snapshot for ${held.key} released after ${current.key} rendered`,
        heldProjectOutcomes: heldProjectFulfilled.length === 1 ? ["delivered"] : ["aborted"],
        heldDetailResponse: `real /portal/api/work-items/${detailOne.id} released after ${detailTwo.title} rendered`,
        heldDetailOutcomes,
        heldResponsesNotApplied:
          "re-asserted after release and after the newer refresh: the held project's data, the held work row and both detail headings are never rendered as newer state",
        newerState: `${newer.title} appeared only from the real refresh response`,
        url: page.url()
      });
    });
  });
});
