// Goal 2 Compose daemon acceptance, driven on the React workspace surfaces.
//
// The production Compose stack completes F1-F7 through the real control plane.
// Fixtures are seeded through the authenticated Portal API: the classic
// project/resource/work dialogs were only seeding here, and their own planning
// surfaces are covered by portal-work-planning.spec.mjs and
// portal-connections.spec.mjs. The classic workspace document and its drawer
// are retired; every observable state below is read back from the rendered SPA
// or the server's own read APIs:
//   - board                -> /portal/projects/:projectId (view=board)
//   - work detail/controls -> /portal/work/:workItemId (drawer at desktop width)
//   - project resources    -> /portal/connections?projectId=
// The retired classic pieces are named in the migration comments at the point
// they were replaced, and in browser/README.md.
import { allowBrowserError, expect, test } from "./fixtures.mjs";
import {
  api,
  attachResource,
  createWorkItem,
  login,
  projectUrl,
  servedColumnTitles,
  uniqueKey,
  workUrl,
  workspace
} from "./readable-fixtures.mjs";

const composeEnabled = process.env.SYMMETRY_COMPOSE_DAEMON === "1";

const uniqueProject = (prefix = "DC") => {
  const key = uniqueKey(prefix);
  return { key, name: `Compose daemon ${key}` };
};

function fixtureDescription(mode, description) {
  return `[symmetry-fake-agent:${mode}]\n${description}`;
}

// The pages render more than one role=status region (notice lines and sr-only
// announcers), so every notice assertion is scoped by its text.
const statusNotice = (scope, text) => scope.locator('p[role="status"]', { hasText: text });
const boardCard = (page, title) => page.getByRole("link", { name: title });
const columnOf = (page, title) =>
  page.locator("[data-board-card]", { has: boardCard(page, title) }).getAttribute("data-board-column");
// The rendered board order and the served order, both by position.
const renderedColumnTitles = (page, state) =>
  page.evaluate(
    (column) =>
      Array.from(document.querySelectorAll(`[data-board-column='${column}'] .work-title`)).map((element) =>
        element.textContent?.trim()
      ),
    state
  );
