// Goal 0007f — Goal-managed work items on the readable work detail.
//
// The Goal-managed row is real: scripts/ci/portal-planning-fixture.exs creates
// the Goal and applies WorkItem.goal_membership_changeset/2 to an existing work
// item through Control's own changesets and database constraints. That is a
// database-level freeze, not the product's admission shape — the seeded
// revision documents are outside the published contracts — and every browser
// interaction below then runs against the running control plane. The fixture
// needs the database coordinates of the server under test, so the case skips
// with its exact missing environment when they are absent instead of
// substituting a captured response.
//
// The server-side refusal assertions (the legacy run/cancel/retry/move paths
// answering 403 `goal_authority_required`) stay part of the evidence, and a
// sibling legacy item in the same project proves on the same page load that the
// suppression comes from the served Goal ownership and not from an unrelated
// rendering path.
import { allowBrowserError, expect, test } from "./fixtures.mjs";
import { applyGoalManaged } from "./planning-fixtures.mjs";
import { api, attachResource, createProject, createWorkItem, login, uniqueKey } from "./readable-fixtures.mjs";

const fixtureEnvironment = ["POSTGRES_HOST", "POSTGRES_PORT", "POSTGRES_DB"];
const presentEnvironment = fixtureEnvironment.filter((name) => Boolean(process.env[name]));
const missingEnvironment = fixtureEnvironment.filter((name) => !process.env[name]);
// The render gate carries the whole fixture environment and exports
// SYMMETRY_RENDER_GATE_REQUIRED=1: a missing variable there is a half-configured
// gate and fails collection instead of skipping green, while the local default
// stays the named skip below.
if (process.env.SYMMETRY_RENDER_GATE_REQUIRED === "1" && missingEnvironment.length > 0) {
  throw new Error(
    `${missingEnvironment.join(", ")} must be set: SYMMETRY_RENDER_GATE_REQUIRED=1 makes the render gate ` +
      "fail collection instead of skipping its Goal-managed fixture case",
  );
}
test.skip(presentEnvironment.length > 0 && presentEnvironment.length < fixtureEnvironment.length,
  `${fixtureEnvironment.join(", ")} must be set together for the Goal-management fixture`);
test.skip(presentEnvironment.length === 0,
  `Set ${fixtureEnvironment.join(", ")} so scripts/ci/portal-planning-fixture.exs can write the real Goal-managed row`);

const ui = {
  start: "Start run",
  cancel: "Cancel run",
  retry: "Retry",
  goalManaged: "Goal-managed"
};

test.describe("Goal-managed work", () => {
  test("a real Goal-managed work item presents no legacy path, and the server refuses one", async ({ page }) => {
    // The four legacy refusals below are the point of the case: 403 is expected noise.
    allowBrowserError(page, /status of 403/);
    await login(page);
    const key = uniqueKey("GM");
    const project = await createProject(page, { key, name: `Goal managed ${key}` });
    // The Goal membership freezes a repository baseline, so the project carries
    // the repository resource the fixture freezes into the item.
    const repository = await attachResource(page, project.id, {
      kind: "repository",
      name: "Goal baseline repository",
      external_ref: `symmetry/goal-baseline-${key}`
    });
    const item = await createWorkItem(page, project.id, {
      title: `Goal item ${key}`,
      description: "A work item whose work is admitted by its Goal.",
      status: "ready",
      priority: "medium",
      assignee_type: "agent",
      agent_profile: `goal-${key.toLowerCase()}`,
      workspace: `goal-${key.toLowerCase()}`
    });
    const sibling = await createWorkItem(page, project.id, {
      title: `Legacy sibling ${key}`,
      description: "A non-Goal item in the same project, keeping its own control path.",
      status: "ready",
      priority: "medium",
      assignee_type: "agent",
      agent_profile: `sibling-${key.toLowerCase()}`,
      workspace: `sibling-${key.toLowerCase()}`
    });

    const fixture = await applyGoalManaged(item.id, repository.id);
    expect(fixture).toContain("goal-managed");

    // The served row itself carries the Goal ownership; nothing here folds a
    // member into a captured response.
    const read = await api(page, "GET", `/portal/api/work-items/${item.id}`);
    expect(read.status, JSON.stringify(read.body)).toBe(200);
    const goalId = read.body.work_item.goal_id;
    expect(goalId, "the fixture wrote the Goal membership onto the real row").toBeTruthy();

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
    await expect(managed).toContainText("no legacy run controls");

    // The server's own refusals back the suppression: every legacy path answers
    // 403 goal_authority_required for this row.
    const refused = await Promise.all([
      api(page, "POST", `/portal/api/work-items/${item.id}/run`, { action_id: crypto.randomUUID() }),
      api(page, "POST", `/portal/api/work-items/${item.id}/cancel`, { action_id: crypto.randomUUID(), generation: 1 }),
      api(page, "POST", `/portal/api/work-items/${item.id}/retry`, { action_id: crypto.randomUUID(), generation: 1 }),
      api(page, "PATCH", `/portal/api/work-items/${item.id}/move`, { version: read.body.work_item.version, status: "in_progress", before_id: null })
    ]);
    for (const response of refused) {
      expect(response.status, JSON.stringify(response.body)).toBe(403);
      expect(response.body.error.code).toBe("goal_authority_required");
    }

    // The sibling legacy item keeps its start control on the same server state,
    // so the suppression is the served Goal ownership and not this surface.
    await page.goto(`/portal/work/${sibling.id}`);
    await expect(page.locator("h1")).toBeVisible();
    await expect(page.getByRole("button", { name: ui.start })).toBeVisible();
    const siblingRead = await api(page, "GET", `/portal/api/work-items/${sibling.id}`);
    expect(siblingRead.body.work_item.goal_id ?? null).toBeNull();
  });
});
