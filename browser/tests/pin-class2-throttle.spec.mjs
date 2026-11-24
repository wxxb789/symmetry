// Connection-management acceptance on the fixed design's /portal/connections
// surface. Every flow below drives the real Portal API, the real PostgreSQL
// state and the real GitHub/Azure DevOps provider modules through a browser.
//
// Provider boundary: the control process under test is started with
// scripts/ci/portal-connection-fixture.exs, which replaces each provider's
// external transport (HTTP client and local CLI session) with canned provider
// documents. No provider host is contacted. The fixture CLI tokens are canaries:
// each test asserts that no canary reaches a recorded same-origin response
// (body and every header, including each Set-Cookie), the DOM, web storage, the
// cookie jar (document.cookie cannot see HttpOnly cookies; the jar is sampled
// at every same-origin response and once more at the end), request headers and
// bodies, or the console.
//
// Fixture identity: the harness starts the fixture with a per-run identity, an
// identity file and a per-run operator token. Every test proves the target is
// that fixture (identity file matches the run and the portal port) and logs in
// with the per-run token before any provider check.
//
// Collection contract (browser/scripts/connection-spec-env.test.mjs drives it):
// without the fixture identity the file cannot prove its target is the fixture,
// so it skips at collection instead of driving a normal Control instance that
// would use its real gh/az sessions — the default target and the README's
// documented `SYMMETRY_PORTAL_URL` / `SYMMETRY_OPERATOR_TOKEN` override are
// normal custom targets, and the repository-wide `pnpm test` commands still
// collect the file. A run that carries part of the fixture identity, however, is
// trying to be the fixture run: an incomplete set is a half-configured harness
// and fails at collection instead of running against an unproven target. The
// harness itself treats a skip as a failed acceptance run.
//
// Project resources, synchronization, assignment and Azure DevOps flows: the
// flows after the five connection flows drive the same real API and database
// with the fixture's resource and synchronization documents (GitHub
// repository/issues/actions documents, Azure DevOps repository/work-item/build
// documents). The fixture is steered per provider through a JSON state file
// named by SYMMETRY_FIXTURE_STATE_FILE, which this spec writes before the
// operations that need an injected HTTP 500, an ambiguous transport failure, a
// held read (the deterministic in-flight window a project switch crosses), a
// changed repository document, or a shrunken issues list. The classic resource
// checks these flows replace are recorded in browser/README.md;
// browser/tests/portal-classic-resources.spec.mjs is retired.
import fs from "node:fs/promises";
import {
  allowBrowserError,
  assertNoBrowserErrors,
  expect as baseExpect,
  monitorBrowserErrors,
  test
} from "./fixtures.mjs";
import { api, attachResource, capture, createProject, deviceContextOptions, login, uniqueKey } from "./readable-fixtures.mjs";

const fixtureVariables = [
  "SYMMETRY_OPERATOR_TOKEN",
  "SYMMETRY_FIXTURE_IDENTITY",
  "SYMMETRY_FIXTURE_IDENTITY_FILE",
  "SYMMETRY_PORTAL_URL",
  "SYMMETRY_FIXTURE_STATE_FILE",
];
const fixtureIdentityVariables = ["SYMMETRY_FIXTURE_IDENTITY", "SYMMETRY_FIXTURE_IDENTITY_FILE"];
const isFixtureRun = fixtureIdentityVariables.some((name) => process.env[name]);
const missingVariables = fixtureVariables.filter((name) => !process.env[name]);
if (isFixtureRun && missingVariables.length > 0) {
  throw new Error(
    `${missingVariables.join(", ")} must be set together: run browser/scripts/run-connection-fixture-tests.ps1, ` +
      "which starts the fixture control plane and publishes the run identity",
  );
}
test.skip(!isFixtureRun, "run browser/scripts/run-connection-fixture-tests.ps1 to publish the fixture identity");
// The placeholders exist only for the skipped collection of a non-fixture run;
// a run with the harness always carries all five values.
const fixtureIdentity = process.env.SYMMETRY_FIXTURE_IDENTITY ?? "";
const fixtureIdentityFile = process.env.SYMMETRY_FIXTURE_IDENTITY_FILE ?? "";
const fixtureStateFile = process.env.SYMMETRY_FIXTURE_STATE_FILE ?? "";
// Only a fixture run consumes the target; a skipped collection keeps the
// loopback placeholder, so a custom target string is never parsed here.
const portalUrl = new URL((isFixtureRun && process.env.SYMMETRY_PORTAL_URL) || "http://127.0.0.1:4021");
if (!["127.0.0.1", "localhost", "::1"].includes(portalUrl.hostname)) {
  throw new Error(`SYMMETRY_PORTAL_URL must be loopback for the fixture run, got ${portalUrl.origin}`);
}
const portalOrigin = portalUrl.origin;

// The acceptance control plane runs on a loaded developer host: measured reads
// on this surface are sub-second, but a page that re-reads behind several
// mutations can take seconds, so this file pins both budgets explicitly: the
// 30s assertion timeout that playwright.config.mjs already sets as the shared
// default, and a 120s per-test budget double the shared 60s test timeout.
// Server-side latency is measured separately.
const expect = baseExpect.configure({ timeout: 30_000 });
test.describe.configure({ timeout: 120_000 });

const canaries = ["gho_fixture_connection_token_canary", "fixture-ado-access-token-canary"];
const secretPattern = new RegExp([...canaries, "Bearer ", "ghp_", "github_pat_"].join("|"));
const suffix = `${Date.now().toString(36)}${Math.round(Math.random() * 1e6).toString(36)}`;
// Playwright 1.65.1's own words for the two ways a recorded body read is lost to
// the scan while the request itself never fails: the document navigated away, or
// the session was torn down. No other unreadable body is tolerated.
const navigatedAway = "Response body is not available for a response that was navigated away from. Read response.body() before triggering any navigation.";
const sessionClosed = "Target page, context or browser has been closed";

// The identity file is written by the fixture process started for this run, and
// it names the port that process serves; the target must be that process before
// any provider check. A normal Control instance cannot satisfy this.
async function assertFixtureTarget() {
  const raw = await fs.readFile(fixtureIdentityFile, "utf8").catch((error) => {
    throw new Error(`fixture identity file ${fixtureIdentityFile} is unreadable: ${error.message}`);
  });
  const identity = JSON.parse(raw);
  expect(identity.identity, "fixture identity file must carry this run's identity").toBe(fixtureIdentity);
  expect(String(identity.port), "fixture identity file must match the portal target port").toBe(portalUrl.port);
}

const connectionsLogin = async (page) => {
  await assertFixtureTarget();
  await login(page, "/portal/connections", { assert: [page.getByRole("heading", { level: 1, name: "Connections" })] });
};

const connectionProject = async (page) => {
  const key = uniqueKey("C");
  return (await createProject(page, { key, name: `Connection acceptance ${key}`, description: "Real Phoenix connection fixture" })).id;
};

// ── Fixture steering, projects, resources and work items ────────────────────

// The fixture reads this document on every provider request; references are the
// resources' external_ref strings and each provider has its own section. Every
// write is a whole document swapped in with rename, so a read never sees half
// of one. A hold blocks the provider read until the reference leaves `hold`
// (bounded by the fixture), which is the only way this suite can keep a real
// sync in flight across a project switch without intercepting the browser.
let fixtureStateWrites = 0;

async function writeFixtureState(document) {
  const temporary = `${fixtureStateFile}.${process.pid}.${(fixtureStateWrites += 1)}.tmp`;
  await fs.writeFile(temporary, JSON.stringify(document));
  // The fixture re-reads this file on every provider request (and every 100 ms
  // while a read is held), so on Windows a rename can briefly hit an open
  // handle. Retry the same atomic rename instead of weakening it to an
  // in-place write, which a concurrent read could observe half-written.
  for (let attempt = 1; ; attempt += 1) {
    try {
      await fs.rename(temporary, fixtureStateFile);
      return;
    } catch (error) {
      if (!["EPERM", "EBUSY", "EACCES"].includes(error?.code) || attempt >= 20) throw error;
      await new Promise((resolve) => setTimeout(resolve, 25));
    }
  }
}

const resetFixtureState = () => writeFixtureState({});

async function createConnectionThroughApi(page, { provider = "github", name, account, capabilities }) {
  const response = await api(page, "POST", "/portal/api/connections", { provider, name, account_ref: account, capabilities });
  expect(response.status, JSON.stringify(response.body)).toBe(201);
  return response.body.connection.id;
}

async function readWorkspace(page, projectId) {
  const response = await api(page, "GET", `/portal/api/workspace?project_id=${encodeURIComponent(projectId)}`);
  expect(response.status, JSON.stringify(response.body)).toBe(200);
  return response.body;
}

function projectFromWorkspace(workspace, projectId) {
  const project = workspace.projects.find((candidate) => candidate.id === projectId);
  expect(project, `the workspace read must serve project ${projectId}`).toBeTruthy();
  return project;
}

async function readBoundResource(page, connectionId, reference) {
  const response = await api(page, "GET", "/portal/api/connections");
  expect(response.status, JSON.stringify(response.body)).toBe(200);
  const connection = response.body.connections.find((candidate) => candidate.id === connectionId);
  expect(connection, `the connections read must serve connection ${connectionId}`).toBeTruthy();
  return connection.resources.find((resource) => resource.external_ref === reference);
}

