import {
  AttentionRead as AttentionReadBoundary,
  ChatRead,
  ConnectionsRead,
  GoalRead as GoalReadBoundary,
  WorkDetailRead,
  WorkTimelineRead,
  WorkspaceRead,
  type SymmetryAttentionReadV1,
  type SymmetryChatReadV1,
  type SymmetryConnectionsReadV1,
  type SymmetryGoalReadV1,
  type SymmetryWorkDetailReadV1,
  type SymmetryWorkTimelineReadV1,
  type SymmetryWorkspaceReadV1,
} from "@symmetry/contracts";
import { MutationRefused } from "./commands";
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

// A project resource mutation returns the served resource projection. The
// console steers from the authoritative project read, so the receipt carries
// the identity and the served state a continuation compares against.
export interface ResourceDraft {
  kind: string;
  connection_id: string | null;
  name: string;
  external_ref: string | null;
}

export interface ResourceUpdate {
  version: number;
  name: string;
  connection_id: string | null;
  external_ref: string | null;
}

export interface ResourceReceipt {
  resourceId: string;
  provider: string | null;
  status: string;
  syncStatus: string;
  syncStatusMessage: string | null;
  lastSyncedAt: string | null;
  version: number;
}

export interface WorkItemAssignment {
  version: number;
  assignee_type: string;
  assignee_name: string | null;
  agent_profile: string | null;
  workspace: string | null;
  repository_resource_id: string | null;
  ci_resource_id: string | null;
}

/**
 * The assignment write's receipt. The assignment editor steers from the served
 * identity and the version it re-reads from, so this receipt carries the version
 * the server accepted rather than the creation disposition of a field edit.
 */
export interface AssignmentReceipt {
  workItemId: string;
  version: number;
}

export type WorkDetail = SymmetryWorkDetailReadV1;
export type WorkTimeline = SymmetryWorkTimelineReadV1;
export type TimelineItem = WorkTimeline["items"][number];
export type Execution = NonNullable<WorkItem["execution"]>;
export type ExecutionReceipt = Execution["latest_command"];
export type WaitingInput = NonNullable<Execution["waiting"]>;
export type Runtime = NonNullable<Workspace["runtimes"]>[number];

/** The server's chat scope vocabulary, as the conversation read serves it. */
export type ChatScope = SymmetryChatReadV1["scope"];

/** The scoped conversation read's decoded projection. */
export type ChatConversation = SymmetryChatReadV1;
export type ChatConversationMessage = ChatConversation["messages"][number];
export type ChatConversationRun = ChatConversation["runs"][number];

/**
 * The scope members one conversation read carries. The query string carries
 * only the members the server accepts for that scope: a project read never
 * carries `run_id` and a run read never carries `project_id`.
 */
export interface ChatConversationQuery {
  readonly scope: ChatScope;
  readonly projectId?: string | null;
  readonly runId?: string | null;
  /** The served `next_before` direction only; the cursor is opaque. */
  readonly before?: string | null;
}

/**
 * One `POST /portal/api/chat/messages` request, frozen at send time. A retry
 * after a lost response resends this exact object: the same `action_id` and a
 * byte-equivalent body, so the server replays its recorded message instead of
 * applying a second one.
 */
export interface ChatMessageRequest {
  readonly scope: ChatScope;
  readonly project_id?: string;
  readonly run_id?: string;
  readonly intent: string;
  readonly content: string;
  readonly action_id: string;
  readonly work_item_id?: string;
  readonly generation?: number;
  readonly waiting_transition_id?: string;
  readonly option_id?: string;
  readonly target_project_id?: string;
  readonly work?: {
    readonly title?: string;
    readonly repository_resource_id?: string;
    readonly ci_resource_id?: string;
  };
}

/** The command receipt members the conversation renders. */
export interface ChatCommandView {
  readonly command_id: string;
  readonly kind: string;
  readonly state: string;
  readonly acknowledgement_outcome: string | null;
  readonly acknowledgement_id: string | null;
  readonly applied_at?: string | null;
}

