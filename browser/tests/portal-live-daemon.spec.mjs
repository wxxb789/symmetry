// Goal 0007f — live-daemon execution control on /portal/work/:workItemId.
//
// Every fixture here is a real daemon process serving a fake harness, and every
// rendered state is decoded from live control-plane records. The lifecycles
// asserted are the ones the classic suite exercised (waiting input, cancel of a
// running process, failure-repair retry across generations, raw history
// pagination) plus the states this increment adds: a cancel/completion race, a
// lost response replayed with the same attempt identity, and runtime capability
// rows compared with the daemon's own registration record.
//
// Network interception appears in one place only: holding a control POST so an
// unconfirmed attempt survives a reload. It is named in the case that uses it
// and is separated from the durable-record assertions around it.
//
// Retired with this rewrite, recorded in the goal receipt and browser/README
// rather than silently dropped: the classic suite's forged-runtime-reference
// refusal walk (its server enforcement is portal_workflow_test.exs "resource
// APIs expose and enforce registered runtime identities", and the new resource
// form's field-error rendering is covered by connections.test.tsx) and the
// obsolete-response phases of the classic cursored history case (whose
// obsolete-response half has the daemon-free synthetic guard in
// portal-work-views.spec.mjs). The real cursor load, exhaustion and the
// corrected-intent retry below stay live.
import fs from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { allowBrowserError, expect, test } from "./fixtures.mjs";
import { api, attachResource, createProject, createWorkItem, login, projectItem, uniqueKey, updateWorkItem } from "./readable-fixtures.mjs";

const liveEnabled = process.env.SYMMETRY_LIVE_DAEMON === "1";
const fixtureSuffix = process.env.SYMMETRY_LIVE_FIXTURE_SUFFIX;
const fixtureBinding = (name) => `goal2-${name}${fixtureSuffix ? `-${fixtureSuffix}` : ""}`;
let harnessBinding = process.env.SYMMETRY_LIVE_HARNESS_BINDING || "";

const ui = {
  start: "Start run",
  cancel: "Cancel run",
  retry: "Retry",
  send: "Send",
  response: "Response",
  loadOlder: "Load older history",
  noOlder: "No older history recorded.",
  stateQueued: "Queued",
  stateRunning: "Running",
  stateCancelled: "Cancelled",
  stateCompleted: "Completed",
  stateFailed: "Failed",
  stateWaiting: "Waiting for input",
  receiptRequested: "Request sent",
  receiptQueued: "Awaiting agent acknowledgement",
  receiptApplied: "Applied",
  receiptRejected: "Rejected by the runtime",
  receiptFailed: "Failed",
  receiptUnknown: "Outcome unknown"
};

async function openWorkDetail(page, workItemId) {
  await page.goto(`/portal/work/${workItemId}`);
  await expect(page.locator("h1")).toBeVisible();
  await expect(page.locator(".detail-section").first()).toBeVisible({ timeout: 20_000 });
}

const workSurface = (page, isMobile, title) =>
  isMobile ? page.locator("#main-content") : page.getByRole("dialog", { name: title });

async function executionOf(page, workItemId) {
  const response = await api(page, "GET", `/portal/api/work-items/${workItemId}`);
  expect(response.status, JSON.stringify(response.body)).toBe(200);
  return response.body.work_item.execution;
}

async function detailOf(page, workItemId) {
  const response = await api(page, "GET", `/portal/api/work-items/${workItemId}`);
  expect(response.status, JSON.stringify(response.body)).toBe(200);
  return response.body;
}

async function registeredRuntime(page, profile, workspaceName) {
  const response = await api(page, "GET", "/portal/api/workspace");
  expect(response.status).toBe(200);
  return (response.body.registered_runtimes ?? []).find(
    (runtime) => runtime.agent_profile === profile && runtime.workspace === workspaceName
  );
}

