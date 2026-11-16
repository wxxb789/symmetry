import {
  ConnectionsRead,
  WorkDetailRead,
  WorkTimelineRead,
  WorkspaceRead,
  type SymmetryConnectionsReadV1,
  type SymmetryWorkDetailReadV1,
  type SymmetryWorkTimelineReadV1,
  type SymmetryWorkspaceReadV1,
} from "@symmetry/contracts";
import { Data, Effect, Either, Layer, ManagedRuntime, Schema } from "effect";

export class Unauthenticated extends Data.TaggedError("Unauthenticated")<{ message: string }> {}
export class Forbidden extends Data.TaggedError("Forbidden")<{ message: string }> {}
export class Conflict extends Data.TaggedError("Conflict")<{ message: string }> {}
export class Validation extends Data.TaggedError("Validation")<{ message: string }> {}
export class Unsupported extends Data.TaggedError("Unsupported")<{ message: string }> {}
export class Network extends Data.TaggedError("Network")<{ message: string }> {}
export class UnexpectedResponse extends Data.TaggedError("UnexpectedResponse")<{
  message: string;
  status: number | null;
  transient: boolean;
}> {}

// The portal API answers a refused command with its own code, message and field
// errors. Carry them verbatim so the operator sees the server's reason instead
// of a status-derived guess; the authoritative state still comes from the read.
export class ServerRefusal extends Data.TaggedError("ServerRefusal")<{
  status: number;
  code: string;
  message: string;
  fields: Record<string, readonly string[]> | null;
}> {}

export type ReadError = Unauthenticated | Forbidden | Conflict | Validation | Unsupported | Network | UnexpectedResponse;
export type ApiError = ReadError | ServerRefusal;
export type Workspace = SymmetryWorkspaceReadV1;
export type Project = Workspace["projects"][number];
export type WorkItem = Project["work_items"][number];
export type ConnectionsReadModel = SymmetryConnectionsReadV1;
export type Connection = ConnectionsReadModel["connections"][number];
export type ConnectionProvider = ConnectionsReadModel["providers"][number];
export type ConnectionResource = Connection["resources"][number];

export interface ConnectionDraft {
  provider: string;
  name: string;
  account_ref: string;
  capabilities: readonly string[];
}

export interface ConnectionUpdate {
  version: number;
  name?: string;
  capabilities?: readonly string[];
}

export interface ConnectionReceipt {
  disposition: string;
  connectionId: string;
}

export type WorkDetail = SymmetryWorkDetailReadV1;
export type WorkTimeline = SymmetryWorkTimelineReadV1;
export type TimelineItem = WorkTimeline["items"][number];

/** A project creation submission: exactly the fields the server's create changeset casts. */
export interface ProjectDraft {
  key: string;
  name: string;
  description: string;
  default_agent_profile: string;
  default_workspace: string;
}

/** A versioned project update. `version` is the revision the editor was opened from. */
export interface ProjectChanges {
  version: number;
  name?: string;
  description?: string;
  default_agent_profile?: string;
  default_workspace?: string;
  status?: string;
}

/** A work-item submission: exactly the fields the server's changesets cast. */
export interface WorkItemDraft {
  title: string;
  description?: string;
  status?: string;
  priority?: string;
  assignee_type?: string;
  assignee_name?: string;
  agent_profile?: string;
  workspace?: string;
  repository_resource_id?: string | null;
  ci_resource_id?: string | null;
  branch?: string;
  blocked?: boolean;
  blocker?: string;
}

/** A versioned work-item edit; status and order move through the move endpoint. */
export type WorkItemChanges = Omit<WorkItemDraft, "status"> & { version: number };

/** The confirmed board movement: status, and the card this item is inserted before. */
export interface MoveIntent {
  version: number;
  status: string;
  beforeId: string | null;
}

/** Write receipts repeat the served projection; this surface consumes the identity. */
export interface WriteReceipt {
  id: string;
}

