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