async function moveByKeyboard(page, title, key) {
  const card = boardCard(page, title);
  await card.focus();
  await page.keyboard.press(`Shift+${key}`);
}
async function itemDetail(page, id) {
  const response = await api(page, "GET", `/portal/api/work-items/${id}`);
  expect(response.status, JSON.stringify(response.body)).toBe(200);
  return response.body;
}
async function openWork(page, id, title) {
  await page.goto(workUrl(id));
  const detail = page.getByRole("dialog", { name: title });
  await expect(detail.getByRole("heading", { level: 1, name: title })).toBeVisible();
  return detail;
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

    // The classic project dialog and its settings edit were seeding for the
    // daemon (default agent/workspace, description); the same values go through
    // the authenticated Portal API, and the SPA project surface renders them.
    const created = await api(page, "POST", "/portal/api/projects", {
      key: project.key,
      name: project.name,
      default_agent_profile: "default",
      default_workspace: "primary",
      description: "F1-F7 production Compose acceptance workspace"
    });
    expect(created.status, JSON.stringify(created.body)).toBe(201);
    const projectId = created.body.project.id;

    await page.goto(projectUrl(projectId, { view: "board" }));
    await expect(page.getByRole("heading", { level: 1, name: project.name })).toBeVisible();

    let runtime;
    await expect
      .poll(async () => {
        const read = await workspace(page, projectId);
        runtime = read.body.registered_runtimes.find(
          (candidate) => candidate.agent_profile === "default" && candidate.workspace === "primary"
        );
        return Boolean(runtime);
      }, { timeout: 30_000 })
      .toBe(true);

    const repository = await attachResource(page, projectId, {
      kind: "repository",
      status: "healthy",
      sync_status: "synced",
      name: "Compose repository",
      provider: "GitHub",
      external_ref: "wxxb789/symmetry",
      url: "https://github.com/wxxb789/symmetry",
      status_message: "Repository is available"
    });
    await attachResource(page, projectId, {
      kind: "ci",
      status: "degraded",
      sync_status: "failed",
      name: "Compose CI",
      provider: "GitHub Actions",
      external_ref: "goal-2-compose",
      url: "https://github.com/wxxb789/symmetry/actions",
      status_message: "CI webhook delivery failed"
    });
    await attachResource(page, projectId, {
      kind: "connection",
      status: "offline",
      sync_status: "stale",
      name: "Compose connection",
      provider: "GitHub",
      external_ref: "github-compose",
      url: "https://github.com",
      status_message: "Reconnect provider credentials"
    });
    await attachResource(page, projectId, {
      kind: "agent",
      status: "healthy",
      sync_status: "synced",
      name: "Compose agent",
      provider: "Symmetry",
      external_ref: "default"
    });
    await attachResource(page, projectId, {
      kind: "runtime",
      status: "healthy",
      sync_status: "synced",
      name: "Compose runtime",
      provider: "Symmetry",
      external_ref: runtime.runtime_id
    });

    // The classic health grid and resource dialog move to the project resource
    // console: the same served state (degraded/offline health, sync state,
    // status detail) is read from the surface the fixed IA gives resources.
    await page.goto(`/portal/connections?projectId=${projectId}`);
    let resources = page.getByRole("list", { name: "Project resources" });
    const connectionRow = resources
      .getByRole("listitem")
      .filter({ has: page.getByRole("button", { name: "Sync Compose connection", exact: true }) });
    await expect(connectionRow).toContainText("Reconnect provider credentials");
    await expect(connectionRow.getByText("Offline", { exact: true })).toBeVisible();
    await expect(connectionRow.getByText("Stale", { exact: true })).toBeVisible();
    await connectionRow.getByRole("button", { name: "Update Compose connection", exact: true }).click();
    const resourceDialog = page.getByRole("dialog", { name: "Update resource" });
    const resourceFacts = resourceDialog.locator("dl.connection-facts");
    await expect(resourceFacts).toContainText("Offline");
    await expect(resourceFacts).toContainText("Stale");
    await expect(resourceFacts).toContainText("Reconnect provider credentials");
    await resourceDialog.getByRole("button", { name: "Cancel", exact: true }).click();
    await expect(resourceDialog).toBeHidden();

    // Project switching: the auxiliary project serves no resources while the
    // main project's console keeps its five seeded rows.
    const auxiliaryCreated = await api(page, "POST", "/portal/api/projects", {
      key: auxiliary.key,
      name: auxiliary.name
    });
    expect(auxiliaryCreated.status, JSON.stringify(auxiliaryCreated.body)).toBe(201);
    await page.goto(`/portal/connections?projectId=${auxiliaryCreated.body.project.id}`);
    await expect(page.getByRole("heading", { name: "No resources yet" })).toBeVisible();
    await page.goto(`/portal/connections?projectId=${projectId}`);
    resources = page.getByRole("list", { name: "Project resources" });
    await expect(resources).toContainText("Compose repository");
    await expect(resources).toContainText("Compose runtime");

    const human = await createWorkItem(page, projectId, {
      title: humanTitle,
      description: "Human-owned work with complete delivery metadata",
      status: "ready",
      priority: "urgent",
      assignee_type: "human",
      assignee_name: "Lina",
      repository_resource_id: repository.id,
      branch: `feat/${project.key.toLowerCase()}`,
      blocked: true,
      blocker: "Needs architecture decision"
    });
    const wait = await createWorkItem(page, projectId, {
      title: waitTitle,
      description: fixtureDescription("wait_input", "Request and persist one human decision."),
      status: "ready",
      priority: "high",
      assignee_type: "agent",
      repository_resource_id: repository.id
    });

    await page.goto(projectUrl(projectId, { view: "board" }));
    await expect(page.getByRole("heading", { level: 1, name: project.name })).toBeVisible();
    await expect.poll(() => servedColumnTitles(page, projectId, "ready")).toEqual([humanTitle, waitTitle]);
    await expect.poll(() => renderedColumnTitles(page, "ready")).toEqual([humanTitle, waitTitle]);

    // The classic "Move down" button becomes the board's keyboard reorder; the
    // served and rendered order both move and survive a reload.
    await moveByKeyboard(page, humanTitle, "ArrowDown");
    await expect(statusNotice(page, "Moved down")).toBeVisible();
    await expect.poll(() => servedColumnTitles(page, projectId, "ready")).toEqual([waitTitle, humanTitle]);
    await expect.poll(() => renderedColumnTitles(page, "ready")).toEqual([waitTitle, humanTitle]);
    await page.reload();
    await expect(page.getByRole("heading", { level: 1, name: project.name })).toBeVisible();
    await expect.poll(() => renderedColumnTitles(page, "ready")).toEqual([waitTitle, humanTitle]);

    // The classic keyboard/dialog case's overflow assertion survives on the SPA
    // board (the harness runs desktop Chromium here; the narrow-width checks run
    // in the other SPA suites' projects).
    const dimensions = await page.evaluate(() => ({
      clientWidth: document.documentElement.clientWidth,
      scrollWidth: document.documentElement.scrollWidth
    }));
    expect(dimensions.scrollWidth).toBeLessThanOrEqual(dimensions.clientWidth);

    // The classic drawer's manual delivery edit becomes the work-item editor,
    // and the detail re-read serves the stored values.
    const humanDetail = await openWork(page, human.id, humanTitle);
    await humanDetail.getByRole("button", { name: "Edit work item" }).click();
    const humanEditor = page.getByRole("dialog", { name: "Edit work item" });
    await humanEditor.getByLabel("Pull request URL").fill("https://github.com/wxxb789/symmetry/pull/42");
    await humanEditor.getByLabel("CI", { exact: true }).selectOption("passed");
    await humanEditor.getByLabel("Review").selectOption("approved");
    await humanEditor.getByRole("button", { name: "Save" }).click();
    await expect(statusNotice(humanDetail, "Work item updated.")).toBeVisible();
    await expect(humanDetail.locator("dl.delivery-list").locator("div", { hasText: "Review" }).first()).toContainText(
      "approved"
    );

    // Ready -> review -> done was one select in the classic drawer; the board's
    // keyboard status move takes one step per rendered column.
    await page.goto(projectUrl(projectId, { view: "board" }));
    for (const state of ["in_progress", "review", "done"]) {
      await moveByKeyboard(page, humanTitle, "ArrowRight");
      await expect.poll(() => columnOf(page, humanTitle)).toBe(state);
    }
    const humanDone = await itemDetail(page, human.id);
    expect(humanDone.work_item).toEqual(
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

    const waitDetail = await openWork(page, wait.id, waitTitle);
    await waitDetail.getByRole("button", { name: "Start run" }).click();
    await expect(waitDetail.getByRole("heading", { name: "Agent needs a decision" })).toBeVisible({ timeout: 30_000 });
    const waiting = await itemDetail(page, wait.id);
    const waitingTransitionId = waiting.work_item.execution.waiting.transition_id;
    expect(waiting.work_item.execution).toEqual(
      expect.objectContaining({ state: "waiting_for_input", generation: 1, runtime_id: runtime.runtime_id })
    );
    const activeWorkspace = (await workspace(page, projectId)).body;
    const activeEntry = activeWorkspace.activity.find((entry) => entry.work_item.title === waitTitle);
    expect(activeEntry.execution).toEqual(
      expect.objectContaining({ state: "waiting_for_input", runtime_id: runtime.runtime_id })
    );
    expect(activeEntry.runtime.runtime_id).toBe(runtime.runtime_id);

    // A lost response: the route forwards the request (the durable command
    // exists) and only the response is dropped, so the browser has no
    // confirmation. The receipt states the unknown outcome — never applied —
    // and the reader's draft survives. If the served waiting signal still
    // offers the request, pressing the same control
    // replays the same browser-minted identity; the run may also have completed
    // already, in which case the faithful snapshot owns the state. Either way
    // the timeline carries exactly one durable input command.
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
    await waitDetail.getByLabel("Response").fill("continue");
    await waitDetail.getByRole("button", { name: "Send" }).click();
    await expect.poll(() => droppedStatus).toBe(202);
    const unknownReceipt = waitDetail.locator(".receipt").filter({ hasText: "Outcome unknown" }).first();
    await expect(unknownReceipt).toBeVisible({ timeout: 20_000 });
    await expect(unknownReceipt).not.toContainText("Applied");
    const durableInput = (await itemDetail(page, wait.id)).raw.timeline.filter(
      (entry) => entry.source === "command" && entry.data.kind === "provide_input"
    );
    expect(durableInput, "the lost response still left a durable input command").toHaveLength(1);
    if ((await waitDetail.getByRole("button", { name: "Send" }).count()) === 1) {
      await expect(waitDetail.getByLabel("Response")).toHaveValue("continue");
      await waitDetail.getByRole("button", { name: "Send" }).click();
      await expect.poll(() => replayStatus).toBe(200);
    }
    await page.unroute("**/portal/api/work-items/*/input");
    await expect(waitDetail.locator(".control-bar .chip")).toHaveText("Completed", { timeout: 30_000 });
    const waited = await itemDetail(page, wait.id);
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
      waited.raw.timeline.filter((entry) => entry.source === "command" && entry.data.kind === "provide_input")
    ).toHaveLength(1);
    allowBrowserError(page, /status of 409/);
    const staleInput = await api(page, "POST", `/portal/api/work-items/${wait.id}/input`, {
      input: { answer: "stale" },
      action_id: `stale-${project.key}`,
      waiting_transition_id: waitingTransitionId
    });
    expect(staleInput.status).toBe(409);
    expect(staleInput.body).toEqual(expect.objectContaining({ error: expect.objectContaining({ code: "state_conflict" }) }));

    // The launched run already moved the item to in_progress; the classic
    // select jumped straight to done, the board's keyboard move takes the
    // remaining steps.
    await page.goto(projectUrl(projectId, { view: "board" }));
    for (const state of ["review", "done"]) {
      await moveByKeyboard(page, waitTitle, "ArrowRight");
      await expect.poll(() => columnOf(page, waitTitle)).toBe(state);
    }

    const cancelItem = await createWorkItem(page, projectId, {
      title: cancelTitle,
      description: fixtureDescription("slow", "Enter a real process and cancel it through the portal."),
      status: "ready",
      assignee_type: "agent",
      repository_resource_id: repository.id
    });
    const cancelDetail = await openWork(page, cancelItem.id, cancelTitle);
    await cancelDetail.getByRole("button", { name: "Start run" }).click();
    await expect(cancelDetail.locator(".control-bar .chip")).toHaveText("Running", { timeout: 30_000 });
    await cancelDetail.getByRole("button", { name: "Cancel run" }).click();
    await expect(cancelDetail.locator(".control-bar .chip")).toHaveText("Cancelled", { timeout: 30_000 });
    await expect(cancelDetail.getByRole("button", { name: "Retry", exact: true })).toBeVisible();
    const cancelled = await itemDetail(page, cancelItem.id);
    expect(cancelled.work_item.execution).toEqual(expect.objectContaining({ state: "cancelled", generation: 1 }));
    expect(cancelled.raw.timeline).toEqual(
      expect.arrayContaining([
        expect.objectContaining({ source: "command", data: expect.objectContaining({ kind: "cancel" }) }),
        expect.objectContaining({ source: "transition", data: expect.objectContaining({ state: "cancelled" }) })
      ])
    );

    const retryItem = await createWorkItem(page, projectId, {
      title: retryTitle,
      description: fixtureDescription("fail_once_then_evidence_success", "Fail once and preserve both generations."),
      status: "ready",
      assignee_type: "agent",
      repository_resource_id: repository.id
    });
    const retryDetail = await openWork(page, retryItem.id, retryTitle);
    await retryDetail.getByRole("button", { name: "Start run" }).click();
    await expect(retryDetail.locator(".control-bar .chip")).toHaveText("Failed", { timeout: 30_000 });
    const failed = await itemDetail(page, retryItem.id);
    expect(failed.work_item.execution).toEqual(expect.objectContaining({ state: "failed", generation: 1 }));
    // The classic health rail's "Executions: Fault" has no SPA render (health is
    // not a surface in the fixed IA); the served value is asserted on the same
    // workspace read that backed the classic rail.
    expect((await workspace(page, projectId)).body.health).toEqual(
      expect.objectContaining({ executions: "fault" })
    );

    await retryDetail.getByRole("button", { name: "Edit work item" }).click();
    const retryEditor = page.getByRole("dialog", { name: "Edit work item" });
    await retryEditor
      .getByLabel("Description")
      .fill(fixtureDescription("fail_once_then_evidence_success", "Corrected intent is used by generation two."));
    await retryEditor.getByRole("button", { name: "Save" }).click();
    await expect(statusNotice(retryDetail, "Work item updated.")).toBeVisible();
    await expect(retryDetail.getByText("Corrected intent is used by generation two.", { exact: false })).toBeVisible();
    await retryDetail.getByRole("button", { name: "Retry", exact: true }).click();
    const acceptedEvidence = retryDetail.getByRole("region", { name: "Latest accepted evidence" });
    await expect(acceptedEvidence.getByText("Completed the requested engineering work", { exact: false })).toBeVisible({
      timeout: 30_000
    });
    await expect(acceptedEvidence.getByText("Verified the requested change", { exact: false })).toBeVisible();
    await expect(acceptedEvidence.getByText("README.md", { exact: false })).toBeVisible();
    await expect(acceptedEvidence.getByText("fake-agent verification", { exact: false })).toBeVisible();

    const retried = await itemDetail(page, retryItem.id);
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

    await retryDetail.getByRole("button", { name: "Edit work item" }).click();
    const deliveryEditor = page.getByRole("dialog", { name: "Edit work item" });
    await expect(deliveryEditor.locator("#work-item-pull-request")).toHaveText(
      "https://example.invalid/pull/42 · agent value, not editable here"
    );
    await expect(deliveryEditor.locator("#work-item-ci-status")).toHaveText("passed · agent value, not editable here");
    await expect(deliveryEditor.locator("#work-item-review-status")).toHaveText(
      "required · agent value, not editable here"
    );
    await deliveryEditor.getByRole("button", { name: "Cancel" }).click();
    await expect(deliveryEditor).toHaveCount(0);

    await page.goto(projectUrl(projectId, { view: "board" }));
    for (const state of ["review", "done"]) {
      await moveByKeyboard(page, retryTitle, "ArrowRight");
      await expect.poll(() => columnOf(page, retryTitle)).toBe(state);
    }
    const completedRetry = await itemDetail(page, retryItem.id);
    expect(completedRetry.work_item.delivery.pull_request).toEqual(
      expect.objectContaining({ source: "agent" })
    );
    expect(completedRetry.work_item.delivery.ci).toEqual(
      expect.objectContaining({ source: "agent", status: "passed" })
    );
    expect(completedRetry.work_item.delivery.review).toEqual(
      expect.objectContaining({ source: "agent", status: "required" })
    );

    // The classic topbar settings dialog and its status select move to the
    // project settings surface; the retired status select's disabled state
    // becomes the board's server-policy refusal of a move.
    await page.goto(projectUrl(projectId));
    await page.getByRole("button", { name: "Project settings" }).click();
    const projectSettings = page.getByRole("dialog", { name: "Project settings" });
    await projectSettings.getByLabel("Status").selectOption("archived");
    await projectSettings.getByRole("button", { name: "Save" }).click();
    await expect(statusNotice(page, "Project archived.")).toBeVisible();
    await expect(page.getByText(/This project is archived/)).toBeVisible();
    await page.goto(projectUrl(projectId, { view: "board" }));
    await expect(boardCard(page, retryTitle)).toBeVisible();
    await moveByKeyboard(page, retryTitle, "ArrowRight");
    await expect(statusNotice(page, "restore it to move work")).toBeVisible();
    await expect.poll(() => columnOf(page, retryTitle)).toBe("done");
    await page.goto(projectUrl(projectId));
    await page.getByRole("button", { name: "Restore project" }).click();
    await expect(statusNotice(page, "Project restored.")).toBeVisible();
    await expect(page.getByRole("button", { name: "New work item" })).toBeVisible();

    // The classic Activity rail is retired with the classic document; plan §6
    // maps it to the project board and the work detail, so each row's observable
    // claim is asserted on the surface that now serves it: the cancelled run's
    // "Retry available" is the served retry control on its detail.
    const cancelSurface = await openWork(page, cancelItem.id, cancelTitle);
    await expect(cancelSurface.locator(".control-bar .chip")).toHaveText("Cancelled");
    await expect(cancelSurface.getByRole("button", { name: "Retry", exact: true })).toBeVisible();

    const persisted = await workspace(page, projectId);
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

    // Persisted execution reads on the SPA board and work detail: the board
    // carries the final columns and the work detail carries the reported
    // output, the generation and the delivery values already proved through the
    // API, without labelling them accepted. This is also where the classic
    // Activity rows' claims are read back: the waiting run's runtime label
    // ("default") as the served owner, and the retried run's "Generation 2" as
    // the reported source.
    await page.goto(projectUrl(projectId, { view: "board" }));
    await expect.poll(() => columnOf(page, humanTitle)).toBe("done");
    await expect.poll(() => columnOf(page, waitTitle)).toBe("done");
    await expect.poll(() => columnOf(page, retryTitle)).toBe("done");
    await page.getByRole("link", { name: waitTitle }).click();
    const waitedSurface = page.getByRole("dialog", { name: waitTitle });
    await expect(waitedSurface.getByRole("heading", { level: 1, name: waitTitle })).toBeVisible();
    await expect(waitedSurface.locator(".control-bar .chip")).toHaveText("Completed");
    const outcomeFacts = waitedSurface.getByRole("region", { name: "Outcome" }).locator("dl.detail-facts");
    await expect(outcomeFacts.locator("div", { hasText: "Owner" })).toContainText("default");
    await expect(
      waitedSurface
        .locator("section.detail-section", { has: page.getByRole("heading", { name: "Execution history" }) })
        .locator("ol.timeline > li.timeline-entry")
        .first()
    ).toBeVisible();
    await page.keyboard.press("Escape");
    await expect(waitedSurface).toHaveCount(0);

    await page.getByRole("link", { name: retryTitle }).click();
    const retriedSurface = page.getByRole("dialog", { name: retryTitle });
    await expect(retriedSurface.getByRole("heading", { level: 1, name: retryTitle })).toBeVisible();
    // The rendered detail must carry the real reported output and delivery
    // values already proved through the API, without labelling them accepted.
    const retriedItemId = persistedProject.work_items.find((item) => item.title === retryTitle).id;
    const servedDetail = await itemDetail(page, retriedItemId);
    // The work detail's conversation entry point: the run this execution carries
    // links into the run scope the chat route serves, scoped to the item's project.
    const retriedRunId = servedDetail.work_item.execution.run_id;
    expect(retriedRunId).toBeTruthy();
    await expect(retriedSurface.getByRole("link", { name: "Open in Chat" }))
      .toHaveAttribute("href", `/portal/chat?scope=run&projectId=${projectId}&runId=${retriedRunId}`);
    const servedReported = servedDetail.outcome.summary ??
      (servedDetail.work_item.execution?.result?.summary ?? null);
    expect(servedReported).toBeTruthy();
    // Scoped to the actual Reported execution output section: the same summary
    // text legitimately repeats as a timeline entry headline.
    const reportedSection = retriedSurface.locator("section.detail-section", {
      has: page.getByRole("heading", { name: "Reported execution output", exact: true })
    });
    await expect(reportedSection.getByText(servedReported, { exact: false })).toBeVisible();
    await expect(reportedSection.getByText("Source: execution generation 2", { exact: false })).toBeVisible();
    await expect(retriedSurface.getByText("No accepted evidence available", { exact: false })).toBeVisible();
    const retriedDelivery = retriedSurface.locator("dl.delivery-list");
    for (const [label, entry] of [
      ["Pull request", servedDetail.work_item.delivery.pull_request],
      ["CI", servedDetail.work_item.delivery.ci],
      ["Review", servedDetail.work_item.delivery.review]
    ]) {
      const row = retriedDelivery.locator("div", { hasText: label }).first();
      await expect(row).toContainText(`source: ${entry.source}`);
      if (typeof entry.url === "string" && entry.url.startsWith("http")) {
        await expect(row.getByRole("link", { name: "Open" })).toHaveAttribute("href", entry.url);
      }
    }

  });
});
