import fs from "node:fs/promises";
import { allowBrowserError, expect, test } from "./fixtures.mjs";

const operatorToken = process.env.SYMMETRY_OPERATOR_TOKEN || "development-operator-token";

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
  await expect(page.getByRole("dialog", { name: "Command palette" })).toBeVisible();
  await capture(page, testInfo, "keyboard-focus", "Command palette renders and traps keyboard focus");
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
  let release;
  const barrier = new Promise((resolve) => { release = resolve; });
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
  await page.keyboard.press("ControlOrMeta+k");
  await page.getByRole("dialog", { name: "Command palette" }).getByRole("link", { name: "Attention", exact: true }).click();
  await expect(page).toHaveURL(new RegExp(`/portal/attention\\?projectId=${blocked.id}$`));
  await page.getByRole("button", { name: "Sign out", exact: true }).click();
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

test("classic and legacy hashes use separate documents without losing existing work", async ({ page }, testInfo) => {
  await login(page);
  const selected = await project(page, "C");
  await page.goto(`/portal?project_id=${selected.id}#board`);
  await expect(page).toHaveURL(new RegExp(`/portal/classic\\?project_id=${selected.id}#board$`));
  await expect(page.locator("#project-title")).toHaveText(selected.name);
  await expect(page.getByRole("button", { name: "New work item", exact: true })).toBeVisible();
  await expect(page.locator('script[src="/assets/portal.js"]')).toHaveCount(1);
  await expect(page.locator('script[type="module"]')).toHaveCount(0);
  await page.goto("/portal/attention");
  await expect(page.getByRole("heading", { name: "Attention", exact: true, level: 1 })).toBeVisible();
  await expect(page.locator('script[src="/assets/portal.js"]')).toHaveCount(0);
  await capture(page, testInfo, "separate-documents", "Classic retains existing controls; only one workspace runtime mounted in each document");
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
    timeout: 6000, intervals: [100, 250, 500]
  }).toBe(401);
  await page.getByRole("button", { name: "Refresh", exact: true }).click();
  await expect(page).toHaveURL(new RegExp(`${expiry}/portal/login\\?return_to=%2Fportal%2Fprojects$`));
  await expect(page.locator("#content-heading")).toHaveCount(0);
  await capture(page, testInfo, "expired-auth", "Real server TTL expired; private workspace removed and safe return path retained");
  await page.getByLabel("Operator token").fill(operatorToken);
  await page.getByRole("button", { name: "Open workspace" }).click();
  await expect(page.getByRole("heading", { level: 1, name: "Projects", exact: true })).toBeVisible();
});
