// Goal 0007i — Portal cutover acceptance: the one-time legacy hash repair, the
// operator invalidation channel, its reconnect catch-up, the polling fallback,
// reduced motion and the narrow viewport.
//
// Real surfaces only: the released React shell against a running Phoenix control
// plane, the real operator socket, real PostgreSQL reads and the accepted Goal
// and work-item APIs. The WebSocket is routed only to control *when* the browser
// may connect; every message the browser receives is the server's own.
import { allowBrowserError, expect, test } from "./fixtures.mjs";
import {
  acceptanceContract,
  api,
  authorityPolicy,
  capture,
  contextManifest,
  controlSocket,
  createProject,
  createWorkItem,
  executionPolicy,
  FALLBACK_READ_GUARD_MS,
  historyWrites,
  login,
  planProposal,
  readsStatus,
  recordHistoryWrites,
  uniqueKey
} from "./readable-fixtures.mjs";

// This suite's frozen acceptance description, passed to the shared revision and
// plan builders; every other payload literal is shared.
const acceptanceDescription = "Operator verifies the cutover boundary.";
// The socket's own reconnect backoff is the subject of the catch-up case, so its
// wait is a backstop over the client's retry schedule rather than a tighter
// observation.
const RECONNECT_GUARD_MS = 30_000;

async function attachRepository(page, projectId) {
  const response = await api(page, "POST", `/portal/api/projects/${projectId}/resources`, {
    kind: "repository",
    name: "Cutover repository",
    provider: "manual",
    external_ref: `cutover-${Date.now()}`,
    status: "healthy",
    sync_status: "synced"
  });
  expect(response.status).toBe(201);
  return response.body.resource.id;
}

/** Creates a draft Goal with one authorable plan decision, through the accepted API. */
async function createGoalWithPlan(page, projectId, repositoryId, title) {
  const created = await api(page, "POST", `/portal/api/projects/${projectId}/goals`, {
    schema_version: "symmetry.goal_create.v1",
    mutation_id: crypto.randomUUID(),
    title,
    initial_revision: {
      objective: `Expose ${title} through the portal Goal surface.`,
      non_goals: [],
      acceptance_contract: acceptanceContract(acceptanceDescription),
      authority_policy: authorityPolicy(),
      execution_policy: executionPolicy(),
      context_manifest: contextManifest(),
      reason: "Initial approved Goal contract"
    }
  });
  expect(created.status, JSON.stringify(created.body)).toBe(201);
  const goalId = created.body.goal.id;

  const proposal = planProposal({ goalId, title, repositoryId, acceptanceDescription });

  const read = await api(page, "GET", `/portal/api/goals/${goalId}`);
  const decision = await api(page, "POST", `/portal/api/goals/${goalId}/commands`, {
    schema_version: "symmetry.goal_command.v1",
    mutation_id: crypto.randomUUID(),
    expected_version: read.body.version,
    expected_revision: read.body.current_revision,
    kind: "request_decision",
    payload: { kind: "plan", work_item_id: null, subject_hash: null, proposal }
  });
  expect(decision.status, JSON.stringify(decision.body)).toBe(200);
  return { goalId, decisionId: decision.body.response.decision.id };
}

/** Sends one accepted Goal command against the versions the server serves now. */
async function goalCommand(page, goalId, kind, payload) {
  const read = await api(page, "GET", `/portal/api/goals/${goalId}`);
  expect(read.status).toBe(200);
  return api(page, "POST", `/portal/api/goals/${goalId}/commands`, {
    schema_version: "symmetry.goal_command.v1",
    mutation_id: crypto.randomUUID(),
    expected_version: read.body.version,
    expected_revision: read.body.current_revision,
    kind,
    payload
  });
}