// The console renders the served projection, so every control is addressed by
// the label that projection produces (plan §4.2). A row is addressed through
// its own Sync button, so two rows that share a reference cannot be confused.
const resourceControl = (page, label) => page.getByRole("button", { name: label, exact: true });
const resourceRow = (page, name) =>
  page.getByRole("list", { name: "Project resources" })
    .getByRole("listitem")
    .filter({ has: page.getByRole("button", { name: `Sync ${name}`, exact: true }) });

async function bindResourceThroughConsole(page, { kind, name, reference, connectionId }) {
  await resourceControl(page, "Bind resource").click();
  const dialog = page.getByRole("dialog", { name: "Bind resource" });
  await expect(dialog).toBeVisible();
  await dialog.getByLabel("Type").selectOption(kind);
  await dialog.getByLabel("Provider connection").selectOption(connectionId);
  await dialog.getByLabel("Name").fill(name);
  await dialog.getByLabel("Reference", { exact: true }).fill(reference);
  await dialog.getByRole("button", { name: "Bind", exact: true }).click();
  await expect(dialog).toBeHidden();
}

// The refusal is rendered either in the dialog that submitted it or on the
// page that owns the receipt; both are the 0007d notice surface, so the alert
// carrying the server's code is asserted without pinning which one holds it.
const refusalAlert = (page, code) => page.getByRole("alert").filter({ hasText: `(${code})` }).first();

function recordApiBodies(page) {
  const responses = [];
  const requests = [];
  const consoleMessages = [];
  const pageErrors = [];
  const cookieJarSamples = [];
  page.on("request", (request) => {
    const url = new URL(request.url());
    if (url.origin !== portalOrigin) return;
    requests.push({ method: request.method(), path: url.pathname, request });
  });
  page.on("response", (response) => {
    const url = new URL(response.url());
    if (url.origin !== portalOrigin) return;
    // The full header list is recorded when the response arrives: response
    // headers are the surface that introduces an HttpOnly cookie (page scripts
    // can neither write nor read HttpOnly state), and this record still holds
    // the evidence after a transient cookie has been cleared.
    responses.push((async () => {
      const headers = (await response.headersArray()).map(({ name, value }) => `${name}: ${value}`);
      const status = response.status();
      if (status >= 300 && status < 400) {
        // A redirect has no readable body; its Location header is scanned in
        // its place, and a redirect without one is an evidence failure (304
        // has no body by protocol).
        const location = response.headers()["location"];
        return {
          path: url.pathname,
          status,
          headers,
          text: location ?? "",
          request: response.request(),
          unreadable: !location && status !== 304 ? "redirect response without a Location header" : undefined
        };
      }
      try {
        const text = await response.text();
        return { path: url.pathname, status, headers, text };
      } catch (error) {
        return { path: url.pathname, status, headers, text: null, unreadable: String(error), request: response.request() };
      }
    })());
    // The jar is sampled while the run is in flight, not only at the end, so a
    // cookie that exists for part of the run is still scanned.
    cookieJarSamples.push(page.context().cookies());
  });
  page.on("console", (message) => consoleMessages.push({ type: message.type(), text: message.text() }));
  page.on("pageerror", (error) => pageErrors.push(String(error?.stack || error)));
  return { responses, requests, consoleMessages, pageErrors, cookieJarSamples };
}

// A body the recorder still has to read cannot survive the document navigation
// that replaces it: Playwright 1.65.1 refuses that read (see `navigatedAway`)
// while the request carries no failure, so every document navigation settles
// the reads already started — the ones that land while this waits included.
const navigate = async (page, recorded, url) => {
  let pending;
  do {
    pending = recorded.responses.length;
    await Promise.all(recorded.responses.slice(0, pending));
  } while (recorded.responses.length > pending);
  await page.goto(url);
};

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
  // document.cookie cannot see HttpOnly cookies; the Playwright cookie jar
  // can. It is sampled at every same-origin response and once more here, so a
  // cookie that was set and later cleared is still scanned, and the final
  // sample must be readable and non-empty as evidence the jar was observable.
  const jar = await page.context().cookies();
  expect(jar.length, "the cookie jar must be readable and non-empty for the credential scan").toBeGreaterThan(0);
  expect(recorded.cookieJarSamples.length, "the jar must have been sampled during the run, not only at the end").toBeGreaterThan(0);
  for (const cookie of [...jar, ...(await Promise.all(recorded.cookieJarSamples)).flat()]) {
    expect(`${cookie.name}=${cookie.value}`, `cookie ${cookie.name} from a jar sample must not contain a provider credential`).not.toMatch(secretPattern);
  }
  expect(recorded.responses.length, "same-origin responses must be recorded for the credential scan").toBeGreaterThan(0);
  for (const entry of await Promise.all(recorded.responses)) {
    expect(entry.headers.length, `response ${entry.path} must have headers for the credential scan`).toBeGreaterThan(0);
    expect(entry.headers.join("\n"), `response headers for ${entry.path} must not contain a provider credential`).not.toMatch(secretPattern);
    if (entry.unreadable !== undefined) {
      // The protocol cannot serve the body of a response whose read the page
      // aborted, or whose document the scan lost to a navigation or a torn-down
      // session. Every case is a read, the navigation/session ones only in the
      // exact words above, and the recorded error stays in the message.
      const method = entry.request?.method();
      expect(["GET", "HEAD"], `an unreadable response must belong to a read: ${method} ${entry.path}: ${entry.unreadable}`).toContain(method);
      if (entry.request?.failure()) continue;
      const lost = entry.unreadable.endsWith(navigatedAway) || entry.unreadable.endsWith(sessionClosed);
      expect(lost, `response ${entry.path} must be readable unless its request was aborted: ${entry.unreadable}`).toBe(true);
      continue;
    }
    expect(entry.text, `response ${entry.path} must not contain a provider credential`).not.toMatch(secretPattern);
  }
  for (const recordedRequest of recorded.requests) {
    // allHeaders() includes the browser-managed headers (cookies) that
    // headers() omits, so the scan sees the full security-relevant header set.
    const headers = await recordedRequest.request.allHeaders();
    expect(JSON.stringify(headers), `request headers for ${recordedRequest.path} must not contain a provider credential`).not.toMatch(secretPattern);
    if (recordedRequest.path.startsWith("/portal/api/")) {
      if (recordedRequest.request.failure() !== null) {
        // A read the page cancelled — a project switch or a navigation unmounts
        // the observer that asked for it — never reached the server, so the
        // browser sent no cookie for it. Reads are the only cancellable
        // requests here; a write must still prove its cookie and CSRF header.
        expect(["GET", "HEAD"], `an aborted same-origin request must be a read: ${recordedRequest.method} ${recordedRequest.path}`).toContain(recordedRequest.method);
      } else {
        expect(headers.cookie, `the cookie header of ${recordedRequest.path} must be visible to the credential scan`).toBeTruthy();
        if (!["GET", "HEAD"].includes(recordedRequest.method)) {
          expect(headers["x-csrf-token"], `the CSRF header of ${recordedRequest.path} must be visible to the credential scan`).toBeTruthy();
        }
      }
    }
    expect(recordedRequest.request.postData() ?? "", `request body for ${recordedRequest.path} must not contain a provider credential`).not.toMatch(secretPattern);
  }
  for (const message of recorded.consoleMessages) {
    expect(`${message.type}: ${message.text}`, "console output must not contain a provider credential").not.toMatch(secretPattern);
  }
  for (const error of recorded.pageErrors) {
    expect(error, "page errors must not contain a provider credential").not.toMatch(secretPattern);
  }
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
  await connectionsLogin(page);
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
  // The removal receipt closes the dialog when the write settles; only then is
  // the row count meaningful (an open modal hides the list from the
  // accessibility tree, so a role-based count can pass while the row persists).
  await expect(page.getByRole("dialog", { name: new RegExp(`Remove ${name}`) })).toBeHidden();
  await expect(page.getByRole("button", { name: new RegExp(name) })).toHaveCount(0);
  // The detail unmounted with the removal; the page-level control that survives
  // it holds the keyboard instead of the document body.
  await expect(page.getByRole("button", { name: "New connection" })).toBeFocused();
  const list = await api(page, "GET", "/portal/api/connections");
  expect(list.status).toBe(200);
  expect(list.body.connections.filter((connection) => connection.name === name)).toEqual([]);
  expect(list.body.providers.map((provider) => provider.id).sort()).toEqual(["azure_devops", "github"]);

  await assertNoCredentials(page, recorded);
  await capture(page, testInfo, "removed", "removal reconciled against the real connection read");
});

