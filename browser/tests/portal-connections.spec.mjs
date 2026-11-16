// Connection-management acceptance on the fixed design's /portal/connections
// surface. Every flow below drives the real Portal API, the real PostgreSQL
// state and the real GitHub/Azure DevOps provider modules through a browser.
//
// Provider boundary: the control process under test is started with
// scripts/ci/portal-connection-fixture.exs, which replaces each provider's
// external transport (HTTP client and local CLI session) with canned provider
// documents. No provider host is contacted. The fixture CLI tokens are canaries:
// each test asserts that no canary reaches a recorded same-origin response, the
// DOM, web storage or the console.
import fs from "node:fs/promises";
import { allowBrowserError, expect as baseExpect, test } from "./fixtures.mjs";

// The acceptance control plane runs on a loaded developer host: measured reads
// on this surface are sub-second, but a page that re-reads behind several
// mutations can take seconds, so this file's assertions get a wider budget than
// the shared 10s default. Server-side latency is measured separately.
const expect = baseExpect.configure({ timeout: 30_000 });
test.describe.configure({ timeout: 120_000 });

const operatorToken = process.env.SYMMETRY_OPERATOR_TOKEN || "development-operator-token";
const portalOrigin = new URL(process.env.SYMMETRY_PORTAL_URL || "http://127.0.0.1:4000").origin;
const canaries = ["gho_fixture_connection_token_canary", "fixture-ado-access-token-canary"];
const secretPattern = new RegExp([...canaries, "Bearer ", "ghp_", "github_pat_"].join("|"));
const suffix = `${Date.now().toString(36)}${Math.round(Math.random() * 1e6).toString(36)}`;
let projectSequence = 0;

async function login(page) {
  await page.goto("/portal/login?return_to=%2Fportal%2Fconnections");
  await page.getByLabel("Operator token").fill(operatorToken);
  await page.getByRole("button", { name: "Open workspace" }).click();
  await expect(page.getByRole("heading", { level: 1, name: "Connections" })).toBeVisible();
}

async function api(page, path, { method = "POST", body } = {}) {
  return page.evaluate(async ({ path, method, body }) => {
    const csrf = document.querySelector('meta[name="csrf-token"]')?.content;
    const headers = method === "GET" ? { accept: "application/json" } : { "content-type": "application/json", "x-csrf-token": csrf };
    const response = await fetch(path, {
      method,
      credentials: "same-origin",
      headers,
      ...(body === undefined ? {} : { body: JSON.stringify(body) })
    });
    const text = await response.text();
    return { status: response.status, contentType: response.headers.get("content-type"), text, body: text ? JSON.parse(text) : null };
  }, { path, method, body });
}

async function createProject(page) {
  // Project keys are ^[A-Z][A-Z0-9]{1,7}$; the sequence keeps runs distinct.
  const key = `C${projectSequence += 1}${suffix}`.toUpperCase().slice(0, 8);
  const response = await api(page, "/portal/api/projects", { body: { key, name: `Connection acceptance ${key}`, description: "Real Phoenix connection fixture" } });
  expect(response.status).toBe(201);
  return response.body.project.id;
}

function recordApiBodies(page) {
  const bodies = [];
  const requests = [];
  page.on("request", (request) => {
    const url = new URL(request.url());
    if (url.origin !== portalOrigin) return;
    requests.push({ method: request.method(), path: url.pathname, headers: { ...request.headers() }, body: request.postData() ?? "" });
  });
  page.on("response", (response) => {
    const url = new URL(response.url());
    if (url.origin !== portalOrigin) return;
    bodies.push(response.text().then((text) => ({ path: url.pathname, status: response.status(), text })).catch(() => ({ path: url.pathname, status: response.status(), text: "unreadable" })));
  });
  return { bodies, requests };
}

