import { expect, test as base } from "@playwright/test";

const errorsByPage = new WeakMap();

export function monitorBrowserErrors(page) {
  if (errorsByPage.has(page)) return;

  const state = { allowed: [], errors: [] };
  errorsByPage.set(page, state);
  page.on("console", (message) => {
    const text = message.text();
    if (message.type() === "error" && !state.allowed.some((pattern) => pattern.test(text))) {
      state.errors.push(`console: ${text}`);
    }
  });
  page.on("pageerror", (error) => state.errors.push(`pageerror: ${error.stack || error.message}`));
}

export function allowBrowserError(page, pattern) {
  monitorBrowserErrors(page);
  errorsByPage.get(page).allowed.push(pattern);
}

export function assertNoBrowserErrors(...pages) {
  const errors = pages.flatMap((page) => errorsByPage.get(page)?.errors || []);
  expect(errors, "browser console and page errors").toEqual([]);
}

export const test = base.extend({
  page: async ({ page }, use) => {
    monitorBrowserErrors(page);
    await use(page);
    assertNoBrowserErrors(page);
  }
});

export { expect };