/** The POST answer: the two durable messages, the target work item and the command. */
export interface ChatMessageOutcome {
  readonly disposition: MutationDisposition;
  readonly message: { readonly id: string };
  readonly reply: { readonly id: string };
  readonly workItemId: string | null;
  readonly command: ChatCommandView | null;
}

/** A caller-minted idempotency identity. One value per proposed operation. */
export type ActionId = string & { readonly ActionId: unique symbol };

/** The exact attempt generation a control request is aimed at. */
export type ExecutionGeneration = number & { readonly ExecutionGeneration: unique symbol };

export type MutationDisposition = "created" | "replayed";

export type Attention = SymmetryAttentionReadV1;
export type GoalAttentionEntry = Attention["goal_attention"]["entries"][number];
export type ProjectBlockedEntry = Attention["project_blocked"]["entries"][number];
export type GoalProjection = SymmetryGoalReadV1;
export type GoalWorkItem = GoalProjection["work_items"][number];
export type GoalDecision = GoalProjection["decisions"][number];
export type GoalGraph = GoalProjection["graph"];

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
  /**
   * The client-minted creation identity, persisted with the draft intent and
   * replayed verbatim on retry: the server answers a replayed identity with the
   * originally created row instead of inserting a second one. Optional and
   * additive; a submission without it keeps the legacy create behaviour.
   */
  creation_id?: string;
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
  // The delivery controls both changesets cast; a deliberate clear travels as
  // null (Ecto skips a nil change, and the column CHECKs pass NULL).
  pull_request_url?: string | null;
  ci_status?: string | null;
  review_status?: string | null;
  blocked?: boolean;
  blocker?: string;
}

/** A versioned work-item edit; status and order move through the move endpoint. */
export type WorkItemChanges = Omit<WorkItemDraft, "status" | "creation_id"> & { version: number };

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

/**
 * A work-item write receipt. The create path marks a replayed identity with
 * `disposition: "replayed"` (the row already existed and was returned instead
 * of a second one); update and move receipts carry the empty default because
 * their disposition is not part of their answer.
 */
export interface WorkItemReceipt extends WriteReceipt {
  disposition: string;
}

// The same-origin bootstrap carries the CSRF token for commands and the
// short-lived, audience-scoped token for the operator invalidation socket. The
// operator credential itself never reaches this read.
const Session = Schema.Struct({
  csrf_token: Schema.NonEmptyString,
  // Additive: this release's server always serves it (pinned by the control
  // session test and by the real socket connect in the browser gate). A partial
  // bootstrap that serves none is still tolerated: the shell connects the
  // invalidation client regardless, and its empty-token branch reports
  // `disconnected` and starts the polling fallback, so a visible client keeps
  // converging without a socket.
  socket_token: Schema.optionalWith(Schema.String, { default: () => "" }),
});
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
const WorkItemReceiptBody = Schema.Struct({
  disposition: Schema.optional(Schema.NonEmptyString),
  work_item: Schema.Struct({ id: Schema.NonEmptyString }),
});
// Every resource mutation answers with the served projection; the fields below
// are the ones a continuation reads. A failed sync answers 502 with the same
// projection plus `error`, which `parseRefusal` carries as the server's reason.
const ResourceReceiptBody = Schema.Struct({
  resource: Schema.Struct({
    id: Schema.NonEmptyString,
    provider: Schema.NullOr(Schema.String),
    status: Schema.NonEmptyString,
    sync_status: Schema.NonEmptyString,
    status_message: Schema.NullOr(Schema.String),
    last_synced_at: Schema.NullOr(Schema.String),
    version: Schema.Number,
  }),
});
const DeletedResourceBody = Schema.Struct({
  deleted_resource_id: Schema.NonEmptyString,
});
const ProjectSyncBody = Schema.Struct({
  synced_resource_ids: Schema.Array(Schema.String),
});
// The assignment write answers with the served work-item projection too, but its
// continuation compares the version it re-reads from, so the version is required
// here rather than optional as in the create receipt.
const AssignmentReceiptBody = Schema.Struct({
  work_item: Schema.Struct({
    id: Schema.NonEmptyString,
    version: Schema.Number,
  }),
});
type Method = "GET" | "POST" | "PATCH" | "DELETE";
interface RequestInit {
  method: Method;
  body?: unknown;
  csrf?: string;
}
const GoalEventShape = Schema.Struct({
  id: Schema.optional(Schema.String),
  sequence: Schema.Number,
  kind: Schema.String,
  actor_ref: Schema.optional(Schema.NullOr(Schema.String)),
  revision: Schema.optional(Schema.NullOr(Schema.Number)),
  payload: Schema.optional(Schema.Record({ key: Schema.String, value: Schema.Unknown })),
  response: Schema.optional(Schema.Record({ key: Schema.String, value: Schema.Unknown })),
  inserted_at: Schema.optional(Schema.NullOr(Schema.String)),
});
const GoalEventsShape = Schema.Struct({
  entries: Schema.Array(GoalEventShape),
  next_after: Schema.NullOr(Schema.Number),
});
export type GoalEvent = typeof GoalEventShape.Type;
export type GoalEvents = typeof GoalEventsShape.Type;
const runtime = ManagedRuntime.make(Layer.empty);

