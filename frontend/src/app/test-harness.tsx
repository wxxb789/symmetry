import { QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory, RouterProvider } from "@tanstack/react-router";
import { render } from "@testing-library/react";
import { afterEach, vi } from "vitest";
import type { Application } from "./application";
import { createApplication } from "./application";
import { createAppRouter } from "./router";

/** Mounts one workspace route: the intercepted global fetch answers through the
 * shared portal double, and an unhandled path fails instead of answering. */
export function renderRoute(
  path: string,
  options: { fetcher?: typeof fetch; location?: { href: string; replace: (url: string) => void } } = {},
) {
  const application = createApplication({
    ...(options.fetcher === undefined ? {} : { fetcher: options.fetcher }),
    location: options.location ?? { href: `http://localhost${path}`, replace: vi.fn() },
  });
  const history = createMemoryHistory({ initialEntries: [path] });
  const router = createAppRouter(application, history);
  const { unmount } = render(<QueryClientProvider client={application.queryClient}><RouterProvider router={router} /></QueryClientProvider>);
  return { application, history, unmount, router };
}

export function trackApplications(): Application[] {
  const applications: Application[] = [];
  afterEach(() => {
    for (const application of applications) application.queryClient.clear();
    applications.length = 0;
  });
  return applications;
}
