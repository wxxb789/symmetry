import "@testing-library/jest-dom/vitest";
import { cleanup } from "@testing-library/react";
import { afterAll, afterEach, beforeAll } from "vitest";
import { portalServer, resetPortalDouble } from "./app/portal-double";

// jsdom implements no matchMedia. The work detail chooses between the desktop
// drawer and the full page on `(min-width: 1024px)`; the stub reports that query
// as matching (desktop) and every subscription is a no-op.
window.matchMedia = (query: string): MediaQueryList => ({
  matches: query.includes("min-width"),
  media: query,
  onchange: null,
  addEventListener: () => undefined,
  removeEventListener: () => undefined,
  addListener: () => undefined,
  removeListener: () => undefined,
  dispatchEvent: () => false,
});

// jsdom implements no window.scrollTo, and the router restores scroll on every
// navigation: without this no-op each call prints a `Not implemented` line.
window.scrollTo = () => undefined;

beforeAll(() => {
  portalServer.listen({ onUnhandledFrame: "error" });
});
afterEach(resetPortalDouble);
afterAll(() => {
  portalServer.close();
});
afterEach(cleanup);