// Control responses carry the same PortalJSON projections the read schemas
// describe, but a control response is a narrower projection than a workspace
// read and does not guarantee every execution member. These structs decode the
// members the surface renders; every other served member stays additive.
const ControlExecution = Schema.Struct({
  task_id: Schema.String,
  run_id: Schema.NullOr(Schema.String),
  generation: Schema.Number,
  state: Schema.String,
});
const ControlCommand = Schema.Struct({
  command_id: Schema.String,
  task_id: Schema.String,
  kind: Schema.String,
  state: Schema.String,
  payload: Schema.optional(Schema.Record({ key: Schema.String, value: Schema.Unknown })),
  issued_at: Schema.optional(Schema.NullOr(Schema.String)),
  applied_at: Schema.optional(Schema.NullOr(Schema.String)),
  acknowledgement_id: Schema.optional(Schema.NullOr(Schema.String)),
  acknowledgement_outcome: Schema.NullOr(Schema.String),
  acknowledged_at: Schema.optional(Schema.NullOr(Schema.String)),
});
const CancelResponse = Schema.Struct({
  disposition: Schema.Literal("created", "replayed"),
  command: ControlCommand,
  execution: ControlExecution,
});
const RetryResponse = Schema.Struct({
  disposition: Schema.Literal("created", "replayed"),
  command: ControlCommand,
  work_item: Schema.Struct({ id: Schema.String }),
  execution: ControlExecution,
});
const StartResponse = Schema.Struct({
  disposition: Schema.Literal("created", "replayed"),
  execution: ControlExecution,
});
const InputResponse = Schema.Struct({
  disposition: Schema.Literal("created", "replayed"),
  command: ControlCommand,
  execution: ControlExecution,
});

// The chat message answer is a narrower receipt than the conversation read: it
// carries the two durable messages of the accepted action, the target work item
// and the action's command. These structs decode the members the surface
// consumes (the identity and the receipt), and the HTTP status — 200 replayed,
// 201 created — is the disposition the answer itself does not carry.
const ChatPostedMessage = Schema.Struct({
  id: Schema.NonEmptyString,
  role: Schema.String,
  intent: Schema.String,
  content: Schema.String,
  inserted_at: Schema.NullOr(Schema.String),
  work_item_id: Schema.NullOr(Schema.String),
});
const ChatPostedCommand = Schema.Struct({
  command_id: Schema.NonEmptyString,
  kind: Schema.NonEmptyString,
  state: Schema.NonEmptyString,
  acknowledgement_outcome: Schema.NullOr(Schema.String),
  acknowledgement_id: Schema.optional(Schema.NullOr(Schema.String)),
  applied_at: Schema.optional(Schema.NullOr(Schema.String)),
});
const ChatMessageResponse = Schema.Struct({
  message: ChatPostedMessage,
  reply: ChatPostedMessage,
  work_item_id: Schema.NullOr(Schema.String),
  command: Schema.NullOr(ChatPostedCommand),
});