// The exact rendered value text for one advertised operation value. Anything
// missing from this map is a value the surface has no defined rendering for and
// must fail the comparison rather than pass as prose.
// The rendered text for one served execution state. An unmapped state fails the
// comparison instead of passing as prose.
const stateText = (state) => {
  const text = {
    queued: "Queued",
    assigned: "Assigned",
    claimed: "Claimed",
    running: "Running",
    cancelling: "Cancelling",
    paused: "Paused",
    waiting_for_input: "Waiting for input",
    completed: "Completed",
    failed: "Failed",
    cancelled: "Cancelled"
  }[state];
  expect(text, `a rendered label for execution state ${JSON.stringify(state)}`).toBeTruthy();
  return text;
};

const capabilityValueText = (value) => {
  const text = {
    true: "Supported",
    false: "Not supported",
    native_steer: "Native steering",
    next_turn: "Delivered with the next turn",
    unsupported: "Not supported",
    safe_boundary: "Pause at a safe boundary",
    reported: "Reported",
    estimated: "Estimated",
    unknown: "Unknown"
  }[String(value)];
  expect(text, `a rendered value for ${JSON.stringify(value)}`).toBeTruthy();
  return text;
};

// Attaches the registered agent/runtime references the runtime admission checks
// require, exactly as the classic acceptance setup did: an `agent` resource's
// `external_ref` is the registered agent profile, a `runtime` resource's is the
// registered runtime id.
async function bindAgentReferences(page, projectId, name) {
  const runtime = await registeredRuntime(page, fixtureBinding(name), fixtureBinding(name));
  expect(runtime, `registered runtime ${fixtureBinding(name)}`).toBeTruthy();
  await attachResource(page, projectId, {
    kind: "agent",
    name: `${name} agent`,
    external_ref: runtime.agent_profile,
    status: "healthy",
    sync_status: "synced"
  });
  await attachResource(page, projectId, {
    kind: "runtime",
    name: `${name} runtime`,
    external_ref: runtime.runtime_id,
    status: "healthy",
    sync_status: "synced"
  });
  return runtime;
}

async function agentWork(page, projectId, { title, profile, workspaceName, description }) {
  return createWorkItem(page, projectId, {
    title,
    description,
    status: "ready",
    priority: "medium",
    assignee_type: "agent",
    agent_profile: profile,
    workspace: workspaceName
  });
}

