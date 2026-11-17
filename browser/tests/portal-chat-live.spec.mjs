// Goal 0007g — live-daemon acceptance for the scoped conversation on
// /portal/chat. Run with browser/scripts/run-chat-daemon-tests.ps1 (an isolated
// cooperative daemon on a fake-agent profile, a real Portal API and PostgreSQL).
//
// The classic live Chat walk at /portal/classic#chat is migrated here: durable
// questions without execution mutation, guidance applied at a real safe
// boundary, pause and resume inside one run, the structured waiting-for-input
// decision package, completion with the guidance retained in the artifact, and
// cancellation with retained artifacts. Added by 0007g: the three scopes in one
// session with per-scope drafts, a reconnect whose receipt history comes from
// durable evidence, one action identity's identical replay and changed-payload
// refusal, a stale-generation control refused by the server, unsafe served
// output rendered inertly on a real run, the failed run's bounded failure text,
// and a keyboard and focus walk over a long conversation.
//
// Phase order is part of the fixture contract: the cooperative agent runs 200
// steps and asks for its decision at step 150, so the run-critical operators
// (question, guidance, pause) happen promptly after the claim, and the phases
// that need no live run (replay, scope switching, unsafe content, keyboard
// walk) run after the run completed.
//
// One case runs the whole walk because the runner starts one daemon with one
// slice of capacity (`existing_checkout` forbids a larger runtime capacity): a
// second independent case would race the first case's capacity-bearing run, and
// its failure would be a fixture artefact rather than a product observation.
import fs from "node:fs/promises";
import path from "node:path";
import { execFile } from "node:child_process";
import { promisify } from "node:util";
import { allowBrowserError, expect, test } from "./fixtures.mjs";
import { api, capture, createProject, login, uniqueKey } from "./readable-fixtures.mjs";

const enabled = process.env.SYMMETRY_CHAT_LIVE === "1";
const profile = process.env.SYMMETRY_CHAT_PROFILE;
const workspaceName = process.env.SYMMETRY_CHAT_WORKSPACE;
const artifacts = process.env.SYMMETRY_CHAT_ARTIFACTS;
const operatorToken = process.env.SYMMETRY_OPERATOR_TOKEN || "development-operator-token";

function requireEnvironment() {
  for (const [name, value] of [["SYMMETRY_CHAT_PROFILE", profile], ["SYMMETRY_CHAT_WORKSPACE", workspaceName], ["SYMMETRY_CHAT_ARTIFACTS", artifacts]]) {
    if (!value) throw new Error(`${name} must be set by browser/scripts/run-chat-daemon-tests.ps1`);
  }
}

async function chat(page, query) {
  const response = await api(page, "GET", `/portal/api/chat?${query}`);
  expect(response.status, JSON.stringify(response.body)).toBe(200);
  return response.body;
}

async function send(page, scope, intent, content, extra = {}, expectedStatus = 201) {
  const response = await api(page, "POST", "/portal/api/chat/messages", {
    ...scope, intent, content, action_id: crypto.randomUUID(), ...extra
  });
  expect(response.status, JSON.stringify(response.body)).toBe(expectedStatus);
  return response.body;
}

async function detail(page, workItemId) {
  const response = await api(page, "GET", `/portal/api/work-items/${workItemId}`);
  expect(response.status).toBe(200);
  return response.body;
}

async function waitState(page, workItemId, wanted, timeout = 30_000) {
  await expect.poll(async () => (await detail(page, workItemId)).work_item.execution?.state, { timeout }).toBe(wanted);
  return detail(page, workItemId);
}

async function commands(page, taskId) {
  const response = await page.request.get(`/api/v1/tasks/${taskId}/commands`, {
    headers: { Authorization: `Bearer ${operatorToken}` }
  });
  expect(response.ok()).toBe(true);
  return (await response.json()).commands;
}

async function localJournal(runId) {
  const directory = path.join(process.env.SYMMETRY_CHAT_STATE, "runs");
  for (const name of await fs.readdir(directory)) {
    if (!name.startsWith("journal-") || !name.endsWith(".json")) continue;
    try {
      const journal = JSON.parse(await fs.readFile(path.join(directory, name), "utf8"));
      if (journal.run_id === runId) return journal;
    } catch (error) {
      if (error.code !== "ENOENT") throw error;
    }
  }
  return null;
}