type ControlCommandValue = typeof ControlCommand.Type;

export type ControlExecution = typeof ControlExecution.Type;
export type ControlReceipt = ControlCommandValue;

export interface StartOutcome {
  disposition: MutationDisposition;
  execution: ControlExecution;
}

export interface CancelOutcome {
  disposition: MutationDisposition;
  execution: ControlExecution;
  command: ControlReceipt;
}

export interface RetryOutcome {
  disposition: MutationDisposition;
  execution: ControlExecution;
  command: ControlReceipt;
  workItemId: string;
}

export interface InputOutcome {
  disposition: MutationDisposition;
  execution: ControlExecution;
  command: ControlReceipt;
}

export interface Api {
  workspace: (projectId: string | null, signal: AbortSignal) => Promise<Workspace>;
  workDetail: (workItemId: string, signal: AbortSignal) => Promise<WorkDetail>;
  workTimeline: (workItemId: string, before: string | null, signal: AbortSignal) => Promise<WorkTimeline>;
  attention: (projectId: string | null, cursor: string | null, signal: AbortSignal) => Promise<Attention>;
  goal: (goalId: string, signal: AbortSignal) => Promise<GoalProjection>;
  goalEvents: (goalId: string, after: number, signal: AbortSignal) => Promise<GoalEvents>;
  session: (signal: AbortSignal) => Promise<Session>;
  connections: (signal: AbortSignal) => Promise<ConnectionsReadModel>;
  createConnection: (draft: ConnectionDraft, csrf: string, signal: AbortSignal) => Promise<ConnectionReceipt>;
  updateConnection: (connectionId: string, update: ConnectionUpdate, csrf: string, signal: AbortSignal) => Promise<ConnectionReceipt>;
  deleteConnection: (connectionId: string, version: number, csrf: string, signal: AbortSignal) => Promise<{ disposition: string; deletedConnectionId: string }>;
  checkConnection: (connectionId: string, csrf: string, signal: AbortSignal) => Promise<ConnectionReceipt>;
  createProject: (draft: ProjectDraft, csrf: string, signal: AbortSignal) => Promise<WriteReceipt>;
  updateProject: (projectId: string, changes: ProjectChanges, csrf: string, signal: AbortSignal) => Promise<WriteReceipt>;
  createWorkItem: (projectId: string, draft: WorkItemDraft, csrf: string, signal: AbortSignal) => Promise<WorkItemReceipt>;
  updateWorkItem: (workItemId: string, changes: WorkItemChanges, csrf: string, signal: AbortSignal) => Promise<WorkItemReceipt>;
  /**
   * The assignment write, on the same work-item endpoint as `updateWorkItem`:
   * the body is the version-fenced assignment and the answer is read as the
   * served identity and version rather than a creation disposition.
   */
  assignWorkItem: (workItemId: string, assignment: WorkItemAssignment, csrf: string, signal: AbortSignal) => Promise<AssignmentReceipt>;
  moveWorkItem: (workItemId: string, intent: MoveIntent, csrf: string, signal: AbortSignal) => Promise<WorkItemReceipt>;
  createResource: (projectId: string, draft: ResourceDraft, csrf: string, signal: AbortSignal) => Promise<ResourceReceipt>;
  updateResource: (resourceId: string, update: ResourceUpdate, csrf: string, signal: AbortSignal) => Promise<ResourceReceipt>;
  deleteResource: (resourceId: string, version: number, csrf: string, signal: AbortSignal) => Promise<{ deletedResourceId: string }>;
  syncResource: (resourceId: string, csrf: string, signal: AbortSignal) => Promise<ResourceReceipt>;
  syncProject: (projectId: string, csrf: string, signal: AbortSignal) => Promise<{ syncedResourceIds: readonly string[] }>;
  /**
   * The scoped conversation read. The query string carries only the members the
   * server accepts for that scope, and the cursor is always the served opaque
   * `before` direction.
   */
  chatConversation: (query: ChatConversationQuery, signal: AbortSignal) => Promise<ChatConversation>;
  /**
   * One conversation message. The payload is the caller's frozen request; the
   * 200/201 status is the disposition the answer itself does not carry. One
   * attempt per call — a caller that must retry resends the same payload.
   */
  chatMessage: (payload: ChatMessageRequest, csrf: string, signal: AbortSignal) => Promise<ChatMessageOutcome>;
  startWork: (workItemId: string, actionId: ActionId, csrf: string, signal: AbortSignal) => Promise<StartOutcome>;
  cancelWork: (workItemId: string, generation: ExecutionGeneration, actionId: ActionId, csrf: string, signal: AbortSignal) => Promise<CancelOutcome>;
  retryWork: (workItemId: string, generation: ExecutionGeneration, actionId: ActionId, csrf: string, signal: AbortSignal) => Promise<RetryOutcome>;
  provideInput: (
    workItemId: string,
    input: Record<string, unknown>,
    waitingTransitionId: string,
    actionId: ActionId,
    csrf: string,
    signal: AbortSignal,
  ) => Promise<InputOutcome>;
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
  // The status is handed to the decoder for the one answer whose disposition is
  // the HTTP status rather than a served member (a replayed chat message
  // answers 200, a created one 201).
  decode: (body: Uint8Array, status: number) => Effect.Effect<A, unknown>,
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
    return yield* decode(body, response.status).pipe(Effect.mapError(() => new UnexpectedResponse({
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
    Effect.map((value) => ({ id: value.work_item.id, disposition: value.disposition ?? "" })),
  );

const assignmentReceipt = (body: Uint8Array) =>
  decodeBody(body).pipe(
    Effect.flatMap((text) => Schema.decodeUnknown(Schema.parseJson(AssignmentReceiptBody))(text)),
    Effect.map((value) => ({ workItemId: value.work_item.id, version: value.work_item.version })),
  );

const startResponse = (body: Uint8Array) =>
  decodeBody(body).pipe(
    Effect.flatMap((text) => Schema.decodeUnknown(Schema.parseJson(StartResponse))(text)),
  );

const cancelResponse = (body: Uint8Array) =>
  decodeBody(body).pipe(
    Effect.flatMap((text) => Schema.decodeUnknown(Schema.parseJson(CancelResponse))(text)),
  );

const retryResponse = (body: Uint8Array) =>
  decodeBody(body).pipe(
    Effect.flatMap((text) => Schema.decodeUnknown(Schema.parseJson(RetryResponse))(text)),
  );

const inputResponse = (body: Uint8Array) =>
  decodeBody(body).pipe(
    Effect.flatMap((text) => Schema.decodeUnknown(Schema.parseJson(InputResponse))(text)),
  );

// A chat message answers 201 when the action created its messages and 200 when
// the identical action was replayed; the answer itself carries no disposition.
const chatMessageResponse = (body: Uint8Array, status: number) =>
  decodeBody(body).pipe(
    Effect.flatMap((text) => Schema.decodeUnknown(Schema.parseJson(ChatMessageResponse))(text)),
    Effect.map((value) => ({
      disposition: status === 200 ? ("replayed" as const) : ("created" as const),
      message: value.message,
      reply: value.reply,
      workItemId: value.work_item_id,
      // Normalized so a receipt is either a complete command or absent, never a
      // member that is present as undefined.
      command: value.command === null ? null : {
        command_id: value.command.command_id,
        kind: value.command.kind,
        state: value.command.state,
        acknowledgement_outcome: value.command.acknowledgement_outcome,
        acknowledgement_id: value.command.acknowledgement_id ?? null,
        applied_at: value.command.applied_at ?? null,
      },
    })),
  );

export function createApi(
  fetcher: typeof fetch = globalThis.fetch.bind(globalThis),
): Api {
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
    attention(projectId: string | null, cursor: string | null, signal: AbortSignal): Promise<Attention> {
      const query = new URLSearchParams();
      if (projectId !== null) query.set("project_id", projectId);
      if (cursor !== null) query.set("cursor", cursor);
      const suffix = query.size === 0 ? "" : `?${query}`;
      return run(request(fetcher, `/portal/api/attention${suffix}`, { method: "GET" }, (body) => decodeBody(body).pipe(Effect.flatMap((text) => AttentionReadBoundary.decodeJson(text)))), signal);
    },
    goal(goalId: string, signal: AbortSignal): Promise<GoalProjection> {
      return run(request(fetcher, `/portal/api/goals/${encodeURIComponent(goalId)}`, { method: "GET" }, (body) => decodeBody(body).pipe(Effect.flatMap((text) => GoalReadBoundary.decodeJson(text)))), signal);
    },
    goalEvents(goalId: string, after: number, signal: AbortSignal): Promise<GoalEvents> {
      const query = `?${new URLSearchParams({ after: String(after) })}`;
      return run(request(fetcher, `/portal/api/goals/${encodeURIComponent(goalId)}/events${query}`, { method: "GET" }, (body) => decodeBody(body).pipe(Effect.flatMap((text) => Schema.decodeUnknown(Schema.parseJson(GoalEventsShape))(text)))), signal);
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
    createWorkItem(projectId: string, draft: WorkItemDraft, csrf: string, signal: AbortSignal): Promise<WorkItemReceipt> {
      return run(request(fetcher, `/portal/api/projects/${encodeURIComponent(projectId)}/work-items`, { method: "POST", body: draft, csrf }, workItemReceipt), signal);
    },
    updateWorkItem(workItemId: string, changes: WorkItemChanges, csrf: string, signal: AbortSignal): Promise<WorkItemReceipt> {
      return run(request(fetcher, `/portal/api/work-items/${encodeURIComponent(workItemId)}`, { method: "PATCH", body: changes, csrf }, workItemReceipt), signal);
    },
    assignWorkItem(workItemId: string, assignment: WorkItemAssignment, csrf: string, signal: AbortSignal): Promise<AssignmentReceipt> {
      return run(request(fetcher, `/portal/api/work-items/${encodeURIComponent(workItemId)}`, { method: "PATCH", body: assignment, csrf }, assignmentReceipt), signal);
    },
    moveWorkItem(workItemId: string, intent: MoveIntent, csrf: string, signal: AbortSignal): Promise<WorkItemReceipt> {
      return run(request(fetcher, `/portal/api/work-items/${encodeURIComponent(workItemId)}/move`, {
        method: "PATCH",
        body: { version: intent.version, status: intent.status, before_id: intent.beforeId },
        csrf,
      }, workItemReceipt), signal);
    },
    createResource(projectId: string, draft: ResourceDraft, csrf: string, signal: AbortSignal): Promise<ResourceReceipt> {
      return run(request(fetcher, `/portal/api/projects/${encodeURIComponent(projectId)}/resources`, { method: "POST", body: draft, csrf }, resourceReceipt), signal);
    },
    updateResource(resourceId: string, update: ResourceUpdate, csrf: string, signal: AbortSignal): Promise<ResourceReceipt> {
      return run(request(fetcher, `/portal/api/resources/${encodeURIComponent(resourceId)}`, { method: "PATCH", body: update, csrf }, resourceReceipt), signal);
    },
    deleteResource(resourceId: string, version: number, csrf: string, signal: AbortSignal): Promise<{ deletedResourceId: string }> {
      return run(request(
        fetcher,
        `/portal/api/resources/${encodeURIComponent(resourceId)}?${new URLSearchParams({ version: String(version) })}`,
        { method: "DELETE", csrf },
        (body) => decodeBody(body).pipe(Effect.flatMap((text) => Schema.decodeUnknown(Schema.parseJson(DeletedResourceBody))(text))),
      ).pipe(Effect.map((value) => ({ deletedResourceId: value.deleted_resource_id }))), signal);
    },
    syncResource(resourceId: string, csrf: string, signal: AbortSignal): Promise<ResourceReceipt> {
      return run(request(fetcher, `/portal/api/resources/${encodeURIComponent(resourceId)}/sync`, { method: "POST", csrf }, resourceReceipt), signal);
    },
    syncProject(projectId: string, csrf: string, signal: AbortSignal): Promise<{ syncedResourceIds: readonly string[] }> {
      return run(request(
        fetcher,
        `/portal/api/projects/${encodeURIComponent(projectId)}/sync`,
        { method: "POST", csrf },
        (body) => decodeBody(body).pipe(Effect.flatMap((text) => Schema.decodeUnknown(Schema.parseJson(ProjectSyncBody))(text))),
      ).pipe(Effect.map((value) => ({ syncedResourceIds: value.synced_resource_ids }))), signal);
    },
    chatConversation(query: ChatConversationQuery, signal: AbortSignal): Promise<ChatConversation> {
      // Only the members the server accepts for this scope: a project read never
      // carries run_id and a run read never carries project_id.
      const params = new URLSearchParams({ scope: query.scope });
      if (query.scope === "project" && query.projectId) params.set("project_id", query.projectId);
      if (query.scope === "run" && query.runId) params.set("run_id", query.runId);
      if (query.before) params.set("before", query.before);
      return run(
        request(fetcher, `/portal/api/chat?${params.toString()}`, { method: "GET" }, (body) => decodeBody(body).pipe(Effect.flatMap((text) => ChatRead.decodeJson(text)))),
        signal,
      );
    },
    chatMessage(payload: ChatMessageRequest, csrf: string, signal: AbortSignal): Promise<ChatMessageOutcome> {
      return run(
        request(fetcher, "/portal/api/chat/messages", { method: "POST", body: payload, csrf }, chatMessageResponse),
        signal,
      );
    },
    /**
     * The four control attempts. Each carries the session read's live CSRF
     * token from its caller and runs through the shared request boundary, so a
     * control-plane refusal arrives as the server's typed `ServerRefusal` — the
     * same contract every other write surface consumes — and a 401 flows to the
     * session-expiry owner like every read.
     */
    startWork(workItemId: string, actionId: ActionId, csrf: string, signal: AbortSignal): Promise<StartOutcome> {
      return run(
        request(
          fetcher,
          `/portal/api/work-items/${encodeURIComponent(workItemId)}/run`,
          { method: "POST", body: { action_id: actionId }, csrf },
          startResponse,
        ),
        signal,
      );
    },
    cancelWork(workItemId: string, generation: ExecutionGeneration, actionId: ActionId, csrf: string, signal: AbortSignal): Promise<CancelOutcome> {
      return run(
        request(
          fetcher,
          `/portal/api/work-items/${encodeURIComponent(workItemId)}/cancel`,
          { method: "POST", body: { action_id: actionId, generation }, csrf },
          cancelResponse,
        ),
        signal,
      );
    },
    retryWork(workItemId: string, generation: ExecutionGeneration, actionId: ActionId, csrf: string, signal: AbortSignal): Promise<RetryOutcome> {
      return run(
        request(
          fetcher,
          `/portal/api/work-items/${encodeURIComponent(workItemId)}/retry`,
          { method: "POST", body: { action_id: actionId, generation }, csrf },
          retryResponse,
        ),
        signal,
      ).then((outcome) => ({
        disposition: outcome.disposition,
        execution: outcome.execution,
        command: outcome.command,
        workItemId: outcome.work_item.id,
      }));
    },
    provideInput(
      workItemId: string,
      input: Record<string, unknown>,
      waitingTransitionId: string,
      actionId: ActionId,
      csrf: string,
      signal: AbortSignal,
    ): Promise<InputOutcome> {
      return run(
        request(
          fetcher,
          `/portal/api/work-items/${encodeURIComponent(workItemId)}/input`,
          { method: "POST", body: { input, action_id: actionId, waiting_transition_id: waitingTransitionId }, csrf },
          inputResponse,
        ),
        signal,
      );
    },
  };
}

const receipt = (body: Uint8Array) =>
  decodeBody(body).pipe(
    Effect.flatMap((text) => Schema.decodeUnknown(Schema.parseJson(ConnectionReceiptBody))(text)),
    Effect.map((value) => ({ disposition: value.disposition, connectionId: value.connection.id })),
  );

const resourceReceipt = (body: Uint8Array) =>
  decodeBody(body).pipe(
    Effect.flatMap((text) => Schema.decodeUnknown(Schema.parseJson(ResourceReceiptBody))(text)),
    Effect.map((value) => ({
      resourceId: value.resource.id,
      provider: value.resource.provider,
      status: value.resource.status,
      syncStatus: value.resource.sync_status,
      syncStatusMessage: value.resource.status_message,
      lastSyncedAt: value.resource.last_synced_at,
      version: value.resource.version,
    })),
  );

const CommandErrorBody = Schema.Struct({
  error: Schema.Struct({
    code: Schema.String,
    message: Schema.String,
    current_version: Schema.optional(Schema.Number),
    current_revision: Schema.optional(Schema.Number),
    allowed_actions: Schema.optional(Schema.Array(Schema.String)),
    details: Schema.optional(Schema.Unknown),
  }),
});

/**
 * One Goal command, authenticated by the portal session's CSRF token. Every
 * refusal is returned as `MutationRefused` carrying exactly the members the
 * server sent, so the surface renders the conflict instead of guessing it; a
 * transport or session failure keeps its own read error.
 */
export async function postGoalCommand(
  fetcher: typeof fetch,
  csrfToken: string,
  goalId: string,
  envelope: unknown,
): Promise<unknown> {
  return postPortalCommand(fetcher, csrfToken, `/portal/api/goals/${encodeURIComponent(goalId)}/commands`, envelope);
}

/** One authenticated portal command POST with the shared refusal decoding. */
export async function postPortalCommand(
  fetcher: typeof fetch,
  csrfToken: string,
  path: string,
  body: unknown,
): Promise<unknown> {
  let response: Response;
  try {
    response = await fetcher(path, {
      method: "POST",
      credentials: "same-origin",
      headers: {
        Accept: "application/json",
        "content-type": "application/json",
        "x-csrf-token": csrfToken,
      },
      body: JSON.stringify(body),
      cache: "no-store",
      redirect: "manual",
    });
  } catch {
    throw new Network({ message: "Cannot reach the server. Check your connection and try again." });
  }
  if (response.type === "opaqueredirect" || response.status === 302 || response.status === 303) {
    throw new Unauthenticated({ message: "Your session has ended. Sign in to continue." });
  }
  const document = Schema.decodeUnknownOption(Schema.parseJson(Schema.Unknown))(await response.text());
  const payload = document._tag === "Some" ? document.value : null;
  if (response.ok) return payload;
  throw refusal(response.status, payload);
}

function refusal(status: number, document: unknown): ReadError | MutationRefused {
  const parsed = Schema.decodeUnknownOption(CommandErrorBody)(document);
  if (parsed._tag === "Some") {
    const { code, message, current_version, current_revision, allowed_actions, details } = parsed.value.error;
    return new MutationRefused(status, code, message, {
      ...(current_version === undefined ? {} : { current_version }),
      ...(current_revision === undefined ? {} : { current_revision }),
      ...(allowed_actions === undefined ? {} : { allowed_actions }),
      ...(details === undefined ? {} : { details }),
    });
  }
  return statusError(status);
}

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