test.describe("portal cutover", () => {
  test("every documented legacy hash location lands on its new route exactly once", async ({ page }, testInfo) => {
    await login(page);
    const project = await createProject(page, { key: uniqueKey("LC"), name: "Legacy cutover project" });
    const cases = [
      // The classic attention and health anchors were project-scoped views of a
      // link that carried `project_id`, and `/portal/attention` accepts a
      // project scope, so the repair keeps it; the workspace-wide routes and the
      // unknown-anchor fallback carry no project.
      { hash: "#attention", expected: `/portal/attention?projectId=${project.id}` },
      { hash: "#health", expected: `/portal/attention?projectId=${project.id}` },
      { hash: "#board", expected: `/portal/projects/${project.id}` },
      { hash: "#activity", expected: `/portal/projects/${project.id}` },
      { hash: "#resources", expected: `/portal/connections?projectId=${project.id}` },
      { hash: "#connections", expected: "/portal/connections" },
      { hash: "#chat", expected: `/portal/chat?scope=project&projectId=${project.id}` },
      { hash: "#unknown-view", expected: "/portal/attention?unavailable=unknown-view" }
    ];
    const observed = [];
    await page.addInitScript(recordHistoryWrites);
    for (const entry of cases) {
      await page.goto(`/portal?project_id=${project.id}${entry.hash}`);
      await expect.poll(() => `${new URL(page.url()).pathname}${new URL(page.url()).search}`).toBe(entry.expected);
      // Exactly once: the destination is stable, the document was loaded once,
      // and no second redirect ran. The redirect evidence is the app's own
      // history writes for this document — exactly one write names a location,
      // and it names the destination. A 200 ms sleep used to stand in for this
      // clause, which made it a property of the clock and let a second redirect
      // that returned to the same URL satisfy it.
      const afterSettle = `${new URL(page.url()).pathname}${new URL(page.url()).search}`;
      expect(afterSettle).toBe(entry.expected);
      expect(await page.evaluate(() => performance.getEntriesByType("navigation").length)).toBe(1);
      expect((await historyWrites(page)).filter((write) => write.url !== "").map((write) => write.url))
        .toEqual([entry.expected]);
      observed.push({ hash: entry.hash, expected: entry.expected, observed: afterSettle });
    }
    await page.goto(`/portal?project_id=${project.id}#board`);
    await expect(page.getByRole("heading", { level: 1, name: project.name })).toBeVisible();
    // The removed presentation is gone, not merely unreachable from the SPA: the
    // classic document and its script answer 404 on this revision.
    const classic = await page.request.get("/portal/classic");
    const classicScript = await page.request.get("/assets/portal.js");
    expect(classic.status()).toBe(404);
    expect(classicScript.status()).toBe(404);
    await capture(page, testInfo, "legacy-hash-routes", {
      routes: observed,
      removedPresentation: { classic: classic.status(), portalScript: classicScript.status() },
      note: "Each documented legacy hash location landed on its new route with a stable URL and a single document load; no classic destination is reachable"
    });
  });

  test("a connected surface refreshes from the operator invalidation channel without a reload", async ({ page, browser }, testInfo) => {
    await login(page);
    const project = await createProject(page, { key: uniqueKey("RT"), name: "Realtime project" });
    const readerContext = await browser.newContext();
    const reader = await readerContext.newPage();
    // The reader's socket is routed through the recording handler before it
    // connects, so this case can state which path carried the change.
    const socket = await controlSocket(reader);
    await login(reader, `/portal/projects/${project.id}`);
    await expect.poll(() => socket.established).toBeGreaterThan(0);
    const title = `Invalidated work ${project.key}`;
    await expect(reader.getByRole("link", { name: title })).toHaveCount(0);
    const documentTime = await reader.evaluate(() => performance.timeOrigin);

    const invalidation = reader.waitForResponse(
      (response) => response.url().includes("/portal/api/workspace") && response.status() === 200
    );
    // Count only the frames this write can have caused.
    socket.pushed = 0;
    socket.pushedAt = null;
    const writeStarted = Date.now();
    await createWorkItem(page, project.id, { title, status: "ready", priority: "medium" });
    const response = await invalidation;
    const readAt = Date.now();
    await expect(reader.getByRole("link", { name: title })).toBeVisible();
    // A push, not the 5 s fallback: the reader's own established socket received
    // the server's invalidation frame between the write and the read, and the
    // read followed it inside the batch window. A connected client stops
    // polling, so no fallback tick can be the cause of this read.
    expect(socket.pushed, "the reader's socket received at least one invalidation frame").toBeGreaterThan(0);
    expect(socket.pushedAt, "the push crossed the reader's socket after the write started").toBeGreaterThanOrEqual(writeStarted);
    expect(socket.pushedAt, "the reader's read followed the pushed invalidation").toBeLessThanOrEqual(readAt);
    // The elapsed time is the contract here, not a standby for one: this case
    // exists to prove the read came from the pushed invalidation rather than
    // from the 5 s fallback tick, so the read must sit inside the batch window
    // and well inside that cadence — measured between the two product events
    // (the frame the reader's socket received and the read it caused), never
    // waited on.
    expect(readAt - writeStarted, "the read landed well inside the 5 s polling cadence").toBeLessThan(3000);
    // The reader never reloaded: the same document received the invalidation.
    expect(await reader.evaluate(() => performance.timeOrigin)).toBe(documentTime);
    await capture(reader, testInfo, "realtime-invalidation", {
      workItem: title,
      pushedInvalidations: socket.pushed,
      readAfterWriteMs: readAt - writeStarted,
      workspaceRead: response.url(),
      note: "The reader's established socket received the server's invalidation frame and the reader's own workspace read followed it before the 5 s fallback cadence could tick; the reader document was not reloaded"
    });
    await readerContext.close();
  });

  test("a reconnect refetches the snapshot and continues the receipts from their last sequence", async ({ page }, testInfo) => {
    // Closing the operator socket is the point of this case: the browser logs the
    // failed connection while it retries.
    allowBrowserError(page, /WebSocket|socket/);
    const socket = await controlSocket(page);
    await login(page);
    expect(socket.socketTokenMinted).toBe(true);
    const project = await createProject(page, { key: uniqueKey("RC"), name: "Reconnect project" });
    const repositoryId = await attachRepository(page, project.id);
    const { goalId } = await createGoalWithPlan(page, project.id, repositoryId, "Reconnect receipts goal");

    await page.goto(`/portal/goals/${goalId}`);
    await expect(page.getByRole("heading", { level: 1, name: "Reconnect receipts goal" })).toBeVisible();
    const receipts = page.locator(".receipt-list li");
    await expect.poll(() => receipts.count()).toBeGreaterThanOrEqual(2);
    const before = await receipts.evaluateAll((rows) => rows.map((row) => row.textContent?.trim() ?? ""));
    const lastSequence = Math.max(
      ...before.map((row) => Number(/#(\d+)/.exec(row)?.[1] ?? 0))
    );
    expect(lastSequence).toBeGreaterThan(0);

    // The served open decision is read before the disconnect so the accepted
    // command below carries the version the server serves now.
    const openDecision = (await api(page, "GET", `/portal/api/goals/${goalId}`)).body.decisions.find(
      (decision) => decision["open?"] === true
    );
    expect(openDecision, "the plan decision is open before the disconnect").toBeTruthy();

    // Disconnect, record one more accepted receipt, then let the browser
    // reconnect: the snapshot read and the receipt cursor must recover both.
    socket.mode = "blocked";
    await expect.poll(() => socket.attempts).toBeGreaterThan(0);
    const resolved = await goalCommand(page, goalId, "resolve_decision", {
      decision_id: openDecision.id,
      expected_decision_version: openDecision.version,
      option_id: "accept",
      comment: "Recorded while this browser held no socket."
    });
    expect(resolved.status, JSON.stringify(resolved.body)).toBe(200);

    const catchUp = page.waitForRequest(
      (request) =>
        request.url().includes(`/portal/api/goals/${goalId}/events`) &&
        new URL(request.url()).searchParams.get("after") === String(lastSequence),
      { timeout: RECONNECT_GUARD_MS }
    );
    socket.mode = "live";
    await catchUp;
    await expect.poll(() => receipts.count()).toBeGreaterThan(before.length);
    const after = await receipts.evaluateAll((rows) => rows.map((row) => row.textContent?.trim() ?? ""));
    const sequences = after.map((row) => Number(/#(\d+)/.exec(row)?.[1] ?? 0));
    expect(new Set(sequences).size).toBe(sequences.length);
    expect(sequences).toEqual([...sequences].sort((left, right) => left - right));
    // The snapshot refetched too: the decision this browser saw open before the
    // disconnect renders from the server as resolved.
    await expect(page.getByText(/^Resolved:/).first()).toBeVisible();
    await capture(page, testInfo, "reconnect-catch-up", {
      goalId,
      lastSequenceBefore: lastSequence,
      receiptSequences: sequences,
      socketAttempts: socket.attempts,
      note: "The browser reconnected through the real socket, fetched the missed receipts from the last rendered sequence, and every sequence appears exactly once in order"
    });
  });

  test("the polling fallback keeps a surface converging while the socket is unavailable", async ({ page, browser }, testInfo) => {
    await login(page);
    const project = await createProject(page, { key: uniqueKey("PF"), name: "Polling fallback project" });
    const readerContext = await browser.newContext();
    const reader = await readerContext.newPage();
    // The unavailable socket has to be the reader's own: a routed socket on any
    // other page would leave this browser connected, and a connected browser stops
    // polling (`syncPolling`) and receives the frame instead of converging on its
    // own reads. Every attempt below is closed before it reaches the server.
    const socket = await controlSocket(reader);
    socket.mode = "blocked";
    await login(reader, `/portal/projects/${project.id}`);
    const title = `Fallback work ${project.key}`;
    await expect(reader.getByRole("link", { name: title })).toHaveCount(0);

    // Every read the reader issues is recorded, so the convergence below can be
    // attributed to a client-initiated read rather than to a pushed message: this
    // handler never establishes a connection at all, so no pushed message exists.
    const readerReads = [];
    reader.on("request", (request) => {
      if (request.url().includes("/portal/api/")) readerReads.push({ url: request.url(), at: Date.now() });
    });

    // The reader's own clock, read before the write: the fallback pass below must
    // have completed after this instant for its reads to be able to carry the
    // change at all.
    const createdAt = await reader.evaluate(() => Date.now());
    const createdBefore = Date.now();
    await createWorkItem(page, project.id, { title, status: "ready", priority: "medium" });
    // The wait is the client's own pass, recorded by the shell where the pass
    // issues its reads: the polling fallback refreshed every read, and the reads
    // that pass issued finished after the write. This replaces the elapsed budget
    // that used to stand in for the cadence — the record is the observation, and
    // FALLBACK_READ_GUARD_MS only decides when a missing pass is missing.
    await expect.poll(async () => {
      const mark = await readsStatus(reader);
      return mark !== null && mark.source === "fallback" && mark.settledAt !== null && mark.settledAt >= createdAt;
    }, {
      message: "the fallback pass completed and refreshed the reads after the write",
      timeout: FALLBACK_READ_GUARD_MS
    }).toBe(true);
    await expect(reader.getByRole("link", { name: title })).toBeVisible();
    // The reader converged with no established operator socket, so its own client
    // is the only thing that could have read the change, and the pass it recorded
    // is a fallback pass — a browser that is connected does not poll.
    const mark = await readsStatus(reader);
    expect(mark, "the shell states the reads' refresh record").not.toBeNull();
    expect(mark.connection, "the client reports the socket unavailable").toBe("disconnected");
    expect(socket.attempts, "the browser kept trying to connect").toBeGreaterThan(0);
    expect(socket.established, "no socket of the reader's ever reached the server").toBe(0);
    expect(socket.pushed, "no invalidation frame crossed the reader's socket").toBe(0);
    const afterCreate = readerReads.filter((read) => read.at >= createdBefore);
    expect(afterCreate.length).toBeGreaterThan(0);
    await capture(reader, testInfo, "polling-fallback", {
      workItem: title,
      refreshRecord: mark,
      readerReadsAfterCreate: afterCreate.map((read) => new URL(read.url).pathname + new URL(read.url).search),
      socketAttempts: socket.attempts,
      establishedSockets: socket.established,
      note: "With every operator-socket attempt closed before the server, the reader converged on the writer's change through its own polling pass — the shell's own record names the fallback and the reads it issued — and never through a pushed message"
    });
    await readerContext.close();
  });

  test("reduced motion renders the workspace without running animation", async ({ page }, testInfo) => {
    await page.emulateMedia({ reducedMotion: "reduce" });
    await login(page);
    // Force the loading state that carries the only animation the shell defines.
    await page.route("**/portal/api/attention*", () => undefined);
    await page.goto("/portal/attention");
    await expect(page.getByText("Loading", { exact: false }).first()).toBeVisible();
    const motion = await page.evaluate(() => {
      const elements = [...document.querySelectorAll("*")];
      return {
        animated: elements
          .filter((element) => getComputedStyle(element).animationName !== "none")
          .map((element) => `${element.tagName.toLowerCase()}.${element.className}`),
        loadingIndicator: document.querySelector(".loading-icon") === null
          ? null
          : getComputedStyle(document.querySelector(".loading-icon")).animationName
      };
    });
    expect(motion.animated).toEqual([]);
    await capture(page, testInfo, "reduced-motion", {
      motion,
      note: "With prefers-reduced-motion: reduce emulated, the shell renders the loading state with no running animation on any element"
    });
  });

  test("the workspace stays within a narrow viewport and restores focus on Escape", async ({ page }, testInfo) => {
    await page.setViewportSize({ width: 390, height: 844 });
    await login(page);
    const project = await createProject(page, { key: uniqueKey("NW"), name: "Narrow project" });
    for (const path of ["/portal/attention", `/portal/projects/${project.id}`, "/portal/connections", "/portal/runtimes"]) {
      await page.goto(path);
      await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
    }
    await page.goto("/portal/attention");
    const trigger = page.getByRole("button", { name: "Search or navigate", exact: true });
    await trigger.focus();
    await page.keyboard.press("Enter");
    await expect(page.getByRole("dialog")).toBeVisible();
    await page.keyboard.press("Escape");
    await expect(page.getByRole("dialog")).toHaveCount(0);
    await expect(trigger).toBeFocused();
    await capture(page, testInfo, "narrow-focus", {
      width: await page.evaluate(() => window.innerWidth),
      note: "Attention, project board, connections and runtimes stay inside a 390px viewport, and Escape returns focus to the control that opened the palette"
    });
  });
});