async function processIdentity(runId) {
  const journal = await localJournal(runId);
  if (!journal) throw new Error("The live run has no retained local journal");
  return { pid: journal.pid, identity: journal.process_identity };
}

/**
 * Reads a run's own step counter from its artifact. The cooperative agent
 * rewrites `result.json` after every step, so a read can meet a partially
 * written file: the bounded poll retries until the field parses, which is an
 * observable value arriving, never a wait for something to have happened.
 */
async function artifactCounter(file) {
  let parsed = null;
  await expect.poll(async () => {
    try {
      const value = JSON.parse(await fs.readFile(file, "utf8"));
      if (typeof value.completed_steps !== "number" || typeof value.total_steps !== "number") return false;
      parsed = value;
      return true;
    } catch {
      return false;
    }
  }, { timeout: 30_000 }).toBe(true);
  return parsed;
}

async function openRunScope(page, runId, projectId) {
  await page.goto(`/portal/chat?scope=run&runId=${runId}&projectId=${projectId}`);
  await expect(page.locator("#chat-scope")).toHaveValue("run");
  await expect(page.locator("#chat-run-select")).toHaveValue(runId);
}

async function startWork(page, projectId, title, goal) {
  await page.goto(`/portal/chat?scope=project&projectId=${projectId}`);
  await expect(page.locator("#chat-scope")).toHaveValue("project");
  await page.locator("#chat-intent").selectOption("start_work");
  await page.locator("#chat-start-fields").evaluate((element) => { element.open = true; });
  await page.locator("#chat-work-title").fill(title);
  await page.locator("#chat-content").fill(goal);
  const answered = page.waitForResponse((response) => response.url().endsWith("/portal/api/chat/messages") && response.request().method() === "POST");
  await page.locator("#chat-send").click();
  const response = await answered;
  expect(response.ok()).toBe(true);
  const body = await response.json();
  expect(body.work_item_id, JSON.stringify(body)).toBeTruthy();
  return body.work_item_id;
}

/**
 * Writes the pull-request URL through the real version-fenced API. A terminal
 * run keeps settling its work item, so the write re-reads the served version on
 * the server's own 409 instead of pinning a version that may already be stale.
 */
async function setPullRequestUrl(page, workItemId, url) {
  for (let attempt = 0; attempt < 10; attempt += 1) {
    const current = (await detail(page, workItemId)).work_item;
    const response = await api(page, "PATCH", `/portal/api/work-items/${workItemId}`, { version: current.version, pull_request_url: url });
    if (response.status === 200) return response.body.work_item;
    expect(response.status, JSON.stringify(response.body)).toBe(409);
    await page.waitForTimeout(500);
  }
  throw new Error("the work item never accepted the pull-request update");
}