const Session = Schema.Struct({ csrf_token: Schema.NonEmptyString });
export type Session = typeof Session.Type;
const ServerError = Schema.Struct({
  error: Schema.Struct({
    code: Schema.NonEmptyString,
    message: Schema.NonEmptyString,
    fields: Schema.optionalWith(
      Schema.NullOr(Schema.Record({ key: Schema.String, value: Schema.Array(Schema.String) })),
      { default: () => null },
    ),
  }),
});
// The receipt repeats the connection read projection; this surface steers from
// the authoritative list read, so it consumes the disposition and identity.
const ConnectionReceiptBody = Schema.Struct({
  disposition: Schema.NonEmptyString,
  connection: Schema.Struct({ id: Schema.NonEmptyString }),
});
const DeletedConnectionBody = Schema.Struct({
  disposition: Schema.NonEmptyString,
  deleted_connection_id: Schema.NonEmptyString,
});
// A write receipt repeats the served project/work-item projection. This surface
// steers from the authoritative read (the projection's unloaded associations
// render as empty arrays), so it consumes the identity and discards the rest.
const ProjectReceiptBody = Schema.Struct({ project: Schema.Struct({ id: Schema.NonEmptyString }) });
const WorkItemReceiptBody = Schema.Struct({ work_item: Schema.Struct({ id: Schema.NonEmptyString }) });
type Method = "GET" | "POST" | "PATCH" | "DELETE";
interface RequestInit {
  method: Method;
  body?: unknown;
  csrf?: string;
}
const runtime = ManagedRuntime.make(Layer.empty);

export interface Api {
  workspace: (projectId: string | null, signal: AbortSignal) => Promise<Workspace>;
  workDetail: (workItemId: string, signal: AbortSignal) => Promise<WorkDetail>;
  workTimeline: (workItemId: string, before: string | null, signal: AbortSignal) => Promise<WorkTimeline>;
  session: (signal: AbortSignal) => Promise<Session>;
  connections: (signal: AbortSignal) => Promise<ConnectionsReadModel>;
  createConnection: (draft: ConnectionDraft, csrf: string, signal: AbortSignal) => Promise<ConnectionReceipt>;
  updateConnection: (connectionId: string, update: ConnectionUpdate, csrf: string, signal: AbortSignal) => Promise<ConnectionReceipt>;
  deleteConnection: (connectionId: string, version: number, csrf: string, signal: AbortSignal) => Promise<{ disposition: string; deletedConnectionId: string }>;
  checkConnection: (connectionId: string, csrf: string, signal: AbortSignal) => Promise<ConnectionReceipt>;
  createProject: (draft: ProjectDraft, csrf: string, signal: AbortSignal) => Promise<WriteReceipt>;
  updateProject: (projectId: string, changes: ProjectChanges, csrf: string, signal: AbortSignal) => Promise<WriteReceipt>;
  createWorkItem: (projectId: string, draft: WorkItemDraft, csrf: string, signal: AbortSignal) => Promise<WriteReceipt>;
  updateWorkItem: (workItemId: string, changes: WorkItemChanges, csrf: string, signal: AbortSignal) => Promise<WriteReceipt>;
  moveWorkItem: (workItemId: string, intent: MoveIntent, csrf: string, signal: AbortSignal) => Promise<WriteReceipt>;
}

// Strict byte-level decoding keeps invalid UTF-8 visible to the schema boundary;
// response.text() would silently substitute U+FFFD and pass corrupted text.
const decodeBody = (body: Uint8Array) =>
  Effect.try({
    try: () => new TextDecoder("utf-8", { fatal: true }).decode(body),
    catch: () => new UnexpectedResponse({ message: "The server response could not be read safely. Refresh or contact your operator.", status: null, transient: false }),
  });

function statusError(status: number): ReadError {
  switch (status) {
    case 401: return new Unauthenticated({ message: "Your session has ended. Sign in to continue." });
    case 403: return new Forbidden({ message: "You do not have access to this workspace." });
    case 409: return new Conflict({ message: "The workspace changed. Refresh to load its current state." });
    case 422: return new Validation({ message: "The server could not accept this read request." });
    case 405:
    case 501: return new Unsupported({ message: "This read is not supported by this server." });
    case 404: return new UnexpectedResponse({ message: "The requested resource was not found or is unavailable.", status, transient: false });
    default: return new UnexpectedResponse({
      message: status >= 500 ? "The server is temporarily unavailable." : "The server returned an unexpected response.",
      status,
      // Every server-side fault is retryable: a command keeps its frozen intent
      // and a read keeps its recorded state instead of being treated as the
      // server's authoritative answer.
      transient: status >= 500 || status === 408 || status === 429,
    });
  }
}

// A refused command is never retried and never rendered as an unknown failure:
// the server's typed reason is the actionable part. 401 always means the session
// ended, and an undescribed 403 on a same-origin command with a bootstrap token
// is a session-protection refusal rather than a missing permission.
function mutationStatusError(status: number): ReadError {
  if (status === 403) {
    return new Forbidden({ message: "This change was refused. If you signed in elsewhere, refresh the page and try again." });
  }
  return statusError(status);
}

