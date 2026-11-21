import { http, HttpResponse, type JsonBodyType } from "msw";
import { setupServer } from "msw/node";
import type { WorkItem } from "../services/api";
import { readAttention, readWorkDetail, type ReadAttentionOptions } from "../services/api.fixtures";
import { asRecord } from "../services/served";

/**
 * The one in-memory portal double every frontend suite drives. A suite seeds the
 * served state, scripts per-case answers with `serve`, and reads what the surface
 * sent from `requests`. A path with no seed-derived answer is either registered
 * here as scripted-only (an unscripted call fails as a transport error) or
 * absent, in which case MSW reports an unhandled request and the call reaches
 * the real network — a case that stops answering a path reddens either way.
 */

export type PortalWorkItem = WorkItem & {
  goal_managed?: boolean;
};

export interface PortalProject {
  id: string;
  key: string;
  name: string;
  description: string | null;
  status: string;
  version?: number;
  default_agent_profile?: string | null;
  default_workspace?: string | null;
  resources?: unknown[];
  work_items?: PortalWorkItem[];
  /** creation_id -> work item id: the server's own creation identity index. */
  creation_ids?: Record<string, string>;
}

export interface PortalRequest {
  method: string;
  path: string;
  url: URL;
  body: Record<string, unknown> | null;
  rawBody: string | null;
  headers: Record<string, string>;
}

export type PortalResolver = (request: PortalRequest) => Response | undefined | Promise<Response | undefined>;

export interface PortalSeed {
  session: { csrfToken: string; socketToken: string | null };
  workspace: {
    /** Absent means "the first seeded project"; an explicit null serves no selection. */
    selectedProjectId?: string | null;
    projects: PortalProject[];
    runtimes?: unknown[];
    registeredRuntimes?: unknown[];
    health?: unknown;
  };
  connections: { providers: unknown[]; connections: unknown[] };
  attention: {
    goals?: ReadAttentionOptions["goals"];
    blocked?: ReadAttentionOptions["blocked"];
    nextCursor?: string | null;
    goalUnavailable?: { code: string; message: string } | null;
  };
  details: Record<string, Record<string, unknown>>;
  timeline: Record<string, Record<string, unknown>>;
  mutations: { failNext: boolean; loseNextCreateResponse: boolean; hold: Promise<void> | null };
}

export type PortalSeedInput = Partial<Omit<PortalSeed, "mutations" | "workspace">> & {
  workspace?: Partial<Omit<PortalSeed["workspace"], "projects">> & { projects?: PortalProject[] };
  mutations?: Partial<PortalSeed["mutations"]>;
};

export type PortalResponder = (request: PortalRequest, state: PortalSeed) => Response;

interface PortalEntry {
  method: "GET" | "POST" | "PATCH" | "DELETE";
  path: string;
  respond?: PortalResponder;
}

export interface PortalDouble {
  seed(seed: PortalSeedInput): void;
  readonly state: PortalSeed;
  readonly requests: PortalRequest[];
  sent(method: string, path: string): PortalRequest[];
  serve(resolver: PortalResolver): void;
  /** Holds every mutation until the returned release runs. */
  holdNextMutation(): () => void;
}

export function jsonResponse(body: JsonBodyType, status = 200): Response {
  return HttpResponse.json(body, { status });
}

const createdProjectId = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb";
const createdWorkItemId = "cccccccc-cccc-4ccc-8ccc-cccccccccccc";

const defaultSeed = (): PortalSeed => ({
  session: { csrfToken: "session-token", socketToken: null },
  workspace: { projects: [] },
  connections: { providers: [], connections: [] },
  attention: {},
  details: {},
  timeline: {},
  mutations: { failNext: false, loseNextCreateResponse: false, hold: null },
});

const mergedSeed = (seed: PortalSeedInput): PortalSeed => {
  const base = defaultSeed();
  return {
    session: { ...base.session, ...seed.session },
    workspace: {
      ...base.workspace,
      ...seed.workspace,
      projects: seed.workspace?.projects ?? [],
    },
    connections: { ...base.connections, ...seed.connections },
    attention: { goals: [], ...seed.attention },
    details: { ...seed.details },
    timeline: { ...seed.timeline },
    mutations: { ...base.mutations, ...seed.mutations },
  };
};

