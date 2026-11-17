// Goal 0007g — scoped engineering conversation on /portal/chat.
//
// This suite is the migration target of the classic Chat acceptance
// (browser/tests/portal-chat.spec.mjs at /portal/classic#chat): the per-scope
// draft/selection/scroll walk, the question-versus-control separation, the
// uncertain-send retry, the cursored history walk and the unsafe-content walk
// now run against the React route and the real Portal API.
//
// Every rendered state this suite asserts is decoded from the running control
// plane: projects, messages, cursors and refusals are real responses, and the
// conversation read is the server's own scope resolution. Network interception
// appears in four places, each named in the case that uses it and each
// separated from real-data acceptance:
//   1. holding a real conversation read past a scope switch (late read);
//   2. aborting one real message POST and rewriting one real request body to
//      produce the server's own refusal (uncertain send, idempotency conflict);
//   3. aborting one real conversation read to observe the reconnect path;
//   4. fulfilling the run-scope conversation and one runtime row from the
//      committed chat-read contract fixture, because no native runtime exists
//      on this host — the real run-scope walk (decision packets, control
//      receipts, guidance delivery on a real runtime) runs in
//      browser/tests/portal-chat-live.spec.mjs against a real daemon, and the
//      injected body can only pass because it is a schema-valid committed
//      fixture, never a hand-written payload.
import fs from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { allowBrowserError, expect, test } from "./fixtures.mjs";
import { api, attachResource, capture, createProject, login, uniqueKey } from "./readable-fixtures.mjs";

// Per-test backstops for the real-data walks below. They seed and read dozens of
// durable messages through the real API (44 messages with three scope changes;
// 66 messages with a cursor walk and a full paging pass), so the per-test maximum
// is the walk's own budget rather than an observation: the guard only decides how
// long an unfinished walk runs before the case is declared failed, and every
// rendered state it asserts is still the control plane's own answer.
const WALK_GUARD_MS = 150_000;
const HISTORY_WALK_GUARD_MS = 240_000;
const repoRoot = path.resolve(fileURLToPath(new URL("../..", import.meta.url)));

function deferred() {
  let resolve;
  const promise = new Promise((done) => { resolve = done; });
  return { promise, resolve };
}

const workspaceScope = { scope: "workspace" };
const projectScope = (projectId) => ({ scope: "project", project_id: projectId });

async function conversation(page, query) {
  const response = await api(page, "GET", `/portal/api/chat?${query}`);
  expect(response.status, JSON.stringify(response.body)).toBe(200);
  return response.body;
}

async function post(page, scope, intent, content, extra = {}) {
  const response = await api(page, "POST", "/portal/api/chat/messages", {
    ...scope, intent, content, action_id: crypto.randomUUID(), ...extra
  });
  expect(response.status, JSON.stringify(response.body)).toBe(201);
  return response.body;
}

async function openChat(page, query) {
  await page.goto(`/portal/chat?${query}`);
  await expect(page.locator("#chat-scope")).toBeVisible();
  await expect(page.getByRole("heading", { level: 1, name: "Chat" })).toBeVisible();
}

function messageRows(page) {
  return page.locator("#chat-messages [data-message-id]");
}

async function setScroll(page, value) {
  const applied = await page.locator("#chat-scroll").evaluate((element, top) => {
    element.scrollTop = top;
    return element.scrollTop;
  }, value);
  expect(applied, "the conversation scrolled to the retained offset").toBe(value);
}

async function scrollOffset(page) {
  return page.locator("#chat-scroll").evaluate((element) => element.scrollTop);
}

// Sets a pane offset and refuses to continue unless the pane can really hold it:
// a pane whose content is shorter than the pane clamps the value, and a clamped
// offset must fail here rather than be reported later as a retained one.
async function setScrolledOffset(page, value) {
  const reachable = await page.locator("#chat-scroll").evaluate((element) => element.scrollHeight - element.clientHeight);
  expect(reachable, `the conversation pane scrolls at least ${value}px`).toBeGreaterThanOrEqual(value);
  await setScroll(page, value);
}

async function currentMessageIds(page) {
  return messageRows(page).evaluateAll((elements) => elements.map((element) => element.dataset.messageId));
}