async function assertNoCredentials(page, recorded) {
  const browserState = await page.evaluate(() => ({
    html: document.documentElement.outerHTML,
    localStorage: JSON.stringify(window.localStorage),
    sessionStorage: JSON.stringify(window.sessionStorage),
    cookies: document.cookie
  }));
  for (const [where, text] of Object.entries(browserState)) {
    expect(text, `${where} must not contain a provider credential`).not.toMatch(secretPattern);
  }
  for (const entry of await Promise.all(recorded.bodies)) {
    expect(entry.text, `response ${entry.path} must not contain a provider credential`).not.toMatch(secretPattern);
  }
  for (const request of recorded.requests) {
    expect(JSON.stringify(request.headers), `request headers for ${request.path} must not contain a provider credential`).not.toMatch(secretPattern);
    expect(request.body, `request body for ${request.path} must not contain a provider credential`).not.toMatch(secretPattern);
  }
}

async function capture(page, testInfo, name, observations) {
  await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  const image = testInfo.outputPath(`${name}.png`);
  await page.screenshot({ path: image, fullPage: true });
  await testInfo.attach(name, { path: image, contentType: "image/png" });
  await fs.writeFile(testInfo.outputPath(`${name}.json`), JSON.stringify({
    url: page.url(), viewport: page.viewportSize(), observations
  }, null, 2));
}

async function createThroughConsole(page, { provider, name, account, capabilities }) {
  await page.getByRole("button", { name: "New connection", exact: true }).click();
  const dialog = page.getByRole("dialog", { name: "New connection" });
  await expect(dialog).toBeVisible();
  await expect(dialog.getByLabel("Credential", { exact: false })).toHaveCount(0);
  if (provider) await dialog.getByLabel("Provider").selectOption(provider);
  await dialog.getByLabel("Name").fill(name);
  await dialog.getByLabel("Account or organization").fill(account);
  for (const capability of ["Repositories", "Work items", "Changes", "CI"]) {
    const box = dialog.getByLabel(capability, { exact: true });
    if (await box.isChecked()) await box.uncheck();
  }
  for (const capability of capabilities) await dialog.getByLabel(capability, { exact: true }).check();
  await dialog.getByRole("button", { name: "Connect", exact: true }).click();
  await expect(dialog).toBeHidden();
}

test("a connection is created, inspected, tested and removed through the real API", async ({ page }, testInfo) => {
  const recorded = recordApiBodies(page);
  await login(page);
  const name = `GitHub fixture ${suffix}`;

  await createThroughConsole(page, { provider: "github", name, account: "acme-fixture", capabilities: ["Work items", "CI"] });
  const row = page.getByRole("button", { name: new RegExp(name) });
  await expect(row).toBeVisible();
  await expect(row).toContainText("acme-fixture · GitHub CLI · HTTPS");
  await expect(row).toContainText("Never checked");
  await expect(row).toContainText("0 bound · 0 needing attention");

  await row.click();
  const detail = page.getByRole("complementary", { name: "Connection detail" });
  await expect(detail).toContainText("GitHub");
  await expect(detail).toContainText("GitHub CLI · HTTPS");
  await capture(page, testInfo, "created", "created connection rendered from the real read projection");

  await detail.getByRole("button", { name: "Test connection", exact: true }).click();
  await expect(detail.getByText("Healthy", { exact: true })).toBeVisible();
  await expect(detail).toContainText(/Last checked\s*\d{4}-\d{2}-\d{2}T/);
  await capture(page, testInfo, "checked", "healthy provider check from controlled fixture transport");

  await detail.getByRole("button", { name: "Edit capabilities", exact: true }).click();
  const settings = page.getByRole("dialog", { name: "Connection settings" });
  await settings.getByLabel("CI", { exact: true }).uncheck();
  await settings.getByRole("button", { name: "Save", exact: true }).click();
  await expect(settings).toBeHidden();
  await expect(page.getByRole("status").first()).toContainText("Connection capabilities updated");
  await expect(page.getByRole("button", { name: new RegExp(name) })).not.toContainText("CI");
  await expect(page.getByRole("button", { name: new RegExp(name) })).toContainText("Work items");
  await expect(detail).toContainText("Capabilities: Work items");

  await page.getByRole("button", { name: new RegExp(name) }).click();
  await page.getByRole("complementary", { name: "Connection detail" }).getByRole("button", { name: "Remove", exact: true }).click();
  await page.getByRole("dialog", { name: new RegExp(`Remove ${name}`) }).getByRole("button", { name: "Remove connection", exact: true }).click();
  await expect(page.getByRole("button", { name: new RegExp(name) })).toHaveCount(0);
  const list = await api(page, "/portal/api/connections", { method: "GET" });
  expect(list.status).toBe(200);
  expect(list.body.connections.filter((connection) => connection.name === name)).toEqual([]);
  expect(list.body.providers.map((provider) => provider.id).sort()).toEqual(["azure_devops", "github"]);

  await assertNoCredentials(page, recorded);
  await capture(page, testInfo, "removed", "removal reconciled against the real connection read");
});