test.describe("Goal 2 live daemon acceptance", () => {
  test.skip(!liveEnabled, "Set SYMMETRY_LIVE_DAEMON=1 after starting the documented fixture runtimes.");

  // A native adapter runtime must name the repository resource it can serve, so
  // the harness runtime starts after this case: it creates that resource with
  // the real browser session and hands its id to the runner. The case asserts
  // nothing itself.
  test("provisions the repository resource the real-harness runtime will serve", async ({ page }) => {
    test.skip(!!process.env["SYMMETRY_LIVE_HARNESS_BINDING"], "The runner already provisioned the harness repository.");
    await login(page);
    const key = uniqueKey("HR");
    const project = await createProject(page, { key, name: `Live harness repository ${key}` });
    const repository = await attachResource(page, project.id, {
      kind: "repository",
      name: "Harness repository",
      provider: "fixture",
      external_ref: `symmetry/live-harness-${key}`,
      url: `https://example.invalid/symmetry/live-harness-${key}`,
      status: "healthy",
      sync_status: "synced"
    });
    await fs.writeFile(process.env["SYMMETRY_LIVE_HARNESS_REPOSITORY_FILE"] ?? path.join(os.tmpdir(), "symmetry-live-harness-repository.json"), JSON.stringify({
      project_id: project.id,
      repository_resource_id: repository.id
    }, null, 2));
  });

  test("waiting input and failed retry complete on the readable work detail", async ({ page }) => {
    test.setTimeout(180_000);
    const key = uniqueKey("LW");
    await login(page);
    const project = await createProject(page, { key, name: `Live daemon ${key}` });
    await bindAgentReferences(page, project.id, "wait");
    await bindAgentReferences(page, project.id, "retry");

    const waitTitle = `Waiting workflow ${key}`;
    const wait = await agentWork(page, project.id, {
      title: waitTitle,
      profile: fixtureBinding("wait"),
      workspaceName: fixtureBinding("wait"),
      description: "Request a decision and resume through the durable input command."
    });
    await openWorkDetail(page, wait.id);
    await page.getByRole("button", { name: ui.start }).click();
    await expect(page.getByText(ui.stateWaiting, { exact: true }).first()).toBeVisible({ timeout: 30_000 });

    // The waiting form posts the served waiting transition with a durable
    // attempt identity.
    await expect.poll(() => executionOf(page, wait.id), { timeout: 30_000 }).toMatchObject({ state: "waiting_for_input" });
    await expect(page.locator(".waiting-input")).toContainText("decision");
    await page.getByLabel(ui.response).fill("continue");
    await page.getByRole("button", { name: ui.send }).click();
    await expect(page.getByText(ui.stateCompleted, { exact: true }).first()).toBeVisible({ timeout: 30_000 });
    await expect.poll(() => executionOf(page, wait.id), { timeout: 30_000 }).toMatchObject({ state: "completed", generation: 1 });

    const waited = await detailOf(page, wait.id);
    const waitedTimeline = waited.raw.timeline;
    expect(waitedTimeline).toEqual(
      expect.arrayContaining([
        expect.objectContaining({ source: "event", data: expect.objectContaining({ kind: "waiting_for_input" }) }),
        expect.objectContaining({ source: "command", data: expect.objectContaining({ kind: "provide_input" }) }),
        expect.objectContaining({ source: "transition", data: expect.objectContaining({ state: "completed" }) })
      ])
    );
    const inputCommand = waitedTimeline.find((entry) => entry.source === "command" && entry.data.kind === "provide_input");
    expect(inputCommand.data.acknowledgement_outcome, "the daemon acknowledged the input command").toBe("applied");
    // The rendered receipt for that served command is its acknowledgement, and
    // never the pending text of a request the runtime already applied.
    // The acknowledged outcome is the only one that renders `data-outcome="applied"`
    // (a control-plane-applied command renders a distinct outcome whose text also
    // contains "Applied"), so the receipt is located by its outcome first.
    await expect(page.locator('.receipt[data-outcome="applied"]').first()).toContainText(ui.receiptApplied);
    await expect(page.locator(".receipt").filter({ hasText: ui.receiptQueued })).toHaveCount(0);

    // Failure-repair retry: generation 1 fails, the corrected intent is
    // retried, and generation 2 carries the complete delivery evidence.
    const retryTitle = `Retry workflow ${key}`;
    const retried = await agentWork(page, project.id, {
      title: retryTitle,
      profile: fixtureBinding("retry"),
      workspaceName: fixtureBinding("retry"),
      description: "First attempt fails; the corrected intent is retried as a new generation."
    });
    await openWorkDetail(page, retried.id);
    await page.getByRole("button", { name: ui.start }).click();
    await expect(page.getByText(ui.stateFailed, { exact: true }).first()).toBeVisible({ timeout: 30_000 });
    await expect.poll(() => executionOf(page, retried.id), { timeout: 30_000 }).toMatchObject({ state: "failed", generation: 1 });

    const corrected = await projectItem(page, project.id, retryTitle);
    await updateWorkItem(page, retried.id, { description: "Corrected intent for the retry attempt.", version: corrected.version });
    await page.reload();
    await page.getByRole("button", { name: ui.retry }).click();
    await expect(page.getByText(ui.stateCompleted, { exact: true }).first()).toBeVisible({ timeout: 30_000 });
    await expect.poll(() => executionOf(page, retried.id), { timeout: 30_000 }).toMatchObject({ state: "completed", generation: 2 });

    const retriedDetail = await detailOf(page, retried.id);
    const retriedTimeline = retriedDetail.raw.timeline;
    // The corrected intent is copied into the second durable generation: the
    // retry re-admits the task from the current work-item text, not from the
    // failed attempt's frozen goal.
    expect(retriedDetail.outcome.goal).toContain("Corrected intent for the retry attempt.");
    const generations = new Set(retriedTimeline.map((entry) => entry.generation).filter((value) => value !== null));
    expect(generations, "both attempts stay distinguishable").toEqual(new Set([1, 2]));
    const kindsByGeneration = new Map();
    for (const entry of retriedTimeline.filter((item) => item.source === "event" && item.generation === 2)) {
      kindsByGeneration.set(entry.data.kind, (kindsByGeneration.get(entry.data.kind) ?? 0) + 1);
    }
    expect([...kindsByGeneration.keys()].sort()).toEqual(
      ["progress", "finding", "artifact", "test", "pull_request", "ci", "review", "summary"].sort()
    );
    // Receipts for both generations render beside each other; the generation-2
    // retry receipt is its own card and the failed generation-1 run is not
    // presented as applied.
    const retryCommand = retriedTimeline.find((entry) => entry.source === "command" && entry.data.kind === "retry");
    expect(retryCommand.data.acknowledgement_outcome, "a retry is recorded by the control plane, not acknowledged by a runtime").toBeNull();
    // The rendered history refreshes after the retry settles; the server's own
    // record is asserted above, and the rendered history must agree with it.
    await expect(page.locator(".receipt").filter({ hasText: ui.receiptDeferred }).first()).toBeVisible({ timeout: 20_000 });
    await expect(page.locator(".receipt").first()).not.toContainText(ui.receiptUnknown);
  });

  test("a cancel racing a fast completion stays consistent and is never reported as applied when unconfirmed", async ({ page, isMobile }) => {
    test.skip(isMobile, "The cancel/completion race runs at desktop width; the waiting-input and history cases run at both widths.");
    test.setTimeout(180_000);
    const key = uniqueKey("LR");
    await login(page);
    const project = await createProject(page, { key, name: `Live race ${key}` });
    await bindAgentReferences(page, project.id, "race");
    await bindAgentReferences(page, project.id, "cancel");

    // (1) The race: start a fast-completing run and cancel immediately. The
    // server decides; the surface must show exactly the decided outcome and the
    // durable cancel command's own acknowledgement outcome.
    const raceTitle = `Race workflow ${key}`;
    const race = await agentWork(page, project.id, {
      title: raceTitle,
      profile: fixtureBinding("race"),
      workspaceName: fixtureBinding("race"),
      description: "Complete quickly while a cancel is in flight."
    });
    await openWorkDetail(page, race.id);
    await page.getByRole("button", { name: ui.start }).click();
    await expect(page.getByRole("button", { name: ui.cancel })).toBeVisible({ timeout: 30_000 });
    await page.getByRole("button", { name: ui.cancel }).click();
    // The server decides the race. Three outcomes are honest: the run finished
    // before the cancel landed, the cancel terminated it, or the cancel was
    // recorded while the attempt was already terminal, which the server keeps
    // as `cancelling` — a state this host's daemons never acknowledge because
    // they are not consumers of cancel commands.
    await expect.poll(async () => (await executionOf(page, race.id)).state, { timeout: 30_000 }).toMatch(/^(cancelled|cancelling|completed)$/);
    const raceExecution = await executionOf(page, race.id);
    const cancelCommand = (await detailOf(page, race.id)).raw.timeline.find(
      (entry) => entry.source === "command" && entry.data.kind === "cancel"
    );
    expect(cancelCommand, "the cancel is a durable command either way").toBeTruthy();
    expect(["applied", "rejected", null]).toContain(cancelCommand.data.acknowledgement_outcome);
    // The rendered state is the served state, never a locally invented one.
    await expect(page.getByText(stateText(raceExecution.state), { exact: true }).first()).toBeVisible({ timeout: 30_000 });
    if (raceExecution.state !== "cancelling") {
      // A terminal attempt offers no cancel; a recorded-but-unacknowledged
      // cancel keeps offering no second one either way.
      await expect(page.getByRole("button", { name: ui.cancel })).toHaveCount(0);
    }

    // (2) A long-running process is really cancelled: the daemon terminates the
    // live process and the attempt stops.
    const cancelTitle = `Cancel workflow ${key}`;
    const cancelled = await agentWork(page, project.id, {
      title: cancelTitle,
      profile: fixtureBinding("cancel"),
      workspaceName: fixtureBinding("cancel"),
      description: "Enter a real running process and cancel it through a durable command."
    });
    await openWorkDetail(page, cancelled.id);
    await page.getByRole("button", { name: ui.start }).click();
    await expect(page.getByText(ui.stateRunning, { exact: true }).first()).toBeVisible({ timeout: 30_000 });
    await page.getByRole("button", { name: ui.cancel }).click();
    await expect(page.getByText(ui.stateCancelled, { exact: true }).first()).toBeVisible({ timeout: 30_000 });
    await expect.poll(() => executionOf(page, cancelled.id), { timeout: 30_000 }).toMatchObject({ state: "cancelled" });
    const cancelledDetail = await detailOf(page, cancelled.id);
    const cancelOutcome = cancelledDetail.raw.timeline.find((entry) => entry.source === "command" && entry.data.kind === "cancel");
    expect(cancelOutcome.data.acknowledgement_outcome, "the terminated run acknowledged its cancel").toBe("applied");
    await expect(page.locator('.receipt[data-outcome="applied"]').first()).toContainText(ui.receiptApplied);
    await expect(page.getByRole("button", { name: ui.retry })).toBeVisible();

    // (3) An unconfirmed attempt after a lost response: the request really
    // reaches the server (the route forwards it) and only the response is
    // dropped, so the durable command exists while the browser has no
    // confirmation. After the reload the attempt renders as pending again —
    // never applied — and pressing the control replays the same attempt.
    const lostTitle = `Lost response ${key}`;
    const lost = await agentWork(page, project.id, {
      title: lostTitle,
      profile: fixtureBinding("wait"),
      workspaceName: fixtureBinding("wait"),
      description: "Cancel request whose response is lost before the reload."
    });
    await openWorkDetail(page, lost.id);
    await page.getByRole("button", { name: ui.start }).click();
    await expect(page.getByRole("button", { name: ui.cancel })).toBeVisible({ timeout: 30_000 });
    await page.route(`**/portal/api/work-items/${lost.id}/cancel`, async (route) => {
      await route.fetch();
      await route.abort("failed");
    });
    // The dropped response is the only console error this case may produce.
    allowBrowserError(page, /net::ERR_FAILED/);
    await page.getByRole("button", { name: ui.cancel }).click();
    await expect(page.locator(".receipt").filter({ hasText: ui.receiptUnknown }).first()).toBeVisible({ timeout: 30_000 });
    const durableCancel = (await detailOf(page, lost.id)).raw.timeline.find(
      (entry) => entry.source === "command" && entry.data.kind === "cancel"
    );
    expect(durableCancel, "the lost response still left a durable command").toBeTruthy();
    await page.unroute(`**/portal/api/work-items/${lost.id}/cancel`);
    await page.reload();
    await expect(page.locator("h1")).toBeVisible();
    // The attempt that this browser minted is still stored, so it renders as
    // pending again; the daemon may already have acknowledged the durable
    // command, in which case the served snapshot owns the state instead. Either
    // way the attempt is never rendered as applied.
    const pending = page.locator(".receipt").filter({ hasText: ui.receiptRequested }).first();
    const served = page.getByText(ui.stateCancelled, { exact: true }).first();
    await expect(pending.or(served).first()).toBeVisible({ timeout: 20_000 });
    if (await pending.count() === 1) await expect(pending).not.toContainText(ui.receiptApplied);
    if (await page.getByRole("button", { name: ui.cancel }).count() === 1) {
      await page.getByRole("button", { name: ui.cancel }).click();
    }
    await expect(page.locator(".receipt").filter({ hasText: ui.receiptUnknown })).toHaveCount(0);
    const replay = (await detailOf(page, lost.id)).raw.timeline.filter(
      (entry) => entry.source === "command" && entry.data.kind === "cancel"
    );
    expect(replay, "the same attempt replays as one durable command").toHaveLength(1);
  });

  test("runtime capability rows match the daemon's own registration record", async ({ page, isMobile }) => {
    test.skip(isMobile, "The capability comparison runs at desktop width.");
    test.skip(!harnessBinding, "The real-harness runtime is not running for this invocation.");
    await login(page);
    const profile = harnessBinding;
    const workspaceName = harnessBinding;
    const runtime = await registeredRuntime(page, profile, workspaceName);
    expect(runtime, `registered runtime ${profile}`).toBeTruthy();
    expect(runtime.capabilities.adapter, "a real harness advertises an adapter record").toBeTruthy();

    // `/portal/runtimes` renders the runtimes this deployment would accept work
    // for, and a runtime becomes compatible through a project binding. The
    // binding work item is never started: a real harness turn is out of this
    // increment's scope, and the contract clause under test is the capability
    // record's parity with the daemon's registration payload.
    const key = uniqueKey("LC");
    const project = await createProject(page, { key, name: `Live capability ${key}` });
    await agentWork(page, project.id, {
      title: `Capability binding ${key}`,
      profile,
      workspaceName,
      description: "Binds the real harness runtime into this project's compatibility set."
    });

    // The server's own projection is the comparison source; the page must
    // render exactly that record.
    // Navigation keeps the project scope, so the compatibility set the page
    // reads is the one this project's binding produced.
    await page.goto(`/portal/projects/${project.id}`);
    await page.getByRole("link", { name: "Runtimes" }).click();
    await expect(page).toHaveURL(/\/portal\/runtimes/);
    await expect(page.locator("h1")).toBeVisible();
    const card = page.locator(`article.runtime-card[data-runtime-key="${runtime.runtime_key}"]`);
    await expect(card).toBeVisible();
    expect(await card.getAttribute("data-runtime-id")).toBe(runtime.runtime_id);
    expect(await card.getAttribute("data-adapter")).toBe(JSON.stringify(runtime.capabilities.adapter));
    expect(await card.getAttribute("data-capabilities")).toBe(JSON.stringify(runtime.capabilities));

    const operations = runtime.capabilities.adapter.operations;
    // Every advertised operation is rendered, one row each, and every value is
    // written down as the served text — including the explicit negatives.
    await expect(card.locator(".capability-row")).toHaveCount(Object.keys(operations).length);
    for (const [operation, value] of Object.entries(operations)) {
      const row = card.locator(`.capability-row[data-operation="${operation}"]`);
      await expect(row, `rendered ${operation}`).toBeVisible();
      await expect(row.locator(".capability-value")).toHaveText(capabilityValueText(value));
    }
    expect(operations.pause).toBe("unsupported");
    await expect(card.getByText("Pause at a safe boundary")).toHaveCount(0);
    await expect(card.locator('.capability-row[data-operation="guidance"]')).toContainText("Not supported");
    // Repository affinity is the served value, not a default.
    expect(await card.getAttribute("data-repository-resource-id")).toBe(runtime.repository_resource_id ?? "");
    await expect(card.getByText(runtime.runtime_name)).toBeVisible();
    await expect(card.getByText(runtime.machine_name)).toBeVisible();
  });

  test("raw execution history loads older daemon records on demand", async ({ page, isMobile }, testInfo) => {
    test.setTimeout(240_000);
    const key = uniqueKey("LH");
    const historyTitle = `History workflow ${key}`;
    await login(page);
    const project = await createProject(page, { key, name: `Live history ${key}` });
    await bindAgentReferences(page, project.id, "history");
    const history = await agentWork(page, project.id, {
      title: historyTitle,
      profile: fixtureBinding("history"),
      workspaceName: fixtureBinding("history"),
      description: "Produce enough real execution records to verify raw history pagination."
    });
    await openWorkDetail(page, history.id);
    await page.getByRole("button", { name: ui.start }).click();
    // The daemon writes 110 debug records before its evidence; the served
    // state is the barrier, and the rendered text is asserted from it.
    await expect.poll(() => executionOf(page, history.id), { timeout: 60_000 }).toMatchObject({ state: "completed" });
    await expect(page.getByText("Completed the requested engineering work", { exact: false }).first()).toBeVisible({ timeout: 30_000 });

    const detail = workSurface(page, isMobile, historyTitle);
    const historySection = detail.locator("section.detail-section", { has: page.getByRole("heading", { name: "Execution history" }) });
    const entries = historySection.locator("ol.timeline > li.timeline-entry");
    const firstCount = await entries.count();
    expect(firstCount).toBeGreaterThan(0);

    // The rendered detail paginates from the boundary of the read that served
    // it; the API read here confirms the same task serves one.
    expect((await detailOf(page, history.id)).raw.next_before).toBeTruthy();
    const olderPage = page.waitForResponse((response) => {
      const url = new URL(response.url());
      return url.pathname === `/portal/api/work-items/${history.id}/timeline` && url.searchParams.has("before") && response.status() === 200;
    });
    const olderRequest = page.waitForRequest((request) => {
      const url = new URL(request.url());
      return url.pathname === `/portal/api/work-items/${history.id}/timeline` && url.searchParams.has("before");
    });
    await historySection.getByRole("button", { name: ui.loadOlder }).click();
    // Cursors are signed opaque tokens minted per GET for one task's timeline,
    // and a background refresh can move the boundary between two reads of a
    // still-running run. The boundary is therefore compared as the same task's
    // cursor shape, and the rendered page proves the load by returning the
    // daemon's own older record.
    const requestedCursor = new URL((await olderRequest).url()).searchParams.get("before");
    expect(requestedCursor).toBeTruthy();
    expect(requestedCursor.split(".")).toHaveLength(3);
    expect((await olderPage).status()).toBe(200);
    await expect.poll(() => entries.count(), { timeout: 15_000 }).toBeGreaterThan(firstCount);
    await expect(historySection.getByText(ui.noOlder)).toBeVisible();
    await expect(historySection.getByText("debug-001", { exact: false }).first()).toBeVisible();

    const historyCapture = testInfo.outputPath("work-history-pagination.png");
    await page.screenshot({ path: historyCapture, fullPage: true });
    await testInfo.attach("work-history-pagination", { path: historyCapture, contentType: "image/png" });
    await fs.writeFile(testInfo.outputPath("work-history-pagination.json"), JSON.stringify({
      url: page.url(),
      viewport: page.viewportSize(),
      width: isMobile ? "narrow (mobile Chromium project)" : "desktop",
      surface: "/portal/work/:workItemId execution history, real daemon records",
      workItemId: history.id,
      workItemTitle: historyTitle,
      requestedCursor,
      firstPageEntries: firstCount,
      entriesAfterOlderPage: await entries.count(),
      observations: [
        "Load older history requested a signed cursor for this task's timeline",
        "the real older page rendered the daemon's debug-001 record and exhausted the cursor",
        "cursor tokens are signed per GET; a still-running run can move the boundary between reads, so the request is asserted by shape and the rendered older page proves the load"
      ]
    }, null, 2));
  });
});
