import "@testing-library/jest-dom/vitest";
import { cleanup } from "@testing-library/react";
import { afterEach } from "vitest";

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

afterEach(cleanup);
