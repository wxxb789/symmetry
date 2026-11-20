// Goal 0007f — execution control on the readable work detail
// (/portal/work/:workItemId), plus the runtime list's empty state on
// /portal/runtimes.
//
// Real-API acceptance: projects and work items are created through the
// authenticated Portal API, and every rendered control state is decoded from
// the running control plane. This surface is the migration target for the
// classic work-detail control assertions retired in the same change: a queued
// run's served state (the classic live timer and Activity-row assertion are
// retired, not migrated — this surface renders no client-side elapsed time),
// cancel -> cancelled, retry withheld while no runtime serves the binding,
// the retry endpoint refusing a human-owned item after reassignment, and an
// idempotent replay of an unconfirmed attempt.
//
// Network interception appears in four places only, each named in the case that
// uses it and each explicitly separated from real-data acceptance:
//   1. holding a control POST response past the rendered receipt, to prove an
//      in-flight request is never rendered as applied;
//   2. refusing a control POST entirely, to render the unknown outcome a lost
//      response produces, then proving the durable attempt is replayed once;
//   3. holding the surface's own detail reads while that replay is pressed, so
//      the press lands on the state the case observed instead of racing the
//      next background refresh — the replay verdict is the server's disposition
//      for the request the press sends, never a clock that could decide it;
//   4. the migrated waiting-input draft case, which injects a schema-valid
//      served waiting state into the real detail response so a background
//      refresh must leave the reader's draft, caret and focus untouched (the
//      real waiting-input lifecycle runs against a daemon in the live suite).
import { allowBrowserError, expect, test } from "./fixtures.mjs";
import { api, createProject, createWorkItem, login, updateWorkItem, uniqueKey } from "./readable-fixtures.mjs";

// One backstop for every wait in this file: a maximum, never a weaker
// observation. The control plane serves these reads in milliseconds; the guard
// only decides how long a genuinely missing state is waited out before the case
// fails, and it is the same magnitude every site already used.
const STATE_GUARD_MS = 20_000;
// The tightest wait here is the background refresh an explicit visibility
// notification triggers; it is named separately because its boundary is
// tighter, not because the observation is weaker.
const BACKGROUND_READ_GUARD_MS = 15_000;

const ui = {
  start: "Start run",
  cancel: "Cancel run",
  retry: "Retry",
  response: "Response",
  stateQueued: "Queued",
  stateCancelled: "Cancelled",
  receiptRequested: "Request sent",
  receiptAwaitingRuntime: "Awaiting a runtime",
  receiptDeferred: "Queued for the runtime",
  receiptApplied: "Applied",
  receiptUnknown: "Outcome unknown",
  closeDetail: "Close work detail"
};

async function openWorkDetail(page, workItemId) {
  await page.goto(`/portal/work/${workItemId}`);
  await expect(page.locator("h1")).toBeVisible();
}

async function executionOf(page, workItemId) {
  const response = await api(page, "GET", `/portal/api/work-items/${workItemId}`);
  expect(response.status, JSON.stringify(response.body)).toBe(200);
  return response.body.work_item.execution;
}

async function timelineOf(page, workItemId) {
  const response = await api(page, "GET", `/portal/api/work-items/${workItemId}`);
  expect(response.status).toBe(200);
  return response.body.raw.timeline;
}

async function workItemVersion(page, workItemId) {
  const response = await api(page, "GET", `/portal/api/work-items/${workItemId}`);
  expect(response.status).toBe(200);
  return response.body.work_item.version;
}

// No runtime serves this binding on this host, so the agent work stays queued:
// exactly the daemon-free case the classic suite exercised.
async function queuedAgentWork(page, prefix) {
  const key = uniqueKey(prefix);
  const project = await createProject(page, { key, name: `Execution control ${key}` });
  const title = `Queued control ${key}`;
  const item = await createWorkItem(page, project.id, {
    title,
    description: "Queued run with no matching runtime, used for control acceptance.",
    status: "ready",
    priority: "medium",
    assignee_type: "agent",
    agent_profile: `control-${key.toLowerCase()}`,
    workspace: `control-${key.toLowerCase()}`
  });
  return item;
}

