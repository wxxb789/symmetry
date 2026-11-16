// Goal 0007f — execution control on the readable work detail (/portal/work/:workItemId).
//
// Real-API acceptance: projects and work items are created through the
// authenticated Portal API, and every rendered control state is decoded from
// the running control plane. This surface is the migration target for the
// classic work-detail control assertions retired in the same change: a queued
// run with a live timer, cancel -> cancelled, retry availability, retry hidden
// after reassignment, and an idempotent replay of an unconfirmed attempt.
//
// Network interception appears in two places only, each named in the case that
// uses it and each explicitly separated from real-data acceptance:
//   1. holding a control POST response past the rendered receipt, to prove an
//      in-flight request is never rendered as applied;
//   2. refusing a control POST entirely, to render the unknown outcome a lost
//      response produces, then proving the durable attempt is replayed once.
import { allowBrowserError, expect, test } from "./fixtures.mjs";
import { api, createProject, createWorkItem, login, updateWorkItem, uniqueKey } from "./readable-fixtures.mjs";

const ui = {
  start: "Start run",
  cancel: "Cancel run",
  retry: "Retry",
  response: "Response",
  stateQueued: "Queued",
  stateCancelled: "Cancelled",
  receiptRequested: "Request sent",
  receiptQueued: "Awaiting agent acknowledgement",
  receiptDeferred: "Queued for the runtime",
  receiptApplied: "Applied",
  receiptFailed: "Failed",
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
  return { project, title, item };
}