function parseRefusal(status: number, body: Uint8Array): ServerRefusal | null {
  try {
    const text = new TextDecoder("utf-8", { fatal: true }).decode(body);
    const decoded = Schema.decodeUnknownEither(Schema.parseJson(ServerError))(text);
    if (Either.isLeft(decoded)) return null;
    return new ServerRefusal({
      status,
      code: decoded.right.error.code,
      message: decoded.right.error.message,
      fields: decoded.right.error.fields,
    });
  } catch {
    return null;
  }
}

function request<A>(
  fetcher: typeof fetch,
  path: string,
  init: RequestInit,
  decode: (body: Uint8Array) => Effect.Effect<A, unknown>,
): Effect.Effect<A, ApiError> {
  return Effect.gen(function* () {
    const { response, body } = yield* Effect.tryPromise({
      try: async (signal) => {
        const response = await fetcher(path, {
          method: init.method,
          credentials: "same-origin",
          headers: {
            Accept: "application/json",
            ...(init.body === undefined ? {} : { "content-type": "application/json" }),
            ...(init.csrf === undefined ? {} : { "x-csrf-token": init.csrf }),
          },
          cache: "no-store",
          redirect: "manual",
          ...(init.body === undefined ? {} : { body: JSON.stringify(init.body) }),
          signal,
        });
        // Read the body only when it is the answer (ok) or a refusal to parse
        // (a failed command): a failed GET's unreadable body must not mask its
        // status-derived error with a retried Network.
        return { response, body: response.ok || init.method !== "GET" ? new Uint8Array(await response.arrayBuffer()) : new Uint8Array(0) };
      },
      catch: () => new Network({ message: "Cannot reach the server. Check your connection and try again." }),
    });
    if (response.type === "opaqueredirect" || response.status === 302 || response.status === 303) {
      return yield* Effect.fail(statusError(401));
    }
    if (!response.ok) {
      if (init.method !== "GET" && response.status !== 401) {
        const refusal = parseRefusal(response.status, body);
        if (refusal) return yield* Effect.fail(refusal);
      }
      return yield* Effect.fail(init.method === "GET" ? statusError(response.status) : mutationStatusError(response.status));
    }
    return yield* decode(body).pipe(Effect.mapError(() => new UnexpectedResponse({
      message: "The server response could not be read safely. Refresh or contact your operator.",
      status: response.status,
      transient: false,
    })));
  });
}

function run<A>(effect: Effect.Effect<A, ApiError>, signal: AbortSignal): Promise<A> {
  return runtime.runPromise(Effect.either(effect), { signal }).then((result) => {
    if (Either.isLeft(result)) throw result.left;
    return result.right;
  });
}

const projectReceipt = (body: Uint8Array) =>
  decodeBody(body).pipe(
    Effect.flatMap((text) => Schema.decodeUnknown(Schema.parseJson(ProjectReceiptBody))(text)),
    Effect.map((value) => ({ id: value.project.id })),
  );

const workItemReceipt = (body: Uint8Array) =>
  decodeBody(body).pipe(
    Effect.flatMap((text) => Schema.decodeUnknown(Schema.parseJson(WorkItemReceiptBody))(text)),
    Effect.map((value) => ({ id: value.work_item.id })),
  );