/** Served state is per-case mutable, so the file's seed is kept out of reach. */
const cloneSeed = (seed: PortalSeed): PortalSeed => ({
  ...structuredClone({ ...seed, mutations: undefined }),
  mutations: { failNext: false, loseNextCreateResponse: false, hold: null },
});

const text = (value: unknown, fallback: string): string => (typeof value === "string" ? value : fallback);

const notFound = () => jsonResponse({ error: { code: "not_found", message: "not found" } }, 404);

const stale = () => jsonResponse({ error: { code: "stale", message: "resource changed since it was loaded" } }, 409);

const validationFailed = (fields: Record<string, string[]>) =>
  jsonResponse({ error: { code: "validation_failed", message: "request contains invalid fields", fields } }, 422);

const goalAuthorityRequired = () =>
  jsonResponse({ error: { code: "goal_authority_required", message: "Goal authority is required for this operation" } }, 403);

const itemOf = (state: PortalSeed, id: string): { project: PortalProject; item: PortalWorkItem } | null => {
  for (const project of state.workspace.projects) {
    const item = (project.work_items ?? []).find((candidate) => candidate.id === id);
    if (item !== undefined) return { project, item };
  }
  return null;
};

const projectOf = (state: PortalSeed, id: string): PortalProject | undefined =>
  state.workspace.projects.find((candidate) => candidate.id === id);

const selectedProjectId = (request: PortalRequest, state: PortalSeed): string | null =>
  request.url.searchParams.get("project_id") ??
  (state.workspace.selectedProjectId === undefined ? state.workspace.projects[0]?.id ?? null : state.workspace.selectedProjectId);

const workspaceBody = (request: PortalRequest, state: PortalSeed) => {
  const selected = selectedProjectId(request, state);
  return {
    selected_project_id: selected,
    // The server serves the project directory plus the selected project's own
    // rows, so a switch cannot render the previous project's work.
    projects: state.workspace.projects.map((project) => ({
      ...project,
      resources: project.id === selected ? project.resources ?? [] : [],
      work_items: project.id === selected ? project.work_items ?? [] : [],
    })),
    ...(state.workspace.runtimes === undefined ? {} : { runtimes: state.workspace.runtimes }),
    ...(state.workspace.registeredRuntimes === undefined ? {} : { registered_runtimes: state.workspace.registeredRuntimes }),
    ...(state.workspace.health === undefined ? {} : { health: state.workspace.health }),
  };
};

const attentionBody = (request: PortalRequest, state: PortalSeed) => {
  const selected = selectedProjectId(request, state);
  const scoped = selected === null ? state.workspace.projects : state.workspace.projects.filter((project) => project.id === selected);
  const blocked =
    state.attention.blocked ??
    scoped.flatMap((project) =>
      (project.work_items ?? [])
        .filter((item) => item.blocked)
        .map((item) => ({
          projectId: project.id,
          projectName: project.name,
          workItemId: item.id,
          key: item.key,
          title: item.title,
          status: item.status,
          blocker: item.blocker,
        })),
    );
  return readAttention({
    selectedProjectId: selected,
    goals: state.attention.goals ?? [],
    blocked,
    nextCursor: state.attention.nextCursor ?? null,
    goalUnavailable: state.attention.goalUnavailable ?? null,
  });
};

const pathParam = (request: PortalRequest, pattern: RegExp): string => pattern.exec(request.url.pathname)?.[1] ?? "";

const workItemPattern = /^\/portal\/api\/work-items\/([^/]+)$/;
const projectPattern = /^\/portal\/api\/projects\/([^/]+)$/;
const createWorkItemPattern = /^\/portal\/api\/projects\/([^/]+)\/work-items$/;
const movePattern = /^\/portal\/api\/work-items\/([^/]+)\/move$/;