test.describe("Execution control", () => {
  test("a queued run starts, shows its live state, and cancels into a stopped run whose retry no runtime serves", async ({ page }) => {
    await login(page);
    const item = await queuedAgentWork(page, "EC");
    await openWorkDetail(page, item.id);

    await page.getByRole("button", { name: ui.start }).click();
    await expect(page.locator(".receipt").first()).toContainText(ui.receiptRequested);
    await expect(page.locator(".receipt").first()).not.toContainText(ui.receiptApplied);
    await expect.poll(() => executionOf(page, item.id), { timeout: STATE_GUARD_MS }).toMatchObject({ state: "queued", generation: 1 });

    // The queued execution renders the served state in the control bar and in
    // the chips. The classic live-timer and Activity-row assertions are retired
    // (recorded in the goal receipt): this surface renders no client-side
    // elapsed time, so the migrated check asserts the served state instead of a
    // timestamp that a run-less detail also carries.
    await expect(page.locator(".control-bar .chip")).toHaveText(ui.stateQueued);
    await expect(page.getByText(ui.stateQueued, { exact: true }).first()).toBeVisible();

    await page.getByRole("button", { name: ui.cancel }).click();
    await expect(page.getByText(ui.stateCancelled, { exact: true }).first()).toBeVisible({ timeout: STATE_GUARD_MS });
    await expect.poll(() => executionOf(page, item.id), { timeout: STATE_GUARD_MS }).toMatchObject({ state: "cancelled" });

    const cancel = (await timelineOf(page, item.id)).filter((entry) => entry.source === "command" && entry.data.kind === "cancel");
    expect(cancel, "one durable cancel command").toHaveLength(1);
    // A task-scoped cancel command carries no run-scoped generation, and it is
    // recorded by the control plane without a runtime acknowledgement, so its
    // receipt renders as queued for the runtime — never as applied.
    expect(cancel[0].generation).toBeNull();
    expect(cancel[0].data.acknowledgement_outcome).toBeNull();
    await expect(page.locator(".receipt").filter({ hasText: ui.receiptDeferred }).first()).toBeVisible();
    await expect(page.locator(".receipt").filter({ hasText: ui.receiptApplied })).toHaveCount(0);

    // Retry availability is the server's own signal: retry re-queues into
    // assignment, no runtime serves this binding, and the capability-bound
    // policy withholds a control no runtime could honour.
    await expect(page.getByRole("button", { name: ui.retry })).toHaveCount(0);

    // A retry aimed at an already-cancelled generation is refused by the
    // server; the refusal is a definitive rejection, never an applied state.
    // The deliberate 409 is the only console error this case may produce.
    allowBrowserError(page, /409 \(Conflict\)/);
    const refused = await api(page, "POST", `/portal/api/work-items/${item.id}/retry`, { action_id: crypto.randomUUID(), generation: 99 });
    expect(refused.status, JSON.stringify(refused.body)).toBe(409);

    // Reassigning away from the agent removes the retry control (migrated
    // classic assertion) with no local policy of our own. This binding has no
    // serving runtime either, so the control was already withheld: the
    // assignee gate is observable at the endpoint that would apply the retry,
    // which refuses the human-owned item for the current generation.
    const current = await workItemVersion(page, item.id);
    await updateWorkItem(page, item.id, { assignee_type: "human", assignee_name: "Casey", version: current });
    await page.reload();
    await expect(page.locator("h1")).toBeVisible();
    await expect(page.getByRole("button", { name: ui.retry })).toHaveCount(0);
    await expect.poll(() => executionOf(page, item.id)).toMatchObject({ state: "cancelled" });

    allowBrowserError(page, /409 \(Conflict\)/);
    const humanOwned = await api(page, "POST", `/portal/api/work-items/${item.id}/retry`, { action_id: crypto.randomUUID(), generation: 1 });
    expect(humanOwned.status, JSON.stringify(humanOwned.body)).toBe(409);

    const dimensions = await page.evaluate(() => ({ clientWidth: document.documentElement.clientWidth, scrollWidth: document.documentElement.scrollWidth }));
    expect(dimensions.scrollWidth).toBeLessThanOrEqual(dimensions.clientWidth);
  });

  test("an in-flight request is never rendered as applied, and a lost response stays unknown until replayed", async ({ page }) => {
    await login(page);
    const item = await queuedAgentWork(page, "EP");

    let release = () => {};
    const held = new Promise((resolve) => { release = resolve; });
    await page.route(`**/portal/api/work-items/${item.id}/run`, async (route) => {
      await held;
      await route.continue();
    });
    await openWorkDetail(page, item.id);
    await page.getByRole("button", { name: ui.start }).click();
    await expect(page.locator(".receipt").first()).toContainText(ui.receiptRequested);
    await expect(page.locator(".receipt").first()).not.toContainText(ui.receiptApplied);
    // A background refresh arriving while the request is open changes nothing
    // about the pending receipt.
    await page.evaluate(() => document.dispatchEvent(new Event("visibilitychange")));
    await expect(page.locator(".receipt").first()).not.toContainText(ui.receiptApplied);
    release();
    await expect(page.locator(".receipt").first()).toContainText(ui.receiptAwaitingRuntime, { timeout: STATE_GUARD_MS });
    await page.unroute(`**/portal/api/work-items/${item.id}/run`);

    const lost = await queuedAgentWork(page, "EL");
    await openWorkDetail(page, lost.id);
    await page.getByRole("button", { name: ui.start }).click();
    await expect(page.getByRole("button", { name: ui.cancel })).toBeVisible({ timeout: STATE_GUARD_MS });
    // The dropped response is the only console error this case may produce.
    allowBrowserError(page, /net::ERR_FAILED/);
    // The settlement the dropped response records server-side reaches this
    // browser only through its own reads, so those reads are held from here on.
    // The press below then lands on the snapshot the case just observed
    // (attempt recorded, control present) instead of on whatever the next
    // background refresh happened to deliver, which is what made the replay
    // assertion skippable: measured locally before this change, the reload's own
    // read had already served the settled execution in three runs of three, so
    // the control was gone and `disposition: "replayed"` was never asserted —
    // the 15 s clock that decided it could only ever pass.
    const heldReads = [];
    await page.route(`**/portal/api/work-items/${lost.id}`, async (route) => {
      heldReads.push(route);
    });
    // The request reaches the server and only its response is dropped, so the
    // durable command exists while the browser has no confirmation at all.
    await page.route(`**/portal/api/work-items/${lost.id}/cancel`, async (route) => {
      await route.fetch();
      await route.abort("failed");
    });
    await page.getByRole("button", { name: ui.cancel }).click();
    const unknownReceipt = page.locator(".receipt").filter({ hasText: ui.receiptUnknown }).first();
    await expect(unknownReceipt).toBeVisible({ timeout: STATE_GUARD_MS });
    await expect(unknownReceipt).not.toContainText(ui.receiptApplied);

    // The durable command exists, so the execution settles from the server even
    // though this browser never saw a response; pressing the control again is
    // that recorded attempt's replay and never mints a second command. The
    // verdict is the server's own disposition for this browser's request: no
    // clock decides whether the claim is asserted.
    await page.unroute(`**/portal/api/work-items/${lost.id}/cancel`);
    const replayResponse = page.waitForResponse((response) => {
      const url = new URL(response.url());
      return url.pathname === `/portal/api/work-items/${lost.id}/cancel`;
    });
    await page.getByRole("button", { name: ui.cancel }).click();
    const replayed = await replayResponse;
    expect(replayed.status()).toBe(200);
    expect((await replayed.json()).disposition).toBe("replayed");
    // A read the page superseded and cancelled cannot be continued; every
    // assertion here is about the state the surface keeps, never about the
    // delivery of a held response.
    for (const route of heldReads) await route.continue().catch(() => undefined);
    await page.unroute(`**/portal/api/work-items/${lost.id}`);

    // The reload reconstructs the same attempt from its stored identity, so the
    // receipt is pending again instead of vanishing — still never applied.
    await page.reload();
    await expect(page.getByRole("button", { name: ui.closeDetail }).or(page.locator("h1")).first()).toBeVisible();
    const pending = page.locator(".receipt").filter({ hasText: ui.receiptRequested }).first();
    const settled = page.getByText(ui.stateCancelled, { exact: true }).first();
    /* The reload reconstructs the attempt before the detail snapshot arrives; if
       the snapshot lands first, the durable command owns the receipt and the
       attempt renders nothing. Either way it is never applied. */
    await expect(pending.or(settled).first()).toBeVisible({ timeout: STATE_GUARD_MS });
    if (await pending.count() === 1) await expect(pending).not.toContainText(ui.receiptApplied);

    await expect.poll(() => executionOf(page, lost.id), { timeout: STATE_GUARD_MS }).toMatchObject({ state: "cancelled" });
    await expect(page.getByText(ui.stateCancelled, { exact: true }).first()).toBeVisible({ timeout: STATE_GUARD_MS });
    const replay = (await timelineOf(page, lost.id)).filter((entry) => entry.source === "command" && entry.data.kind === "cancel");
    expect(replay, "exactly one durable cancel command after a lost response").toHaveLength(1);
  });

  test("unsupported operations are explained and never rendered as live controls", async ({ page }) => {
    await login(page);
    const item = await queuedAgentWork(page, "EU");
    await openWorkDetail(page, item.id);

    await expect(page.getByRole("button", { name: "Pause" })).toHaveCount(0);
    await expect(page.getByRole("button", { name: "Resume" })).toHaveCount(0);
    await expect(page.getByRole("button", { name: "Send guidance" })).toHaveCount(0);
    await expect(page.locator(".control-note")).toContainText("Chat");

    await page.getByRole("button", { name: ui.start }).click();
    await expect.poll(() => executionOf(page, item.id), { timeout: STATE_GUARD_MS }).toMatchObject({ state: "queued" });
    await expect(page.getByRole("button", { name: "Pause" })).toHaveCount(0);
    await expect(page.getByRole("button", { name: "Resume" })).toHaveCount(0);
  });

  test("the work detail keeps a waiting-input draft and focus across a background refresh", async ({ page }) => {
    await login(page);
    const item = await queuedAgentWork(page, "ED");

    // Migrated from the classic drawer case: the served waiting state is
    // injected into the real response (schema-valid, every consumed member
    // present) so a background refresh must leave the reader's draft, caret and
    // focus untouched. The real waiting-input lifecycle itself runs against a
    // daemon in portal-live-daemon.spec.mjs.
    let detailRequests = 0;
    await page.route(`**/portal/api/work-items/${item.id}`, async (route) => {
      const response = await route.fetch();
      const body = await response.json();
      detailRequests += 1;
      body.work_item.execution = {
        task_id: "00000000-0000-4000-8000-000000000203",
        run_id: "00000000-0000-4000-8000-000000000201",
        runtime_id: null,
        generation: 1,
        state: "waiting_for_input",
        waiting: {
          run_id: "00000000-0000-4000-8000-000000000201",
          generation: 1,
          transition_id: "00000000-0000-4000-8000-000000000204",
          question: "Choose a deployment target",
          decision: null,
          payload: {},
          recorded_at: "2026-11-16T08:00:00Z"
        },
        latest_command: null,
        result: null,
        failure: null,
        timing: { state_since: "2026-11-16T08:00:00Z", started_at: "2026-11-16T07:59:00Z", finished_at: null },
        supervisory_control: false,
        can_guide: false,
        can_pause: false,
        can_resume: false,
        can_cancel: true,
        can_retry: false,
        intent_locked: true
      };
      await route.fulfill({ response, json: body });
    });

    await openWorkDetail(page, item.id);
    const answer = page.getByLabel(ui.response);
    await expect(answer).toBeVisible();
    await answer.fill("staging-blue");
    await answer.focus();
    await answer.evaluate((input) => input.setSelectionRange(7, 7));

    await page.evaluate(() => document.dispatchEvent(new Event("visibilitychange")));
    await expect.poll(() => detailRequests, { timeout: BACKGROUND_READ_GUARD_MS }).toBeGreaterThan(1);
    await expect(answer).toHaveValue("staging-blue");
    await expect(answer).toBeFocused();
    expect(await answer.evaluate((input) => input.selectionStart)).toBe(7);
    await page.unroute(`**/portal/api/work-items/${item.id}`);
  });
});