export function createApi(fetcher: typeof fetch = globalThis.fetch.bind(globalThis)): Api {
  return {
    workspace(projectId: string | null, signal: AbortSignal): Promise<Workspace> {
      const query = projectId === null ? "" : `?${new URLSearchParams({ project_id: projectId })}`;
      return run(request(fetcher, `/portal/api/workspace${query}`, { method: "GET" }, (body) => decodeBody(body).pipe(Effect.flatMap((text) => WorkspaceRead.decodeJson(text)))), signal);
    },
    workDetail(workItemId: string, signal: AbortSignal): Promise<WorkDetail> {
      return run(request(fetcher, `/portal/api/work-items/${encodeURIComponent(workItemId)}`, { method: "GET" }, (body) => decodeBody(body).pipe(Effect.flatMap((text) => WorkDetailRead.decodeJson(text)))), signal);
    },
    workTimeline(workItemId: string, before: string | null, signal: AbortSignal): Promise<WorkTimeline> {
      // The cursor is an opaque signed token: only the served `before` direction exists.
      const query = before === null ? "" : `?${new URLSearchParams({ before })}`;
      return run(request(fetcher, `/portal/api/work-items/${encodeURIComponent(workItemId)}/timeline${query}`, { method: "GET" }, (body) => decodeBody(body).pipe(Effect.flatMap((text) => WorkTimelineRead.decodeJson(text)))), signal);
    },
    session(signal: AbortSignal): Promise<Session> {
      return run(request(fetcher, "/portal/api/session", { method: "GET" }, (body) => decodeBody(body).pipe(Effect.flatMap((text) => Schema.decodeUnknown(Schema.parseJson(Session))(text)))), signal);
    },
    connections(signal: AbortSignal): Promise<ConnectionsReadModel> {
      return run(request(fetcher, "/portal/api/connections", { method: "GET" }, (body) => decodeBody(body).pipe(Effect.flatMap((text) => ConnectionsRead.decodeJson(text)))), signal);
    },
    createConnection(draft: ConnectionDraft, csrf: string, signal: AbortSignal): Promise<ConnectionReceipt> {
      return run(request(fetcher, "/portal/api/connections", { method: "POST", body: draft, csrf }, receipt), signal);
    },
    updateConnection(connectionId: string, update: ConnectionUpdate, csrf: string, signal: AbortSignal): Promise<ConnectionReceipt> {
      return run(request(fetcher, `/portal/api/connections/${encodeURIComponent(connectionId)}`, { method: "PATCH", body: update, csrf }, receipt), signal);
    },
    deleteConnection(connectionId: string, version: number, csrf: string, signal: AbortSignal): Promise<{ disposition: string; deletedConnectionId: string }> {
      return run(request(
        fetcher,
        `/portal/api/connections/${encodeURIComponent(connectionId)}?${new URLSearchParams({ version: String(version) })}`,
        { method: "DELETE", csrf },
        (body) => decodeBody(body).pipe(Effect.flatMap((text) => Schema.decodeUnknown(Schema.parseJson(DeletedConnectionBody))(text))),
      ).pipe(Effect.map((value) => ({ disposition: value.disposition, deletedConnectionId: value.deleted_connection_id }))), signal);
    },
    checkConnection(connectionId: string, csrf: string, signal: AbortSignal): Promise<ConnectionReceipt> {
      return run(request(fetcher, `/portal/api/connections/${encodeURIComponent(connectionId)}/check`, { method: "POST", csrf }, receipt), signal);
    },
    createProject(draft: ProjectDraft, csrf: string, signal: AbortSignal): Promise<WriteReceipt> {
      return run(request(fetcher, "/portal/api/projects", { method: "POST", body: draft, csrf }, projectReceipt), signal);
    },
    updateProject(projectId: string, changes: ProjectChanges, csrf: string, signal: AbortSignal): Promise<WriteReceipt> {
      return run(request(fetcher, `/portal/api/projects/${encodeURIComponent(projectId)}`, { method: "PATCH", body: changes, csrf }, projectReceipt), signal);
    },
    createWorkItem(projectId: string, draft: WorkItemDraft, csrf: string, signal: AbortSignal): Promise<WriteReceipt> {
      return run(request(fetcher, `/portal/api/projects/${encodeURIComponent(projectId)}/work-items`, { method: "POST", body: draft, csrf }, workItemReceipt), signal);
    },
    updateWorkItem(workItemId: string, changes: WorkItemChanges, csrf: string, signal: AbortSignal): Promise<WriteReceipt> {
      return run(request(fetcher, `/portal/api/work-items/${encodeURIComponent(workItemId)}`, { method: "PATCH", body: changes, csrf }, workItemReceipt), signal);
    },
    moveWorkItem(workItemId: string, intent: MoveIntent, csrf: string, signal: AbortSignal): Promise<WriteReceipt> {
      return run(request(fetcher, `/portal/api/work-items/${encodeURIComponent(workItemId)}/move`, {
        method: "PATCH",
        body: { version: intent.version, status: intent.status, before_id: intent.beforeId },
        csrf,
      }, workItemReceipt), signal);
    },
  };
}

const receipt = (body: Uint8Array) =>
  decodeBody(body).pipe(
    Effect.flatMap((text) => Schema.decodeUnknown(Schema.parseJson(ConnectionReceiptBody))(text)),
    Effect.map((value) => ({ disposition: value.disposition, connectionId: value.connection.id })),
  );

export function readRetry(failureCount: number, error: unknown): boolean {
  return failureCount < 2 && (error instanceof Network || (error instanceof UnexpectedResponse && error.transient));
}

export function errorMessage(error: unknown): string {
  return error instanceof Unauthenticated || error instanceof Forbidden || error instanceof Conflict ||
    error instanceof Validation || error instanceof Unsupported || error instanceof Network ||
    error instanceof UnexpectedResponse || error instanceof ServerRefusal
    ? error.message
    : "The workspace could not be loaded. Try again.";
}
