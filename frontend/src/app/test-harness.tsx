import { QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory, RouterProvider } from "@tanstack/react-router";
import { render } from "@testing-library/react";
import { afterEach, vi } from "vitest";
import type { Application } from "./application";
import { createApplication } from "./application";
import { createAppRouter } from "./router";

/**
 * Renders one workspace route against a caller-supplied transport. Test-only:
 * the fetcher receives exactly the requests the shell makes, so a mock that
 * answers a path the surface does not call fails the assertion instead of
 * silently passing.
 */
export function renderRoute(
  path: string,
  fetcher: typeof fetch,
  options: { location?: { href: string; replace: (url: string) => void } } = {},
) {
  const application = createApplication({ fetcher, location: options.location ?? { href: `http://localhost${path}`, replace: vi.fn() } });
  const history = createMemoryHistory({ initialEntries: [path] });
  const router = createAppRouter(application, history);
  const { unmount } = render(<QueryClientProvider client={application.queryClient}><RouterProvider router={router} /></QueryClientProvider>);
  return { application, history, unmount, router };
}

export function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } });
}

export function requestPath(input: RequestInfo | URL): string {
  return input instanceof Request ? input.url : String(input);
}

type Fetcher = (input: RequestInfo | URL, init?: RequestInit) => Promise<Response>;

type ResponseHandler = (path: string, init?: RequestInit) => Response | Promise<Response>;

export function recordingFetcher(handler: ResponseHandler) {
  const calls: { path: string; init: RequestInit | undefined }[] = [];
  const fetcher = vi.fn<Fetcher>(async (input, init) => {
    const path = requestPath(input);
    calls.push({ path, init });
    return handler(path, init);
  });
  return { fetcher, calls };
}

/**
 * Applications a suite's render helpers created, with every query client
 * cleared after the case so a write that settles late cannot leak into the
 * next one.
 */
export function trackApplications(): Application[] {
  const applications: Application[] = [];
  afterEach(() => {
    for (const application of applications) application.queryClient.clear();
    applications.length = 0;
  });
  return applications;
}