test("an expired provider session, a repeated submission and a refused submission stay actionable", async ({ page }, testInfo) => {
  allowBrowserError(page, /status of (401|422|502)/);
  const recorded = recordApiBodies(page);
  await login(page);
  const name = `Expired fixture ${suffix}`;

  await createThroughConsole(page, { provider: "github", name, account: "expired-fixture", capabilities: ["CI"] });
  const row = page.getByRole("button", { name: new RegExp(name) });
  await row.click();
  const detail = page.getByRole("complementary", { name: "Connection detail" });
  await detail.getByRole("button", { name: "Test connection", exact: true }).click();
  await expect(page.getByRole("status").first()).toContainText("external provider authentication failed (provider_unauthorized)");
  await expect(page.getByRole("status").first()).toContainText("Reauthenticate the provider session");
  await expect(detail.getByText("Degraded", { exact: true })).toBeVisible();
  await expect(detail).toContainText("Provider authentication failed");
  await capture(page, testInfo, "expired-session", "expired provider session rendered as an actionable, redacted reason");

  // The same normalized submission is the same intent: the server replays the
  // applied connection instead of creating a second row.
  const repeated = await api(page, "/portal/api/connections", { body: { provider: "github", name, account_ref: "expired-fixture", capabilities: ["ci"] } });
  expect(repeated.status).toBe(200);
  expect(repeated.body.disposition).toBe("replayed");
  const conflicting = await api(page, "/portal/api/connections", { body: { provider: "github", name, account_ref: "expired-fixture", capabilities: ["repositories"] } });
  expect(conflicting.status).toBe(422);
  expect(conflicting.body.error.fields.provider.join(" ")).toContain("already has a connection with this name");
  await page.getByRole("button", { name: "Refresh", exact: true }).click();
  await expect(page.getByRole("button", { name: new RegExp(name) })).toHaveCount(1);
  const list = await api(page, "/portal/api/connections", { method: "GET" });
  expect(list.body.connections.filter((connection) => connection.name === name)).toHaveLength(1);

  // The console refuses the conflicting submission with the server's own reason
  // and field error instead of inventing a client-side rule.
  await page.getByRole("button", { name: "New connection", exact: true }).click();
  const dialog = page.getByRole("dialog", { name: "New connection" });
  await dialog.getByLabel("Name").fill(name);
  await dialog.getByLabel("Account or organization").fill("expired-fixture");
  await dialog.getByRole("button", { name: "Connect", exact: true }).click();
  await expect(dialog.getByText("already has a connection with this name")).toBeVisible();
  await expect(dialog.getByText(/validation_failed/).first()).toBeVisible();
  await capture(page, testInfo, "refused-submission", "server field error rendered on the conflicting submission");
  // A refused submission adds field and refusal notices. The dialog scrolls
  // internally, and once the browser brings the action row into view it must sit
  // fully inside the viewport: a fixed-position dialog whose own box runs past the
  // viewport bottom cannot satisfy that, which is what made Cancel unclickable.
  const cancel = dialog.getByRole("button", { name: "Cancel", exact: true });
  await cancel.scrollIntoViewIfNeeded();
  await expect(cancel).toBeInViewport({ ratio: 1 });
  await cancel.click();

  await assertNoCredentials(page, recorded);
});