test("an expired provider session, a repeated submission and a refused submission stay actionable", async ({ page }, testInfo) => {
  allowBrowserError(page, /status of (401|422|502)/);
  const recorded = recordApiBodies(page);
  await connectionsLogin(page);
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
  const repeated = await api(page, "POST", "/portal/api/connections", { provider: "github", name, account_ref: "expired-fixture", capabilities: ["ci"] });
  expect(repeated.status).toBe(200);
  expect(repeated.body.disposition).toBe("replayed");
  const conflicting = await api(page, "POST", "/portal/api/connections", { provider: "github", name, account_ref: "expired-fixture", capabilities: ["repositories"] });
  expect(conflicting.status).toBe(422);
  expect(conflicting.body.error.fields.provider.join(" ")).toContain("already has a connection with this name");
  await page.getByRole("button", { name: "Refresh", exact: true }).click();
  await expect(page.getByRole("button", { name: new RegExp(name) })).toHaveCount(1);
  const list = await api(page, "GET", "/portal/api/connections");
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
  await connectionsLogin(page);
  const name = `Stale fixture ${suffix}`;

  await createThroughConsole(page, { provider: "github", name, account: "acme-fixture", capabilities: ["Repositories"] });
  const listed = await api(page, "GET", "/portal/api/connections");
  const created = listed.body.connections.find((connection) => connection.name === name);
  expect(created).toBeTruthy();

  await page.getByRole("button", { name: new RegExp(name) }).click();
  await page.getByRole("complementary", { name: "Connection detail" }).getByRole("button", { name: "Edit capabilities", exact: true }).click();
  const settings = page.getByRole("dialog", { name: "Connection settings" });
  await settings.getByLabel("Work items", { exact: true }).check();

  // Another operator changes the connection after this editor froze the version
  // it opened with, so the submission below is refused by the server's fence.
  const renamed = `${name} renamed`;
  const external = await api(page, "PATCH", `/portal/api/connections/${created.id}`, { version: created.version, name: renamed });
  expect(external.status).toBe(200);

  await settings.getByRole("button", { name: "Save", exact: true }).click();
  await expect(settings.getByRole("alert")).toContainText("(stale)");
  await expect(settings.getByLabel("Name")).toBeDisabled();
  await capture(page, testInfo, "stale-update", "server refused a stale write with its own code");
  await settings.getByRole("button", { name: "Cancel", exact: true }).click();
  await expect(page.getByRole("button", { name: new RegExp(renamed) })).toBeVisible();

  // A concurrent removal wins: the second delete is refused and the console
  // reconciles to the server's state instead of reporting success. The dialog is
  // open before the other operator's delete, and the connections read that
  // delete's invalidation triggers (about 220 ms after the row is written, the
  // failing traces' re-read pair) would unmount the dialog first: it is held
  // until the refusal is asserted, then released as the reconcile observed below.
  const current = await api(page, "GET", "/portal/api/connections");
  const target = current.body.connections.find((connection) => connection.name === renamed);
  await page.getByRole("button", { name: new RegExp(renamed) }).click();
  await page.getByRole("complementary", { name: "Connection detail" }).getByRole("button", { name: "Remove", exact: true }).click();
  const removal = page.getByRole("dialog", { name: new RegExp(`Remove ${renamed}`) }).getByRole("button", { name: "Remove connection", exact: true });
  await expect(removal).toBeVisible();
  const heldReads = [];
  let holding = true;
  await page.route(/\/portal\/api\/connections(\?|$)/, async (route) => {
    if (!holding) return route.continue();
    heldReads.push(route);
  });
  const removedElsewhere = await api(page, "DELETE", `/portal/api/connections/${target.id}?version=${target.version}`);
  expect(removedElsewhere.status).toBe(200);
  const throttle = await page.context().newCDPSession(page);
  await throttle.send("Emulation.setCPUThrottlingRate", { rate: 12 });
  await removal.click();
  await throttle.send("Emulation.setCPUThrottlingRate", { rate: 1 });
  await expect(page.getByRole("status").first()).toContainText("(not_found)");
  await expect(page.getByRole("status").first()).toContainText("The server's current state is shown instead.");
  holding = false;
  for (const route of heldReads) await route.continue().catch(() => undefined);
  await page.unroute(/\/portal\/api\/connections(\?|$)/);
  await expect(page.getByRole("button", { name: new RegExp(renamed) })).toHaveCount(0);
  await capture(page, testInfo, "concurrent-delete", "concurrent removal refused and reconciled");

  await assertNoCredentials(page, recorded);
});

