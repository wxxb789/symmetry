// Shared real-API fixture helpers for the readable work view acceptance suite.
//
// Every fixture here is created through the authenticated Portal API with the
// session CSRF token; the read-only shell provides no mutation controls. No
// helper substitutes a synthetic payload for a response the browser exercises.
import fs from "node:fs/promises";
import { expect } from "./fixtures.mjs";

export const operatorToken = process.env.SYMMETRY_OPERATOR_TOKEN || "development-operator-token";

// One backstop for the shell's own session probe: the probe is a real read on a
// host these suites share with live daemons and their browsers, so the wait
// matches that load instead of failing the run before any product behaviour is
// observed. It is a maximum, never a verdict: every assertion after it decodes
// the running control plane.
const LOGIN_GUARD_MS = 30_000;

export function uniqueKey(prefix = "RW") {
  const suffix = `${Date.now().toString(36)}${Math.random().toString(36).slice(2, 5)}`
    .slice(-6)
    .toUpperCase();
  return `${prefix}${suffix}`.slice(0, 8);
}

export async function login(page, path = "/portal/attention") {
  await page.goto(`/portal/login?return_to=${encodeURIComponent(path)}`);
  await page.getByLabel("Operator token").fill(operatorToken);
  await page.getByRole("button", { name: "Open workspace" }).click();
  await expect(page.getByRole("button", { name: "Refresh", exact: true })).toBeVisible({ timeout: LOGIN_GUARD_MS });
  await expect(page.getByRole("combobox", { name: "Project", exact: true })).toBeVisible({ timeout: LOGIN_GUARD_MS });
}

export async function api(page, method, path, body) {
  return page.evaluate(
    async ({ method: requestMethod, path: requestPath, body: requestBody }) => {
      const csrf = document.querySelector('meta[name="csrf-token"]')?.content || "";
      const response = await fetch(requestPath, {
        method: requestMethod,
        credentials: "same-origin",
        headers: {
          Accept: "application/json",
          ...(requestBody === undefined ? {} : { "Content-Type": "application/json" }),
          ...(requestMethod === "GET" ? {} : { "x-csrf-token": csrf })
        },
        ...(requestBody === undefined ? {} : { body: JSON.stringify(requestBody) })
      });
      const text = await response.text();
      return { status: response.status, body: text ? JSON.parse(text) : {} };
    },
    { method, path, body }
  );
}

/** The deep links the readable work views and planning suites both address. */
export const projectUrl = (id, params = {}) => {
  const query = new URLSearchParams(params).toString();
  return `/portal/projects/${id}${query ? `?${query}` : ""}`;
};
export const workUrl = (id, params = {}) => {
  const query = new URLSearchParams(params).toString();
  return `/portal/work/${id}${query ? `?${query}` : ""}`;
};

// Detail is a modal side panel on desktop and a full page at narrow widths.
export const detailSurface = (page, isMobile, title) =>
  isMobile ? page.locator("#main-content") : page.getByRole("dialog", { name: title });

// The detail's own close controls, in the accessible names the frontend
// contract implements; a mobile detail returns through its back link.
export async function closeDetail(page, isMobile, { escape = false, keys } = {}) {
  if (isMobile) await page.getByRole("link", { name: /^Back to projects?$/ }).click();
  else if (escape && keys) await keys.press(page, "Escape");
  else if (escape) await page.keyboard.press("Escape");
  else await page.getByRole("button", { name: "Close work detail" }).click();
}

export async function createProject(page, { key, name, description = "Readable work views acceptance fixture", defaultAgentProfile, defaultWorkspace }) {
  // `defaultAgentProfile`/`defaultWorkspace` are additive: a case that must
  // reach a registered runtime names the same binding the daemon registered.
  const response = await api(page, "POST", "/portal/api/projects", {
    key,
    name,
    description,
    ...(defaultAgentProfile === undefined ? {} : { default_agent_profile: defaultAgentProfile }),
    ...(defaultWorkspace === undefined ? {} : { default_workspace: defaultWorkspace })
  });
  expect(response.status, JSON.stringify(response.body)).toBe(201);
  return { id: response.body.project.id, key, name };
}

export async function createWorkItem(page, projectId, item) {
  const response = await api(page, "POST", `/portal/api/projects/${projectId}/work-items`, item);
  expect(response.status, JSON.stringify(response.body)).toBe(201);
  return response.body.work_item;
}