test("stale updates and concurrent deletion reconcile to the server's current state", async ({ page }, testInfo) => {
  allowBrowserError(page, /status of (404|409)/);
  const recorded = recordApiBodies(page);
  await login(page);
  const name = `Stale fixture ${suffix}`;

  await createThroughConsole(page, { provider: "github", name, account: "acme-fixture", capabilities: ["Repositories"] });
  const listed = await api(page, "/portal/api/connections", { method: "GET" });
  const created = listed.body.connections.find((connection) => connection.name === name);
  expect(created).toBeTruthy();

  // Another operator changes the connection while this page is open.
  const renamed = `${name} renamed`;
  const external = await api(page, `/portal/api/connections/${created.id}`, { method: "PATCH", body: { version: created.version, name: renamed } });
  expect(external.status).toBe(200);

  await page.getByRole("button", { name: new RegExp(name) }).click();
  await page.getByRole("complementary", { name: "Connection detail" }).getByRole("button", { name: "Edit capabilities", exact: true }).click();
  const settings = page.getByRole("dialog", { name: "Connection settings" });
  await settings.getByLabel("Work items", { exact: true }).check();
  await settings.getByRole("button", { name: "Save", exact: true }).click();
  await expect(settings.getByRole("alert")).toContainText("(stale)");
  await expect(settings.getByLabel("Name")).toBeDisabled();
  await capture(page, testInfo, "stale-update", "server refused a stale write with its own code");
  await settings.getByRole("button", { name: "Cancel", exact: true }).click();
  await expect(page.getByRole("button", { name: new RegExp(renamed) })).toBeVisible();

  // A concurrent removal wins: the second delete is refused and the console
  // reconciles to the server's state instead of reporting success.
  const current = await api(page, "/portal/api/connections", { method: "GET" });
  const target = current.body.connections.find((connection) => connection.name === renamed);
  const removedElsewhere = await api(page, `/portal/api/connections/${target.id}?version=${target.version}`, { method: "DELETE" });
  expect(removedElsewhere.status).toBe(200);
  await page.getByRole("button", { name: new RegExp(renamed) }).click();
  await page.getByRole("complementary", { name: "Connection detail" }).getByRole("button", { name: "Remove", exact: true }).click();
  await page.getByRole("dialog", { name: new RegExp(`Remove ${renamed}`) }).getByRole("button", { name: "Remove connection", exact: true }).click();
  await expect(page.getByRole("status").first()).toContainText("(not_found)");
  await expect(page.getByRole("status").first()).toContainText("The server's current state is shown instead.");
  await expect(page.getByRole("button", { name: new RegExp(renamed) })).toHaveCount(0);
  await capture(page, testInfo, "concurrent-delete", "concurrent removal refused and reconciled");

  await assertNoCredentials(page, recorded);
});

test("a capability update visibly resets dependent resource health and sync state", async ({ page }, testInfo) => {
  const recorded = recordApiBodies(page);
  await login(page);
  const projectId = await createProject(page);
  const name = `Bound fixture ${suffix}`;

  await createThroughConsole(page, { provider: "github", name, account: "acme-fixture", capabilities: ["Repositories", "Changes", "CI"] });
  const listed = await api(page, "/portal/api/connections", { method: "GET" });
  const connection = listed.body.connections.find((candidate) => candidate.name === name);
  // A provider resource must be owned by the connection's account: the GitHub
  // module refuses a reference whose owner is not the connection's account_ref.
  const attached = await api(page, `/portal/api/projects/${projectId}/resources`, { body: {
    kind: "repository", name: "symmetry", external_ref: "acme-fixture/symmetry", connection_id: connection.id
  } });
  expect(attached.status).toBe(201);
  const resourceId = attached.body.resource.id;

  const synced = await api(page, `/portal/api/resources/${resourceId}/sync`, {});
  expect(synced.status).toBe(200);
  expect(synced.body.resource.status).toBe("healthy");
  expect(synced.body.resource.sync_status).toBe("synced");

  // The binding and the sync above went through the API directly, so the page
  // re-reads the authoritative projection before the detail is inspected.
  await page.getByRole("button", { name: "Refresh", exact: true }).click();
  await page.getByRole("button", { name: new RegExp(name) }).click();
  const detail = page.getByRole("complementary", { name: "Connection detail" });
  await expect(detail).toContainText("Bound resources 1");
  await expect(detail).toContainText("symmetry");
  await expect(detail.getByText("Synced", { exact: true })).toBeVisible();
  await capture(page, testInfo, "bound-synced", "bound resource synchronized through the real provider fixture boundary");

  await detail.getByRole("button", { name: "Edit capabilities", exact: true }).click();
  const settings = page.getByRole("dialog", { name: "Connection settings" });
  await settings.getByLabel("CI", { exact: true }).uncheck();
  await settings.getByRole("button", { name: "Save", exact: true }).click();
  await expect(settings).toBeHidden();
  await expect(page.getByRole("status").first()).toContainText("Bound resource health and sync state was reset");

  const after = await api(page, "/portal/api/connections", { method: "GET" });
  const reset = after.body.connections.find((candidate) => candidate.name === name);
  expect(reset.resources).toHaveLength(1);
  expect(reset.resources[0].status).toBe("unknown");
  expect(reset.resources[0].sync_status).toBe("unknown");
  expect(reset.resources[0].last_synced_at).toBeNull();

  await page.getByRole("button", { name: new RegExp(name) }).click();
  const refreshed = page.getByRole("complementary", { name: "Connection detail" });
  await expect(refreshed.getByText("Synced", { exact: true })).toHaveCount(0);
  // The capability change resets the connection's own check state and the bound
  // resource's health and sync state in the same transaction: three "Unknown".
  await expect(refreshed.getByText("Unknown", { exact: true })).toHaveCount(3);
  await capture(page, testInfo, "capability-reset", "capability change visibly reset bound resource health and sync state");

  await assertNoCredentials(page, recorded);
});

