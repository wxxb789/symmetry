import fs from "node:fs/promises";
import { allowBrowserError, expect, test } from "./fixtures.mjs";

const operatorToken = process.env.SYMMETRY_OPERATOR_TOKEN || "development-operator-token";

// The expiry release is started with a two-second session lifetime
// (SYMMETRY_PORTAL_SESSION_MAX_AGE_SECONDS=2 in CI's render step). The guard is
// three of those lifetimes, and the intervals are tight because the case is
// about the first server read after the TTL, not about a long wait. What is
// waited on is the server's own 401 — never elapsed time standing in for it.
const EXPIRY_GUARD_MS = 6_000;
const EXPIRY_POLL_INTERVALS = [100, 250, 500];

// Records the application's own history writes before its module runs. The
// one-time legacy repair replaces the location once; the router then writes its
// own entry state with an empty URL, which names no location and is not a
// navigation. Counting the writes that do name a location is how a case can
// state "no second redirect" from the app's own behaviour instead of from a
// sleep that a returning redirect would satisfy.
function recordHistoryWrites() {
  window.__historyWrites = [];
  for (const name of ["replaceState", "pushState"]) {
    const original = window.history[name].bind(window.history);
    window.history[name] = (...args) => {
      window.__historyWrites.push({ name, url: String(args[2] ?? "") });
      return original(...args);
    };
  }
}

const historyWrites = (page) => page.evaluate(() => window.__historyWrites);

async function login(page, path = "/portal/attention") {
  await page.goto(`/portal/login?return_to=${encodeURIComponent(path)}`);
  await page.getByLabel("Operator token").fill(operatorToken);
  await page.getByRole("button", { name: "Open workspace" }).click();
  await expect(page.getByRole("button", { name: "Refresh", exact: true })).toBeVisible();
  await expect(page.getByRole("combobox", { name: "Project", exact: true })).toBeVisible();
  await expect(page.getByRole("button", { name: "Search or navigate", exact: true })).toBeVisible();
}

async function api(page, path, body) {
  return page.evaluate(async ({ path, body }) => {
    const csrf = document.querySelector('meta[name="csrf-token"]')?.content;
    const response = await fetch(path, {
      method: "POST",
      credentials: "same-origin",
      headers: { "content-type": "application/json", "x-csrf-token": csrf },
      body: JSON.stringify(body)
    });
    return { status: response.status, body: await response.json() };
  }, { path, body });
}