test("a capability update visibly resets dependent resource health and sync state", async ({ page }, testInfo) => {
  const recorded = recordApiBodies(page);
  await connectionsLogin(page);
  const projectId = await connectionProject(page);
  const name = `Bound fixture ${suffix}`;

  await createThroughConsole(page, { provider: "github", name, account: "acme-fixture", capabilities: ["Repositories", "Changes", "CI"] });
  const listed = await api(page, "GET", "/portal/api/connections");
  const connection = listed.body.connections.find((candidate) => candidate.name === name);
  // A provider resource must be owned by the connection's account: the GitHub
  // module refuses a reference whose owner is not the connection's account_ref.
  const attached = await api(page, "POST", `/portal/api/projects/${projectId}/resources`, {
    kind: "repository", name: "symmetry", external_ref: "acme-fixture/symmetry", connection_id: connection.id
  });
  expect(attached.status).toBe(201);
  const resourceId = attached.body.resource.id;

  const synced = await api(page, "POST", `/portal/api/resources/${resourceId}/sync`);
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

  const after = await api(page, "GET", "/portal/api/connections");
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
  await connectionsLogin(page);
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
  // The removal receipt closes the dialog when the write settles; only then is
  // the row count meaningful (an open modal hides the list from the
  // accessibility tree, so a role-based count can pass while the row persists).
  await expect(page.getByRole("dialog", { name: new RegExp(`Remove ${name}`) })).toBeHidden();
  await expect(page.getByRole("button", { name: new RegExp(name) })).toHaveCount(0);
  const removed = await api(page, "GET", "/portal/api/connections");
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

test("a GitHub repository resource is bound, synced, updated and unbound through the console", async ({ page }, testInfo) => {
  allowBrowserError(page, /status of 409/);
  const recorded = recordApiBodies(page);
  await connectionsLogin(page);
  const projectId = await connectionProject(page);
  const connectionName = `Resource fixture ${suffix}`;
  await createThroughConsole(page, { provider: "github", name: connectionName, account: "acme-fixture", capabilities: ["Repositories"] });
  const directory = await api(page, "GET", "/portal/api/connections");
  const connection = directory.body.connections.find((candidate) => candidate.name === connectionName);
  expect(connection, "the connections read must serve this test's connection").toBeTruthy();
  const reference = `acme-fixture/repository-${suffix}`;
  const nextReference = `acme-fixture/repository-next-${suffix}`;

  // The project was created out of band, so the page re-reads before the scope
  // is selected. Both steps stay in-app: a document navigation can strand a
  // response body the credential scan still has to read.
  await page.getByRole("button", { name: "Refresh", exact: true }).click();
  // The refresh's own read settles before the scope changes: switching would
  // unmount the observer of the current read and cancel it in flight.
  await page.waitForLoadState("networkidle");
  await page.getByLabel("Project", { exact: true }).selectOption(projectId);
  await expect(page.getByRole("heading", { name: "Project resources" })).toBeVisible();

  // Bind through the console: the row renders the served identity and the
  // untouched state (unknown health and sync, never synchronized).
  await bindResourceThroughConsole(page, { kind: "repository", name: "Alpha", reference, connectionId: connection.id });
  const boundRow = resourceRow(page, "Alpha");
  await expect(boundRow).toContainText(reference);
  await expect(boundRow).toContainText(connectionName);
  await expect(boundRow).toContainText(/Never synced/);
  await expect(boundRow).toContainText(/Unknown/);
  await capture(page, testInfo, "resource-bound", {
    surface: "/portal/connections project resources",
    projectId,
    reference,
    connection: connectionName,
    state: "bound through the console, never synced"
  });

  // The fixture repository document is the readback: the row shows the served
  // state and the served timestamp, not a local assumption.
  await resourceControl(page, "Sync Alpha").click();
  await expect(boundRow).toContainText(/Synced/);
  await expect(boundRow).not.toContainText(/Failed/);
  const synced = await readBoundResource(page, connection.id, reference);
  expect(synced.status).toBe("healthy");
  expect(synced.sync_status).toBe("synced");
  expect(synced.url).toBe(`https://github.com/${reference}`);
  await expect(boundRow).toContainText(synced.last_synced_at);
  await capture(page, testInfo, "resource-synced", {
    reference,
    served: { status: synced.status, sync_status: synced.sync_status, url: synced.url, last_synced_at: synced.last_synced_at }
  });

  // Version fence (update): the dialog opens with the version this page holds,
  // and another operator moves the resource before the submission.
  await resourceControl(page, "Update Alpha").click();
  const updateDialog = page.getByRole("dialog", { name: "Update resource" });
  await expect(updateDialog).toBeVisible();
  const movedElsewhere = await api(page, "PATCH", `/portal/api/resources/${synced.id}`, { version: synced.version, name: "Beta" });
  expect(movedElsewhere.status, JSON.stringify(movedElsewhere.body)).toBe(200);
  await updateDialog.getByLabel("Name").fill("Alpha renamed");
  await updateDialog.getByRole("button", { name: "Save", exact: true }).click();
  await expect(refusalAlert(page, "stale")).toBeVisible();
  await expect(page.getByRole("alert").filter({ hasText: "Reload to see the server's current values." }).first()).toBeVisible();
  await capture(page, testInfo, "resource-update-refused", {
    refusal: "stale",
    draft: "Alpha renamed",
    servedName: "Beta",
    note: "the server's code and the operator's next step are rendered"
  });
  await page.keyboard.press("Escape");
  await expect(updateDialog).toBeHidden();
  // The refusal re-reads: the row is the server's current resource, not the draft.
  await expect(resourceRow(page, "Beta")).toBeVisible();

  // Version fence (unbind): the resource moves again after the page reconciled,
  // and the unbind dialog submits the version it opened with.
  const reconciled = await readBoundResource(page, connection.id, reference);
  expect(reconciled.name).toBe("Beta");
  const movedAgain = await api(page, "PATCH", `/portal/api/resources/${reconciled.id}`, { version: reconciled.version, name: "Gamma" });
  expect(movedAgain.status, JSON.stringify(movedAgain.body)).toBe(200);
  await resourceControl(page, "Unbind Beta").click();
  const unbindDialog = page.getByRole("dialog", { name: "Unbind Beta?" });
  await expect(unbindDialog).toBeVisible();
  await unbindDialog.getByRole("button", { name: "Unbind resource", exact: true }).click();
  await expect(refusalAlert(page, "stale")).toBeVisible();
  await capture(page, testInfo, "resource-unbind-refused", {
    refusal: "stale",
    servedName: "Gamma",
    note: "the version-fenced unbind is refused with the server's code"
  });
  await page.keyboard.press("Escape");
  await expect(unbindDialog).toBeHidden();
  await expect(resourceRow(page, "Gamma")).toBeVisible();

  // Successful update: changing the reference is an identity change, and the
  // console re-reads the reset the server served instead of predicting it.
  await resourceControl(page, "Update Gamma").click();
  const identityDialog = page.getByRole("dialog", { name: "Update resource" });
  await expect(identityDialog).toBeVisible();
  await identityDialog.getByLabel("Name").fill("Delta");
  await identityDialog.getByLabel("Reference", { exact: true }).fill(nextReference);
  await identityDialog.getByRole("button", { name: "Save", exact: true }).click();
  await expect(identityDialog).toBeHidden();
  const reset = await readBoundResource(page, connection.id, nextReference);
  expect(reset).toMatchObject({ name: "Delta", status: "unknown", sync_status: "unknown", last_synced_at: null, url: null });
  const resetRow = resourceRow(page, "Delta");
  await expect(resetRow).toContainText(nextReference);
  await expect(resetRow).toContainText(/Never synced/);
  await capture(page, testInfo, "resource-updated", {
    reference: nextReference,
    served: { status: reset.status, sync_status: reset.sync_status, last_synced_at: reset.last_synced_at },
    note: "an identity change resets the served state; the row follows the read"
  });

  // Successful unbind through the console: the row and the readback both lose it.
  await resourceControl(page, "Unbind Delta").click();
  const finalUnbind = page.getByRole("dialog", { name: "Unbind Delta?" });
  await expect(finalUnbind).toBeVisible();
  await finalUnbind.getByRole("button", { name: "Unbind resource", exact: true }).click();
  await expect(finalUnbind).toBeHidden();
  await expect(page.getByRole("status").filter({ hasText: "Resource detached — Delta." })).toBeVisible();
  await expect(resetRow).toHaveCount(0);
  expect(await readBoundResource(page, connection.id, nextReference)).toBeUndefined();
  await capture(page, testInfo, "resource-unbound", {
    reference: nextReference,
    readback: "the resource is absent from the connection read"
  });

  await assertNoCredentials(page, recorded);
});

test("a provider failure and an ambiguous outcome stay explicit and the next readback writes the provider's answer", async ({ page, isMobile }, testInfo) => {
  allowBrowserError(page, /status of 502/);
  const recorded = recordApiBodies(page);
  await connectionsLogin(page);
  const projectId = await connectionProject(page);
  const connectionName = `Sync fixture ${suffix}`;
  const connectionId = await createConnectionThroughApi(page, {
    name: connectionName,
    account: "acme-fixture",
    // `ci` makes the repository sync's delivery read also answer the CI status
    // for the pull request's head sha, so all three delivery rows are
    // provider-owned here (the retired classic assertion pinned all three).
    capabilities: ["repositories", "work_items", "changes", "ci"]
  });
  const issuesReference = `acme-fixture/issues-${suffix}`;
  const repositoryReference = `acme-fixture/repository-${suffix}`;
  const issues = await attachResource(page, projectId, { kind: "work_tracking", name: "Issues", external_ref: issuesReference, connection_id: connectionId });
  await attachResource(page, projectId, { kind: "repository", name: "Repository", external_ref: repositoryReference, connection_id: connectionId });
  await writeFixtureState({ github: { issues: { [issuesReference]: [101, 102] } } });

  await page.getByRole("button", { name: "Refresh", exact: true }).click();
  await page.waitForLoadState("networkidle");
  await page.getByLabel("Project", { exact: true }).selectOption(projectId);

  // A work-tracking sync imports the fixture issues document.
  await resourceControl(page, "Sync Issues").click();
  const issuesRow = resourceRow(page, "Issues");
  await expect(issuesRow).toContainText(/Synced/);
  const issuesSynced = await readBoundResource(page, connectionId, issuesReference);
  expect(issuesSynced.status).toBe("healthy");
  expect(issuesSynced.sync_status).toBe("synced");
  expect(issuesSynced.url).toBe(`https://github.com/${issuesReference}/issues`);
  await expect(issuesRow).toContainText(issuesSynced.last_synced_at);
  const importedWorkspace = await readWorkspace(page, projectId);
  const importedItems = projectFromWorkspace(importedWorkspace, projectId).work_items.filter((item) => item.external?.resource_id === issues.id);
  expect(importedItems.map((item) => item.external.id).sort()).toEqual(["101", "102"]);
  await capture(page, testInfo, "issues-synced", {
    reference: issuesReference,
    served: { url: issuesSynced.url, last_synced_at: issuesSynced.last_synced_at },
    importedExternalIds: importedItems.map((item) => item.external.id).sort()
  });

  // Definitive provider failure: the fixture answers HTTP 500, the server maps
  // it to its fixed message, and the row never reads Synced while unresolved.
  await writeFixtureState({ github: { issues: { [issuesReference]: [101, 102] }, fail: [issuesReference] } });
  await resourceControl(page, "Sync Issues").click();
  await expect(issuesRow).toContainText(/Failed/);
  await expect(issuesRow).toContainText("Provider returned HTTP 500");
  await expect(issuesRow.getByText("Synced", { exact: true })).toHaveCount(0);
  const failed = await readBoundResource(page, connectionId, issuesReference);
  expect(failed.status).toBe("degraded");
  expect(failed.sync_status).toBe("failed");
  expect(failed.status_message).toBe("Provider returned HTTP 500");
  expect(failed.last_synced_at).toBe(issuesSynced.last_synced_at);
  // The degraded resource health the retired classic health rail surfaced is
  // served by the row's own health badge (moved from portal.spec.mjs).
  await expect(issuesRow.getByText("Degraded", { exact: true })).toBeVisible();
  await capture(page, testInfo, "provider-failure", {
    injected: "provider read answered HTTP 500",
    served: { status: failed.status, sync_status: failed.sync_status, status_message: failed.status_message },
    lastSyncedAtHeld: failed.last_synced_at === issuesSynced.last_synced_at
  });

  // Ambiguous outcome: the request left, the answer never arrived. The server
  // records the explicit failure and the row shows it.
  await writeFixtureState({ github: { transport_fail: [issuesReference] } });
  await resourceControl(page, "Sync Issues").click();
  await expect(issuesRow).toContainText("Provider transport failed");
  await expect(issuesRow.getByText("Synced", { exact: true })).toHaveCount(0);
  const ambiguous = await readBoundResource(page, connectionId, issuesReference);
  expect(ambiguous.sync_status).toBe("failed");
  expect(ambiguous.status_message).toBe("Provider transport failed");
  expect(ambiguous.last_synced_at).toBe(issuesSynced.last_synced_at);
  await capture(page, testInfo, "transport-failure", {
    injected: "ambiguous provider transport failure",
    served: { sync_status: ambiguous.sync_status, status_message: ambiguous.status_message },
    lastSyncedAtHeld: ambiguous.last_synced_at === issuesSynced.last_synced_at
  });

  // Repository baseline, then the ambiguous outcome, then a readback whose
  // document contradicts the pre-failure assumption: the sync writes exactly
  // what the provider answered at readback time.
  await resetFixtureState();
  await resourceControl(page, "Sync Repository").click();
  const repositoryRow = resourceRow(page, "Repository");
  await expect(repositoryRow).toContainText(/Synced/);
  const repositorySynced = await readBoundResource(page, connectionId, repositoryReference);
  expect(repositorySynced.url).toBe(`https://github.com/${repositoryReference}`);
  expect(repositorySynced.metadata.archived).toBe(false);
  const baselineSyncedAt = repositorySynced.last_synced_at;
  expect(baselineSyncedAt).toBeTruthy();

  await writeFixtureState({ github: { transport_fail: [repositoryReference] } });
  await resourceControl(page, "Sync Repository").click();
  await expect(repositoryRow).toContainText("Provider transport failed");
  const repositoryAmbiguous = await readBoundResource(page, connectionId, repositoryReference);
  expect(repositoryAmbiguous.url).toBe(repositorySynced.url);
  expect(repositoryAmbiguous.metadata.archived).toBe(false);
  expect(repositoryAmbiguous.last_synced_at).toBe(baselineSyncedAt);

  const archivedUrl = `https://github.com/${repositoryReference}-archived`;
  await writeFixtureState({ github: { repository: { [repositoryReference]: { archived: true, html_url: archivedUrl } } } });
  await resourceControl(page, "Sync Repository").click();
  await expect(repositoryRow).toContainText(/Synced/);
  await expect(repositoryRow).not.toContainText(/Failed/);
  const resolved = await readBoundResource(page, connectionId, repositoryReference);
  expect(resolved.status).toBe("healthy");
  expect(resolved.sync_status).toBe("synced");
  expect(resolved.url).toBe(archivedUrl);
  expect(resolved.metadata.archived).toBe(true);
  expect(Date.parse(resolved.last_synced_at)).toBeGreaterThan(Date.parse(baselineSyncedAt));
  await expect(repositoryRow).toContainText(resolved.last_synced_at);
  await capture(page, testInfo, "readback-resolved", {
    reference: repositoryReference,
    served: { url: resolved.url, archived: resolved.metadata.archived, last_synced_at: resolved.last_synced_at },
    replacedAssumption: "archived: true and a different html_url were written from the readback"
  });

  await resetFixtureState();

  // Provider-sourced delivery: a work item bound to the repository resource
  // whose pull request URL the fixture answers as open with an approved review.
  // The console sync reads that pull request through the real provider module,
  // and the real work item route renders exactly the provider-owned evidence.
  const deliveryRepository = await readBoundResource(page, connectionId, repositoryReference);
  expect(deliveryRepository.sync_status).toBe("synced");
  const deliveryPullRequestUrl = `https://github.com/${repositoryReference}/pull/42`;
  const deliveryTitle = `Delivery evidence ${suffix}`;
  const deliveryCreated = await api(page, "POST", `/portal/api/projects/${projectId}/work-items`, {
    title: deliveryTitle,
    repository_resource_id: deliveryRepository.id,
    pull_request_url: deliveryPullRequestUrl
  });
  expect(deliveryCreated.status, JSON.stringify(deliveryCreated.body)).toBe(201);
  const deliveryWorkItemId = deliveryCreated.body.work_item.id;
  // Until a sync reads the provider's answer, the item serves the operator's
  // URL as a manual delivery and the provider owns nothing.
  expect(deliveryCreated.body.work_item.delivery).toMatchObject({
    pull_request: { source: "manual", value: deliveryPullRequestUrl, url: deliveryPullRequestUrl, generation: null },
    review: { source: "none", status: "none" }
  });
  // Only a provider read adds the recorded time; the manual projection has none.
  expect(deliveryCreated.body.work_item.delivery.pull_request.recorded_at).toBeUndefined();

  const deliverySync = page.waitForResponse((response) =>
    response.request().method() === "POST" &&
    new URL(response.url()).pathname === `/portal/api/resources/${deliveryRepository.id}/sync`
  );
  await resourceControl(page, "Sync Repository").click();
  expect((await deliverySync).status()).toBe(200);
  const deliveryRepositorySynced = await readBoundResource(page, connectionId, repositoryReference);
  expect(deliveryRepositorySynced.sync_status).toBe("synced");
  const deliveryRead = await api(page, "GET", `/portal/api/work-items/${deliveryWorkItemId}`);
  expect(deliveryRead.status, JSON.stringify(deliveryRead.body)).toBe(200);
  const providerDelivery = deliveryRead.body.work_item.delivery;
  expect(providerDelivery.pull_request.source).toBe("provider");
  expect(providerDelivery.pull_request.provider).toBe("github");
  expect(providerDelivery.pull_request.value).toBe(deliveryPullRequestUrl);
  expect(providerDelivery.pull_request.url).toBe(deliveryPullRequestUrl);
  expect(providerDelivery.pull_request.status).toBe("open");
  expect(providerDelivery.pull_request.generation).toBeNull();
  expect(typeof providerDelivery.pull_request.recorded_at).toBe("string");
  expect(providerDelivery.review.source).toBe("provider");
  expect(providerDelivery.review.value).toBe("approved");
  expect(providerDelivery.review.status).toBe("approved");
  expect(providerDelivery.review.generation).toBeNull();
  // The CI half of the same provider read: the actions run for the pull
  // request's head sha owns the third delivery row (the retired classic
  // assertion pinned all three rendered source labels).
  expect(providerDelivery.ci.source).toBe("provider");
  expect(providerDelivery.ci.provider).toBe("github");
  expect(providerDelivery.ci.value).toBe("passed");
  expect(providerDelivery.ci.status).toBe("passed");
  expect(providerDelivery.ci.generation).toBeNull();
  expect(typeof providerDelivery.ci.recorded_at).toBe("string");

  await navigate(page, recorded, `/portal/work/${deliveryWorkItemId}`);
  const deliveryDetail = isMobile ? page.locator("#main-content") : page.getByRole("dialog", { name: deliveryTitle });
  await expect(deliveryDetail.getByRole("heading", { level: 1, name: deliveryTitle })).toBeVisible();
  const deliveryRows = deliveryDetail.locator(".delivery-list > div");
  const deliveryPullRequestRow = deliveryRows.filter({ hasText: "Pull request" });
  const deliveryCiRow = deliveryRows.filter({ hasText: "CI" });
  const deliveryReviewRow = deliveryRows.filter({ hasText: "Review" });
  await expect(deliveryPullRequestRow.locator("dd .chip")).toHaveText("open");
  // The rendered source is the delivery's own provenance ("provider") plus the
  // provider that authored it; the served projection below is the same values.
  await expect(deliveryPullRequestRow.locator("dd")).toContainText("source: provider · github");
  await expect(deliveryPullRequestRow.getByRole("link", { name: "Open", exact: true }))
    .toHaveAttribute("href", deliveryPullRequestUrl);
  await expect(deliveryReviewRow.locator("dd .chip")).toHaveText("approved");
  await expect(deliveryReviewRow.locator("dd")).toContainText("source: provider · github");
  // Every provider-owned row carries its attribution, the CI row included.
  await expect(deliveryCiRow.locator("dd .chip")).toContainText("passed");
  await expect(deliveryCiRow.locator("dd")).toContainText("source: provider · github");
  await capture(page, testInfo, "delivery-evidence", {
    workItemId: deliveryWorkItemId,
    pullRequestUrl: deliveryPullRequestUrl,
    served: {
      value: providerDelivery.pull_request.value,
      status: providerDelivery.pull_request.status,
      source: providerDelivery.pull_request.source,
      provider: providerDelivery.pull_request.provider,
      ci: providerDelivery.ci,
      review: providerDelivery.review
    }
  });

  // Negative direction: the provider module refuses a work item whose pull
  // request URL belongs to another repository. The console renders the
  // server's receipt, the resource takes the server's failure state, and
  // neither work item's delivery is rewritten.
  // Back to the connections surface.
  await navigate(page, recorded, `/portal/connections?projectId=${projectId}`);
  await expect(page.getByRole("heading", { name: "Project resources" })).toBeVisible();
  allowBrowserError(page, /status of 400/);
  const mismatchedPullRequestUrl = `https://github.com/acme-fixture/other-${suffix}/pull/42`;
  const mismatchedTitle = `Mismatched delivery ${suffix}`;
  const mismatchedCreated = await api(page, "POST", `/portal/api/projects/${projectId}/work-items`, {
    title: mismatchedTitle,
    repository_resource_id: deliveryRepository.id,
    pull_request_url: mismatchedPullRequestUrl
  });
  expect(mismatchedCreated.status, JSON.stringify(mismatchedCreated.body)).toBe(201);
  const mismatchedWorkItemId = mismatchedCreated.body.work_item.id;
  const mismatchedDelivery = mismatchedCreated.body.work_item.delivery;
  expect(mismatchedDelivery).toMatchObject({
    pull_request: { source: "manual", value: mismatchedPullRequestUrl, url: mismatchedPullRequestUrl, generation: null },
    review: { source: "none", status: "none" }
  });

  const refusedSync = page.waitForResponse((response) =>
    response.request().method() === "POST" &&
    new URL(response.url()).pathname === `/portal/api/resources/${deliveryRepository.id}/sync`
  );
  await resourceControl(page, "Sync Repository").click();
  expect((await refusedSync).status()).toBe(400);
  await expect(page.getByRole("status").filter({ hasText: "(invalid_request)" }))
    .toContainText("request payload is invalid (invalid_request)");
  await expect(repositoryRow).toContainText(/Failed/);
  await expect(repositoryRow).toContainText("Pull request URL does not match the bound repository");
  const refusedRepository = await readBoundResource(page, connectionId, repositoryReference);
  expect(refusedRepository.status).toBe("degraded");
  expect(refusedRepository.sync_status).toBe("failed");
  expect(refusedRepository.status_message).toBe("Pull request URL does not match the bound repository");
  // The refusal moved the check marker but kept the last good sync marker.
  expect(Date.parse(refusedRepository.last_synced_at)).toBeLessThan(Date.parse(refusedRepository.last_checked_at));

  const mismatchedAfter = await api(page, "GET", `/portal/api/work-items/${mismatchedWorkItemId}`);
  expect(mismatchedAfter.body.work_item.delivery).toEqual(mismatchedDelivery);
  const providerDeliveryAfter = await api(page, "GET", `/portal/api/work-items/${deliveryWorkItemId}`);
  // The refusal writes nothing: the sibling item's provider-owned delivery is
  // intact, and the console sync's own receipt is the state above.
  expect(providerDeliveryAfter.body.work_item.delivery).toMatchObject({
    pull_request: {
      source: "provider",
      provider: "github",
      value: deliveryPullRequestUrl,
      url: deliveryPullRequestUrl,
      status: "open",
      generation: null
    },
    review: { source: "provider", value: "approved", status: "approved", generation: null }
  });
  await capture(page, testInfo, "delivery-refused", {
    workItemId: mismatchedWorkItemId,
    refusal: "invalid_request (request payload is invalid)",
    servedMessage: refusedRepository.status_message,
    served: {
      resource: { status: refusedRepository.status, sync_status: refusedRepository.sync_status },
      delivery: mismatchedAfter.body.work_item.delivery
    }
  });

  await assertNoCredentials(page, recorded);
});

test("project switching discards a late sync continuation and never writes into the new project", async ({ page, browser }, testInfo) => {
  allowBrowserError(page, /status of 502/);
  const recorded = recordApiBodies(page);
  await connectionsLogin(page);
  const first = await connectionProject(page);
  const second = await connectionProject(page);
  const connectionName = `Switch fixture ${suffix}`;
  const connectionId = await createConnectionThroughApi(page, {
    name: connectionName,
    account: "acme-fixture",
    capabilities: ["repositories", "work_items"]
  });
  const firstRepositoryReference = `acme-fixture/first-repository-${suffix}`;
  const firstIssuesReference = `acme-fixture/first-issues-${suffix}`;
  const secondReference = `acme-fixture/second-repository-${suffix}`;
  await attachResource(page, first, { kind: "repository", name: "First repo", external_ref: firstRepositoryReference, connection_id: connectionId });
  await attachResource(page, first, { kind: "work_tracking", name: "First issues", external_ref: firstIssuesReference, connection_id: connectionId });
  await attachResource(page, second, { kind: "repository", name: "Second repo", external_ref: secondReference, connection_id: connectionId });
  await resetFixtureState();

  await page.getByRole("button", { name: "Refresh", exact: true }).click();
  await page.waitForLoadState("networkidle");
  await page.getByLabel("Project", { exact: true }).selectOption(first);
  await expect(resourceRow(page, "First repo")).toBeVisible();
  await expect(resourceRow(page, "First issues")).toBeVisible();

  // A per-resource sync held open: the console mirrors the single-flight
  // contract while the real request is pending, and the operator switches
  // projects before it settles.
  await writeFixtureState({ github: { hold: [firstRepositoryReference] } });
  await resourceControl(page, "Sync First repo").click();
  await expect(resourceControl(page, "Sync First repo")).toBeDisabled();
  await expect(resourceControl(page, "Update First repo")).toBeDisabled();
  await expect(resourceControl(page, "Unbind First repo")).toBeDisabled();
  await expect(resourceControl(page, "Bind resource")).toBeDisabled();
  await expect(resourceControl(page, "Synchronize project")).toBeDisabled();
  await capture(page, testInfo, "held-resource-sync", {
    reference: firstRepositoryReference,
    pending: "the sync is in flight and every mutation control refuses a second start"
  });
  // `exact` keeps this on the shell's project picker: the project-resources
  // surface labels its own region "Project resources".
  await page.getByLabel("Project", { exact: true }).selectOption(second);
  await expect(resourceRow(page, "Second repo")).toBeVisible();
  await expect(page.getByText(firstRepositoryReference)).toHaveCount(0);
  await expect(resourceControl(page, "Sync First repo")).toHaveCount(0);
  // Releasing the hold settles the first project's sync; the second project's
  // rows still hold only its own served state.
  await resetFixtureState();
  await expect.poll(async () => {
    const resource = await readBoundResource(page, connectionId, firstRepositoryReference);
    return resource ? resource.sync_status : "missing";
  }, { message: "the first project's held sync must settle after the hold is released" }).toBe("synced");
  await expect(resourceRow(page, "Second repo")).toBeVisible();
  await expect(resourceRow(page, "Second repo")).not.toContainText(/Synced/);
  await expect(page.getByText(firstRepositoryReference)).toHaveCount(0);
  await expect(resourceControl(page, "Bind resource")).toBeEnabled();
  await expect(resourceControl(page, "Synchronize project")).toBeEnabled();
  await capture(page, testInfo, "switch-after-resource-sync", {
    selected: second,
    retained: "the second project's rows are its own; the settled first-project sync wrote nothing here"
  });

  // A project sync held open, then released into a failure after the switch:
  // the late failure for the old project surfaces nothing on the new one.
  await page.getByLabel("Project", { exact: true }).selectOption(first);
  await expect(resourceRow(page, "First repo")).toBeVisible();
  await writeFixtureState({ github: { hold: [firstRepositoryReference, firstIssuesReference] } });
  await resourceControl(page, "Synchronize project").click();
  await expect(resourceControl(page, "Synchronize project")).toBeDisabled();
  await expect(resourceControl(page, "Bind resource")).toBeDisabled();
  await expect(resourceControl(page, "Sync First repo")).toBeDisabled();
  await expect(resourceControl(page, "Update First issues")).toBeDisabled();
  await page.getByLabel("Project", { exact: true }).selectOption(second);
  await expect(resourceRow(page, "Second repo")).toBeVisible();
  await writeFixtureState({ github: { transport_fail: [firstRepositoryReference, firstIssuesReference] } });
  await expect.poll(async () => {
    const repository = await readBoundResource(page, connectionId, firstRepositoryReference);
    const issues = await readBoundResource(page, connectionId, firstIssuesReference);
    return [repository ? repository.sync_status : "missing", issues ? issues.sync_status : "missing"];
  }, { message: "the released project sync must settle as an explicit failure" }).toEqual(["failed", "failed"]);
  // Every continuation of the finished project sync was discarded.
  await expect(page.getByText(/Provider transport failed/)).toHaveCount(0);
  await expect(page.getByText(/\(provider_failure\)/)).toHaveCount(0);
  await expect(resourceRow(page, "Second repo")).toBeVisible();
  await expect(page.getByText(firstRepositoryReference)).toHaveCount(0);
  await expect(resourceControl(page, "Bind resource")).toBeEnabled();
  await expect(resourceControl(page, "Synchronize project")).toBeEnabled();
  await capture(page, testInfo, "switch-after-project-sync-failure", {
    selected: second,
    discarded: "the late failure for the first project surfaced nothing on the second project"
  });
  // Going back shows the served truth the re-read reports, not a carry-over.
  await page.waitForLoadState("networkidle");
  await page.getByLabel("Project", { exact: true }).selectOption(first);
  await expect(resourceRow(page, "First repo")).toContainText("Provider transport failed");
  await expect(resourceRow(page, "First issues")).toContainText("Provider transport failed");
  await expect(resourceRow(page, "First repo")).toContainText(/Failed/);
  await capture(page, testInfo, "first-project-served-failure", {
    selected: first,
    served: "both first-project resources carry the server's failure after the re-read"
  });

  // Cross-operator: an independent session reads the same served state and
  // settles its own project while the first page keeps the second selected.
  await resetFixtureState();
  const context = await browser.newContext(deviceContextOptions(testInfo, { baseURL: portalOrigin }));
  const operator = await context.newPage();
  monitorBrowserErrors(operator);
  const operatorRecorded = recordApiBodies(operator);
  try {
    await connectionsLogin(operator);
    // The fresh session reads the whole project directory at login, so the
    // scope is selected in-app instead of loading a second document.
    await operator.getByLabel("Project", { exact: true }).selectOption(first);
    await expect(resourceRow(operator, "First repo")).toContainText("Provider transport failed");
    await expect(resourceRow(operator, "Second repo")).toHaveCount(0);
    await operator.getByLabel("Project", { exact: true }).selectOption(second);
    await expect(resourceRow(operator, "Second repo")).toBeVisible();
    await operator.getByRole("button", { name: "Sync Second repo", exact: true }).click();
    await expect(resourceRow(operator, "Second repo")).toContainText(/Synced/);
    await capture(operator, testInfo, "cross-operator-readback", {
      firstProject: "provider failure served to an independent operator session",
      secondProject: "the second project's resource synchronized by that session"
    });
    await assertNoCredentials(operator, operatorRecorded);
    assertNoBrowserErrors(operator);
  } finally {
    await context.close();
  }
  await resetFixtureState();
  await assertNoCredentials(page, recorded);
});

test("external work items keep their identity, their provider-owned lock and their availability", async ({ page, isMobile }, testInfo) => {
  allowBrowserError(page, /status of 409/);
  const recorded = recordApiBodies(page);
  await connectionsLogin(page);
  const projectId = await connectionProject(page);
  const otherProjectId = await connectionProject(page);
  const connectionName = `Assignment fixture ${suffix}`;
  const connectionId = await createConnectionThroughApi(page, {
    name: connectionName,
    account: "acme-fixture",
    capabilities: ["repositories", "work_items", "ci"]
  });
  const issuesReference = `acme-fixture/issues-${suffix}`;
  const repositoryReference = `acme-fixture/repository-${suffix}`;
  const pipelineReference = `acme-fixture/pipeline-${suffix}`;
  const otherReference = `acme-fixture/other-${suffix}`;
  const issues = await attachResource(page, projectId, { kind: "work_tracking", name: "Issues", external_ref: issuesReference, connection_id: connectionId });
  const repository = await attachResource(page, projectId, { kind: "repository", name: "Repository", external_ref: repositoryReference, connection_id: connectionId });
  const pipeline = await attachResource(page, projectId, { kind: "ci", name: "Pipeline", external_ref: pipelineReference, connection_id: connectionId });
  await attachResource(page, otherProjectId, { kind: "repository", name: "Other repository", external_ref: otherReference, connection_id: connectionId });
  await writeFixtureState({ github: { issues: { [issuesReference]: [101, 102] } } });

  const synchronized = await api(page, "POST", `/portal/api/resources/${issues.id}/sync`);
  expect(synchronized.status, JSON.stringify(synchronized.body)).toBe(200);
  const workspace = await readWorkspace(page, projectId);
  const imported = projectFromWorkspace(workspace, projectId).work_items.filter((item) => item.external?.resource_id === issues.id);
  expect(imported.map((item) => item.external.id).sort()).toEqual(["101", "102"]);
  const subject = imported.find((item) => item.external.id === "101");
  const closed = imported.find((item) => item.external.id === "102");
  expect(subject.external.url).toBe(`https://github.com/${issuesReference}/issues/101`);
  expect(subject.external.state).toBe("open");
  expect(subject.external.available).toBe(true);
  expect(subject.external.assignee).toBe("fixture-assignee");

  const detailRead = await api(page, "GET", `/portal/api/work-items/${subject.id}`);
  expect(detailRead.status, JSON.stringify(detailRead.body)).toBe(200);
  expect(detailRead.body.work_item.external.id).toBe("101");
  expect(detailRead.body.work_item.external.url).toBe(subject.external.url);
  expect(detailRead.body.work_item.external.available).toBe(true);

  await navigate(page, recorded, `/portal/work/${subject.id}`);
  const detail = isMobile ? page.locator("#main-content") : page.getByRole("dialog", { name: subject.title });
  await expect(detail.getByRole("heading", { level: 1, name: subject.title })).toBeVisible();
  await expect(detail.getByRole("link", { name: "Open external record" })).toHaveAttribute("href", subject.external.url);
  await capture(page, testInfo, "external-item", {
    workItemId: subject.id,
    external: { id: subject.external.id, url: subject.external.url, state: subject.external.state, available: subject.external.available }
  });

  // The assignment editor offers only the item's own project resources: the
  // other project's repository is not an option, and each option carries the
  // resource id and the exact reference.
  await detail.getByRole("button", { name: "Edit assignment", exact: true }).click();
  const dialog = page.getByRole("dialog", { name: "Assignment" });
  await expect(dialog).toBeVisible();
  // The option sets come from the item's project read: wait for that read to
  // land (the narrow surface has no background project board) and pin the
  // sets at exactly the empty option plus this project's one resource.
  await expect(dialog.getByLabel("Repository")).toContainText(repositoryReference);
  await expect(dialog.getByLabel("Repository").locator("option")).toHaveCount(2);
  await expect(dialog.getByLabel("CI resource")).toContainText(pipelineReference);
  await expect(dialog.getByLabel("CI resource").locator("option")).toHaveCount(2);
  const repositoryOptions = await dialog.getByLabel("Repository").locator("option").evaluateAll((options) =>
    options.map((option) => ({ value: option.value, text: option.textContent ?? "" }))
  );
  const pipelineOptions = await dialog.getByLabel("CI resource").locator("option").evaluateAll((options) =>
    options.map((option) => ({ value: option.value, text: option.textContent ?? "" }))
  );
  expect(repositoryOptions.some((option) => option.value === repository.id), "the repository options must carry this project's repository resource id").toBe(true);
  expect(repositoryOptions.some((option) => option.text.includes(repositoryReference)), "the repository option must carry the exact reference").toBe(true);
  expect(repositoryOptions.some((option) => /not linked/i.test(option.text)), "the repository select must offer the unlinked option").toBe(true);
  expect(repositoryOptions.every((option) => !option.text.includes(otherReference)), "another project's resource must not be offered").toBe(true);
  expect(pipelineOptions.some((option) => option.value === pipeline.id), "the CI options must carry this project's CI resource id").toBe(true);
  expect(pipelineOptions.some((option) => option.text.includes(pipelineReference)), "the CI option must carry the exact reference").toBe(true);
  expect(pipelineOptions.some((option) => /use repository provider/i.test(option.text)), "the CI select must offer the provider-fallback option").toBe(true);
  expect(pipelineOptions.every((option) => !option.text.includes(otherReference)), "another project's resource must not be offered as CI").toBe(true);
  expect(repositoryOptions.some((option) => option.value === pipeline.id)).toBe(false);
  expect(pipelineOptions.some((option) => option.value === repository.id)).toBe(false);

  await dialog.getByLabel("Owner type").selectOption("agent");
  await dialog.getByLabel("Owner", { exact: true }).fill("Codex");
  await dialog.getByLabel("Agent profile").fill("codex");
  await dialog.getByLabel("Workspace").fill("primary");
  await dialog.getByLabel("Repository").selectOption(repository.id);
  await dialog.getByLabel("CI resource").selectOption(pipeline.id);
  await dialog.getByRole("button", { name: "Save assignment", exact: true }).click();
  await expect(dialog).toBeHidden();
  const assigned = await api(page, "GET", `/portal/api/work-items/${subject.id}`);
  expect(assigned.body.work_item.repository_resource_id).toBe(repository.id);
  expect(assigned.body.work_item.ci_resource_id).toBe(pipeline.id);
  expect(assigned.body.work_item.assignee.type).toBe("agent");
  expect(assigned.body.work_item.assignee.name).toBe("Codex");
  expect(assigned.body.work_item.assignee.agent_profile).toBe("codex");
  expect(assigned.body.work_item.workspace).toBe("primary");
  expect(assigned.body.work_item.can_start).toBe(true);
  await capture(page, testInfo, "assignment-saved", {
    workItemId: subject.id,
    served: {
      repository_resource_id: assigned.body.work_item.repository_resource_id,
      ci_resource_id: assigned.body.work_item.ci_resource_id,
      assignee: assigned.body.work_item.assignee,
      can_start: assigned.body.work_item.can_start
    },
    options: {
      repository: repositoryOptions.map((option) => option.value),
      ci: pipelineOptions.map((option) => option.value)
    }
  });

  // Provider-owned fields stay locked on the API even though this surface
  // renders no control for them.
  const refusedTitle = await api(page, "PATCH", `/portal/api/work-items/${subject.id}`, { version: assigned.body.work_item.version, title: "Provider-owned change" });
  expect(refusedTitle.status, JSON.stringify(refusedTitle.body)).toBe(409);
  expect(refusedTitle.body.error.code).toBe("provider_owned");
  expect(await detail.getByRole("textbox", { name: /title/i }).count()).toBe(0);

  // The execution entry point for an available item is asserted on the work
  // detail the project surface opens: the served `can_start` renders
  // `Start run`, and the readback that omits the item must remove it (below).
  await navigate(page, recorded, `/portal/projects/${projectId}`);
  await page.getByRole("link", { name: subject.title }).click();
  const availableDetail = isMobile
    ? page.locator("#main-content")
    : page.getByRole("dialog", { name: subject.title });
  await expect(availableDetail.getByRole("button", { name: "Start run" })).toBeVisible();
  await page.keyboard.press("Escape");

  // A readback that omits the item marks it unavailable: the detail renders it
  // unavailable, no run or retry control exists, and the readback refuses it.
  await writeFixtureState({ github: { issues: { [issuesReference]: [102] } } });
  const shrunk = await api(page, "POST", `/portal/api/resources/${issues.id}/sync`);
  expect(shrunk.status, JSON.stringify(shrunk.body)).toBe(200);
  const unavailable = await api(page, "GET", `/portal/api/work-items/${subject.id}`);
  expect(unavailable.body.work_item.external.available).toBe(false);
  expect(unavailable.body.work_item.can_start).toBe(false);
  const stillAvailable = await api(page, "GET", `/portal/api/work-items/${closed.id}`);
  expect(stillAvailable.body.work_item.external.available).toBe(true);
  // Back on the read-only detail (the clause check above left the classic
  // surface open), the served readback renders it unavailable.
  await navigate(page, recorded, `/portal/work/${subject.id}`);
  const reloaded = isMobile ? page.locator("#main-content") : page.getByRole("dialog", { name: subject.title });
  await expect(reloaded.getByText(/unavailable/i).first()).toBeVisible();
  await expect(reloaded.getByRole("button", { name: /^(start run|run|retry)$/i })).toHaveCount(0);
  await capture(page, testInfo, "external-unavailable", {
    workItemId: subject.id,
    served: { available: unavailable.body.work_item.external.available, can_start: unavailable.body.work_item.can_start },
    omittedByReadback: "issue 101",
    retained: `issue 102 stayed available: ${stillAvailable.body.work_item.external.available}`,
    executionControl: "none rendered"
  });

  // The project surface now opens the omitted item with no execution entry
  // point: the served availability removes `Start run` for the same item.
  await navigate(page, recorded, `/portal/projects/${projectId}`);
  await page.getByRole("link", { name: subject.title }).click();
  const omittedDetail = isMobile
    ? page.locator("#main-content")
    : page.getByRole("dialog", { name: subject.title });
  await expect(omittedDetail.getByText(/unavailable/i).first()).toBeVisible();
  await expect(omittedDetail.getByRole("button", { name: /^(start run|run|retry)$/i })).toHaveCount(0);
  await page.keyboard.press("Escape");

  await resetFixtureState();
  await assertNoCredentials(page, recorded);
});

test("Azure DevOps repository, board and pipeline resources bind and sync against the fixture documents", async ({ page }, testInfo) => {
  const recorded = recordApiBodies(page);
  await connectionsLogin(page);
  const projectId = await connectionProject(page);
  const connectionName = `Azure resources ${suffix}`;
  await createThroughConsole(page, { provider: "azure_devops", name: connectionName, account: "acme-fixture", capabilities: ["Repositories", "Work items", "CI"] });
  const directory = await api(page, "GET", "/portal/api/connections");
  const connection = directory.body.connections.find((candidate) => candidate.name === connectionName);
  expect(connection, "the connections read must serve this test's connection").toBeTruthy();
  const references = {
    repository: "Platform/symmetry",
    work_tracking: "Platform",
    ci: "Platform/pipeline/7"
  };

  await page.getByRole("button", { name: "Refresh", exact: true }).click();
  await page.waitForLoadState("networkidle");
  await page.getByLabel("Project", { exact: true }).selectOption(projectId);
  await expect(page.getByRole("heading", { name: "Project resources" })).toBeVisible();
  // The console accepts the exact provider references: project/repository,
  // project, and project/pipeline/<id>.
  for (const resource of [
    { kind: "repository", name: "Azure repo", reference: references.repository },
    { kind: "work_tracking", name: "Azure boards", reference: references.work_tracking },
    { kind: "ci", name: "Azure pipeline", reference: references.ci }
  ]) {
    await bindResourceThroughConsole(page, { ...resource, connectionId: connection.id });
    await expect(resourceRow(page, resource.name)).toContainText(resource.reference);
  }
  await expect(resourceRow(page, "Azure repo")).toContainText(/Never synced/);
  await capture(page, testInfo, "azure-resources-bound", {
    references,
    connection: connectionName,
    state: "three kinds bound through the console, never synced"
  });

  await resourceControl(page, "Sync Azure repo").click();
  await expect(resourceRow(page, "Azure repo")).toContainText(/Synced/);
  await resourceControl(page, "Sync Azure boards").click();
  await expect(resourceRow(page, "Azure boards")).toContainText(/Synced/);
  await resourceControl(page, "Sync Azure pipeline").click();
  await expect(resourceRow(page, "Azure pipeline")).toContainText(/Synced/);
  for (const row of ["Azure repo", "Azure boards", "Azure pipeline"]) {
    await expect(resourceRow(page, row)).not.toContainText(/Failed/);
  }

  const repositoryResource = await readBoundResource(page, connection.id, references.repository);
  const boardResource = await readBoundResource(page, connection.id, references.work_tracking);
  const pipelineResource = await readBoundResource(page, connection.id, references.ci);
  expect(repositoryResource.status).toBe("healthy");
  expect(repositoryResource.sync_status).toBe("synced");
  expect(repositoryResource.url).toBe("https://dev.azure.com/acme-fixture/Platform/_git/symmetry");
  expect(repositoryResource.metadata.default_branch).toBe("refs/heads/main");
  expect(boardResource.sync_status).toBe("synced");
  expect(boardResource.url).toBe("https://dev.azure.com/acme-fixture/Platform/_boards");
  expect(pipelineResource.sync_status).toBe("synced");
  await expect(resourceRow(page, "Azure repo")).toContainText(repositoryResource.last_synced_at);
  await expect(resourceRow(page, "Azure boards")).toContainText(boardResource.last_synced_at);
  await expect(resourceRow(page, "Azure pipeline")).toContainText(pipelineResource.last_synced_at);

  // The Boards sync imported the fixture work item with its exact provider
  // identity, and the pipeline document supplied the CI metadata.
  //
  // Fixture limit: the fixture transport answers `_apis/wit/wiql` and
  // `_apis/wit/workitemsbatch` from its own state
  // (`dev_azure_work_item_ids/3`, default `[101]`) without reading the request
  // body, and its `az` handler returns the fixture token for any argument list
  // (`scripts/ci/portal-connection-fixture.exs`). This case therefore pins the
  // served import result - id 101 and its fields - not which ids or which query
  // the server asked for.
  const workspace = await readWorkspace(page, projectId);
  const imported = projectFromWorkspace(workspace, projectId).work_items.filter((item) => item.external?.resource_id === boardResource.id);
  expect(imported.map((item) => item.external.id)).toEqual(["101"]);
  expect(imported[0].external.provider).toBe("azure_devops");
  expect(imported[0].external.url).toBe("https://dev.azure.com/acme-fixture/Platform/_workitems/edit/101");
  expect(imported[0].external.state).toBe("Active");
  expect(imported[0].status).toBe("ready");
  expect(imported[0].external.available).toBe(true);
  expect(imported[0].external.assignee).toBe("Fixture Assignee");
  await capture(page, testInfo, "azure-resources-synced", {
    served: {
      repository: { url: repositoryResource.url, last_synced_at: repositoryResource.last_synced_at },
      work_tracking: { url: boardResource.url, last_synced_at: boardResource.last_synced_at },
      ci: { last_synced_at: pipelineResource.last_synced_at }
    },
    importedExternalId: imported[0].external.id,
    importedExternalUrl: imported[0].external.url
  });

  await assertNoCredentials(page, recorded);
});

test("a failed workspace re-read after a write surfaces the failure instead of the stale rows", async ({ page }, testInfo) => {
  // The aborted workspace read is this flow's injected failure: the browser
  // reports it as a console error like any failed subresource.
  allowBrowserError(page, /net::ERR_INTERNET_DISCONNECTED/);
  const recorded = recordApiBodies(page);
  await connectionsLogin(page);
  const projectId = await connectionProject(page);
  const connectionId = await createConnectionThroughApi(page, {
    name: `Re-read failure ${suffix}`,
    account: "acme-fixture",
    capabilities: ["work_items"]
  });
  const issuesReference = `acme-fixture/re-read-issues-${suffix}`;
  await page.getByRole("button", { name: "Refresh", exact: true }).click();
  await page.waitForLoadState("networkidle");
  await page.getByLabel("Project", { exact: true }).selectOption(projectId);

  // A real write through the console, with its receipt and the post-write row
  // on screen.
  await bindResourceThroughConsole(page, { kind: "work_tracking", name: "Re-read Issues", reference: issuesReference, connectionId });
  const written = resourceRow(page, "Re-read Issues");
  await expect(written).toContainText(issuesReference);
  await expect(page.getByRole("status").filter({ hasText: "Resource attached — Re-read Issues." })).toBeVisible();

  // The next read fails at the network layer: that one read is aborted for the
  // duration of the refresh, so the failure is a real fetch error — no response
  // is substituted and the API and database stay real (an offline emulation
  // would only pause React Query's reads instead of failing them).
  await page.route(/\/portal\/api\/workspace(\?|$)/, (route) => route.abort("internetdisconnected"));
  await page.getByRole("button", { name: "Refresh", exact: true }).click();
  const resources = page.getByRole("region", { name: "Project resources" });
  await expect(resources.getByText("Refresh failed — showing last loaded data")).toBeVisible();
  // The last loaded rows stay visible, and the failed read is the surface's
  // only claim about freshness: the write receipt never stands in for it.
  await expect(resources.getByRole("list", { name: "Project resources" }).getByRole("listitem").filter({ hasText: issuesReference })).toBeVisible();
  await capture(page, testInfo, "failed-re-read", {
    projectId,
    injected: "GET /portal/api/workspace was aborted at the browser's network layer during the refresh; no response was substituted",
    shown: "the failed-refresh state with the last loaded rows"
  });

  // The retry re-reads the real API and the failure clears.
  await page.unroute(/\/portal\/api\/workspace(\?|$)/);
  await resources.getByRole("button", { name: "Try again" }).click();
  await expect(resources.getByText("Refresh failed — showing last loaded data")).toHaveCount(0);
  await expect(written).toContainText(issuesReference);

  await assertNoCredentials(page, recorded);
});
