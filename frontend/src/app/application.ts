import { infiniteQueryOptions, mutationOptions, MutationCache, QueryCache, QueryClient, queryOptions } from "@tanstack/react-query";
import {
  createApi,
  readRetry,
  Unauthenticated,
  type ActionId,
  type Api,
  type ChatMessageRequest,
  type ChatScope,
  type ExecutionGeneration,
} from "../services/api";
import { createRealtime, type RealtimeHandle, type RealtimeSocket } from "../services/realtime";

interface BrowserLocation {
  href: string;
  replace: (url: string) => void;
}

export interface Application {
  api: Api;
  /** The same fetch transport the reads use, for the authenticated command POSTs. */
  fetcher: typeof fetch;
  queryClient: QueryClient;
  /**
   * The operator invalidation client. It is the single owner of change
   * discovery: server invalidations while connected, the polling fallback while
   * not, and a snapshot refetch on every (re)connect.
   */
  realtime: RealtimeHandle;
  /**
   * The authenticated bootstrap hands the short-lived socket token here. It is
   * held in memory only; nothing writes it to storage.
   */
  rememberSocketToken: (token: string) => void;
  auth: {
    getSnapshot: () => boolean;
    subscribe: (listener: () => void) => () => void;
  };
}

export function safeReturnTo(href: string): string {
  try {
    const url = new URL(href);
    if (/^\/portal\/(attention|chat|connections|goals\/[^/]+|projects(?:\/[^/]+)?|runtimes|work\/[^/]+)\/?$/.test(url.pathname)) {
      return `${url.pathname}${url.search}`;
    }
  } catch {
    // Unknown locations recover to the authenticated shell landing page.
  }
  return "/portal/attention";
}

export function createApplication(options: {
  fetcher?: typeof fetch;
  location?: BrowserLocation;
  realtime?: { socketFactory?: (token: () => string) => RealtimeSocket; pollIntervalMs?: number; batchMs?: number; visibility?: () => boolean };
} = {}): Application {
  const fetcher = options.fetcher ?? globalThis.fetch.bind(globalThis);
  const api = createApi(fetcher);
  const location = options.location ?? window.location;
  const listeners = new Set<() => void>();
  let signedOut = false;
  let socketToken = "";
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
  // A connect attempt reads the token synchronously; a refused or dropped
  // connection refreshes it from the same-origin bootstrap so the client's own
  // retry carries a live token. The token is short-lived by design, so it is
  // re-read rather than cached for the session's lifetime.
  const refreshSocketToken = async () => {
    try {
      const session = await api.session(new AbortController().signal);
      socketToken = session.socket_token;
    } catch {
      // A failed refresh leaves the previous token in place; the next attempt
      // refreshes again, and the polling fallback keeps reads converging.
    }
  };
  const realtime = createRealtime({
    queryClient,
    token: () => socketToken,
    refreshToken: () => { void refreshSocketToken(); },
    ...(options.realtime ?? {}),
  });
  return {
    api,
    fetcher,
    queryClient,
    realtime,
    rememberSocketToken: (token: string) => { socketToken = token; },
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
 * The attention entry point. The Goal feed is paginated by the server's opaque
 * cursor and the project blocked items are project-scoped, so each page is its
 * own query key: an older page never overwrites the newest one.
 */
export const attentionOptions = (api: Api, projectId: string | null, cursor: string | null) => queryOptions({
  queryKey: ["attention", { projectId, cursor }],
  queryFn: ({ signal }) => api.attention(projectId, cursor, signal),
});

export const goalOptions = (api: Api, goalId: string) => queryOptions({
  queryKey: ["goal", { goalId }],
  queryFn: ({ signal }) => api.goal(goalId, signal),
});

/**
 * One page of Goal audit receipts, keyed by the served `after` sequence the
 * page starts from. `after = 0` is the first page; later pages are separate keys
 * so an older page never overwrites a newer one.
 */
export const goalEventsOptions = (api: Api, goalId: string, after: number) => queryOptions({
  queryKey: ["goal-events", { goalId, after }],
  queryFn: ({ signal }) => api.goalEvents(goalId, after, signal),
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
  mutationFn: ({ workItemId, actionId, csrf }: { workItemId: string; actionId: ActionId; csrf: string }) =>
    api.startWork(workItemId, actionId, csrf, new AbortController().signal),
});

export const cancelWorkMutation = (api: Api) => mutationOptions({
  mutationKey: ["work-control", "cancel"],
  mutationFn: ({ workItemId, generation, actionId, csrf }: { workItemId: string; generation: ExecutionGeneration; actionId: ActionId; csrf: string }) =>
    api.cancelWork(workItemId, generation, actionId, csrf, new AbortController().signal),
});

export const retryWorkMutation = (api: Api) => mutationOptions({
  mutationKey: ["work-control", "retry"],
  mutationFn: ({ workItemId, generation, actionId, csrf }: { workItemId: string; generation: ExecutionGeneration; actionId: ActionId; csrf: string }) =>
    api.retryWork(workItemId, generation, actionId, csrf, new AbortController().signal),
});

export const provideInputMutation = (api: Api) => mutationOptions({
  mutationKey: ["work-control", "input"],
  mutationFn: ({ workItemId, input, waitingTransitionId, actionId, csrf }: {
    workItemId: string;
    input: Record<string, unknown>;
    waitingTransitionId: string;
    actionId: ActionId;
    csrf: string;
  }) => api.provideInput(workItemId, input, waitingTransitionId, actionId, csrf, new AbortController().signal),
});

/** The scope members one conversation query is keyed and read by. */
export interface ChatScopeKey {
  readonly scope: ChatScope;
  readonly projectId: string | null;
  readonly runId: string | null;
}

/**
 * The scoped conversation read. `staleTime: 0` keeps an explicit refresh and a
 * window-focus refetch honest re-reads of the server, and the key is per scope,
 * so a background update for one scope can never touch another scope's data.
 */
export const chatConversationOptions = (api: Api, key: ChatScopeKey) => queryOptions({
  queryKey: ["chat", key],
  queryFn: ({ signal }) => api.chatConversation(key, signal),
  staleTime: 0,
});

/**
 * One older conversation page, by the served `before` cursor. It stays disabled
 * until the reader asks for it through `Load older`, and the page it returns is
 * retained by the surface rather than by this cursor's own key.
 */
export const chatOlderPageOptions = (api: Api, key: ChatScopeKey, before: string) => queryOptions({
  queryKey: ["chat-older", { ...key, before }],
  queryFn: ({ signal }) => api.chatConversation({ ...key, before }, signal),
  staleTime: 0,
  enabled: false,
});

/**
 * One conversation message. The payload is frozen by the caller before it is
 * sent: a retry passes the same object, so the server replays the recorded
 * message instead of applying a second one.
 */
export const chatMessageMutation = (api: Api) => mutationOptions({
  mutationKey: ["chat-message"],
  mutationFn: ({ payload, csrf }: { payload: ChatMessageRequest; csrf: string }) =>
    api.chatMessage(payload, csrf, new AbortController().signal),
});