async function project(page, suffix, withWork = false) {
  const key = `S${Date.now().toString(36).slice(-5)}${suffix}`.toUpperCase().slice(0, 8);
  const name = `Shell acceptance ${key}`;
  const response = await api(page, "/portal/api/projects", { key, name, description: "Real Phoenix workspace fixture" });
  expect(response.status).toBe(201);
  const id = response.body.project.id;
  if (withWork) {
    const work = await api(page, `/portal/api/projects/${id}/work-items`, {
      title: `Review the exact workspace subject ${key}`,
      description: "Read-only shell evidence",
      status: "ready",
      priority: "high",
      assignee_type: "unassigned",
      blocked: true,
      blocker: "Operator review required"
    });
    expect(work.status).toBe(201);
  }
  return { id, name, key };
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

test("signed-out entry, authenticated empty shell, release assets and logout", async ({ page }, testInfo) => {
  const response = await page.goto("/portal/projects");
  expect(response.status()).toBe(200);
  await expect(page).toHaveURL(/\/portal\/login\?return_to=%2Fportal%2Fprojects$/);
  await expect(page.getByLabel("Operator token")).toBeFocused();
  await capture(page, testInfo, "signed-out", "Phoenix login renders, password focus, deep-link return path");
  await page.getByLabel("Operator token").fill(operatorToken);
  await page.getByRole("button", { name: "Open workspace" }).click();
  await expect(page.getByRole("heading", { level: 1, name: "Projects", exact: true })).toBeVisible();
  const empty = await project(page, "E");
  const html = await page.goto(`/portal/projects/${empty.id}`);
  expect(html.headers()["cache-control"]).toContain("no-store");
  await expect(page.getByRole("heading", { level: 1, name: empty.name })).toBeVisible();
  await expect(page.getByRole("heading", { name: "No work items", exact: true })).toBeVisible();
  await capture(page, testInfo, "empty", "Real empty project rendered, no horizontal overflow");
  const asset = await page.locator('script[type="module"]').getAttribute("src");
  expect(asset).toMatch(/^\/portal\/assets\/.*-[\w-]+\.js$/);
  const built = await page.request.get(asset);
  expect(built.status()).toBe(200);
  expect(built.headers()["cache-control"]).toContain("immutable");
  const unknownApi = await page.request.get("/portal/api/not-a-route");
  const unknownAsset = await page.request.get("/portal/assets/not-an-asset.js");
  expect(unknownApi.status()).toBe(404);
  expect(unknownAsset.status()).toBe(404);
  if (!await page.getByRole("button", { name: "Sign out", exact: true }).isVisible())
    await page.getByRole("button", { name: "Open navigation" }).click();
  await page.getByRole("button", { name: "Sign out", exact: true }).click();
  await expect(page).toHaveURL(/\/portal\/login$/);
  expect((await page.request.get("/portal/api/session")).status()).toBe(401);
});

test("project URL, refresh recovery, history and keyboard focus", async ({ page, isMobile }, testInfo) => {
  await login(page);
  const selected = await project(page, "N", true);
  await page.goto("/portal/projects");
  await page.getByLabel("Project", { exact: true }).selectOption(selected.id);
  await expect(page).toHaveURL(new RegExp(`/portal/projects/${selected.id}$`));
  const heading = page.getByRole("heading", { level: 1, name: selected.name });
  await expect(heading).toBeFocused();
  await expect(page.getByText("Operator review required", { exact: false })).toBeVisible();
  await page.reload();
  await expect(page.getByLabel("Project", { exact: true })).toHaveValue(selected.id);
  await expect(heading).toBeVisible();
  await page.getByRole("button", { name: "Refresh", exact: true }).focus();
  await page.getByRole("button", { name: "Refresh", exact: true }).click();
  await expect(page.getByRole("button", { name: "Refresh", exact: true })).toBeFocused();
  await page.keyboard.press("ControlOrMeta+k");
  const palette = page.getByRole("dialog", { name: "Command palette" });
  await expect(palette).toBeVisible();
  const search = palette.getByLabel("Search destinations");
  const close = palette.getByRole("button", { name: "Close command palette" });
  const lastResult = palette.locator(".command-result").last();
  // The initial focus lands inside the panel, on the close button.
  await expect(close).toBeFocused();
  // Tab reaches the search input from the first panel control.
  await page.keyboard.press("Tab");
  await expect(search).toBeFocused();
  // Shift+Tab from the search returns to the close button, and one more
  // Shift+Tab wraps from the first control to the last result.
  await page.keyboard.press("Shift+Tab");
  await expect(close).toBeFocused();
  await page.keyboard.press("Shift+Tab");
  await expect(lastResult).toBeFocused();
  // Tab cycles back into the panel from the last control instead of
  // escaping to the background Project, Refresh or Sign out controls.
  await page.keyboard.press("Tab");
  await expect(close).toBeFocused();
  await page.keyboard.press("Tab");
  await expect(search).toBeFocused();
  await capture(page, testInfo, "keyboard-focus", "Command palette renders and traps keyboard focus; initial focus inside the panel, Tab and Shift+Tab wrap between the first and last panel controls, and focus never reaches the background Project, Refresh or Sign out controls");
  await page.keyboard.press("Escape");
  await expect(page.getByRole("button", { name: "Refresh", exact: true })).toBeFocused();
  if (isMobile) {
    const menu = page.getByRole("button", { name: "Open navigation" });
    await menu.click();
    await expect(page.getByRole("dialog", { name: "Navigation", exact: true })).toBeVisible();
    await page.keyboard.press("Escape");
    await expect(menu).toBeFocused();
    await menu.click();
  }
  await page.getByRole("navigation", { name: "Workspace navigation" }).getByRole("link", { name: "Projects", exact: true }).click();
  await expect(page.getByRole("heading", { level: 1, name: "Projects", exact: true })).toBeFocused();
  await page.goBack();
  await expect(heading).toBeVisible();
  await page.goForward();
  await expect(page.getByRole("heading", { level: 1, name: "Projects", exact: true })).toBeVisible();
  await capture(page, testInfo, "navigation", "Project selection lives in URL, reload/back/forward recover, menu Escape returns focus");
});

test("real response loading, network failure and recovery", async ({ page }, testInfo) => {
  await login(page);
  const { promise: barrier, resolve: release } = Promise.withResolvers();
  await page.route("**/portal/api/workspace*", async (route) => {
    await barrier;
    await route.continue();
  });
  await page.reload();
  await expect(page.getByRole("status").filter({ hasText: "Loading blocked work…" })).toBeVisible();
  await capture(page, testInfo, "loading", "Real workspace response held; accessible loading indicator rendered");
  release();
  await page.unrouteAll({ behavior: "wait" });
  await expect(page.getByRole("button", { name: "Refresh", exact: true })).toBeVisible();
  allowBrowserError(page, /net::ERR_FAILED|Failed to fetch|Failed to load resource/);
  // The attention entry point reads the workspace only for its project scope,
  // so a workspace-only failure there correctly renders the stale variant
  // ("Refresh failed — showing last loaded data") because the attention read
  // itself succeeded. The un-stale failure heading belongs on the projects
  // surface, whose primary read IS the workspace one, so the assertions below
  // move there instead of being weakened.
  await page.goto("/portal/projects");
  await expect(page.getByRole("button", { name: "Refresh", exact: true })).toBeVisible();
  await page.route("**/portal/api/workspace*", (route) => route.abort("failed"));
  await page.reload();
  await expect(page.getByRole("heading", { name: "Unable to load workspace" })).toBeVisible();
  await capture(page, testInfo, "error", "Actual network abort rendered error with retry, no fabricated backend response");
  await page.unrouteAll({ behavior: "wait" });
  await page.getByRole("button", { name: "Try again", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Unable to load workspace" })).not.toBeVisible();
  await expect(page.getByRole("button", { name: "Refresh", exact: true })).toBeVisible();
});

test("Attention scope survives selection and login with fractional resource metadata", async ({ page, isMobile }, testInfo) => {
  await login(page);
  const empty = await project(page, "A");
  const blocked = await project(page, "B", true);
  const resource = await api(page, `/portal/api/projects/${blocked.id}/resources`, {
    kind: "repository", name: "Fractional observation", provider: "manual",
    external_ref: "workspace-read-evidence", metadata: { ratio: 0.5, weight: 1e-3 }
  });
  expect(resource.status).toBe(201);
  await page.goto(`/portal/attention?projectId=${empty.id}`);
  await expect(page.getByRole("heading", { name: "No blocked work", exact: true })).toBeVisible();
  await page.getByLabel("Project", { exact: true }).selectOption(blocked.id);
  await expect(page).toHaveURL(new RegExp(`/portal/attention\\?projectId=${blocked.id}$`));
  await expect(page.getByRole("heading", { level: 1, name: "Attention", exact: true })).toBeVisible();
  await expect(page.getByText("Operator review required", { exact: false })).toBeVisible();
  await page.reload();
  await expect(page.getByLabel("Project", { exact: true })).toHaveValue(blocked.id);
  await page.goBack();
  await expect(page.getByLabel("Project", { exact: true })).toHaveValue(empty.id);
  await expect(page.getByRole("heading", { name: "No blocked work", exact: true })).toBeVisible();
  await page.goForward();
  await expect(page.getByText("Operator review required", { exact: false })).toBeVisible();
  await page.goto(`/portal/projects/${blocked.id}`);
  if (isMobile) await page.getByRole("button", { name: "Open navigation" }).click();
  await page.getByRole("navigation", { name: "Workspace navigation" }).getByRole("link", { name: "Attention", exact: true }).click();
  await expect(page).toHaveURL(new RegExp(`/portal/attention\\?projectId=${blocked.id}$`));
  await page.goto(`/portal/projects/${blocked.id}`);
  // After a full page load the project heading renders before the shell
  // mounts; wait for the shell's palette trigger before using the shortcut.
  await page.getByRole("button", { name: "Search or navigate", exact: true }).waitFor();
  await page.keyboard.press("ControlOrMeta+k");
  await page.getByRole("dialog", { name: "Command palette" }).getByRole("link", { name: "Attention", exact: true }).click();
  await expect(page).toHaveURL(new RegExp(`/portal/attention\\?projectId=${blocked.id}$`));
  await page.getByRole("button", { name: "Sign out", exact: true }).click();
  // The shell awaits the current session token before the native logout POST;
  // wait for the logout navigation to finish before following the deep link.
  await expect(page).toHaveURL(/\/portal\/login$/);
  const destination = `/portal/attention?projectId=${blocked.id}`;
  await page.goto(destination);
  await expect(page).toHaveURL(/\/portal\/login\?return_to=/);
  expect(new URL(page.url()).searchParams.get("return_to")).toBe(destination);
  await page.getByLabel("Operator token").fill(operatorToken);
  await page.getByRole("button", { name: "Open workspace" }).click();
  await expect(page.getByText("Operator review required", { exact: false })).toBeVisible();
  expect(new URL(page.url()).pathname + new URL(page.url()).search).toBe(destination);
  await capture(page, testInfo, "attention-scope", "Attention selection, history, nav, palette and unauthenticated deep link preserve the project; real resource metadata contains fractional numbers");
});

test("existing tab refreshes logout CSRF after shared-cookie login on explicit visibility notification", async ({ page, context }, testInfo) => {
  await login(page);
  const originalDocument = await page.evaluate(() => performance.timeOrigin);
  const other = await context.newPage();
  await other.goto("/portal/attention");
  await other.getByRole("button", { name: "Sign out", exact: true }).click();
  await expect(other).toHaveURL(/\/portal\/login$/);
  await login(other);
  const refreshed = page.waitForResponse((response) => response.url().endsWith("/portal/api/session") && response.status() === 200);
  await other.close();
  await page.bringToFront();
  // Automated pages may stay visible even after native window minimization.
  // Exercise the visibility notification seam; cookies, refetch and logout stay real.
  await page.evaluate(() => window.dispatchEvent(new Event("visibilitychange")));
  const bootstrap = await (await refreshed).json();
  await expect.poll(async () => (await page.locator('form[action="/portal/logout"] input[name="_csrf_token"]').inputValue()) === bootstrap.csrf_token).toBe(true);
  expect(await page.evaluate(() => performance.timeOrigin)).toBe(originalDocument);
  await page.getByRole("button", { name: "Sign out", exact: true }).click();
  await expect(page).toHaveURL(/\/portal\/login$/);
  await capture(page, testInfo, "fresh-csrf", "Shared-cookie tab renewed the session; explicit visibility notification triggered real bootstrap refetch and logout succeeded without a reload; native background visibility is not claimed");
});

test("the legacy hash repair lands on the new route exactly once", async ({ page }, testInfo) => {
  await login(page);
  const selected = await project(page, "C");
  const documentLoads = [];
  const recordLoad = (request) => {
    if (request.resourceType() === "document") documentLoads.push(request.url());
  };
  page.on("request", recordLoad);
  await page.addInitScript(recordHistoryWrites);
  await page.goto(`/portal?project_id=${selected.id}#board`);
  await expect(page).toHaveURL(new RegExp(`/portal/projects/${selected.id}$`));
  // The destination's own read has rendered its subject: the repair has landed
  // and no navigation of it is still in flight behind the render.
  await expect(page.getByRole("heading", { level: 1, name: selected.name })).toBeVisible();
  const destination = `/portal/projects/${selected.id}`;
  // Exactly once: one document load, and no redirect after it.
  expect(documentLoads).toHaveLength(1);
  page.off("request", recordLoad);
  // The retired presentation is gone: neither its document nor its assets exist.
  expect((await page.request.get("/portal/classic")).status()).toBe(404);
  expect((await page.request.get("/assets/portal.js")).status()).toBe(404);
  // The last observation point of the case, after two live requests the app
  // served: the URL, the document count, the page's own navigation entries and
  // the app's own history writes are all read here. A 300 ms sleep used to be
  // the whole evidence for "no further redirect", which made that clause a
  // property of the clock — and a second redirect that returned to this URL
  // satisfied it. The written locations are counted instead: exactly one, the
  // repair's own replacement.
  await expect(page).toHaveURL(new RegExp(`/portal/projects/${selected.id}$`));
  expect(documentLoads).toHaveLength(1);
  expect(await page.evaluate(() => performance.getEntriesByType("navigation").length)).toBe(1);
  expect((await historyWrites(page)).filter((write) => write.url !== "").map((write) => write.url))
    .toEqual([destination]);
  await capture(page, testInfo, "legacy-hash-repair", "The legacy project board hash landed on /portal/projects/<id> with one document load, one history replacement and no second redirect; the retired classic document and asset answer 404");
});

test("signed-out legacy project board link survives real login onto its new route", async ({ page }, testInfo) => {
  await login(page);
  const selected = await project(page, "L");
  await page.getByRole("button", { name: "Sign out", exact: true }).click();
  // The shell awaits the current session token before the native logout POST;
  // wait for the logout navigation to finish before following the legacy link.
  await expect(page).toHaveURL(/\/portal\/login$/);
  const destination = `/portal?project_id=${selected.id}#board`;
  await page.goto(destination);
  await expect(page.getByLabel("Operator token")).toBeVisible();
  expect(new URL(page.url()).searchParams.get("return_to")).toBe(destination.split("#")[0]);
  await page.getByLabel("Operator token").fill(operatorToken);
  await page.getByRole("button", { name: "Open workspace" }).click();
  await expect(page).toHaveURL(new RegExp(`/portal/projects/${selected.id}$`));
  await expect(page.getByRole("heading", { level: 1, name: selected.name })).toBeVisible();
  await capture(page, testInfo, "legacy-login-scope", "Real logout, the original legacy project board link and login recover the named project on /portal/projects/<id> through the one-time hash repair");
});

test("invalid credentials and a missing CSRF header are refused by the real server", async ({ page }) => {
  allowBrowserError(page, /status of 401/);
  allowBrowserError(page, /status of 403/);
  await page.goto("/portal/login");
  await page.getByLabel("Operator token").fill("invalid-token");
  await page.getByRole("button", { name: "Open workspace" }).click();
  await expect(page.getByRole("alert")).toContainText("Operator token is invalid");

  await login(page);
  // The migrated session-boundary probe: an authenticated same-origin POST
  // without the session's CSRF header is refused by the server, not by a stub.
  const withoutCsrf = await page.evaluate(async () => {
    const response = await fetch("/portal/api/projects", {
      method: "POST",
      credentials: "same-origin",
      headers: { "Content-Type": "application/json", Accept: "application/json" },
      body: JSON.stringify({ name: "Rejected", key: "NOPE" })
    });
    return response.status;
  });
  expect(withoutCsrf).toBe(403);
});

test("expired authentication clears private content and recovers the deep link", async ({ page }, testInfo) => {
  test.skip(!process.env.SYMMETRY_SHELL_EXPIRY_URL, "Requires a real release with a two-second session lifetime.");
  const expiry = process.env.SYMMETRY_SHELL_EXPIRY_URL;
  allowBrowserError(page, /401|Unauthorized|Failed to load resource/);
  await page.goto(`${expiry}/portal/login?return_to=%2Fportal%2Fprojects`);
  await page.getByLabel("Operator token").fill(operatorToken);
  await page.getByRole("button", { name: "Open workspace" }).click();
  await expect(page.getByRole("heading", { level: 1, name: "Projects", exact: true })).toBeVisible();
  await expect.poll(async () => (await page.request.get(`${expiry}/portal/api/session`)).status(), {
    timeout: EXPIRY_GUARD_MS, intervals: EXPIRY_POLL_INTERVALS
  }).toBe(401);
  await page.getByRole("button", { name: "Refresh", exact: true }).click();
  await expect(page).toHaveURL(new RegExp(`${expiry}/portal/login\\?return_to=%2Fportal%2Fprojects$`));
  await expect(page.locator("#content-heading")).toHaveCount(0);
  await capture(page, testInfo, "expired-auth", "Real server TTL expired; private workspace removed and safe return path retained");
  await page.getByLabel("Operator token").fill(operatorToken);
  await page.getByRole("button", { name: "Open workspace" }).click();
  await expect(page.getByRole("heading", { level: 1, name: "Projects", exact: true })).toBeVisible();
});