export async function updateWorkItem(page, workItemId, attrs) {
  const response = await api(page, "PATCH", `/portal/api/work-items/${workItemId}`, attrs);
  expect(response.status, JSON.stringify(response.body)).toBe(200);
  return response.body.work_item;
}

export async function moveWorkItem(page, workItemId, { version, status, beforeId = null }) {
  const response = await api(page, "PATCH", `/portal/api/work-items/${workItemId}/move`, {
    version,
    status,
    before_id: beforeId
  });
  expect(response.status, JSON.stringify(response.body)).toBe(200);
  return response.body.work_item;
}

export async function attachResource(page, projectId, resource) {
  const response = await api(page, "POST", `/portal/api/projects/${projectId}/resources`, resource);
  expect(response.status, JSON.stringify(response.body)).toBe(201);
  return response.body.resource;
}

export async function workspace(page, projectId) {
  return api(page, "GET", `/portal/api/workspace?project_id=${projectId}`);
}

export async function projectItem(page, projectId, title) {
  const response = await workspace(page, projectId);
  expect(response.status).toBe(200);
  const project = response.body.projects.find((candidate) => candidate.id === projectId);
  expect(project, `project ${projectId} in workspace`).toBeTruthy();
  const item = project.work_items.find((candidate) => candidate.title === title);
  expect(item, `work item "${title}" in project`).toBeTruthy();
  return item;
}

/**
 * Creates a project with its resources and work items through the real API.
 * `name` and `description` are templates over the minted key, so each suite
 * keeps its own fixture identity while the mechanics live here.
 */
export async function seedProject(page, {
  prefix,
  name = (key) => `Readable work views ${key}`,
  description = () => "Readable work views acceptance fixture",
  items = [],
  resources = []
}) {
  const key = uniqueKey(prefix);
  const project = await createProject(page, { key, name: name(key), description: description(key) });
  const attached = [];
  for (const resource of resources) attached.push(await attachResource(page, project.id, resource));
  const created = new Map();
  for (const item of items) created.set(item.title, await createWorkItem(page, project.id, item));
  return { ...project, resources: attached, items: created };
}

/** The served order of one board column: `status` titles sorted by served position. */
export async function servedColumnTitles(page, projectId, status = "ready") {
  const response = await workspace(page, projectId);
  expect(response.status, JSON.stringify(response.body)).toBe(200);
  const project = response.body.projects.find((candidate) => candidate.id === projectId);
  expect(project, `project ${projectId} in workspace`).toBeTruthy();
  return project.work_items
    .filter((item) => item.status === status)
    .sort((left, right) => left.position - right.position)
    .map((item) => item.title);
}

// Records the exact bytes and work-item count the server serves for one project
// snapshot. The list read has no server-side pagination, so the largest observed
// snapshot is reported as a limitation instead of inventing a list endpoint.
export async function snapshotMetrics(page, projectId) {
  return page.evaluate(async (id) => {
    const response = await fetch(`/portal/api/workspace?project_id=${id}`, {
      credentials: "same-origin",
      headers: { Accept: "application/json" }
    });
    // Read the body exactly once and parse those bytes: a Response body can
    // only be consumed once, and the byte count must come from the same read
    // that produced the parsed snapshot.
    const bytes = new Uint8Array(await response.arrayBuffer());
    const body = JSON.parse(new TextDecoder("utf-8").decode(bytes));
    const project = body.projects.find((candidate) => candidate.id === id);
    return { status: response.status, bytes: bytes.byteLength, workItemCount: project.work_items.length };
  }, projectId);
}

/**
 * Captures a page for owner acceptance. `options.fullPage: false` frames the
 * viewport instead: a full-page capture resizes the emulated viewport, which
 * disturbs an open nested modal, so a case that captures an open editor uses
 * the viewport framing.
 */
export async function capture(page, testInfo, name, observations, options = {}) {
  await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  const image = testInfo.outputPath(`${name}.png`);
  await page.screenshot({ path: image, fullPage: options.fullPage ?? true });
  await testInfo.attach(name, { path: image, contentType: "image/png" });
  await fs.writeFile(testInfo.outputPath(`${name}.json`), JSON.stringify({
    url: page.url(), viewport: page.viewportSize(), observations
  }, null, 2));
}