const createProject = (request: PortalRequest, state: PortalSeed): Response => {
  const body = request.body;
  const key = text(body?.key, "");
  if (state.workspace.projects.some((project) => project.key === key)) {
    return validationFailed({ key: ["has already been taken"] });
  }
  const created: PortalProject = {
    id: createdProjectId,
    key,
    name: text(body?.name, ""),
    description: text(body?.description, ""),
    status: "active",
    version: 1,
    default_agent_profile: text(body?.default_agent_profile, "default"),
    default_workspace: text(body?.default_workspace, "primary"),
    resources: [],
    creation_ids: {},
    work_items: [],
  };
  state.workspace.projects = [...state.workspace.projects, created];
  return jsonResponse({ project: { id: created.id } }, 201);
};

const updateProject = (request: PortalRequest, state: PortalSeed): Response => {
  const project = projectOf(state, pathParam(request, projectPattern));
  if (project === undefined) return notFound();
  const body = request.body;
  if (body?.version !== project.version) return stale();
  if (project.status === "archived" && Object.keys(body ?? {}).some((key) => key !== "version" && key !== "status")) {
    return jsonResponse({ error: { code: "state_conflict", message: "state has already advanced" } }, 409);
  }
  if (typeof body?.name === "string") project.name = body.name;
  if (typeof body?.description === "string") project.description = body.description;
  if (typeof body?.status === "string") project.status = body.status;
  project.version = (project.version ?? 0) + 1;
  return jsonResponse({ project: { id: project.id } });
};

const createWorkItem = (request: PortalRequest, state: PortalSeed): Response => {
  const project = projectOf(state, pathParam(request, createWorkItemPattern));
  if (project === undefined) return notFound();
  const body = request.body;
  const title = text(body?.title, "");
  if (title.trim() === "") return validationFailed({ title: ["can't be blank"] });
  // The server's creation identity: a replayed identity answers with the row the
  // original submission created instead of inserting a second one.
  const creationId = typeof body?.creation_id === "string" ? body.creation_id : "";
  const replayedId = creationId === "" ? undefined : project.creation_ids?.[creationId];
  if (replayedId !== undefined) return jsonResponse({ disposition: "replayed", work_item: { id: replayedId } });
  const created: PortalWorkItem = {
    id: createdWorkItemId,
    key: "ONE-1",
    project_id: project.id,
    title,
    description: null,
    status: "backlog",
    priority: "medium",
    position: 0,
    blocked: false,
    blocker: null,
    version: 1,
    assignee: { type: "unassigned", name: null, agent_profile: null },
    delivery: {
      pull_request: { value: null, source: "none", generation: null, url: null, status: null },
      ci: { value: "unknown", source: "none", generation: null, url: null, status: "unknown" },
      review: { value: "none", source: "none", generation: null, url: null, status: "none" },
    },
    execution: null,
    external: null,
    updated_at: null,
    ...(typeof body?.status === "string" ? { status: body.status } : {}),
  };
  project.work_items = [...(project.work_items ?? []), created];
  if (creationId !== "") {
    project.creation_ids = { ...project.creation_ids, [creationId]: created.id };
  }
  if (state.mutations.loseNextCreateResponse) {
    state.mutations.loseNextCreateResponse = false;
    // Committed on the double; the answer dies at the transport.
    return HttpResponse.error();
  }
  return jsonResponse({ disposition: "created", work_item: { id: created.id } }, 201);
};

const moveWorkItem = (request: PortalRequest, state: PortalSeed): Response => {
  const found = itemOf(state, pathParam(request, movePattern));
  if (found === null) return notFound();
  if (found.item.goal_managed === true) return goalAuthorityRequired();
  const body = request.body;
  if (body?.version !== found.item.version) return stale();
  found.item.status = text(body?.status, found.item.status);
  found.item.position = body?.before_id === null ? 99 : 0;
  found.item.version = (found.item.version ?? 0) + 1;
  return jsonResponse({ work_item: { id: found.item.id } });
};