async function refresh(page) {
  await page.getByRole("button", { name: "Refresh", exact: true }).click();
}

// Seeds real durable messages through the authenticated API; one post creates
// the human message and its assistant reply, exactly as the classic suite's
// fixture model did with two records per submission.
async function seedMessages(page, scope, count, prefix) {
  for (let index = 1; index <= count; index += 1) {
    const detail = "Preserve the rollback path and record the compatibility finding in the progress report.";
    await post(page, scope, "discuss", `${prefix} ${index} — ${detail}`);
  }
}

test.describe("Scoped conversation", () => {
  test("each scope keeps its own draft, intent and scroll, and a late read never overwrites another scope's draft", async ({ page }, testInfo) => {
    // A real-data walk: 44 seeded messages, three scope changes and a held read.
    test.setTimeout(WALK_GUARD_MS);
    await login(page, "/portal/chat");
    const key = uniqueKey("CS");
    const project = await createProject(page, { key, name: `Chat scopes ${key}` });
    const repository = await attachResource(page, project.id, { kind: "repository", name: `Repo ${key}`, external_ref: `acme/${key}` });
    const ci = await attachResource(page, project.id, { kind: "ci", name: `CI ${key}` });
    // Enough real messages that both scopes scroll at both widths.
    await seedMessages(page, projectScope(project.id), 22, `${key} project history`);
    await seedMessages(page, workspaceScope, 22, `${key} workspace history`);

    await openChat(page, `scope=workspace&projectId=${project.id}`);
    await expect(page.locator("#chat-scope-context")).toContainText("Workspace conversation");
    await expect(page.locator("#chat-messages")).toContainText(`${key} workspace history 1`);
    // The transcript pane carries rendered messages before the walk positions
    // it; while the read is pending the pane is the loading state instead.
    await expect(messageRows(page).first()).toBeVisible();
    await page.locator("#chat-intent").selectOption("status");
    await page.locator("#chat-content").fill(`${key} workspace draft`);
    await setScroll(page, 40);

    // The start-work selections are part of the scope's kept state: the target
    // project, the repository and the CI resource survive a scope switch (the
    // migrated classic assertion).
    await page.locator("#chat-intent").selectOption("start_work");
    await page.locator("#chat-start-fields").evaluate((element) => { element.open = true; });
    await page.locator("#chat-target-project").selectOption(project.id);
    await page.locator("#chat-repository").selectOption(repository.id);
    await page.locator("#chat-ci").selectOption(ci.id);

    // Scope switching is the surface's own control, not only the URL.
    await page.locator("#chat-scope").selectOption("project");
    await expect(page.locator("#chat-scope-context")).toContainText("Project conversation");
    await expect(page.locator("#chat-content")).toHaveValue("");
    await expect(messageRows(page).first()).toBeVisible();
    await page.locator("#chat-intent").selectOption("discuss");
    await page.locator("#chat-content").fill(`${key} project draft`);
    await setScroll(page, 120);

    await page.locator("#chat-scope").selectOption("workspace");
    await expect(page.locator("#chat-scope-context")).toContainText("Workspace conversation");
    await expect(page.locator("#chat-content")).toHaveValue(`${key} workspace draft`);
    await expect(page.locator("#chat-intent")).toHaveValue("start_work");
    await expect(page.locator("#chat-target-project")).toHaveValue(project.id);
    await expect(page.locator("#chat-repository")).toHaveValue(repository.id);
    await expect(page.locator("#chat-ci")).toHaveValue(ci.id);
    await expect(messageRows(page).first()).toBeVisible();
    expect(await scrollOffset(page)).toBe(40);

    // A held read for the project scope is released while the workspace scope
    // is selected: it may only update its own scope.
    const held = deferred();
    const started = deferred();
    await page.route("**/portal/api/chat?**", async (route) => {
      const url = new URL(route.request().url());
      if (held.pending && url.searchParams.get("scope") === "project") {
        held.pending = false;
        started.resolve();
        await held.promise;
      }
      await route.continue();
    });
    held.pending = true;
    await page.locator("#chat-scope").selectOption("project");
    await started.promise;
    await page.locator("#chat-scope").selectOption("workspace");
    await expect(page.locator("#chat-scope-context")).toContainText("Workspace conversation");
    await page.locator("#chat-content").fill(`${key} workspace draft after late read`);
    held.resolve();
    await expect(page.locator("#chat-scope")).toHaveValue("workspace");
    await expect(page.locator("#chat-scope-context")).toContainText("Workspace conversation");
    await expect(page.locator("#chat-content")).toHaveValue(`${key} workspace draft after late read`);
    // The pane restores its own scroll when it renders this scope's history, so
    // the scroll observation follows the render instead of racing it.
    await expect(page.locator("#chat-messages [data-message-id]").first()).toBeVisible();
    expect(await scrollOffset(page)).toBe(40);

    // The project scope still holds its own draft, selection and messages.
    await page.locator("#chat-scope").selectOption("project");
    await expect(page.locator("#chat-content")).toHaveValue(`${key} project draft`);
    await expect(page.locator("#chat-intent")).toHaveValue("discuss");
    await expect(page.locator("#chat-messages")).toContainText(`${key} project history 22`);
    expect(await scrollOffset(page)).toBe(120);
    await capture(page, testInfo, "chat-scope-switch", {
      scope: await page.locator("#chat-scope").inputValue(),
      workspaceDraftKept: `${key} workspace draft after late read`,
      projectDraft: await page.locator("#chat-content").inputValue(),
      projectScroll: await scrollOffset(page),
      renderedMessages: (await currentMessageIds(page)).length
    });
  });

  test("a question is saved without a command, and an uncertain send retries with the same action identity exactly once", async ({ page }) => {
    await login(page, "/portal/chat");
    const key = uniqueKey("CU");
    const project = await createProject(page, { key, name: `Chat uncertain ${key}` });
    await openChat(page, `scope=project&projectId=${project.id}`);
    await expect(page.locator("#chat-scope-context")).toContainText("Project conversation");

    const bodies = [];
    page.on("request", (request) => {
      if (new URL(request.url()).pathname === "/portal/api/chat/messages") bodies.push(request.postDataJSON());
    });

    await page.locator("#chat-content").fill(`${key} does canceling lose our work?`);
    await page.locator("#chat-send").click();
    await expect(page.locator("#chat-send-status")).toHaveText("Saved to this context.");
    expect(bodies, "one attempt for one question").toHaveLength(1);
    expect(bodies[0].intent).toBe("discuss");
    const asked = await conversation(page, `scope=project&project_id=${project.id}`);
    const question = asked.messages.filter((message) => message.role === "human" && message.content === `${key} does canceling lose our work?`);
    expect(question, "one durable question").toHaveLength(1);
    expect(question[0].command, "a question never carries a command").toBeNull();

    // The next POST is aborted: the response is lost, so the surface keeps the
    // draft and the frozen action identity instead of claiming a save.
    allowBrowserError(page, /net::ERR_CONNECTION_FAILED|Failed to fetch|Failed to load resource/);
    let abortOnce = true;
    await page.route("**/portal/api/chat/messages", async (route) => {
      if (abortOnce) {
        abortOnce = false;
        await route.abort("connectionfailed");
        return;
      }
      await route.continue();
    });
    const uncertain = `${key} uncertain question`;
    await page.locator("#chat-content").fill(uncertain);
    await page.locator("#chat-send").click();
    await expect(page.locator("#chat-send-status")).toContainText("Send not confirmed");
    await expect(page.locator("#chat-content")).toHaveValue(uncertain);
    expect(bodies, "the abort happened after the request was sent").toHaveLength(2);

    await page.locator("#chat-send").click();
    await expect(page.locator("#chat-send-status")).toHaveText("Saved to this context.");
    expect(bodies, "the retry is a new attempt").toHaveLength(3);
    expect(bodies[2], "the retry reuses the frozen payload byte for byte").toEqual(bodies[1]);
    expect(bodies[2].action_id).toBe(bodies[1].action_id);
    const after = await conversation(page, `scope=project&project_id=${project.id}`);
    expect(
      after.messages.filter((message) => message.role === "human" && message.content === uncertain),
      "a replayed identity never applies a second message"
    ).toHaveLength(1);
    await expect(page.locator("#chat-messages")).toContainText(uncertain);
    await expect(messageRows(page).filter({ hasText: uncertain })).toHaveCount(1);
  });

  test("a changed replay of one action identity is refused by the server and rendered as the server's refusal", async ({ page }) => {
    // The deliberate 409 is the only console error this case may produce.
    allowBrowserError(page, /409 \(Conflict\)/);
    await login(page, "/portal/chat");
    const key = uniqueKey("CR");
    const project = await createProject(page, { key, name: `Chat replay ${key}` });
    const actionId = crypto.randomUUID();
    const original = { scope: "project", project_id: project.id, intent: "discuss", content: `${key} original question`, action_id: actionId };

    const created = await api(page, "POST", "/portal/api/chat/messages", original);
    expect(created.status, JSON.stringify(created.body)).toBe(201);
    const replayed = await api(page, "POST", "/portal/api/chat/messages", original);
    expect(replayed.status, "an identical replay answers the original record").toBe(200);
    expect(replayed.body.message.id).toBe(created.body.message.id);
    const changed = await api(page, "POST", "/portal/api/chat/messages", { ...original, content: `${key} changed question` });
    expect(changed.status, JSON.stringify(changed.body)).toBe(409);
    expect(changed.body.error.code).toBe("idempotency_conflict");

    const after = await conversation(page, `scope=project&project_id=${project.id}`);
    const humans = after.messages.filter((message) => message.role === "human");
    expect(humans.filter((message) => message.content === `${key} original question`)).toHaveLength(1);
    expect(humans.filter((message) => message.content === `${key} changed question`)).toHaveLength(0);

    // The surface renders the same refusal: one real request body is rewritten
    // to reuse the already-committed action identity with changed content, so
    // the 409 is the server's own answer to a changed replay.
    await openChat(page, `scope=project&projectId=${project.id}`);
    await page.route("**/portal/api/chat/messages", async (route) => {
      const body = route.request().postDataJSON();
      await route.continue({ postData: JSON.stringify({ ...body, action_id: actionId, content: `${key} changed question` }) });
    });
    await page.locator("#chat-content").fill(`${key} surface question`);
    await page.locator("#chat-send").click();
    await expect(page.locator("#chat-send-status")).toContainText("Refused");
    await expect(page.locator("#chat-send-status")).toContainText("idempotency");
    await expect(page.locator("#chat-content")).toHaveValue(`${key} surface question`);
    await expect(page.locator("#chat-messages")).not.toContainText(`${key} changed question`);
    const settled = await conversation(page, `scope=project&project_id=${project.id}`);
    expect(settled.messages.filter((message) => message.role === "human" && message.content === `${key} changed question`)).toHaveLength(0);
  });

  test("history follows the served cursor, keeps loaded pages across a refresh, and names an unloaded boundary instead of presenting it as continuous", async ({ page }, testInfo) => {
    // A real-data walk: 66 seeded messages, the served cursor, a recovery read
    // and a full API paging pass.
    test.setTimeout(HISTORY_WALK_GUARD_MS);
    await login(page, "/portal/chat");
    const key = uniqueKey("CH");
    const project = await createProject(page, { key, name: `Chat history ${key}` });
    const prefix = `${key} history`;
    await seedMessages(page, projectScope(project.id), 30, prefix);

    await openChat(page, `scope=project&projectId=${project.id}`);
    // The read carries the newest page plus the served cursor.
    await expect(messageRows(page)).toHaveCount(50);
    await expect(page.locator("#chat-load-older")).toBeVisible();
    await page.locator("#chat-load-older").click();
    await expect(messageRows(page)).toHaveCount(60);
    await expect(page.locator("#chat-load-older")).toBeHidden();
    await expect(page.locator("#chat-history-gap")).toHaveCount(0);

    // Six more durable messages arrive while the older page is displayed.
    await seedMessages(page, projectScope(project.id), 3, prefix);
    await refresh(page);
    await expect(page.locator("#chat-load-older")).toBeVisible();
    const refreshedPage = await conversation(page, `scope=project&project_id=${project.id}`);
    const newestPageOldestId = refreshedPage.messages[0].id;
    // The refreshed page and the retained page share no message id: the
    // boundary between them was never loaded, and the surface says so.
    const gap = page.locator("#chat-history-gap");
    await expect(gap).toBeVisible();
    await expect(gap).toHaveAttribute("data-boundary-current", newestPageOldestId);
    const retainedBoundary = await gap.getAttribute("data-boundary-retained");
    const renderedIds = await currentMessageIds(page);
    expect(renderedIds).toContain(retainedBoundary);
    await capture(page, testInfo, "chat-history-boundary", {
      renderedMessages: renderedIds.length,
      boundaryCurrent: newestPageOldestId,
      boundaryRetained: retainedBoundary,
      loadOlderVisible: await page.locator("#chat-load-older").isVisible()
    });

    // Loading older continues from the refreshed boundary and closes the gap.
    await page.locator("#chat-load-older").click();
    await expect(page.locator("#chat-history-gap")).toHaveCount(0);

    // Every durable message stays reachable: page the API with the served
    // cursors and compare the full id set with what the surface rendered.
    const allIds = [];
    let page1 = await conversation(page, `scope=project&project_id=${project.id}`);
    allIds.push(...page1.messages.map((message) => message.id));
    while (page1.next_before) {
      page1 = await conversation(page, `scope=project&project_id=${project.id}&before=${encodeURIComponent(page1.next_before)}`);
      allIds.push(...page1.messages.map((message) => message.id));
    }
    expect(allIds).toHaveLength(66);
    const surfaceIds = await currentMessageIds(page);
    expect(new Set(surfaceIds)).toEqual(new Set(allIds));
    await expect(page.locator("#chat-load-older")).toBeHidden();

    // Reconnect: the read fails, the loaded conversation stays visible as
    // stale, and the recovery read reconciles with the server's own snapshot.
    allowBrowserError(page, /503|Failed to load resource/);
    let failReads = true;
    await page.route("**/portal/api/chat?**", async (route) => {
      if (failReads) {
        await route.abort("connectionfailed");
        return;
      }
      await route.continue();
    });
    // Reads retry by policy, so the failure persists until the stale state has
    // been observed; the recovery then goes through the surface's own control.
    await refresh(page);
    await expect(page.locator(".notice-error")).toBeVisible();
    await expect(messageRows(page)).toHaveCount(66);
    failReads = false;
    await page.getByRole("button", { name: "Try again" }).click();
    await expect(page.locator(".notice-error")).toHaveCount(0);
    expect(new Set(await currentMessageIds(page))).toEqual(new Set(allIds));
    await expect(page.locator("#chat-history-gap")).toHaveCount(0);
  });

  test("run scope renders the served scope, decision packet, durable receipts and inert unsafe output", async ({ page }) => {
    // Synthetic guard (daemon-free by construction): the run projection and the
    // runtime row are the committed chat-read contract fixture plus one
    // schema-valid runtime row, because no native runtime exists on this host.
    // The real run-scope walk lives in the live-daemon suite.
    const fixture = JSON.parse(await fs.readFile(path.join(repoRoot, "contracts/fixtures/valid/chat-read.run-waiting-decision.json"), "utf8"));
    const runId = fixture.run_id;
    const projectId = fixture.project_id;
    const runtimeId = fixture.runs[0].work_item.execution.runtime_id;
    const waiting = fixture.runs[0].work_item.execution.waiting;

    await login(page, "/portal/chat");
    await page.route("**/portal/api/chat?**", async (route) => {
      const url = new URL(route.request().url());
      if (url.searchParams.get("scope") === "run" && url.searchParams.get("run_id") === runId) {
        await route.fulfill({ json: fixture });
        return;
      }
      await route.continue();
    });
    // One schema-valid runtime row is added to every workspace read so the
    // fixture run resolves its adapter declaration (native steering / no pause).
    const fixtureRuntime = {
      machine_id: "00000000-0000-4000-8000-00000000c0de",
      machine_name: "fixture machine",
      runtime_id: runtimeId,
      runtime_key: "fixture-native",
      runtime_name: "Fixture native runtime",
      status: "online",
      agent_profile: "fixture",
      workspace: "primary",
      capabilities: {
        adapter: {
          kind: "pi",
          native_version: "1.5.2",
          implementation_version: "pi-rpc-v1",
          protocol_version: 1,
          operations: {
            start: true, events: true, cancel: true, resume: true, handoff: false,
            guidance: "next_turn", pause: "unsupported", approval_response: false,
            usage: "unknown", hard_cost_limit: false
          }
        }
      }
    };
    await page.route("**/portal/api/workspace**", async (route) => {
      const response = await route.fetch();
      if (response.ok()) {
        const body = await response.json();
        body.runtimes = [...(body.runtimes || []), fixtureRuntime];
        await route.fulfill({ json: body });
        return;
      }
      await route.fulfill({ json: { selected_project_id: null, projects: [], runtimes: [fixtureRuntime], registered_runtimes: [] } });
    });

    await openChat(page, `scope=run&runId=${runId}&projectId=${projectId}`);
    await expect(page.locator("#chat-scope-context")).toContainText("Run conversation");
    await expect(page.locator("#chat-run-select")).toHaveValue(runId);
    await expect(page.locator("#chat-scope")).toHaveValue("run");

    // The decision packet is the server's, option by option.
    const packet = page.locator("#chat-decision");
    await expect(packet).toContainText(waiting.question);
    await expect(packet).toContainText(waiting.decision.context);
    for (const option of waiting.decision.options) {
      const button = page.locator(`[data-chat-decision="${option.id}"]`);
      await expect(button).toContainText(option.label);
      await expect(button).toContainText(option.consequence);
    }
    await expect(page.locator(`[data-chat-decision="${waiting.decision.recommended_option_id}"]`)).toContainText("Recommended");

    // Receipts come from the served command records.
    const guidance = fixture.messages.find((message) => message.command && message.command.kind === "guidance");
    const decision = fixture.messages.find((message) => message.command && message.command.kind === "decision");
    await expect(page.locator(`[data-receipt-for="${guidance.id}"]`)).toContainText("awaiting agent acknowledgement");
    await expect(page.locator(`[data-receipt-for="${decision.id}"]`)).toContainText("Applied");

    // Guidance delivery and the unsupported pause are named, never rendered as
    // live controls: the runtime row advertises next-turn guidance and no pause.
    await expect(page.locator("#chat-guidance-delivery")).toContainText("next turn");
    await expect(page.locator("#chat-pause-support")).toContainText("stop the run from the work detail");
    await expect(page.locator("#chat-intent option[value=\"pause\"]")).toHaveCount(0);

    // Unsafe output: the fixture's message body and pull-request URL are
    // rendered as text, while the server's https CI link stays a real link.
    await expect(page.locator("#chat-messages img, #chat-messages script, #chat-scope-context img, #chat-scope-context script")).toHaveCount(0);
    await expect(page.locator("a[href^=\"javascript:\"]")).toHaveCount(0);
    await expect(page.locator("a[href=\"https://example.test/build/42\"]")).toHaveCount(1);
    expect(await page.evaluate(() => window.chatUnsafe)).toBeUndefined();
    await expect(page.locator("#chat-messages")).toContainText("<img src=x onerror=");
  });

  test("the run scope keeps its own draft, intent and scroll across an in-page round trip", async ({ page }, testInfo) => {
    // Synthetic guard (daemon-free by construction), as in the run-scope case
    // above: the run projection is the committed chat-read contract fixture plus
    // one schema-valid runtime row, because no native runtime exists on this
    // host. The project scope of the round trip is a real project with real
    // messages, and the run scope is entered and left through the surface's own
    // scope control — never a document load — so this case observes the
    // `run:<id>` key of the per-scope state.
    //
    // The fixture's run conversation is short, so this case runs in a window
    // whose pane is genuinely smaller than that conversation: the offsets it
    // asserts are real scrolls of real content, and `setScrolledOffset` refuses
    // to continue when the pane cannot scroll that far (a pane that cannot
    // scroll is a failure here, never a silent 0).
    test.setTimeout(WALK_GUARD_MS);
    const fixture = JSON.parse(await fs.readFile(path.join(repoRoot, "contracts/fixtures/valid/chat-read.run-waiting-decision.json"), "utf8"));
    const runId = fixture.run_id;
    const runtimeId = fixture.runs[0].work_item.execution.runtime_id;

    await login(page, "/portal/chat");
    const key = uniqueKey("CV");
    const project = await createProject(page, { key, name: `Chat run round trip ${key}` });
    // Enough real project history that the project pane scrolls at any width.
    await seedMessages(page, projectScope(project.id), 22, `${key} project history`);

    await page.route("**/portal/api/chat?**", async (route) => {
      const url = new URL(route.request().url());
      if (url.searchParams.get("scope") === "run" && url.searchParams.get("run_id") === runId) {
        await route.fulfill({ json: fixture });
        return;
      }
      await route.continue();
    });
    // One schema-valid runtime row is added to every workspace read so the
    // fixture run resolves its adapter declaration, exactly as the run-scope
    // case above does it.
    const fixtureRuntime = {
      machine_id: "00000000-0000-4000-8000-00000000c0de",
      machine_name: "fixture machine",
      runtime_id: runtimeId,
      runtime_key: "fixture-native",
      runtime_name: "Fixture native runtime",
      status: "online",
      agent_profile: "fixture",
      workspace: "primary",
      capabilities: {
        adapter: {
          kind: "pi",
          native_version: "1.5.2",
          implementation_version: "pi-rpc-v1",
          protocol_version: 1,
          operations: {
            start: true, events: true, cancel: true, resume: true, handoff: false,
            guidance: "next_turn", pause: "unsupported", approval_response: false,
            usage: "unknown", hard_cost_limit: false
          }
        }
      }
    };
    await page.route("**/portal/api/workspace**", async (route) => {
      const response = await route.fetch();
      if (response.ok()) {
        const body = await response.json();
        body.runtimes = [...(body.runtimes || []), fixtureRuntime];
        await route.fulfill({ json: body });
        return;
      }
      await route.fulfill({ json: { selected_project_id: null, projects: [], runtimes: [fixtureRuntime], registered_runtimes: [] } });
    });

    const viewport = page.viewportSize();
    await page.setViewportSize({ width: viewport.width, height: 260 });

    // The run scope is a deep link (run scopes are addressable); every switch
    // after this first read is the surface's own control.
    await openChat(page, `scope=run&runId=${runId}&projectId=${project.id}`);
    await expect(page.locator("#chat-scope-context")).toContainText("Run conversation");
    await page.locator("#chat-intent").selectOption("status");
    await page.locator("#chat-content").fill(`${key} run draft`);
    // The pane positions only over rendered messages: while the read is pending
    // the pane is the loading state, which cannot hold an offset at all.
    await expect(messageRows(page).first()).toBeVisible();
    await setScrolledOffset(page, 40);

    // In-page switch to the real project conversation.
    await page.locator("#chat-scope").selectOption("project");
    await expect(page.locator("#chat-scope-context")).toContainText("Project conversation");
    await expect(page.locator("#chat-content")).toHaveValue("");
    await expect(messageRows(page).first()).toBeVisible();
    await page.locator("#chat-intent").selectOption("start_work");
    await page.locator("#chat-content").fill(`${key} project draft`);
    await setScrolledOffset(page, 120);

    // ... and back to the same run through the same control: the run identity
    // travels with the scope switch, so the round trip returns to this run.
    await page.locator("#chat-scope").selectOption("run");
    await expect(page.locator("#chat-scope-context")).toContainText("Run conversation");
    await expect(page.locator("#chat-run-select")).toHaveValue(runId);
    await expect(page.locator("#chat-content")).toHaveValue(`${key} run draft`);
    await expect(page.locator("#chat-intent")).toHaveValue("status");
    await expect(messageRows(page).first()).toBeVisible();
    expect(await scrollOffset(page)).toBe(40);

    // The project scope kept its own draft, intent and offset throughout.
    await page.locator("#chat-scope").selectOption("project");
    await expect(page.locator("#chat-scope-context")).toContainText("Project conversation");
    await expect(page.locator("#chat-content")).toHaveValue(`${key} project draft`);
    await expect(page.locator("#chat-intent")).toHaveValue("start_work");
    await expect(messageRows(page).first()).toBeVisible();
    expect(await scrollOffset(page)).toBe(120);
    await capture(page, testInfo, "chat-run-scope-round-trip", {
      scope: await page.locator("#chat-scope").inputValue(),
      runDraftKept: `${key} run draft`,
      projectDraft: await page.locator("#chat-content").inputValue(),
      projectScroll: await scrollOffset(page),
      renderedMessages: (await currentMessageIds(page)).length
    });
  });
});