// Focus-moving key presses recorded for the keyboard walk evidence.
export function recordKeys() {
  const actions = [];
  const press = async (page, key) => {
    actions.push(key);
    await page.keyboard.press(key);
  };
  return { actions, press };
}

/**
 * The shell's own statement of the invalidation client's refresh record: which
 * connection serves the reads, which of the client's own passes last refreshed
 * every read, when that pass issued its reads and when those reads finished.
 *
 * Every value is the element's own attribute, written where the work happens —
 * the status by the client's socket hooks, the pass by the code that issues the
 * reads — so a case that waits on it waits on product state, never on a hook
 * beside the product. `null` members are the states the element states as empty:
 * no pass yet, or a pass whose reads are still in flight.
 */
export function readsStatus(page) {
  return page.evaluate(() => {
    const status = document.querySelector(".reads-status");
    if (status === null) return null;
    const number = (name) => {
      const raw = status.getAttribute(name);
      return raw === null || raw === "" ? null : Number(raw);
    };
    return {
      connection: status.getAttribute("data-connection"),
      source: status.getAttribute("data-refresh-source") || null,
      issuedAt: number("data-refresh-issued-at"),
      settledAt: number("data-refresh-settled-at")
    };
  });
}

/**
 * Routes the operator socket the way the cutover and readable-work suites need:
 * while `mode` is "live" messages are forwarded to the real server in both
 * directions; while it is "blocked" every connection attempt is closed, so the
 * browser observes a real disconnection and reconnects through this same
 * handler. `attempts` counts every connection the browser opened; `established`
 * counts the ones this handler let reach the real server, so a case can state
 * that a convergence happened while no socket carried a single message; `pushed`
 * and `pushedAt` count and time the server-to-browser frames that carried an
 * invalidation, so a case can attribute a read to the push path instead of
 * assuming it.
 *
 * While `hold` is set, the page's WebSocket is opened by Playwright as a mock —
 * so the browser sees an open transport, reports `connected` and stops its
 * polling fallback — but this handler makes **no upstream connection at all**,
 * keeps the browser's own bytes (`withheld` counts them) and connects to the
 * server only when the case calls `release()`, replaying the withheld bytes in
 * order. That is the window the product's join epoch exists for, held open
 * deterministically: the server has run no join clause for this browser, so a
 * frame published now is addressed to no subscriber at all, and nothing this
 * handler does can forward it. The hold is released by the case, never by
 * elapsed time, and the withheld count is the case's own evidence that the
 * browser's request never reached the server.
 */
export async function controlSocket(page) {
  const state = {
    mode: "live",
    hold: false,
    attempts: 0,
    established: 0,
    pushed: 0,
    pushedAt: null,
    withheld: 0,
    held: [],
    release: () => {}
  };
  await page.routeWebSocket(/\/portal\/socket\//, (ws) => {
    state.attempts += 1;
    if (state.mode === "blocked") {
      ws.close();
      return;
    }
    let upstream = null;
    const connect = () => {
      if (upstream !== null) return upstream;
      upstream = ws.connectToServer();
      state.established += 1;
      upstream.onMessage((message) => {
        if (String(message).includes('"invalidation"')) {
          state.pushed += 1;
          if (state.pushedAt === null) state.pushedAt = Date.now();
        }
        ws.send(message);
      });
      return upstream;
    };
    ws.onMessage((message) => {
      if (state.hold) {
        state.withheld += 1;
        // The browser's bytes are replayed in order on release, exactly as sent:
        // the harness never parses them, so it cannot hold the wrong one (a
        // Phoenix frame is a JSON array on the wire, not an object). The cap keeps
        // a wedged hold bounded; a replayed request the client has since abandoned
        // is answered by the server and ignored by the client.
        state.held.push(message);
        if (state.held.length > 16) state.held.shift();
        return;
      }
      connect().send(message);
    });
    state.release = () => {
      state.hold = false;
      const held = state.held;
      state.held = [];
      const server = connect();
      for (const message of held) server.send(message);
    };
    if (!state.hold) connect();
  });
  await page.route("**/portal/api/session*", async (route) => {
    const response = await route.fetch();
    const body = await response.json();
    // The run reads the bootstrap directly: record the served token so the
    // scenario can state that a real token was minted, never a substituted one.
    state.socketTokenMinted = typeof body.socket_token === "string" && body.socket_token.length > 0;
    await route.fulfill({ response, json: body });
  });
  return state;
}