const updateWorkItem = (request: PortalRequest, state: PortalSeed): Response => {
  const found = itemOf(state, pathParam(request, workItemPattern));
  if (found === null) return notFound();
  const body = request.body;
  if (body !== null && ("status" in body || "position" in body)) {
    return validationFailed({ status: ["must be changed through a Kanban move"] });
  }
  if (found.item.goal_managed === true) return goalAuthorityRequired();
  if (body?.version !== found.item.version) return stale();
  const owned = found.item.external?.provider_owned_fields ?? [];
  if (owned.some((field) => field in (body ?? {}))) {
    return jsonResponse({ error: { code: "provider_owned", message: "field is authoritative in the external provider" } }, 409);
  }
  if (body?.blocked === true && !text(body.blocker, "").trim()) {
    return validationFailed({ blocker: ["must be present when blocked"] });
  }
  Object.assign(found.item, Object.fromEntries(Object.entries(body ?? {}).filter(([key]) => key !== "version")));
  found.item.version = (found.item.version ?? 0) + 1;
  return jsonResponse({ work_item: { id: found.item.id } });
};

const readWorkItemDetail = (request: PortalRequest, state: PortalSeed): Response => {
  const id = pathParam(request, workItemPattern);
  const found = itemOf(state, id);
  const projectId = found?.project.id ?? state.workspace.selectedProjectId ?? state.workspace.projects[0]?.id ?? "";
  const served =
    found === null
      ? { id, projectId, title: "Item" }
      : { id, projectId: found.project.id, title: found.item.title, status: found.item.status };
  return jsonResponse(state.details[id] ?? readWorkDetail(served));
};

const readTimeline = (request: PortalRequest, state: PortalSeed): Response =>
  jsonResponse(state.timeline[pathParam(request, workItemPattern)] ?? { items: [], next_before: null });

const readConversation = (request: PortalRequest): Response => {
  const scope = request.url.searchParams.get("scope") ?? "workspace";
  return jsonResponse({
    scope,
    project_id: scope === "project" ? request.url.searchParams.get("project_id") : null,
    run_id: scope === "run" ? request.url.searchParams.get("run_id") : null,
    messages: [],
    runs: [],
    next_before: null,
  });
};

/** Answers a path a case must script: an unscripted call fails as a transport error. */
const scriptedOnly = (): Response => HttpResponse.error();

const entries: PortalEntry[] = [
  {
    method: "GET",
    path: "/portal/api/session",
    respond: (_request, state) =>
      jsonResponse({
        csrf_token: state.session.csrfToken,
        ...(state.session.socketToken === null ? {} : { socket_token: state.session.socketToken }),
      }),
  },
  { method: "GET", path: "/portal/api/workspace", respond: (request, state) => jsonResponse(workspaceBody(request, state)) },
  { method: "GET", path: "/portal/api/connections", respond: (_request, state) => jsonResponse({ providers: state.connections.providers, connections: state.connections.connections }) },
  { method: "GET", path: "/portal/api/attention", respond: (request, state) => jsonResponse(attentionBody(request, state)) },
  { method: "GET", path: "/portal/api/chat", respond: (request) => readConversation(request) },
  { method: "GET", path: "/portal/api/work-items/:id", respond: readWorkItemDetail },
  { method: "GET", path: "/portal/api/work-items/:id/timeline", respond: readTimeline },
  { method: "POST", path: "/portal/api/projects", respond: createProject },
  { method: "PATCH", path: "/portal/api/projects/:id", respond: updateProject },
  { method: "POST", path: "/portal/api/projects/:id/work-items", respond: createWorkItem },
  { method: "PATCH", path: "/portal/api/work-items/:id", respond: updateWorkItem },
  { method: "PATCH", path: "/portal/api/work-items/:id/move", respond: moveWorkItem },
  { method: "POST", path: "/portal/api/connections", respond: scriptedOnly },
  { method: "PATCH", path: "/portal/api/connections/:id", respond: scriptedOnly },
  { method: "DELETE", path: "/portal/api/connections/:id", respond: scriptedOnly },
  { method: "POST", path: "/portal/api/connections/:id/check", respond: scriptedOnly },
  { method: "POST", path: "/portal/api/chat/messages", respond: scriptedOnly },
  { method: "GET", path: "/portal/api/goals/:id", respond: scriptedOnly },
  { method: "GET", path: "/portal/api/goals/:id/events", respond: scriptedOnly },
  { method: "POST", path: "/portal/api/goals/:id/commands", respond: scriptedOnly },
  { method: "POST", path: "/portal/api/projects/:id/goals", respond: scriptedOnly },
  { method: "POST", path: "/portal/api/projects/:id/resources", respond: scriptedOnly },
  { method: "POST", path: "/portal/api/projects/:id/sync", respond: scriptedOnly },
  { method: "PATCH", path: "/portal/api/resources/:id", respond: scriptedOnly },
  { method: "DELETE", path: "/portal/api/resources/:id", respond: scriptedOnly },
  { method: "POST", path: "/portal/api/resources/:id/sync", respond: scriptedOnly },
  { method: "POST", path: "/portal/api/work-items/:id/run", respond: scriptedOnly },
  { method: "POST", path: "/portal/api/work-items/:id/cancel", respond: scriptedOnly },
  { method: "POST", path: "/portal/api/work-items/:id/retry", respond: scriptedOnly },
  { method: "POST", path: "/portal/api/work-items/:id/input", respond: scriptedOnly },
];