test("Azure DevOps connection management runs on the new route", async ({ page }, testInfo) => {
  allowBrowserError(page, /status of (401|502)/);
  const recorded = recordApiBodies(page);
  await login(page);
  const name = `Azure fixture ${suffix}`;

  await createThroughConsole(page, { provider: "azure_devops", name, account: "acme-fixture", capabilities: ["Work items"] });
  const row = page.getByRole("button", { name: new RegExp(name) });
  await expect(row).toContainText("Azure DevOps");
  await expect(row).toContainText("Microsoft Entra ID");
  await row.click();
  const detail = page.getByRole("complementary", { name: "Connection detail" });
  await expect(detail).toContainText("Microsoft Entra ID");
  await detail.getByRole("button", { name: "Test connection", exact: true }).click();
  await expect(detail.getByText("Healthy", { exact: true })).toBeVisible();

  // Update and remove run through the same console for the second provider.
  await detail.getByRole("button", { name: "Edit capabilities", exact: true }).click();
  const settings = page.getByRole("dialog", { name: "Connection settings" });
  await settings.getByLabel("Repositories", { exact: true }).check();
  await settings.getByRole("button", { name: "Save", exact: true }).click();
  await expect(settings).toBeHidden();
  await expect(page.getByRole("status").first()).toContainText("Connection capabilities updated");
  await expect(page.getByRole("button", { name: new RegExp(name) })).toContainText("Repositories");

  await page.getByRole("button", { name: new RegExp(name) }).click();
  await page.getByRole("complementary", { name: "Connection detail" }).getByRole("button", { name: "Remove", exact: true }).click();
  await page.getByRole("dialog", { name: new RegExp(`Remove ${name}`) }).getByRole("button", { name: "Remove connection", exact: true }).click();
  await expect(page.getByRole("button", { name: new RegExp(name) })).toHaveCount(0);
  const removed = await api(page, "/portal/api/connections", { method: "GET" });
  expect(removed.status).toBe(200);
  expect(removed.body.connections.filter((connection) => connection.name === name)).toEqual([]);

  const expiredName = `Azure expired ${suffix}`;
  await createThroughConsole(page, { provider: "azure_devops", name: expiredName, account: "expired-fixture", capabilities: ["Work items"] });
  await page.getByRole("button", { name: new RegExp(expiredName) }).click();
  const expiredDetail = page.getByRole("complementary", { name: "Connection detail" });
  await expiredDetail.getByRole("button", { name: "Test connection", exact: true }).click();
  await expect(expiredDetail.getByText("Degraded", { exact: true })).toBeVisible();
  await expect(expiredDetail).toContainText("Provider authentication failed");
  await capture(page, testInfo, "azure", "Azure DevOps connection created, tested and refused on the new route");

  await assertNoCredentials(page, recorded);
});
