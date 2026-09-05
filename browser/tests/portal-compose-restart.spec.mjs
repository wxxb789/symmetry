import { expect, test } from "./fixtures.mjs";

const projectId = process.env.SYMMETRY_COMPOSE_PROJECT_ID;
const projectKey = process.env.SYMMETRY_COMPOSE_PROJECT_KEY;
const projectName = process.env.SYMMETRY_COMPOSE_PROJECT_NAME;
const operatorToken = process.env.SYMMETRY_OPERATOR_TOKEN || "development-operator-token";

async function workItemDetail(page, id) {
  return page.evaluate(async (workItemId) => {
    const response = await fetch(`/portal/api/work-items/${workItemId}`, { credentials: "same-origin" });
    return response.json();
  }, id);
}

test.describe("Goal 2 Compose restart acceptance", () => {
  test.skip(!projectId || !projectKey || !projectName, "Run through run-compose-daemon-tests.ps1.");
  test.skip(({ isMobile }) => isMobile, "Restart persistence is verified once against desktop Chromium.");

  test("the control restart preserves the complete F1-F7 workspace", async ({ page }) => {
    const humanTitle = `Human workflow ${projectKey}`;
    const waitTitle = `Waiting workflow ${projectKey}`;
    const cancelTitle = `Cancel workflow ${projectKey}`;
    const retryTitle = `Retry workflow ${projectKey}`;

    await page.goto("/portal/login");
    await page.getByLabel("Operator token").fill(operatorToken);
    await Promise.all([
      page.waitForURL(/\/portal(?:\?|#|$)/),
      page.getByRole("button", { name: "Open workspace" }).click()
    ]);

    await page.goto(`/portal?project_id=${projectId}#activity`);
    await expect(page.locator("#project-key")).toHaveText(projectKey);
    await expect(page.locator("#project-title")).toHaveText(projectName);
    await expect(page.locator(".activity-row", { hasText: waitTitle })).toContainText("default");
    await expect(page.locator(".activity-row", { hasText: waitTitle })).toContainText("Done");
    await expect(page.locator(".activity-row", { hasText: cancelTitle })).toContainText("Retry available");
    await expect(page.locator(".activity-row", { hasText: retryTitle })).toContainText("Generation 2");
    await expect(page.locator(".activity-row", { hasText: retryTitle })).toContainText("Done");

    const workspace = await page.evaluate(async (id) => {
      const response = await fetch(`/portal/api/workspace?project_id=${id}`, { credentials: "same-origin" });
      return { status: response.status, body: await response.json() };
    }, projectId);

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
  });
});
