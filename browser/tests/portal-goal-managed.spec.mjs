// Goal 0007f — Goal-managed work items on the readable work detail.
//
// The DB-level fixture that makes a work item Goal-managed is asserted in the
// Elixir acceptance path (control/test/symmetry_control_web/controllers/
// portal_workflow_test.exs) because this environment has no running Goal
// scheduler to drive plan admission through the operator API. Here a captured
// real detail response is replayed with the served `goal_id` folded in, so the
// surface decision is proved for exactly the shape the server serves; the
// legacy-path refusals that back it are asserted server-side by the same change.
import { expect, test } from "./fixtures.mjs";
import { api, createProject, createWorkItem, login, uniqueKey } from "./readable-fixtures.mjs";

const ui = {
  start: "Start run",
  cancel: "Cancel run",
  retry: "Retry",
  goalManaged: "Goal-managed",
  closeDetail: "Close work detail"
};

test.describe("Goal-managed work", () => {
  test("a Goal-managed work item presents no legacy control path and names its Goal", async ({ page }) => {
    await login(page);
    const key = uniqueKey("GM");
    const project = await createProject(page, { key, name: `Goal managed ${key}` });
    const title = `Goal item ${key}`;
    const item = await createWorkItem(page, project.id, {
      title,
      description: "A work item whose work is admitted by its Goal.",
      status: "ready",
      priority: "medium",
      assignee_type: "agent",
      agent_profile: `goal-${key.toLowerCase()}`,
      workspace: `goal-${key.toLowerCase()}`
    });

    // Confirm the fixture really is a legacy item before the replay, so the
    // suppression below can only come from the folded-in Goal ownership.
    const started = await api(page, "POST", `/portal/api/work-items/${item.id}/run`, { action_id: crypto.randomUUID() });
    expect(started.status, JSON.stringify(started.body)).toBe(202);
    const captured = await api(page, "GET", `/portal/api/work-items/${item.id}`);
    expect(captured.status).toBe(200);
    expect(captured.body.work_item.goal_id ?? null).toBeNull();
    const goalId = "00000000-0000-4000-8000-00000000a6f7";
    const replayBody = {
      ...captured.body,
      work_item: { ...captured.body.work_item, goal_id: goalId }
    };

    await page.route(`**/portal/api/work-items/${item.id}`, (route) => route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(replayBody)
    }));
    await page.goto(`/portal/work/${item.id}`);
    await expect(page.locator("h1")).toBeVisible();

    // A Goal-managed item must not use the legacy run/retry/move paths.
    await expect(page.getByRole("button", { name: ui.start })).toHaveCount(0);
    await expect(page.getByRole("button", { name: ui.cancel })).toHaveCount(0);
    await expect(page.getByRole("button", { name: ui.retry })).toHaveCount(0);
    const managed = page.locator(".goal-managed");
    await expect(managed).toBeVisible();
    await expect(managed).toContainText(ui.goalManaged);
    await expect(managed).toContainText(goalId.toLowerCase());
    await expect(managed).toContainText("Goal");
    await page.unroute(`**/portal/api/work-items/${item.id}`);

    // The same response without the Goal ownership keeps the control, so the
    // suppression comes from the served member and not from an unrelated
    // rendering path.
    await page.goto(`/portal/work/${item.id}`);
    await expect(page.getByRole("button", { name: ui.cancel })).toBeVisible();
    void ui.closeDetail;
  });
});
