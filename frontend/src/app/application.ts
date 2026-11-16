import { infiniteQueryOptions, mutationOptions, MutationCache, QueryCache, QueryClient, queryOptions } from "@tanstack/react-query";
import {
  createApi,
  readRetry,
  Unauthenticated,
  type ActionId,
  type Api,
  type ExecutionGeneration,
} from "../services/api";

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
    if (/^\/portal\/(attention|connections|projects(?:\/[^/]+)?|work\/[^/]+|runtimes)\/?$/.test(url.pathname)) {
      return `${url.pathname}${url.search}`;
    }
  } catch {
    // Unknown locations recover to the authenticated shell landing page.
  }
  return "/portal/attention";
}

export function createApplication(options: { fetcher?: typeof fetch; location?: BrowserLocation; csrfToken?: () => string } = {}): Application {
  const csrfToken = options.csrfToken ?? (() => document.querySelector<HTMLMetaElement>("meta[name='csrf-token']")?.content ?? "");
  const api = createApi(options.fetcher, csrfToken);
  const location = options.location ?? window.location;
  const listeners = new Set<() => void>();
  let signedOut = false;
  // Session expiry is one recovery for reads and for commands: cancel in-flight
  // work, drop the private cache, then return to the login page with a safe path.
  const expireSession = () => {
    if (signedOut) return;
    signedOut = true;
    for (const listener of listeners) listener();
    void queryClient.cancelQueries().then(() => {
      queryClient.clear();
      location.replace(`/portal/login?return_to=${encodeURIComponent(safeReturnTo(location.href))}`);
    });
  };
  const queryClient = new QueryClient({
    queryCache: new QueryCache({
      onError: (error) => {
        if (error instanceof Unauthenticated) expireSession();
      },
    }),
    // The cache-level handler runs for every mutation, including the ones that
    // supply their own onError: a command refusal must expire the session even
    // when its mutation also re-reads an inactive query.
    mutationCache: new MutationCache({
      onError: (error) => {
        if (error instanceof Unauthenticated) expireSession();
      },
    }),
    defaultOptions: {
      queries: {
        retry: readRetry,
        retryDelay: (attempt) => Math.min(500 * 2 ** attempt, 2_000),
        staleTime: 15_000,
        refetchOnWindowFocus: true,
      },
      mutations: {
        // Mutation retries default off; an explicit retry reuses the frozen
        // normalized intent, and 401/403/409/422 are never retried.
        retry: false,
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

export const workDetailOptions = (api: Api, workItemId: string) => queryOptions({
  queryKey: ["work-item", { workItemId }],
  queryFn: ({ signal }) => api.workDetail(workItemId, signal),
});

/**
 * Execution history. The newest page arrives with the detail read, so this query
 * stays disabled until the reader asks for older entries through the served
 * `before` cursor.
 */
export const workTimelineOptions = (api: Api, workItemId: string, cursor: string | null) => infiniteQueryOptions({
  // The served boundary is part of the identity: a refreshed detail re-keys the
  // disabled query, so its page param restarts from the newly served cursor.
  queryKey: ["work-item-timeline", { workItemId, cursor }],
  queryFn: ({ signal, pageParam }) => api.workTimeline(workItemId, pageParam, signal),
  initialPageParam: cursor,
  getNextPageParam: (lastPage) => lastPage.next_before,
  enabled: false,
});

export const connectionsOptions = (api: Api) => queryOptions({
  queryKey: ["connections"],
  queryFn: ({ signal }) => api.connections(signal),
});

/**
 * Runtime and machine rows for `/portal/runtimes`. The workspace read already
 * carries both runtime projections, so this reuses the shell's own query key
 * for the selected project and adds no request when that workspace is warm.
 * The rows are the runtimes this deployment would accept work for, which is
 * what the unselected read's compatibility filter means.
 */
export const runtimesOptions = (api: Api, projectId: string | null) => workspaceOptions(api, projectId);

/**
 * Execution controls. One attempt per call: Effect performs one HTTP attempt and
 * these options disable retries, so a mutation is never silently re-sent. A
 * caller that must try again passes the same action id.
 */
export const startWorkMutation = (api: Api) => mutationOptions({
  mutationKey: ["work-control", "start"],
  mutationFn: ({ workItemId, actionId }: { workItemId: string; actionId: ActionId }) =>
    api.startWork(workItemId, actionId, new AbortController().signal),
});

export const cancelWorkMutation = (api: Api) => mutationOptions({
  mutationKey: ["work-control", "cancel"],
  mutationFn: ({ workItemId, generation, actionId }: { workItemId: string; generation: ExecutionGeneration; actionId: ActionId }) =>
    api.cancelWork(workItemId, generation, actionId, new AbortController().signal),
});

export const retryWorkMutation = (api: Api) => mutationOptions({
  mutationKey: ["work-control", "retry"],
  mutationFn: ({ workItemId, generation, actionId }: { workItemId: string; generation: ExecutionGeneration; actionId: ActionId }) =>
    api.retryWork(workItemId, generation, actionId, new AbortController().signal),
});

export const provideInputMutation = (api: Api) => mutationOptions({
  mutationKey: ["work-control", "input"],
  mutationFn: ({ workItemId, input, waitingTransitionId, actionId }: {
    workItemId: string;
    input: Record<string, unknown>;
    waitingTransitionId: string;
    actionId: ActionId;
  }) => api.provideInput(workItemId, input, waitingTransitionId, actionId, new AbortController().signal),
});