const decodeBody = (raw: string | null): Record<string, unknown> | null => {
  if (raw === null || raw === "") return null;
  try {
    return asRecord(JSON.parse(raw));
  } catch {
    return null;
  }
};

const recorded = async (request: Request): Promise<PortalRequest> => {
  // The clone keeps the delivered request's body readable for a later resolution.
  const rawBody = request.method === "GET" || request.method === "HEAD" ? null : await request.clone().text();
  const url = new URL(request.url);
  return {
    method: request.method.toUpperCase(),
    path: url.pathname + url.search,
    url,
    body: decodeBody(rawBody),
    rawBody,
    headers: Object.fromEntries(request.headers.entries()),
  };
};

let seedSnapshot: PortalSeed = defaultSeed();
let state: PortalSeed = cloneSeed(seedSnapshot);
const requests: PortalRequest[] = [];
const layers: PortalResolver[] = [];
/** MSW resolves a handler twice per request; only the first resolution answers. */
const answered = new Map<string, Response>();

const dispatch = async (entry: PortalEntry, request: Request, requestId: string): Promise<Response> => {
  const memo = answered.get(requestId);
  if (memo !== undefined) return memo;
  const call = await recorded(request);
  requests.push(call);
  for (const layer of layers) {
    const answer = await layer(call);
    if (answer !== undefined) {
      answered.set(requestId, answer);
      return answer;
    }
  }
  if (call.method !== "GET") {
    if (state.mutations.failNext) {
      state.mutations.failNext = false;
      return HttpResponse.error();
    }
    if (state.mutations.hold !== null) await state.mutations.hold;
  }
  const response = entry.respond?.(call, state) ?? HttpResponse.error();
  answered.set(requestId, response);
  return response;
};

const handle = (entry: PortalEntry) => {
  const resolver = ({ request, requestId }: { request: Request; requestId: string }) => dispatch(entry, request, requestId);
  switch (entry.method) {
    case "GET":
      return http.get(entry.path, resolver);
    case "POST":
      return http.post(entry.path, resolver);
    case "PATCH":
      return http.patch(entry.path, resolver);
    case "DELETE":
      return http.delete(entry.path, resolver);
  }
};

export const portalServer = setupServer(...entries.map(handle));

export const portal: PortalDouble = {
  seed(seed) {
    seedSnapshot = mergedSeed(seed);
    state = cloneSeed(seedSnapshot);
  },
  get state() {
    return state;
  },
  get requests() {
    return requests;
  },
  sent(method, path) {
    return requests.filter((request) => request.method === method && request.path === path);
  },
  serve(resolver) {
    layers.push(resolver);
  },
  holdNextMutation() {
    const resolvers = Promise.withResolvers<void>();
    state.mutations.hold = resolvers.promise;
    return () => {
      state.mutations.hold = null;
      resolvers.resolve();
    };
  },
};

export function resetPortalDouble(): void {
  layers.length = 0;
  requests.length = 0;
  answered.clear();
  state = cloneSeed(seedSnapshot);
}