test.describe("Goal 0007g live scoped conversation", () => {
  test.skip(!enabled, "Run browser/scripts/run-chat-daemon-tests.ps1 for the isolated cooperative daemon.");
  test.skip(({ isMobile }) => isMobile, "Desktop owns live execution; the deterministic /portal/chat suite covers both widths.");

  test("questions, guidance, pause, resume, decisions and completion share one real run history through the scoped conversation", async ({ page }, testInfo) => {
    test.setTimeout(600_000);
    requireEnvironment();
    // The deliberate 409s are the only console errors this walk may produce.
    allowBrowserError(page, /409 \(Conflict\)/);
    allowBrowserError(page, /net::ERR_CONNECTION_FAILED|Failed to load resource/);
    await login(page, "/portal/chat");
    const key = uniqueKey("CH");
    const project = await createProject(page, {
      key,
      name: `Chat live ${key}`,
      defaultAgentProfile: profile,
      defaultWorkspace: workspaceName
    });
    const projectScope = { scope: "project", project_id: project.id };

    // The workspace scope carries enough real history to scroll and to page;
    // the walk switches scopes around it (this runs before the claim so it
    // cannot delay the operator's first control).
    for (let index = 1; index <= 60; index += 1) {
      await send(page, { scope: "workspace" }, "discuss", `${key} workspace history ${index}`);
    }

    const goal = "Implement an autonomous progress report and retain the guidance in its final artifact.";
    const workId = await startWork(page, project.id, `Autonomous progress report ${key}`, goal);
    let snapshot = await waitState(page, workId, "running");
    const { task_id: taskId, run_id: runId, generation } = snapshot.work_item.execution;
    expect(runId).toBeTruthy();
    const originalProcess = await processIdentity(runId);
    expect(originalProcess.pid).toBeGreaterThan(0);
    const runScope = { scope: "run", run_id: runId };
    await openRunScope(page, runId, project.id);
    await expect(page.locator("#chat-scope-context")).toContainText("Attempt 1");

    // A question is read-only: no command is created and the run identity is
    // unchanged. The word "cancel" in the question is not an instruction.
    const beforeQuestion = await commands(page, taskId);
    await send(page, runScope, "discuss", `What is happening, and why? Mentioning cancel here is not an instruction. ${key}`);
    const afterQuestion = await commands(page, taskId);
    expect(afterQuestion).toEqual(beforeQuestion);
    expect((await detail(page, workId)).work_item.execution.run_id).toBe(runId);

    // The generic fake-agent binding advertises the legacy supervisory protocol:
    // guidance is delivered at the worker's next safe execution boundary and
    // pause is offered, because the server's own can_pause says so.
    await expect(page.locator("#chat-guidance-delivery")).toContainText("next safe execution boundary");
    await expect(page.locator("#chat-intent option[value=\"pause\"]")).toHaveCount(1);

    // The client-side byte limit protects the real 32,768-byte bound the command
    // payload enforces; the draft survives the refusal and no command is created.
    const guidance = `Include the durability finding in every subsequent progress report ${key}.`;
    await page.locator("#chat-intent").selectOption("guidance");
    const oversized = "€".repeat(10_924);
    await page.locator("#chat-content").fill(oversized);
    await page.locator("#chat-send").click();
    await expect(page.locator("#chat-send-status")).toHaveText("Guidance must be 32,768 UTF-8 bytes or fewer.");
    await expect(page.locator("#chat-content")).toHaveValue(oversized);
    expect(await commands(page, taskId)).toEqual(afterQuestion);

    // Guidance through the surface, observed while its response is pending.
    await page.locator("#chat-intent").selectOption("guidance");
    await page.locator("#chat-content").fill(guidance);
    await page.locator("#chat-send").click();
    await expect(page.locator("#chat-send-status")).toContainText("awaiting agent acknowledgement");
    await expect(page.locator("#chat-messages")).toContainText(guidance);
    // The run's semantic evidence is rendered as inert text from the served
    // timeline; raw tool rows never reach this surface.
    const servedTimeline = (await chat(page, `scope=run&run_id=${runId}`)).runs[0].timeline;
    const withMessages = servedTimeline
      .filter((entry) => entry.source === "event" && typeof entry.data.payload?.message === "string")
      .sort((left, right) => String(left.recorded_at ?? "").localeCompare(String(right.recorded_at ?? "")));
    expect(withMessages.length, "the served timeline carries semantic progress entries").toBeGreaterThan(0);
    await expect(page.locator("#chat-execution-evidence")).toContainText(withMessages[0].data.payload.message.slice(0, 40));
    expect((await chat(page, `scope=run&run_id=${runId}`)).runs[0].raw.timeline).toEqual([]);

    // Pause at a real safe boundary before the agent reaches its decision step;
    // the guidance is already in the artifact.
    await page.locator("#chat-intent").selectOption("pause");
    await page.locator("#chat-content").fill(`Pause at the next safe boundary ${key}.`);
    await page.locator("#chat-send").click();
    snapshot = await waitState(page, workId, "paused", 90_000);
    expect(snapshot.work_item.execution.run_id).toBe(runId);
    expect(await processIdentity(runId)).toEqual(originalProcess);
    const artifactFile = path.join(artifacts, "progress.md");
    const pausedArtifact = await fs.readFile(artifactFile, "utf8");
    expect(pausedArtifact).toContain(guidance);

    if (process.env.SYMMETRY_CHAT_CONTROL_CONTAINER) {
      await page.goto("about:blank");
      await promisify(execFile)("docker", ["restart", process.env.SYMMETRY_CHAT_CONTROL_CONTAINER], { timeout: 60_000 });
      await expect.poll(async () => {
        try { return (await page.request.get("/healthz", { timeout: 2_000 })).ok(); } catch { return false; }
      }, { timeout: 45_000 }).toBe(true);
      expect(await fs.readFile(artifactFile, "utf8")).toBe(pausedArtifact);
      expect(await processIdentity(runId)).toEqual(originalProcess);
    }

    // Reconnect while the run is paused: the conversation read fails, the loaded
    // conversation stays visible, and the recovery read carries the durable
    // guidance receipt. Reads retry by policy, so the failure persists until the
    // stale state is observed; recovery goes through the surface's own control.
    let failReads = true;
    await page.route("**/portal/api/chat?**", async (route) => {
      if (failReads) {
        await route.abort("connectionfailed");
        return;
      }
      await route.continue();
    });
    await page.getByRole("button", { name: "Refresh", exact: true }).click();
    await expect(page.locator(".notice-error")).toBeVisible();
    await expect(page.locator("#chat-messages")).toContainText(guidance);
    // Observed while the surface's own reads are still failing, so this reads
    // the server through the context's request API (not routed by page.route).
    await expect.poll(async () => {
      const response = await page.request.get(`/portal/api/chat?scope=run&run_id=${runId}`);
      if (!response.ok()) return `status ${response.status()}`;
      const conversation = await response.json();
      return conversation.messages.find((message) => message.content === guidance)?.command?.acknowledgement_outcome;
    }, { timeout: 90_000 }).toBe("applied");
    failReads = false;
    await page.getByRole("button", { name: "Try again" }).click();
    await expect(page.locator(".notice-error")).toHaveCount(0);
    await page.getByRole("button", { name: "Refresh", exact: true }).click();
    const guidanceMessage = (await chat(page, `scope=run&run_id=${runId}`)).messages.find((message) => message.content === guidance);
    await expect(page.locator(`[data-receipt-for="${guidanceMessage.id}"]`)).toContainText("Applied");
    await expect(page.locator("#chat-history-gap")).toHaveCount(0);

    // Resume inside the same run, then the daemon's structured decision packet.
    await openRunScope(page, runId, project.id);
    await expect(page.locator("#chat-intent option[value=\"resume\"]")).toHaveCount(1);
    await page.locator("#chat-intent").selectOption("resume");
    await page.locator("#chat-content").fill(`Continue with the retained guidance ${key}.`);
    await page.locator("#chat-send").click();
    await waitState(page, workId, "running");
    snapshot = await waitState(page, workId, "waiting_for_input", 120_000);
    const waiting = snapshot.work_item.execution.waiting;
    expect(waiting.payload.decision.options.length).toBeGreaterThanOrEqual(2);
    expect(waiting.payload.decision.context).toBeTruthy();

    await page.getByRole("button", { name: "Refresh", exact: true }).click();
    const packet = page.locator("#chat-decision");
    await expect(packet).toBeVisible();
    await expect(packet).toContainText(waiting.question);
    await expect(page.locator(`[data-chat-decision="${waiting.payload.decision.recommended_option_id}"]`)).toContainText("Recommended");
    await capture(page, testInfo, "chat-decision", {
      scope: "run",
      question: waiting.question,
      optionIds: waiting.payload.decision.options.map((option) => option.id)
    });
    await page.locator(`[data-chat-decision="${waiting.payload.decision.recommended_option_id}"]`).click();
    await expect(page.locator("#chat-send-status")).toContainText("awaiting agent acknowledgement");

    snapshot = await waitState(page, workId, "completed", 120_000);
    expect(snapshot.work_item.execution.run_id).toBe(runId);
    expect(snapshot.work_item.execution.generation).toBe(generation);
    const finalArtifact = await fs.readFile(path.join(artifacts, "result.json"), "utf8");
    expect(finalArtifact).toContain(guidance);
    const persisted = await chat(page, `scope=run&run_id=${runId}`);
    expect(persisted.runs[0].work_item.execution.state).toBe("completed");
    expect(persisted.messages.some((message) => message.content === guidance)).toBe(true);
    expect(persisted.messages.filter((message) => message.role === "human" && message.intent === "decision")).toHaveLength(1);

    // One action identity: an identical replay answers the original record, a
    // changed payload is refused, and neither ever applies a second message.
    const actionId = crypto.randomUUID();
    const first = await send(page, projectScope, "discuss", `${key} one recorded question`, { action_id: actionId });
    const replay = await send(page, projectScope, "discuss", `${key} one recorded question`, { action_id: actionId }, 200);
    expect(replay.message.id).toBe(first.message.id);
    const changed = await api(page, "POST", "/portal/api/chat/messages", {
      ...projectScope, intent: "discuss", content: `${key} a changed question`, action_id: actionId
    });
    expect(changed.status, JSON.stringify(changed.body)).toBe(409);
    expect(changed.body.error.code).toBe("idempotency_conflict");
    const settled = await chat(page, `scope=project&project_id=${project.id}`);
    expect(settled.messages.filter((message) => message.role === "human" && message.content === `${key} one recorded question`)).toHaveLength(1);
    expect(settled.messages.filter((message) => message.role === "human" && message.content === `${key} a changed question`)).toHaveLength(0);

    // Scope switching in one session: each scope keeps its own draft, selection
    // and scroll, and the run scope keeps its own conversation.
    await page.goto(`/portal/chat?scope=project&projectId=${project.id}`);
    await expect(page.locator("#chat-scope-context")).toContainText("Project conversation");
    // The project scope offers the question and start-work intents; guidance,
    // pause, resume and decisions belong to a run scope.
    await page.locator("#chat-intent").selectOption("status");
    await page.locator("#chat-content").fill(`${key} project draft`);
    await page.locator("#chat-scope").selectOption("workspace");
    await expect(page.locator("#chat-scope-context")).toContainText("Workspace conversation");
    // The workspace scope accumulates everything earlier runs wrote, so the
    // walk's counts come from the served pages instead of a seeded literal.
    const workspacePage = await chat(page, "scope=workspace");
    const olderWorkspacePage = workspacePage.next_before
      ? await chat(page, `scope=workspace&before=${encodeURIComponent(workspacePage.next_before)}`)
      : { messages: [] };
    const workspaceTotal = workspacePage.messages.length + olderWorkspacePage.messages.length;
    await expect(page.locator("#chat-content")).toHaveValue("");
    await page.locator("#chat-content").fill(`${key} workspace draft`);
    // The transcript pane carries rendered messages before the walk positions
    // it; while the read is pending the pane is the loading state instead.
    // The workspace scope's served page is the largest read in this walk and
    // shares the host with the live daemon, so it gets a load-sized wait; the
    // observation itself (this scope renders its own history) is unchanged.
    await expect(page.locator("#chat-messages [data-message-id]").first()).toBeVisible({ timeout: 30_000 });
    await page.locator("#chat-scroll").evaluate((element) => { element.scrollTop = 60; });
    expect(await page.locator("#chat-scroll").evaluate((element) => element.scrollTop)).toBe(60);
    await page.locator("#chat-scope").selectOption("project");
    await expect(page.locator("#chat-content")).toHaveValue(`${key} project draft`);
    await expect(page.locator("#chat-intent")).toHaveValue("status");
    await page.locator("#chat-scope").selectOption("workspace");
    await expect(page.locator("#chat-content")).toHaveValue(`${key} workspace draft`);
    expect(await page.locator("#chat-scroll").evaluate((element) => element.scrollTop)).toBe(60);

    // Unsafe served output on a real run: a message body carrying markup. Its
    // content is durable record, so the assertion is that the rendered
    // conversation shows the text inertly and creates no element from it.
    const injected = `Unsafe body ${key} <img src=x onerror="window.chatUnsafe=true">`;
    await send(page, runScope, "discuss", injected);
    await openRunScope(page, runId, project.id);
    await expect(page.locator("#chat-messages")).toContainText(injected);
    await expect(page.locator("#chat-messages img, #chat-scope-context img")).toHaveCount(0);
    await expect(page.locator("a[href^=\"javascript:\"]")).toHaveCount(0);
    expect(await page.evaluate(() => window.chatUnsafe)).toBeUndefined();

    // Keyboard and focus walk over this long conversation: the composer keeps
    // focus, caret and draft when a background refresh is announced (the 0007a
    // host keeps its documents visible after native minimization), the scopes
    // stay reachable by keyboard, and Load older keeps the reader's place.
    const composer = page.locator("#chat-content");
    const keyboardDraft = `${key} keyboard draft`;
    await composer.fill("");
    await composer.click();
    await page.keyboard.type(keyboardDraft);
    await composer.evaluate((element) => element.setSelectionRange(4, 9));
    // The background refresh is observed where it lands. The conversation read
    // is `staleTime: 0`, so a window-focus refetch is a real read of the server:
    // the walk waits for that read's response (a state change) instead of
    // sleeping past it, and the focus, draft and caret assertions then run
    // against the refreshed render. The event goes to `window`, which is where
    // TanStack Query's focus manager listens (`window.addEventListener
    // ("visibilitychange", …)`); a `document`-dispatched event with no bubbling
    // never reaches that listener, which is why the previous sleep-only version
    // could not tell a refresh from no refresh at all.
    const refetched = page.waitForResponse(
      (response) => response.request().method() === "GET" && response.url().includes("/portal/api/chat?"),
      { timeout: 30_000 }
    );
    await page.evaluate(() => window.dispatchEvent(new Event("visibilitychange")));
    await refetched;
    await expect(composer).toBeFocused();
    await expect(composer).toHaveValue(keyboardDraft);
    expect(await composer.evaluate((element) => [element.selectionStart, element.selectionEnd])).toEqual([4, 9]);
    await page.locator("#chat-scope").focus();
    await page.keyboard.press("Home");
    await expect(page.locator("#chat-scope-context")).toContainText("Workspace conversation");
    await composer.click();
    // The run-scope visit reloaded the document (the scope lives in the URL and
    // run scopes are addressable), so this scope starts from its own served
    // history again: the walk types its draft here and regains focus by keyboard.
    await page.keyboard.type(`${key} keyboard workspace draft`);
    await expect(page.locator("#chat-messages [data-message-id]")).toHaveCount(workspacePage.messages.length, { timeout: 30_000 });
    const loadOlder = page.locator("#chat-load-older");
    await loadOlder.focus();
    await page.keyboard.press("Enter");
    await expect(page.locator("#chat-messages [data-message-id]")).toHaveCount(workspaceTotal, { timeout: 30_000 });
    await expect(loadOlder).toBeFocused();
    await capture(page, testInfo, "chat-keyboard-walk", {
      scope: await page.locator("#chat-scope").inputValue(),
      composerFocused: await composer.evaluate((element) => document.activeElement === element),
      caret: await composer.evaluate((element) => [element.selectionStart, element.selectionEnd]),
      renderedMessages: await page.locator("#chat-messages [data-message-id]").count()
    });

    // Cancellation stays a real control-plane effect with retained artifacts,
    // while the surface never dispatched a cancel of its own (0007f owns it).
    // A control request whose generation the server has already left behind is
    // refused while that run is live: one real request body is rewritten to
    // freeze the stale generation, so the answer is the server's own 409.
    const cancelId = await startWork(page, project.id, `Cancel and retain progress ${key}`, "Write a progress report that can be cancelled.");
    const cancelSnapshot = await waitState(page, cancelId, "running");
    const cancelRunId = cancelSnapshot.work_item.execution.run_id;
    await openRunScope(page, cancelRunId, project.id);
    await page.locator("#chat-intent").selectOption("guidance");
    await page.locator("#chat-content").fill(`${key} stale guidance`);
    await page.route("**/portal/api/chat/messages", async (route) => {
      const body = route.request().postDataJSON();
      await route.continue({ postData: JSON.stringify({ ...body, generation: 99 }) });
    });
    await page.locator("#chat-send").click();
    await expect(page.locator("#chat-send-status")).toContainText("Refused");
    await expect(page.locator("#chat-content")).toHaveValue(`${key} stale guidance`);
    expect((await chat(page, `scope=run&run_id=${cancelRunId}`)).messages.some((message) => message.content === `${key} stale guidance`)).toBe(false);
    await page.unroute("**/portal/api/chat/messages");

    // Cancellation is asserted on the run's own recorded state, never on text a
    // completed or running attempt would also carry: this work item's goal
    // contains the word "cancelled", so a `progress.md` substring match is
    // satisfied before any cancel takes effect. The artifact's step counter is
    // the observable instead, and it is read before the control is requested.
    const resultFile = path.join(artifacts, "result.json");
    const beforeCancel = await artifactCounter(resultFile);
    expect(beforeCancel.goal, "the artifact belongs to the attempt being cancelled").toContain("can be cancelled");
    const cancelCommand = await send(page, { scope: "run", run_id: cancelRunId }, "cancel", "Cancel this execution.", {
      work_item_id: cancelId,
      generation: cancelSnapshot.work_item.execution.generation
    });
    await waitState(page, cancelId, "cancelled");
    await expect.poll(async () => {
      const list = await commands(page, cancelSnapshot.work_item.execution.task_id);
      return list.find((command) => command.command_id === cancelCommand.command.command_id);
    }, { timeout: 90_000 }).toMatchObject({ state: "acknowledged", acknowledgement_outcome: "applied" });
    const stepsAtAcknowledgement = (await artifactCounter(resultFile)).completed_steps;
    // The daemon reaps the cancelled attempt: with its local journal released no
    // process is left to write, and the counter is compared across that
    // observable state change. A writer that survived the cancellation advances
    // it here, and a run that was never stopped shows it at completion below.
    await expect.poll(() => localJournal(cancelRunId), { timeout: 20_000 }).toBeNull();
    const cancelledResult = await artifactCounter(resultFile);
    expect(cancelledResult.completed_steps, "no writer advanced the artifact after the cancel was acknowledged").toBe(stepsAtAcknowledgement);
    expect(cancelledResult.completed_steps, "the cancelled attempt never lost the steps it had recorded").toBeGreaterThanOrEqual(beforeCancel.completed_steps);
    expect(cancelledResult.completed_steps, "the cancelled attempt's artifact is the stopped attempt's, never a completed run's").toBeLessThan(cancelledResult.total_steps);

    // Task-level requirements reach the daemon; a malformed wait fails clearly
    // without stranding the event outbox or poisoning the next assignment, and
    // the failed run's served failure text is bounded, escaped text in its scope.
    const invalidWait = await send(page, projectScope, "start_work",
      "[symmetry-fake-agent:wait_input]\nRequire a structured decision packet.",
      { work: { title: `Reject an incomplete decision packet ${key}` } });
    // The daemon releases the cancelled run's capacity slot and claims this one;
    // both steps travel over its own poll, so the wait matches the live suite's
    // other state waits instead of the desktop default.
    const invalidSnapshot = await waitState(page, invalidWait.work_item_id, "failed", 120_000);
    expect(JSON.stringify(invalidSnapshot.work_item.execution.failure)).toContain("decision packet");
    expect(invalidSnapshot.work_item.execution.waiting).toBeNull();
    await openRunScope(page, invalidSnapshot.work_item.execution.run_id, project.id);
    const failure = page.locator("#chat-failure");
    await expect(failure).toBeVisible();
    await expect(failure).toHaveAttribute("data-failure-state", "failed");
    await expect(failure).not.toContainText("{");
    expect((await failure.innerText()).length).toBeLessThanOrEqual(2_001);
    // A terminal run's work item is editable, so its pull-request URL is a real
    // served value. The server itself refuses a script scheme (the restriction
    // is not only cosmetic), and the https URL renders as the one anchor.
    // (`execution_fields_editable` refuses this edit while a task is live, which
    // is why the write happens on the failed run.)
    for (let attempt = 0; ; attempt += 1) {
      const current = (await detail(page, invalidWait.work_item_id)).work_item;
      const unsafeUpdate = await api(page, "PATCH", `/portal/api/work-items/${invalidWait.work_item_id}`, {
        version: current.version,
        pull_request_url: "javascript:alert(1)"
      });
      if (unsafeUpdate.status === 422) {
        expect(unsafeUpdate.body.error.code).toBe("validation_failed");
        expect(JSON.stringify(unsafeUpdate.body.error.fields)).toContain("pull_request_url");
        break;
      }
      expect(unsafeUpdate.status, JSON.stringify(unsafeUpdate.body)).toBe(409);
      expect(attempt, "the work item never settled enough to validate the unsafe link").toBeLessThan(10);
      await page.waitForTimeout(500);
    }
    await page.getByRole("button", { name: "Refresh", exact: true }).click();
    await expect(page.locator("a[href^=\"javascript:\"]")).toHaveCount(0);
    expect(await page.evaluate(() => window.chatUnsafe)).toBeUndefined();
    await setPullRequestUrl(page, invalidWait.work_item_id, "https://github.com/acme/symmetry/pull/41");
    await page.getByRole("button", { name: "Refresh", exact: true }).click();
    await expect(page.locator("a[href=\"https://github.com/acme/symmetry/pull/41\"]")).toHaveCount(1);
    const afterInvalid = await send(page, projectScope, "start_work",
      "[symmetry-fake-agent:success]\nContinue normally after the invalid adapter response.",
      { work: { title: `Recover after invalid adapter output ${key}` } });
    await waitState(page, afterInvalid.work_item_id, "completed", 120_000);
  });
});
