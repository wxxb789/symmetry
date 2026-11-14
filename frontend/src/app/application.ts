import { QueryCache, QueryClient, queryOptions } from "@tanstack/react-query";
import { createApi, readRetry, Unauthenticated, type Api } from "../services/api";

interface BrowserLocation {
  href: string;
  replace: (url: string) => void;
}

export interface Application {
  api: Api;
  queryClient: QueryClient;
  auth: {
    getSnapshot: () => boolean;
    subscribe: (listener: () => void) => () => void;
  };
}

export function safeReturnTo(href: string): string {
  try {
    const url = new URL(href);
    if (/^\/portal\/(attention|projects(?:\/[^/]+)?)\/?$/.test(url.pathname)) {
      return `${url.pathname}${url.search}`;
    }
  } catch {
    // Unknown locations recover to the authenticated shell landing page.
  }
  return "/portal/attention";
}

export function createApplication(options: { fetcher?: typeof fetch; location?: BrowserLocation } = {}): Application {
  const api = createApi(options.fetcher);
  const location = options.location ?? window.location;
  const listeners = new Set<() => void>();
  let signedOut = false;
  const queryClient = new QueryClient({
    queryCache: new QueryCache({
      onError: (error) => {
        if (!(error instanceof Unauthenticated) || signedOut) return;
        signedOut = true;
        for (const listener of listeners) listener();
        void queryClient.cancelQueries().then(() => {
          queryClient.clear();
          location.replace(`/portal/login?return_to=${encodeURIComponent(safeReturnTo(location.href))}`);
        });
      },
    }),
    defaultOptions: {
      queries: {
        retry: readRetry,
        retryDelay: (attempt) => Math.min(500 * 2 ** attempt, 2_000),
        staleTime: 15_000,
        refetchOnWindowFocus: true,
      },
    },
  });
  return {
    api,
    queryClient,
    auth: {
      getSnapshot: () => signedOut,
      subscribe: (listener: () => void) => {
        listeners.add(listener);
        return () => { listeners.delete(listener); };
      },
    },
  };
}

export const sessionOptions = (api: Api) => queryOptions({
  queryKey: ["session"],
  queryFn: ({ signal }) => api.session(signal),
  staleTime: 0,
  gcTime: 0,
});

export const workspaceOptions = (api: Api, projectId: string | null) => queryOptions({
  queryKey: ["workspace", { projectId }],
  queryFn: ({ signal }) => api.workspace(projectId, signal),
});
