import fs from "node:fs/promises";
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
  await page.goto("/portal/login?return_to=%2Fportal%2Fclassic");
  await page.getByLabel("Operator token").fill(operatorToken);
  await Promise.all([
    page.waitForURL(/\/portal\/classic(?:\?|#|$)/),
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
  await dialog.getByLabel("Provider", { exact: true }).fill(resource.provider);
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

  test("waiting input and failed retry complete through the portal", async ({ page, isMobile }) => {
    test.skip(isMobile, "The live lifecycle matrix runs once at desktop width; the history cursor scenario also runs at narrow width.");
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

  test("raw execution history loads older daemon records on demand", async ({ page, isMobile }, testInfo) => {
    const project = uniqueProject();
    const historyTitle = `History workflow ${project.key}`;
    const secondHistoryTitle = `History follow-up ${project.key}`;

    await login(page);
    await createProject(page, project);
    const projectId = new URL(page.url()).searchParams.get("project_id");
    expect(projectId).toBeTruthy();
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
    const history = await detailJSON(page);
    expect(history.work_item.execution.state).toBe("completed");
    expect(history.raw.next_before).not.toBeNull();
    await page.getByRole("button", { name: "Close details" }).click();

    await createAgentWork(page, {
      title: secondHistoryTitle,
      profile: fixtureBinding("history"),
      workspace: fixtureBinding("history"),
      description: "Second real execution so a failing older-history page can be proved not to mutate a newer detail."
    });
    await openWork(page, secondHistoryTitle);
    await page.getByRole("button", { name: "Start run" }).click();
    await expect(page.locator("#detail-content")).toContainText("Completed the requested engineering work", {
      timeout: 30_000
    });
    const secondHistory = await detailJSON(page);
    expect(secondHistory.work_item.execution.state).toBe("completed");
    expect(secondHistory.raw.next_before).not.toBeNull();
    await page.getByRole("button", { name: "Close details" }).click();

    // The daemon mutations above stay classic; the raw history read now runs on
    // /portal/work/:workItemId, where older pages load through the real cursor.
    // Desktop renders the detail as a side panel, narrow widths as a full page.
    const historySection = (detail) =>
      detail.locator("section.detail-section", { has: detail.page().getByRole("heading", { name: "Execution history" }) });
    const reportedSection = (detail) =>
      detail.locator("section.detail-section", { has: detail.page().getByRole("heading", { name: "Reported execution output", exact: true }) });
    const entriesOf = (section) => section.locator("ol.timeline > li.timeline-entry");
    const workSurface = (title) =>
      isMobile ? page.locator("#main-content") : page.getByRole("dialog", { name: title });
    const leaveWorkSurface = async () => {
      if (isMobile) await page.getByRole("link", { name: /^Back to projects?$/ }).click();
      else await page.keyboard.press("Escape");
    };
    const detailResponseFor = (workItemId) => page.waitForResponse((response) => {
      const url = new URL(response.url());
      return url.pathname === `/portal/api/work-items/${workItemId}` && response.status() === 200;
    });
    const timelineRequestArm = (workItemId) => page.waitForRequest((request) => {
      const url = new URL(request.url());
      return url.pathname === `/portal/api/work-items/${workItemId}/timeline` && url.searchParams.has("before");
    });

    // Phase 1: the first work item paginates successfully on the cursor of the
    // detail response this navigation consumed. Cursors are signed opaque
    // tokens minted per GET, so tokens are never compared across independent
    // reads and never decoded or normalized.
    await page.goto(`/portal/projects/${projectId}`);
    const firstDetailResponse = detailResponseFor(history.work_item.id);
    await page.getByRole("link", { name: historyTitle }).click();
    const historyDetail = workSurface(historyTitle);
    await expect(historyDetail.getByRole("heading", { level: 1, name: historyTitle })).toBeVisible();
    const consumedCursor = (await (await firstDetailResponse).json()).raw.next_before;
    expect(consumedCursor).toBeTruthy();
    const historyPanel = historySection(historyDetail);
    const firstPageEntries = entriesOf(historyPanel);
    const firstCount = await firstPageEntries.count();
    expect(firstCount).toBeGreaterThan(0);
    const olderPageRequest = page.waitForResponse((response) => {
      const url = new URL(response.url());
      return url.pathname === `/portal/api/work-items/${history.work_item.id}/timeline` &&
        url.searchParams.has("before") && response.status() === 200;
    });
    const olderPageCursor = timelineRequestArm(history.work_item.id);
    await historyPanel.getByRole("button", { name: "Load older history" }).click();
    expect(new URL((await olderPageCursor).url()).searchParams.get("before")).toBe(consumedCursor);
    const olderPage = await olderPageRequest;
    expect(olderPage.status()).toBe(200);
    await expect.poll(() => firstPageEntries.count(), { timeout: 15_000 }).toBeGreaterThan(firstCount);
    await expect(historyPanel.getByText("No older history recorded.")).toBeVisible();
    const firstEntriesAfterOlderPage = await firstPageEntries.count();
    const olderRecords = await portalRequest(
      page,
      `/portal/api/work-items/${history.work_item.id}/timeline?before=${encodeURIComponent(consumedCursor)}`
    );
    expect(olderRecords.status).toBe(200);
    expect(olderRecords.body.items.length).toBeGreaterThan(0);
    expect(JSON.stringify(olderRecords.body.items)).toContain("debug-001");
    await leaveWorkSurface();

    // Phase 2: an in-flight older-history failure for the second item must not
    // notify or mutate the detail the operator selects meanwhile. That
    // navigation may cancel the obsolete request, so the settle barrier is the
    // handler's own completion plus the visible UI state, never a timer; handler
    // completion means the response was delivered, not that the page applied it.
    const secondDetailResponse = detailResponseFor(secondHistory.work_item.id);
    await page.getByRole("link", { name: secondHistoryTitle }).click();
    const secondDetail = workSurface(secondHistoryTitle);
    await expect(secondDetail.getByRole("heading", { level: 1, name: secondHistoryTitle })).toBeVisible();
    const secondPanel = historySection(secondDetail);
    const secondEntries = entriesOf(secondPanel);
    const secondCount = await secondEntries.count();
    expect(secondCount).toBeGreaterThan(0);
    const secondConsumedCursor = (await (await secondDetailResponse).json()).raw.next_before;
    expect(secondConsumedCursor).toBeTruthy();
    allowBrowserError(page, /status of 503/);
    const failureStarted = Promise.withResolvers();
    const failureRelease = Promise.withResolvers();
    const failureSettled = Promise.withResolvers();
    const failureCursors = [];
    const failureOutcomes = [];
    await page.route(`**/portal/api/work-items/${secondHistory.work_item.id}/timeline?before=*`, async (route) => {
      failureCursors.push(new URL(route.request().url()).searchParams.get("before"));
      failureStarted.resolve();
      await failureRelease.promise;
      try {
        await route.fulfill({
          status: 503,
          contentType: "application/json",
          body: JSON.stringify({ error: { code: "provider_failure", message: "Older history failed" } })
        });
        failureOutcomes.push("delivered");
      } catch {
        failureOutcomes.push("aborted");
      } finally {
        failureSettled.resolve();
      }
    });
    await secondPanel.getByRole("button", { name: "Load older history" }).click();
    await failureStarted.promise;
    expect(failureCursors.length).toBeGreaterThan(0);
    expect(new Set(failureCursors)).toEqual(new Set([secondConsumedCursor]));
    await leaveWorkSurface();
    await page.getByRole("link", { name: historyTitle }).click();
    const newerDetail = workSurface(historyTitle);
    await expect(newerDetail.getByRole("heading", { level: 1, name: historyTitle })).toBeVisible();
    failureRelease.resolve();
    await failureSettled.promise;
    await expect(newerDetail.getByRole("heading", { level: 1, name: historyTitle })).toBeVisible();
    await expect(newerDetail.getByText("Earlier history could not be loaded.")).toHaveCount(0);
    // Delivery is not application: refresh the selected detail from its own
    // real 200 response and re-assert the obsolete failure left no stale error
    // and no stale data on this surface.
    const newerRefresh = page.waitForResponse((response) => {
      const url = new URL(response.url());
      return url.pathname === `/portal/api/work-items/${history.work_item.id}` && response.status() === 200;
    });
    await newerDetail.getByRole("button", { name: "Refresh", exact: true }).click();
    expect((await newerRefresh).status()).toBe(200);
    await expect(newerDetail.getByRole("heading", { level: 1, name: historyTitle })).toBeVisible();
    await expect(newerDetail.getByText("Earlier history could not be loaded.")).toHaveCount(0);
    await expect(newerDetail.getByText(secondHistoryTitle)).toHaveCount(0);
    await leaveWorkSurface();
    await page.unrouteAll({ behavior: "wait" });
    // Phase 3: recovery on a freshly loaded project document, so the detail
    // response and its cursor belong only to this navigation. The first active
    // older-history load is faulted, then retried once against the real
    // endpoint; an obsolete failure is never assumed to be cached.
    await page.goto(`/portal/projects/${projectId}`);
    const currentDetailResponse = detailResponseFor(secondHistory.work_item.id);
    await page.getByRole("link", { name: secondHistoryTitle }).click();
    const currentDetail = workSurface(secondHistoryTitle);
    await expect(currentDetail.getByRole("heading", { level: 1, name: secondHistoryTitle })).toBeVisible();
    const currentPanel = historySection(currentDetail);
    const currentEntries = entriesOf(currentPanel);
    const currentCount = await currentEntries.count();
    expect(currentCount).toBeGreaterThan(0);
    const currentCursor = (await (await currentDetailResponse).json()).raw.next_before;
    expect(currentCursor).toBeTruthy();
    // The real served projection renders its reported output and delivery
    // values; expectations are derived from the same response the API serves,
    // reported output is never presented as accepted, and the injected fault
    // above stays isolated from this real data.
    const servedDetail = (await portalRequest(page, `/portal/api/work-items/${secondHistory.work_item.id}`)).body;
    const servedReported = servedDetail.outcome.summary ??
      (servedDetail.work_item.execution?.result?.summary ?? null);
    expect(servedReported).toBeTruthy();
    // Scoped to the actual Reported execution output section: the same summary
    // text legitimately repeats as a timeline entry headline.
    await expect(reportedSection(currentDetail).getByText(servedReported, { exact: false })).toBeVisible();
    await expect(currentDetail.getByText("No accepted evidence available", { exact: false })).toBeVisible();
    const deliveryList = currentDetail.locator("dl.delivery-list");
    for (const [label, entry] of [
      ["Pull request", servedDetail.work_item.delivery.pull_request],
      ["CI", servedDetail.work_item.delivery.ci],
      ["Review", servedDetail.work_item.delivery.review]
    ]) {
      const row = deliveryList.locator("div", { hasText: label }).first();
      await expect(row).toContainText(`source: ${entry.source}`);
      if (typeof entry.url === "string" && entry.url.startsWith("http")) {
        await expect(row.getByRole("link", { name: "Open" })).toHaveAttribute("href", entry.url);
      }
    }

    const faultStarted = Promise.withResolvers();
    const faultRelease = Promise.withResolvers();
    const faultSettled = Promise.withResolvers();
    const faultCursors = [];
    const faultOutcomes = [];
    await page.route(`**/portal/api/work-items/${secondHistory.work_item.id}/timeline?before=*`, async (route) => {
      faultCursors.push(new URL(route.request().url()).searchParams.get("before"));
      faultStarted.resolve();
      await faultRelease.promise;
      try {
        await route.fulfill({
          status: 503,
          contentType: "application/json",
          body: JSON.stringify({ error: { code: "provider_failure", message: "Older history failed" } })
        });
        faultOutcomes.push("delivered");
      } catch {
        faultOutcomes.push("aborted");
      } finally {
        faultSettled.resolve();
      }
    });
    await currentPanel.getByRole("button", { name: "Load older history" }).click();
    await faultStarted.promise;
    expect(new Set(faultCursors)).toEqual(new Set([currentCursor]));
    faultRelease.resolve();
    await faultSettled.promise;
    // UI state barrier: the failed page is reported on its own surface.
    await expect(currentPanel.getByText("Earlier history could not be loaded.")).toBeVisible();
    await expect(currentPanel.getByRole("button", { name: "Retry history" })).toBeVisible();
    await page.unrouteAll({ behavior: "wait" });

    const retryRequest = timelineRequestArm(secondHistory.work_item.id);
    const retryResponse = page.waitForResponse((response) => {
      const url = new URL(response.url());
      return url.pathname === `/portal/api/work-items/${secondHistory.work_item.id}/timeline` && response.status() === 200;
    });
    await currentPanel.getByRole("button", { name: "Retry history" }).click();
    const retryCursor = new URL((await retryRequest).url()).searchParams.get("before");
    expect(retryCursor).toBe(currentCursor);
    expect((await retryResponse).status()).toBe(200);
    await expect.poll(() => currentEntries.count(), { timeout: 15_000 }).toBeGreaterThan(currentCount);
    await expect(currentPanel.getByText("No older history recorded.")).toBeVisible();
    // The rendered older page itself carries the older daemon records, not only
    // the separate API read.
    await expect(currentPanel.getByText("debug-001", { exact: false }).first()).toBeVisible();

    const historyCapture = testInfo.outputPath("work-history-pagination.png");
    await page.screenshot({ path: historyCapture, fullPage: true });
    await testInfo.attach("work-history-pagination", { path: historyCapture, contentType: "image/png" });
    await fs.writeFile(testInfo.outputPath("work-history-pagination.json"), JSON.stringify({
      url: page.url(),
      viewport: page.viewportSize(),
      width: isMobile ? "narrow (mobile Chromium project)" : "desktop",
      surface: "/portal/work/:workItemId execution history, real daemon records",
      currentWorkItem: secondHistory.work_item.id,
      currentWorkItemTitle: secondHistoryTitle,
      firstWorkItem: history.work_item.id,
      firstWorkItemTitle: historyTitle,
      firstNavigationCursor: consumedCursor,
      obsoleteFailureNavigationCursor: secondConsumedCursor,
      currentNavigationCursor: currentCursor,
      failureAttemptCursors: failureCursors,
      obsoleteFailureOutcomes: failureOutcomes,
      faultAttemptCursors: faultCursors,
      faultAttemptOutcomes: faultOutcomes,
      retryCursorBoundTo: retryCursor,
      firstPageEntries: firstCount,
      firstEntriesAfterOlderPage,
      obsoleteFailureEntries: secondCount,
      currentEntries: currentCount,
      currentEntriesAfterRetry: await currentEntries.count(),
      observations: [
        "first item: Load older history requested the cursor of the detail response that navigation consumed",
        "cursor tokens are signed per GET; only cursors from the same UI navigation are compared",
        "obsolete in-flight 503 settled without touching the detail selected meanwhile",
        "delivered means the response was sent by the handler; the visible UI state and the post-release refreshes prove what was applied",
        "fresh document navigation: the first older-history load was faulted, its current cursor asserted, and the error plus Retry rendered",
        "Retry history requested the current navigation's consumed cursor, loaded the real older page and exhausted it"
      ]
    }, null, 2));
  });
});
