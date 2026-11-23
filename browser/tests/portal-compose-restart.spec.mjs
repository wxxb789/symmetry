import { expect, test } from "./fixtures.mjs";
import { api, login, workUrl } from "./readable-fixtures.mjs";

const projectId = process.env.SYMMETRY_COMPOSE_PROJECT_ID;
const projectKey = process.env.SYMMETRY_COMPOSE_PROJECT_KEY;
const projectName = process.env.SYMMETRY_COMPOSE_PROJECT_NAME;

const columnOf = (page, title) =>
  page
    .locator("[data-board-card]", { has: page.getByRole("link", { name: title }) })
    .getAttribute("data-board-column");

async function workItemDetail(page, id) {
  const response = await api(page, "GET", `/portal/api/work-items/${id}`);
  expect(response.status, JSON.stringify(response.body)).toBe(200);
  return response.body;
}

test.describe("Goal 2 Compose restart acceptance", () => {
  test.skip(!projectId || !projectKey || !projectName, "Run through run-compose-daemon-tests.ps1.");
  test.skip(({ isMobile }) => isMobile, "Restart persistence is verified once against desktop Chromium.");

  test("the control restart preserves the complete F1-F7 workspace", async ({ page }) => {
    const humanTitle = `Human workflow ${projectKey}`;
    const waitTitle = `Waiting workflow ${projectKey}`;
    const cancelTitle = `Cancel workflow ${projectKey}`;
    const retryTitle = `Retry workflow ${projectKey}`;

    await login(page);

    // The classic workspace document is retired; the read-after-restart check
    // runs on the SPA routes that now serve the same state. The classic
    // #project-key/#project-title read becomes the project heading and the item
    // keys the project board serves.
    await page.goto(`/portal/projects/${projectId}?view=board`);
    await expect(page.getByRole("heading", { level: 1, name: projectName })).toBeVisible();
    await expect(page.getByRole("link", { name: waitTitle })).toContainText(projectKey);

    const workspace = await api(page, "GET", `/portal/api/workspace?project_id=${projectId}`);

    expect(workspace.status).toBe(200);
    expect(workspace.body.selected_project_id).toBe(projectId);
    expect(workspace.body.health).toEqual(
      expect.objectContaining({ connections: "degraded", runtimes: "healthy", synchronization: "attention" })
    );

    const project = workspace.body.projects.find((candidate) => candidate.id === projectId);
    expect(project).toEqual(
      expect.objectContaining({
        key: projectKey,
        name: projectName,
        status: "active",
        default_agent_profile: "default",
        default_workspace: "primary",
        description: "F1-F7 production Compose acceptance workspace"
      })
    );
    expect(project.resources).toEqual(
      expect.arrayContaining([
        expect.objectContaining({ name: "Compose repository", kind: "repository", status: "healthy", sync_status: "synced" }),
        expect.objectContaining({ name: "Compose CI", kind: "ci", status: "degraded", sync_status: "failed" }),
        expect.objectContaining({ name: "Compose connection", kind: "connection", status: "offline", sync_status: "stale" }),
        expect.objectContaining({ name: "Compose agent", kind: "agent", external_ref: "default" }),
        expect.objectContaining({ name: "Compose runtime", kind: "runtime" })
      ])
    );

    const human = project.work_items.find((item) => item.title === humanTitle);
    const waited = project.work_items.find((item) => item.title === waitTitle);
    const cancelled = project.work_items.find((item) => item.title === cancelTitle);
    const retried = project.work_items.find((item) => item.title === retryTitle);

    expect(human).toEqual(
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
    expect(waited).toEqual(
      expect.objectContaining({ status: "done", execution: expect.objectContaining({ state: "completed", generation: 1 }) })
    );
    expect(cancelled.execution).toEqual(
      expect.objectContaining({ state: "cancelled", generation: 1, can_retry: true })
    );
    expect(retried).toEqual(
      expect.objectContaining({
        status: "done",
        execution: expect.objectContaining({ state: "completed", generation: 2 }),
        delivery: expect.objectContaining({
          pull_request: expect.objectContaining({ source: "manual", url: "https://github.com/wxxb789/symmetry/pull/84" }),
          ci: expect.objectContaining({ source: "manual", status: "passed" }),
          review: expect.objectContaining({ source: "manual", status: "approved" })
        })
      })
    );
    const orderedDoneItems = project.work_items
      .filter((item) => item.status === "done")
      .sort((left, right) => left.position - right.position);
    expect(orderedDoneItems.map((item) => item.title)).toEqual([humanTitle, waitTitle, retryTitle]);
    expect(orderedDoneItems.map((item) => item.position)).toEqual([0, 1, 2]);

    const waitActivity = workspace.body.activity.find((entry) => entry.work_item.title === waitTitle);
    expect(waitActivity.execution.runtime_id).toBe(waited.execution.runtime_id);
    expect(waitActivity.runtime.runtime_id).toBe(waited.execution.runtime_id);

    const waitedDetail = await workItemDetail(page, waited.id);
    expect(waitedDetail.raw.timeline).toEqual(
      expect.arrayContaining([
        expect.objectContaining({ source: "event", data: expect.objectContaining({ kind: "waiting_for_input" }) }),
        expect.objectContaining({ source: "command", data: expect.objectContaining({ kind: "provide_input" }) }),
        expect.objectContaining({ source: "transition", data: expect.objectContaining({ state: "completed" }) })
      ])
    );
    expect(
      waitedDetail.raw.timeline.filter(
        (entry) => entry.source === "command" && entry.data.kind === "provide_input"
      )
    ).toHaveLength(1);

    const cancelledDetail = await workItemDetail(page, cancelled.id);
    expect(cancelledDetail.raw.timeline).toEqual(
      expect.arrayContaining([
        expect.objectContaining({ source: "command", data: expect.objectContaining({ kind: "cancel" }) }),
        expect.objectContaining({ source: "transition", data: expect.objectContaining({ state: "cancelled" }) })
      ])
    );

    const retriedDetail = await workItemDetail(page, retried.id);
    expect(new Set(retriedDetail.raw.timeline.map((entry) => entry.generation))).toEqual(new Set([1, 2]));
    expect(
      retriedDetail.raw.timeline
        .filter((entry) => entry.source === "event" && entry.generation === 2)
        .map((entry) => entry.data.kind)
    ).toEqual(expect.arrayContaining(["progress", "finding", "artifact", "test", "pull_request", "ci", "review", "summary"]));

    // The classic Activity rows' observable claims are asserted on the board
    // and the work details that now serve them: the waiting row's "Done" and
    // its runtime label as the served owner, the cancelled row's "Retry
    // available" as the served retry control, and the retried row's
    // "Generation 2" and "Done".
    await expect.poll(() => columnOf(page, waitTitle)).toBe("done");
    await expect.poll(() => columnOf(page, retryTitle)).toBe("done");
    await page.goto(workUrl(cancelled.id));
    const cancelledSurface = page.getByRole("dialog", { name: cancelTitle });
    await expect(cancelledSurface.getByRole("heading", { level: 1, name: cancelTitle })).toBeVisible();
    await expect(cancelledSurface.locator(".control-bar .chip")).toHaveText("Cancelled");
    await expect(cancelledSurface.getByRole("button", { name: "Retry" })).toBeVisible();

    // The post-restart read also renders on the work detail surface; the
    // persisted lifecycle assertions above stay unchanged. The waiting workflow
    // serves no summary, so the rendered reported read is asserted against the
    // retried workflow, whose generation-2 summary event is proved above.
    await page.goto(`/portal/projects/${projectId}?view=board`);
    await page.getByRole("link", { name: waitTitle }).click();
    const waitedSurface = page.getByRole("dialog", { name: waitTitle });
    await expect(waitedSurface.getByRole("heading", { level: 1, name: waitTitle })).toBeVisible();
    await expect(waitedSurface.locator(".control-bar .chip")).toHaveText("Completed");
    await expect(
      waitedSurface.locator("dl.detail-facts").locator("div", { hasText: "Owner" })
    ).toContainText("default");
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
    const servedReported = retriedDetail.outcome.summary ??
      (retriedDetail.work_item.execution?.result?.summary ?? null);
    expect(servedReported).toBeTruthy();
    // Scoped to the actual Reported execution output section: the same summary
    // text legitimately repeats as a timeline entry headline in the sibling
    // Execution history section.
    const reportedSection = retriedSurface.locator("section.detail-section", {
      has: page.getByRole("heading", { name: "Reported execution output", exact: true })
    });
    await expect(reportedSection.getByText(servedReported, { exact: false })).toBeVisible();
    await expect(reportedSection.getByText("Source: execution generation 2", { exact: false })).toBeVisible();
    await expect(retriedSurface.getByText("No accepted evidence available", { exact: false })).toBeVisible();
    const retriedDelivery = retriedSurface.locator("dl.delivery-list");
    for (const [label, entry] of [
      ["Pull request", retriedDetail.work_item.delivery.pull_request],
      ["CI", retriedDetail.work_item.delivery.ci],
      ["Review", retriedDetail.work_item.delivery.review]
    ]) {
      const row = retriedDelivery.locator("div", { hasText: label }).first();
      await expect(row).toContainText(`source: ${entry.source}`);
      if (typeof entry.url === "string" && entry.url.startsWith("http")) {
        await expect(row.getByRole("link", { name: "Open" })).toHaveAttribute("href", entry.url);
      }
    }
  });
});
