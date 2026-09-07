import { allowBrowserError, expect, test } from "./fixtures.mjs";

const operatorToken = process.env.SYMMETRY_OPERATOR_TOKEN || "development-operator-token";
const projectId = "10000000-0000-0000-0000-000000000041";
const workId = "20000000-0000-0000-0000-000000000041";
const runId = "30000000-0000-0000-0000-000000000041";

function deferred() {
  let resolve;
  const promise = new Promise((done) => { resolve = done; });
  return { promise, resolve };
}

async function chatFixture(page, { waiting = false, history = [], pageSize = null } = {}) {
  // Refreshes are driven explicitly so timer timing cannot mask request races.
  await page.addInitScript(() => {
    const original = window.setInterval;
    window.setInterval = (callback, delay, ...args) => delay === 5_000 ? 0 : original(callback, delay, ...args);
  });
  const item = {
    id: workId, key: "CHAT-1", project_id: projectId, title: "Implement a reversible migration",
    description: "Preserve the original schema while validating the replacement.", status: "in_progress",
    priority: "high", position: 1, assignee: { type: "agent", name: "Supervised worker" },
    workspace: "primary", blocked: false, blocker: null, repository: "symmetry", repository_resource_id: null,
    ci_resource: null, ci_resource_id: null, branch: "codex/migration", can_start: false, external: null,
    version: 1, pull_request_url: "https://github.com/acme/symmetry/pull/41", ci_status: "passed", review_status: "required",
    delivery: {
      pull_request: { url: "https://github.com/acme/symmetry/pull/41", status: "open", source: "provider", provider: "github" },
      ci: { status: "passed", source: "provider", provider: "github" },
      review: { status: "required", source: "provider", provider: "github" }
    },
    execution: {
      task_id: "task-41", run_id: runId, generation: 3, state: waiting ? "waiting_for_input" : "running",
      supervisory_control: true, can_guide: !waiting, can_pause: !waiting, can_resume: false,
      can_cancel: true, can_retry: false, intent_locked: true, latest_command: null,
      timing: { state_since: "2026-09-06T10:00:00Z" },
      waiting: waiting ? {
        transition_id: "40000000-0000-0000-0000-000000000041",
        question: "Which migration strategy should be used?",
        decision: {
          reason: "irreversible", context: "Removing the legacy column closes the rollback path.",
          recommended_option_id: "staged",
          options: [
            { id: "staged", label: "Stage migration", consequence: "Keeps rollback available." },
            { id: "defer", label: "Defer migration", consequence: "Preserves current behavior and keeps work blocked." }
          ]
        }
      } : null
    }
  };
  const detail = {
    work_item: item,
    outcome: {
      phase: item.execution.state, owner: item.assignee, summary: "The adapter is implemented; compatibility tests passed.",
      findings: [{ message: "The old schema remains readable.", severity: "info" }],
      changed_artifacts: [{ path: "src/migration.ex" }], tests: [{ name: "Compatibility", status: "passed", summary: "12 checks" }],
      result: null, failure: null, blocker: waiting ? "Choose a migration strategy." : null
    },
    timeline: [], raw: { task: {}, timeline: [{ tool_output: "RAW_TOOL_NOISE_41" }], next_before: null }
  };
  const project = {
    id: projectId, key: "CHAT", name: "Chat engineering", description: "Shared work and conversation",
    status: "active", version: 1, default_agent_profile: "supervised", default_workspace: "primary",
    resources: [], work_items: [item]
  };
  const workspace = {
    selected_project_id: projectId, projects: [project], connections: [], runtimes: [], registered_runtimes: [], activity: [],
    health: { connections: "healthy", runtimes: "healthy", executions: "running", synchronization: "healthy" }
  };
  const model = { item, detail, project, workspace, messages: history, posts: [], reads: [], beforeGet: null, beforePost: null, beforeWorkspace: null, failWorkspace: null, nextFailure: false };

  await page.route("**/portal/api/**", async (route) => {
    const req = route.request();
    const url = new URL(req.url());
    if (req.method() === "GET" && url.pathname === "/portal/api/workspace") {
      const selected = url.searchParams.get("project_id") || workspace.selected_project_id;
      if (model.failWorkspace === selected) {
        await route.fulfill({ status: 503, json: { error: { message: "Workspace unavailable" } } });
        return;
      }
      await model.beforeWorkspace?.(selected);
      await route.fulfill({ json: { ...workspace, selected_project_id: selected } });
    } else if (req.method() === "GET" && url.pathname === `/portal/api/work-items/${workId}`) {
      await route.fulfill({ json: detail });
    } else if (req.method() === "GET" && url.pathname === "/portal/api/chat") {
      const scope = url.searchParams.get("scope");
      model.reads.push(scope);
      const history = model.messages.filter((message) => !message.scope || message.scope === scope)
        .sort((a, b) => a.inserted_at.localeCompare(b.inserted_at) || a.id.localeCompare(b.id));
      const end = url.searchParams.has("before") ? history.findIndex((message) => message.id === url.searchParams.get("before")) : history.length;
      expect(end).toBeGreaterThanOrEqual(0);
      const start = pageSize ? Math.max(0, end - pageSize) : 0;
      const messages = history.slice(start, end);
      const response = JSON.parse(JSON.stringify({
        scope, project_id: scope === "project" ? projectId : null, run_id: scope === "run" ? runId : null,
        messages, runs: [detail], next_before: start > 0 ? messages[0].id : null
      }));
      await model.beforeGet?.(scope);
      await route.fulfill({ json: response });
    } else if (req.method() === "POST" && url.pathname === "/portal/api/chat/messages") {
      const payload = req.postDataJSON();
      model.posts.push(payload);
      expect(req.headers()["x-csrf-token"]).toBeTruthy();
      if (model.nextFailure) {
        model.nextFailure = false;
        await route.abort("connectionfailed");
        return;
      }
      await model.beforePost?.(payload);
      const command = ["guidance", "pause", "resume", "cancel", "decision"].includes(payload.intent)
        ? { command_id: `command-${model.posts.length}`, kind: payload.intent, state: "pending", acknowledgement_outcome: null }
        : null;
      if (command) item.execution.latest_command = command;
      const message = {
        id: `message-${model.posts.length}`, role: "human", intent: payload.intent, scope: payload.scope,
        content: payload.content, inserted_at: `2026-09-06T11:00:${String(model.posts.length).padStart(2, "0")}Z`, command
      };
      const reply = {
        id: `reply-${model.posts.length}`, role: "assistant", intent: payload.intent, scope: payload.scope,
        content: payload.intent === "start_work" ? "Work queued. The agent will use the project defaults." : "Recorded progress: compatibility tests passed. CI passed; review is required.",
        inserted_at: `2026-09-06T11:01:${String(model.posts.length).padStart(2, "0")}Z`
      };
      model.messages.push(message, reply);
      await route.fulfill({ status: 201, json: { message, reply, work_item_id: workId, command } });
    } else {
      throw new Error(`Unexpected API request: ${req.method()} ${url.pathname}`);
    }
  });
  await page.goto("/portal/login");
  await page.getByLabel("Operator token").fill(operatorToken);
  await Promise.all([
    page.waitForURL(/\/portal(?:\?|#|$)/),
    page.getByRole("button", { name: "Open workspace" }).click()
  ]);
  await page.goto(`/portal?project_id=${projectId}#chat`);
  await expect(page.locator("#chat-run-context")).toContainText("Work in this context");
  return model;
}

async function selectRun(page) {
  await page.locator("#chat-scope").selectOption("run");
  await page.locator("#chat-run-select").selectOption(runId);
  await expect(page.locator("#chat-run-context")).toContainText("CHAT-1 · Attempt 3");
}

async function send(page, intent, content) {
  await page.locator("#chat-intent").selectOption(intent);
  await page.locator("#chat-content").fill(content);
  await page.locator("#chat-send").click();
  await expect(page.locator("#chat-send-status")).toContainText("Saved");
  await expect(page.locator("#chat-send")).toBeEnabled();
}

test("Chat separates discussion from starting real project work and preserves the complete goal", async ({ page }) => {
  const model = await chatFixture(page);
  await page.locator("#chat-scope").selectOption("workspace");
  await expect(page.locator("#chat-run-context")).toContainText("Work in this context");
  const question = "Would canceling the migration lose our existing work?";
  const readsBeforeSend = model.reads.length;
  await send(page, "discuss", question);
  expect(model.reads.length - readsBeforeSend).toBe(1);
  expect(model.posts[0]).toMatchObject({ scope: "workspace", intent: "discuss", content: question });
  expect(model.posts[0]).not.toHaveProperty("generation");
  expect(model.item.execution.state).toBe("running");
  await expect(page.locator("#chat-messages")).toContainText("Recorded progress");
  await expect(page.locator("#chat-messages")).not.toContainText("RAW_TOOL_NOISE_41");

  const goal = "Implement schema compatibility without dropping existing data.\n" + "Keep the rollback path and validate all readers. ".repeat(12);
  model.beforePost = (payload) => {
    if (payload.intent === "start_work") {
      model.project.work_items.push({ ...model.item, id: "new-work", key: "CHAT-2", title: payload.work.title, description: payload.content, status: "ready", execution: null });
    }
  };
  await page.locator("#chat-intent").selectOption("start_work");
  await page.locator("#chat-start-fields summary").click();
  await page.locator("#chat-work-title").fill("Keep schema rollback available");
  await send(page, "start_work", goal);
  expect(model.posts[1]).toMatchObject({ scope: "workspace", target_project_id: projectId, intent: "start_work", content: goal, work: { title: "Keep schema rollback available" } });
  await page.getByRole("link", { name: "Board", exact: true }).click();
  await expect(page.locator(".work-card", { hasText: "Keep schema rollback available" })).toBeVisible();
  await page.getByRole("link", { name: "Chat", exact: true }).click();
  await expect(page.locator("#chat-messages")).toContainText(goal);
  await page.reload();
  await expect(page.locator("#chat-scope")).toHaveValue("workspace");
  await expect(page.locator("#chat-messages")).toContainText("Work queued");
});

test("run guidance and controls remain durable requests until a safe-boundary acknowledgement", async ({ page }) => {
  const model = await chatFixture(page);
  await selectRun(page);
  await send(page, "status", "What is happening?");
  expect(model.posts[0].intent).toBe("status");
  expect(model.item.execution.state).toBe("running");
  const oversizedGuidance = "€".repeat(10_923);
  await page.locator("#chat-intent").selectOption("guidance");
  await page.locator("#chat-content").fill(oversizedGuidance);
  await page.locator("#chat-send").click();
  await expect(page.locator("#chat-send-status")).toHaveText("Guidance must be 32,768 UTF-8 bytes or fewer.");
  await expect(page.locator("#chat-content")).toHaveValue(oversizedGuidance);
  expect(model.posts).toHaveLength(1);
  await send(page, "guidance", "Use the existing adapter and preserve rollback.");
  expect(model.posts[1]).toMatchObject({ scope: "run", run_id: runId, intent: "guidance", work_item_id: workId, generation: 3 });
  await expect(page.locator("#chat-messages")).toContainText("awaiting agent acknowledgement");
  await page.locator('[data-chat-control="pause"]').click();
  await expect(page.locator("#chat-send-status")).toContainText("Saved");
  expect(model.posts[2]).toMatchObject({ intent: "pause", generation: 3, run_id: runId });
  await expect(page.locator("#chat-run-context .state-badge").first()).toHaveText("Running");
  Object.assign(model.item.execution.latest_command, { state: "acknowledged", acknowledgement_outcome: "applied" });
  Object.assign(model.item.execution, { state: "paused", can_pause: false, can_resume: true });
  await page.locator("#refresh-button").click();
  await expect(page.locator('[data-chat-control="resume"]')).toBeVisible();
  await expect(page.locator("#chat-messages")).toContainText("Applied at a safe boundary");
  await expect(page.locator("#chat-send-status")).toHaveText("Applied at a safe boundary");
  await page.locator('[data-chat-control="resume"]').click();
  await expect(page.locator("#chat-send-status")).toContainText("Saved");
  expect(model.posts[3]).toMatchObject({ intent: "resume", generation: 3 });
  await expect(page.locator("#chat-send-status")).toHaveText("Saved · awaiting agent acknowledgement");
  await page.locator("#chat-scope").selectOption("workspace");
  await expect(page.locator("#chat-run-context")).toContainText("Work in this context");
  await expect(page.locator("#chat-send-status")).toHaveText("Messages are saved to this context.");
  Object.assign(model.item.execution.latest_command, { state: "acknowledged", acknowledgement_outcome: "applied" });
  Object.assign(model.item.execution, { state: "running", can_pause: true, can_resume: false });
  await page.locator("#chat-scope").selectOption("run");
  await expect(page.locator('[data-chat-control="pause"]')).toBeVisible();
  await expect(page.locator("#chat-send-status")).toHaveText("Applied at a safe boundary");
  await page.locator('[data-chat-control="cancel"]').click();
  await expect(page.locator("#confirm-dialog")).toContainText("keep its history and artifacts");
  await page.locator("#confirm-dialog").getByRole("button", { name: "Cancel run", exact: true }).click();
  await expect(page.locator("#chat-send-status")).toContainText("Saved");
  expect(model.posts[4]).toMatchObject({ intent: "cancel", generation: 3, run_id: runId });
  await expect(page.locator("#chat-run-context")).toContainText("CI Passed");
  await expect(page.locator("#chat-run-context").getByRole("link", { name: "Open PR" })).toHaveAttribute("href", model.item.pull_request_url);
});

test("decision packets bind the selected consequence to the waiting transition and link to collapsed diagnostics", async ({ page, isMobile }) => {
  if (!isMobile) await page.setViewportSize({ width: 1280, height: 720 });
  const model = await chatFixture(page, { waiting: true });
  await selectRun(page);
  await expect(page.locator("#chat-run-context > .chat-context-heading + .chat-decision")).toHaveCount(1);
  await expect(page.locator("#chat-run-context .chat-blocker")).toHaveCount(0);
  await expect(page.locator(".chat-decision")).toContainText("Removing the legacy column closes the rollback path.");
  const option = page.locator('[data-chat-decision="staged"]');
  await expect(option).toContainText("Recommended");
  await expect(option).toContainText("Keeps rollback available.");
  if (!isMobile) await expect(option).toBeInViewport({ ratio: 1 });
  await expect(page.locator('[data-chat-decision="defer"]')).toContainText("keeps work blocked");
  await option.click();
  await expect(page.locator("#chat-send-status")).toContainText("Saved");
  expect(model.posts[0]).toMatchObject({ intent: "decision", option_id: "staged", generation: 3, waiting_transition_id: model.item.execution.waiting.transition_id });
  await expect(page.locator(".chat-diagnostics")).not.toHaveAttribute("open", "");
  await expect(page.locator("#chat-view")).not.toContainText("RAW_TOOL_NOISE_41");
  await page.locator("#chat-open-details").click();
  await expect(page.locator("#raw-execution-data")).toBeHidden();
  await page.locator(".raw-details summary").click();
  await expect(page.locator("#raw-execution-data")).toContainText("RAW_TOOL_NOISE_41");
  await page.locator("#open-chat-button").click();
  await expect(page.locator("#detail-drawer")).toHaveAttribute("aria-hidden", "true");
  await expect(page.locator("#chat-run-select")).toHaveValue(runId);
});

test("late context reads and sends cannot overwrite another context or its draft", async ({ page }) => {
  const model = await chatFixture(page);
  await selectRun(page);
  const readStarted = deferred();
  const readRelease = deferred();
  model.beforeGet = async (scope) => {
    if (scope === "run") {
      model.beforeGet = null;
      readStarted.resolve();
      await readRelease.promise;
    }
  };
  await page.locator("#refresh-button").click();
  await readStarted.promise;
  await page.locator("#chat-content").fill("Run guidance draft");
  await page.locator("#chat-scope").selectOption("workspace");
  await expect(page.locator("#chat-run-context")).toContainText("Work in this context");
  await page.locator("#chat-content").fill("Workspace draft");
  const lateRead = page.waitForResponse((response) => response.url().includes("/portal/api/chat?") && response.url().includes("scope=run"));
  readRelease.resolve();
  await lateRead;
  await expect(page.locator("#chat-scope")).toHaveValue("workspace");
  await expect(page.locator("#chat-content")).toHaveValue("Workspace draft");

  const postStarted = deferred();
  const postRelease = deferred();
  model.beforePost = async () => { postStarted.resolve(); await postRelease.promise; };
  await page.locator("#chat-send").click();
  await postStarted.promise;
  await page.locator("#chat-scope").selectOption("project");
  await page.locator("#chat-content").fill("Project draft must survive");
  const latePost = page.waitForResponse((response) => response.url().endsWith("/portal/api/chat/messages"));
  postRelease.resolve();
  await latePost;
  await expect(page.locator("#chat-content")).toHaveValue("Project draft must survive");
  await expect(page.locator("#chat-messages")).not.toContainText("Workspace draft");
  await page.locator("#chat-scope").selectOption("run");
  await expect(page.locator("#chat-content")).toHaveValue("Run guidance draft");
  await expect(page.locator("#chat-run-select")).toHaveValue(runId);
});

test("uncertain sends retry with the same action ID and refresh preserves editing, scroll, and safe text", async ({ page }) => {
  const history = Array.from({ length: 25 }, (_, index) => ({
    id: `history-${index}`, role: "assistant", intent: "status", content: `Recorded finding ${index}: compatibility remains intact.`,
    inserted_at: `2026-09-06T09:${String(index).padStart(2, "0")}:00Z`
  }));
  const model = await chatFixture(page, { history });
  allowBrowserError(page, /net::ERR_CONNECTION_FAILED|Failed to fetch|Failed to load resource/);
  model.nextFailure = true;
  const content = '<img src=x onerror="window.chatUnsafe=true"> Keep this draft.';
  await page.locator("#chat-content").fill(content);
  await page.locator("#chat-send").click();
  await expect(page.locator("#chat-send-status")).toContainText("Send not confirmed");
  await expect(page.locator("#chat-content")).toHaveValue(content);
  await page.locator("#chat-send").click();
  await expect(page.locator("#chat-send-status")).toContainText("Saved");
  expect(model.posts[0].action_id).toBe(model.posts[1].action_id);
  await expect(page.locator("#chat-messages")).toContainText(content);
  await expect(page.locator("#chat-messages img")).toHaveCount(0);
  expect(await page.evaluate(() => window.chatUnsafe)).toBeUndefined();

  await page.locator("#chat-content").fill("Keep editing during refresh");
  await page.locator("#chat-content").focus();
  const refreshed = page.waitForResponse((response) => response.url().includes("/portal/api/chat?"));
  await page.evaluate(() => {
    document.querySelector("#chat-content").setSelectionRange(5, 12);
    document.querySelector("#chat-scroll").scrollTop = 30;
    document.dispatchEvent(new Event("visibilitychange"));
  });
  await refreshed;
  await expect(page.locator("#chat-content")).toBeFocused();
  await expect(page.locator("#chat-content")).toHaveValue("Keep editing during refresh");
  expect(await page.locator("#chat-content").evaluate((field) => [field.selectionStart, field.selectionEnd])).toEqual([5, 12]);
  expect(await page.locator("#chat-scroll").evaluate((element) => element.scrollTop)).toBe(30);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
});

test("project-switch failures keep run context and saved messages do not supersede a later project switch", async ({ page }) => {
  const model = await chatFixture(page);
  const otherId = "10000000-0000-0000-0000-000000000042";
  model.workspace.projects.push({ ...model.project, id: otherId, key: "OTHER", name: "Other engineering", work_items: [] });
  await page.locator("#refresh-button").click();
  await expect(page.locator(`#project-switcher option[value="${otherId}"]`)).toHaveCount(1);
  await selectRun(page);
  await page.locator("#chat-content").fill("Keep this run question");
  model.failWorkspace = otherId;
  allowBrowserError(page, /503|Failed to load resource/);
  await page.locator("#project-switcher").selectOption(otherId);
  await expect(page.locator("#toast")).toContainText("Workspace unavailable");
  await expect(page.locator("#project-switcher")).toHaveValue(projectId);
  await expect(page.locator("#chat-scope")).toHaveValue("run");
  await expect(page.locator("#chat-content")).toHaveValue("Keep this run question");

  model.failWorkspace = null;
  const postStarted = deferred();
  const postRelease = deferred();
  const switchStarted = deferred();
  const switchRelease = deferred();
  model.beforePost = async () => { postStarted.resolve(); await postRelease.promise; };
  model.beforeWorkspace = async (selected) => {
    if (selected === otherId) { switchStarted.resolve(); await switchRelease.promise; }
  };
  await page.locator("#chat-send").click();
  await postStarted.promise;
  expect(model.posts[0]).toMatchObject({ scope: "run", run_id: runId, content: "Keep this run question" });
  await page.locator("#project-switcher").selectOption(otherId);
  await switchStarted.promise;
  postRelease.resolve();
  await expect(page.locator("#chat-form")).toHaveAttribute("aria-busy", "false");
  switchRelease.resolve();
  await expect(page.locator("#project-switcher")).toHaveValue(otherId);
  await expect(page.locator("#chat-scope")).toHaveValue("project");
  await expect(page.locator("#chat-project-label")).toHaveText("Other engineering");
});

test("workspace drafts restore their selected project's repository and CI choices", async ({ page }) => {
  const model = await chatFixture(page);
  const otherId = "10000000-0000-0000-0000-000000000042";
  model.workspace.projects.push({
    ...model.project, id: otherId, key: "OTHER", name: "Other engineering", work_items: [],
    resources: [
      { id: "repo-other", project_id: otherId, name: "Other repository", kind: "repository", status: "healthy", sync_status: "synced" },
      { id: "ci-other", project_id: otherId, name: "Other CI", kind: "ci", status: "healthy", sync_status: "synced" }
    ]
  });
  await page.locator("#refresh-button").click();
  await expect(page.locator(`#project-switcher option[value="${otherId}"]`)).toHaveCount(1);
  await page.locator("#chat-scope").selectOption("workspace");
  await page.locator("#chat-intent").selectOption("start_work");
  await page.locator("#chat-target-project").selectOption(otherId);
  await page.locator("#chat-start-fields summary").click();
  await page.locator("#chat-repository").selectOption("repo-other");
  await page.locator("#chat-ci").selectOption("ci-other");
  await page.locator("#chat-content").fill("A goal for another project");
  await page.locator("#chat-scope").selectOption("project");
  await page.locator("#chat-scope").selectOption("workspace");
  await expect(page.locator("#chat-target-project")).toHaveValue(otherId);
  await expect(page.locator("#chat-repository")).toHaveValue("repo-other");
  await expect(page.locator("#chat-ci")).toHaveValue("ci-other");
  await expect(page.locator("#chat-content")).toHaveValue("A goal for another project");
});

test("autonomous semantic updates appear in the conversation without raw events or lost control focus", async ({ page }) => {
  const model = await chatFixture(page);
  await selectRun(page);
  model.detail.timeline = [
    { source: "event", run_id: runId, generation: 3, recorded_at: "2026-09-06T12:00:00Z", data: { event_id: "progress-41", kind: "progress", payload: { message: "All readers now use the compatibility adapter." } } },
    { source: "event", run_id: runId, generation: 3, recorded_at: "2026-09-06T12:01:00Z", data: { event_id: "tool-41", kind: "tool_call", payload: { message: "RAW_TOOL_NOISE_41" } } }
  ];
  model.detail.outcome.summary = "The agent's adapter & rollback checks are ready.";
  const pause = page.locator('[data-chat-control="pause"]');
  await pause.focus();
  const refreshed = page.waitForResponse((response) => response.url().includes("/portal/api/chat?"));
  await page.evaluate(() => document.dispatchEvent(new Event("visibilitychange")));
  await refreshed;
  await expect(page.locator("#chat-messages")).toContainText("All readers now use the compatibility adapter.");
  await expect(page.locator("#chat-messages")).toContainText("Recorded execution evidence");
  await expect(page.locator("#chat-messages")).not.toContainText("RAW_TOOL_NOISE_41");
  await expect(pause).toBeFocused();
  expect(model.posts).toHaveLength(0);

  await page.locator(".chat-diagnostics summary").click();
  await pause.focus();
  await page.evaluate(() => {
    window.chatStableNodes = {
      message: document.querySelector("#chat-messages .chat-message"),
      pause: document.querySelector('[data-chat-control="pause"]'),
      diagnostics: document.querySelector(".chat-diagnostics"),
      option: document.querySelector("#chat-run-select option")
    };
  });
  const unchanged = page.waitForResponse((response) => response.url().includes("/portal/api/chat?"));
  await page.evaluate(() => document.dispatchEvent(new Event("visibilitychange")));
  await unchanged;
  expect(await page.evaluate(() => ({
    message: window.chatStableNodes.message === document.querySelector("#chat-messages .chat-message"),
    pause: window.chatStableNodes.pause === document.querySelector('[data-chat-control="pause"]'),
    diagnostics: window.chatStableNodes.diagnostics === document.querySelector(".chat-diagnostics"),
    option: window.chatStableNodes.option === document.querySelector("#chat-run-select option")
  }))).toEqual({ message: true, pause: true, diagnostics: true, option: true });
  await expect(page.locator(".chat-diagnostics")).toHaveAttribute("open", "");
  await expect(pause).toBeFocused();
});

test("refresh after a remote history gap keeps every message reachable and refreshes linked cached receipts", async ({ page }) => {
  const messageAt = (index) => ({
    id: `paged-${String(index).padStart(3, "0")}`,
    role: index % 2 ? "human" : "assistant", intent: "discuss",
    content: `Durable message ${index}`,
    inserted_at: new Date(Date.UTC(2026, 8, 6, 10, 0, index)).toISOString()
  });
  const history = Array.from({ length: 60 }, (_, index) => messageAt(index));
  history[5].command = { command_id: "old-guidance", kind: "guidance", state: "pending", acknowledgement_outcome: null };
  history[6].command = { command_id: "unrefreshed-guidance", kind: "guidance", state: "pending", acknowledgement_outcome: null };
  const model = await chatFixture(page, { history, pageSize: 50 });
  const messages = page.locator("#chat-messages [data-message-id]");
  await expect(messages).toHaveCount(50);
  await page.locator("#chat-load-older").click();
  await expect(messages).toHaveCount(60);
  await expect(page.locator("#chat-load-older")).toBeHidden();
  await expect(page.locator('[data-message-id="paged-005"]')).toContainText("awaiting agent acknowledgement");

  model.messages.push(...Array.from({ length: 71 }, (_, offset) => messageAt(60 + offset)));
  model.item.execution.latest_command = { ...history[5].command, state: "acknowledged", acknowledgement_outcome: "applied" };
  await page.locator("#refresh-button").click();
  await expect(messages).toHaveCount(110);
  await expect(page.locator("#chat-load-older")).toBeVisible();
  await expect(page.locator('[data-message-id="paged-005"]')).toContainText("Applied at a safe boundary");
  await expect(page.locator('[data-message-id="paged-006"]')).toContainText("acknowledgement not refreshed with this page");

  await page.locator("#chat-load-older").click();
  await expect(messages).toHaveCount(131);
  await page.locator("#chat-load-older").click();
  await expect(page.locator("#chat-load-older")).toBeHidden();
  const ids = await messages.evaluateAll((elements) => elements.map((element) => element.dataset.messageId).sort());
  expect(ids).toEqual(model.messages.map((message) => message.id).sort());
  expect(new Set(ids).size).toBe(131);
});

test("failed runs show bounded escaped error text in Chat and shared work state", async ({ page }) => {
  const model = await chatFixture(page);
  const failure = '<img src=x onerror="window.chatUnsafe=true"> Missing the required decision packet. ' + "Diagnostic detail. ".repeat(150);
  const visibleFailure = `${failure.slice(0, 2_000)}…`;
  Object.assign(model.item.execution, { state: "failed", can_guide: false, can_pause: false, can_cancel: false, can_retry: true });
  model.detail.outcome.summary = null;
  model.detail.outcome.failure = { message: { raw: "RAW_FAILURE_DIAGNOSTICS" }, summary: null, error: failure };
  model.detail.timeline = [
    { source: "transition", run_id: runId, generation: 3, recorded_at: "2026-09-06T12:00:00Z", data: { transition_id: "failed-41", state: "failed", payload: { error: failure } } }
  ];
  await selectRun(page);
  await expect(page.locator("#chat-run-context > .chat-context-heading + .chat-failure")).toHaveCount(1);
  await expect(page.locator("#chat-run-context .chat-failure p")).toHaveText(visibleFailure);
  await expect(page.locator("#chat-messages .chat-message-content")).toHaveText(visibleFailure);
  await expect(page.locator("#chat-run-context")).not.toContainText("The agent has not recorded a summary yet.");
  await expect(page.locator("#chat-view")).not.toContainText("RAW_FAILURE_DIAGNOSTICS");
  await expect(page.locator("#chat-view img")).toHaveCount(0);
  await expect(page.locator(".chat-diagnostics")).not.toHaveAttribute("open", "");
  expect(await page.evaluate(() => window.chatUnsafe)).toBeUndefined();
});