test.describe("Runtime visibility", () => {
  test("the runtimes list names its compatibility filter instead of claiming an empty registry", async ({ page }) => {
    await login(page);
    const key = uniqueKey("RT");
    // The unique default profile/workspace cannot match any registered runtime,
    // so this project's compatibility set is empty by construction and the
    // scoped read below is the oracle for the page.
    const created = await api(page, "POST", "/portal/api/projects", {
      key,
      name: `Runtime scope ${key}`,
      default_agent_profile: `unbound-${key.toLowerCase()}`,
      default_workspace: `unbound-${key.toLowerCase()}`
    });
    expect(created.status, JSON.stringify(created.body)).toBe(201);
    const projectId = created.body.project.id;

    const scopedRead = await api(page, "GET", `/portal/api/workspace?project_id=${projectId}`);
    expect(scopedRead.status).toBe(200);
    expect(scopedRead.body.runtimes, "a fresh project binds no runtime").toHaveLength(0);
    const registered = scopedRead.body.registered_runtimes ?? [];
    await page.goto(`/portal/runtimes?projectId=${projectId}`);
    await expect(page.locator("h1")).toHaveText("Runtimes");
    await expect(page.locator("article.runtime-card")).toHaveCount(0);
    // The empty state names the filter it read and cites the served registered
    // count; it never claims the deployment's registry itself is empty.
    await expect(page.locator(".empty-state h2")).toHaveText("No compatible runtimes");
    const registryPhrase = registered.length === 0
      ? "No runtime is registered with this deployment"
      : `${registered.length} runtimes are registered with this deployment`;
    await expect(page.getByText(registryPhrase)).toBeVisible();
    await expect(page.getByText("No runtimes registered")).toHaveCount(0);

    // The unscoped URL reads the same compatibility filter for the
    // server-selected project; the page must agree with that read.
    const unscopedRead = await api(page, "GET", "/portal/api/workspace");
    expect(unscopedRead.status).toBe(200);
    const compatible = unscopedRead.body.runtimes ?? [];
    await page.goto("/portal/runtimes");
    await expect(page.locator("h1")).toHaveText("Runtimes");
    await expect(page.locator("article.runtime-card")).toHaveCount(compatible.length);
    if (compatible.length === 0) {
      // The served read selects a project unless the deployment has none, and
      // the empty title must follow that read rather than report an empty
      // registry the read may not have served.
      const emptyTitle = unscopedRead.body.selected_project_id === null ? "No project selected" : "No compatible runtimes";
      await expect(page.locator(".empty-state h2")).toHaveText(emptyTitle);
      await expect(page.getByText("No runtimes registered")).toHaveCount(0);
    }
  });
});