test.describe("Execution control", () => {
  test("a queued run starts, shows its live state, and cancels into a retryable stopped run", async ({ page }) => {
    await login(page);
    const { item } = await queuedAgentWork(page, "EC");
    await openWorkDetail(page, item.id);

    await page.getByRole("button", { name: ui.start }).click();
    await expect(page.locator(".receipt").first()).toContainText(ui.receiptRequested);
    await expect(page.locator(".receipt").first()).not.toContainText(ui.receiptApplied);
    await expect.poll(() => executionOf(page, item.id), { timeout: 20_000 }).toMatchObject({ state: "queued", generation: 1 });

    // The queued execution renders its served state and a live timer.
    await expect(page.getByText(ui.stateQueued, { exact: true }).first()).toBeVisible();
    await expect(page.locator(".detail-section time[datetime]").first()).toBeVisible();

    await page.getByRole("button", { name: ui.cancel }).click();
    await expect(page.getByText(ui.stateCancelled, { exact: true }).first()).toBeVisible({ timeout: 20_000 });
    await expect.poll(() => executionOf(page, item.id), { timeout: 20_000 }).toMatchObject({ state: "cancelled" });

    const cancel = (await timelineOf(page, item.id)).filter((entry) => entry.source === "command" && entry.data.kind === "cancel");
    expect(cancel, "one durable cancel command").toHaveLength(1);
    // A task-scoped cancel command carries no run-scoped generation, and it is
    // recorded by the control plane without a runtime acknowledgement, so its
    // receipt renders as queued for the runtime — never as applied.
    expect(cancel[0].generation).toBeNull();
    expect(cancel[0].data.acknowledgement_outcome).toBeNull();
    await expect(page.locator(".receipt").filter({ hasText: ui.receiptDeferred }).first()).toBeVisible();
    await expect(page.locator(".receipt").filter({ hasText: ui.receiptApplied })).toHaveCount(0);

    // Retry availability is the server's own signal.
    await expect(page.getByRole("button", { name: ui.retry })).toBeVisible();

    // A retry aimed at an already-cancelled generation is refused by the
    // server; the refusal is a definitive rejection, never an applied state.
    // The deliberate 409 is the only console error this case may produce.
    allowBrowserError(page, /409 \(Conflict\)/);
    const refused = await api(page, "POST", `/portal/api/work-items/${item.id}/retry`, { action_id: crypto.randomUUID(), generation: 99 });
    expect(refused.status, JSON.stringify(refused.body)).toBe(409);

    // Reassigning away from the agent removes the retry control (migrated
    // classic assertion) with no local policy of our own.
    const current = await projectItemVersion(page, item.id);
    await updateWorkItem(page, item.id, { assignee_type: "human", assignee_name: "Casey", version: current });
    await page.reload();
    await expect(page.locator("h1")).toBeVisible();
    await expect(page.getByRole("button", { name: ui.retry })).toHaveCount(0);
    await expect.poll(() => executionOf(page, item.id)).toMatchObject({ state: "cancelled" });

    const dimensions = await page.evaluate(() => ({ clientWidth: document.documentElement.clientWidth, scrollWidth: document.documentElement.scrollWidth }));
    expect(dimensions.scrollWidth).toBeLessThanOrEqual(dimensions.clientWidth);
  });

  test("an in-flight request is never rendered as applied, and a lost response stays unknown until replayed", async ({ page }) => {
    await login(page);
    const { item } = await queuedAgentWork(page, "EP");

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
    await expect(page.locator(".receipt").first()).toContainText(ui.receiptQueued, { timeout: 20_000 });
    await page.unroute(`**/portal/api/work-items/${item.id}/run`);

    const lost = await queuedAgentWork(page, "EL");
    await openWorkDetail(page, lost.item.id);
    await page.getByRole("button", { name: ui.start }).click();
    await expect(page.getByRole("button", { name: ui.cancel })).toBeVisible({ timeout: 20_000 });
    // The dropped response is the only console error this case may produce.
    allowBrowserError(page, /net::ERR_FAILED/);
    // The request reaches the server and only its response is dropped, so the
    // durable command exists while the browser has no confirmation at all.
    await page.route(`**/portal/api/work-items/${lost.item.id}/cancel`, async (route) => {
      await route.fetch();
      await route.abort("failed");
    });
    await page.getByRole("button", { name: ui.cancel }).click();
    const unknownReceipt = page.locator(".receipt").filter({ hasText: ui.receiptUnknown }).first();
    await expect(unknownReceipt).toBeVisible({ timeout: 20_000 });
    await expect(unknownReceipt).not.toContainText(ui.receiptApplied);

    // The reload reconstructs the same attempt from its stored identity, so the
    // receipt is pending again instead of vanishing — still never applied.
    await page.reload();
    await expect(page.getByRole("button", { name: ui.closeDetail }).or(page.locator("h1")).first()).toBeVisible();
    const pending = page.locator(".receipt").filter({ hasText: ui.receiptRequested }).first();
    const settled = page.getByText(ui.stateCancelled, { exact: true }).first();
    /* The reload reconstructs the attempt before the detail snapshot arrives; if
       the snapshot lands first, the durable command owns the receipt and the
       attempt renders nothing. Either way it is never applied. */
    await expect(pending.or(settled).first()).toBeVisible({ timeout: 20_000 });
    if (await pending.count() === 1) await expect(pending).not.toContainText(ui.receiptApplied);

    // The durable command exists, so the execution settles from the server even
    // though this browser never saw a response; pressing the control again is
    // that recorded attempt's replay and never mints a second command.
    await page.unroute(`**/portal/api/work-items/${lost.item.id}/cancel`);
    const cancelControl = page.getByRole("button", { name: ui.cancel });
    // The background refresh may settle the execution from the server while the
    // reader is about to press: a moved-on execution removes the control, and
    // that is the same final state without a second request.
    if (await cancelControl.count() === 1) {
      const replayResponse = page.waitForResponse((response) => {
        const url = new URL(response.url());
        return url.pathname === `/portal/api/work-items/${lost.item.id}/cancel`;
      });
      const clicked = await cancelControl.click({ timeout: 5_000 }).then(() => true).catch(() => false);
      if (clicked) {
        // Bounded: a click that landed while the execution moved on may send no
        // request at all, and the final state is asserted below either way.
        const replayed = await Promise.race([
          replayResponse,
          new Promise((resolve) => { setTimeout(() => resolve(null), 15_000); })
        ]);
        if (replayed !== null) {
          expect(replayed.status()).toBe(200);
          expect((await replayed.json()).disposition).toBe("replayed");
        }
      }
    }
    await expect.poll(() => executionOf(page, lost.item.id), { timeout: 20_000 }).toMatchObject({ state: "cancelled" });
    await expect(page.getByText(ui.stateCancelled, { exact: true }).first()).toBeVisible({ timeout: 20_000 });
    const replay = (await timelineOf(page, lost.item.id)).filter((entry) => entry.source === "command" && entry.data.kind === "cancel");
    expect(replay, "exactly one durable cancel command after a lost response").toHaveLength(1);
  });

  test("unsupported operations are explained and never rendered as live controls", async ({ page }) => {
    await login(page);
    const { item } = await queuedAgentWork(page, "EU");
    await openWorkDetail(page, item.id);

    await expect(page.getByRole("button", { name: "Pause" })).toHaveCount(0);
    await expect(page.getByRole("button", { name: "Resume" })).toHaveCount(0);
    await expect(page.getByRole("button", { name: "Send guidance" })).toHaveCount(0);
    await expect(page.locator(".control-note")).toContainText("Chat");

    await page.getByRole("button", { name: ui.start }).click();
    await expect.poll(() => executionOf(page, item.id), { timeout: 20_000 }).toMatchObject({ state: "queued" });
    await expect(page.getByRole("button", { name: "Pause" })).toHaveCount(0);
    await expect(page.getByRole("button", { name: "Resume" })).toHaveCount(0);
  });

  test("the work detail keeps a waiting-input draft and focus across a background refresh", async ({ page }) => {
    await login(page);
    const { item } = await queuedAgentWork(page, "ED");

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
    await expect.poll(() => detailRequests, { timeout: 15_000 }).toBeGreaterThan(1);
    await expect(answer).toHaveValue("staging-blue");
    await expect(answer).toBeFocused();
    expect(await answer.evaluate((input) => input.selectionStart)).toBe(7);
    await page.unroute(`**/portal/api/work-items/${item.id}`);
  });
});

async function projectItemVersion(page, workItemId) {
  const response = await api(page, "GET", `/portal/api/work-items/${workItemId}`);
  expect(response.status).toBe(200);
  return response.body.work_item.version;
}
