import {
  WorkDetailRead,
  WorkTimelineRead,
  WorkspaceRead,
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

export type ReadError = Unauthenticated | Forbidden | Conflict | Validation | Unsupported | Network | UnexpectedResponse;
export type Workspace = SymmetryWorkspaceReadV1;
export type Project = Workspace["projects"][number];
export type WorkItem = Project["work_items"][number];
export type WorkDetail = SymmetryWorkDetailReadV1;
export type WorkTimeline = SymmetryWorkTimelineReadV1;
export type TimelineItem = WorkTimeline["items"][number];

const Session = Schema.Struct({ csrf_token: Schema.NonEmptyString });
export type Session = typeof Session.Type;
const runtime = ManagedRuntime.make(Layer.empty);

export interface Api {
  workspace: (projectId: string | null, signal: AbortSignal) => Promise<Workspace>;
  workDetail: (workItemId: string, signal: AbortSignal) => Promise<WorkDetail>;
  workTimeline: (workItemId: string, before: string | null, signal: AbortSignal) => Promise<WorkTimeline>;
  session: (signal: AbortSignal) => Promise<Session>;
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
      transient: status === 408 || status === 429 || status === 502 || status === 503 || status === 504,
    });
  }
}

function request<A>(
  fetcher: typeof fetch,
  path: string,
  decode: (body: Uint8Array) => Effect.Effect<A, unknown>,
): Effect.Effect<A, ReadError> {
  return Effect.gen(function* () {
    const { response, body } = yield* Effect.tryPromise({
      try: async (signal) => {
        const response = await fetcher(path, {
          method: "GET",
          credentials: "same-origin",
          headers: { Accept: "application/json" },
          cache: "no-store",
          redirect: "manual",
          signal,
        });
        return { response, body: response.ok ? new Uint8Array(await response.arrayBuffer()) : new Uint8Array(0) };
      },
      catch: () => new Network({ message: "Cannot reach the server. Check your connection and try again." }),
    });
    if (response.type === "opaqueredirect" || response.status === 302 || response.status === 303) {
      return yield* Effect.fail(statusError(401));
    }
    if (!response.ok) return yield* Effect.fail(statusError(response.status));
    return yield* decode(body).pipe(Effect.mapError(() => new UnexpectedResponse({
      message: "The server response could not be read safely. Refresh or contact your operator.",
      status: response.status,
      transient: false,
    })));
  });
}

function run<A>(effect: Effect.Effect<A, ReadError>, signal: AbortSignal): Promise<A> {
  return runtime.runPromise(Effect.either(effect), { signal }).then((result) => {
    if (Either.isLeft(result)) throw result.left;
    return result.right;
  });
}

export function createApi(fetcher: typeof fetch = globalThis.fetch.bind(globalThis)): Api {
  return {
    workspace(projectId: string | null, signal: AbortSignal): Promise<Workspace> {
      const query = projectId === null ? "" : `?${new URLSearchParams({ project_id: projectId })}`;
      return run(request(fetcher, `/portal/api/workspace${query}`, (body) => decodeBody(body).pipe(Effect.flatMap((text) => WorkspaceRead.decodeJson(text)))), signal);
    },
    workDetail(workItemId: string, signal: AbortSignal): Promise<WorkDetail> {
      return run(request(fetcher, `/portal/api/work-items/${encodeURIComponent(workItemId)}`, (body) => decodeBody(body).pipe(Effect.flatMap((text) => WorkDetailRead.decodeJson(text)))), signal);
    },
    workTimeline(workItemId: string, before: string | null, signal: AbortSignal): Promise<WorkTimeline> {
      // The cursor is an opaque signed token: only the served `before` direction exists.
      const query = before === null ? "" : `?${new URLSearchParams({ before })}`;
      return run(request(fetcher, `/portal/api/work-items/${encodeURIComponent(workItemId)}/timeline${query}`, (body) => decodeBody(body).pipe(Effect.flatMap((text) => WorkTimelineRead.decodeJson(text)))), signal);
    },
    session(signal: AbortSignal): Promise<Session> {
      return run(request(fetcher, "/portal/api/session", (body) => decodeBody(body).pipe(Effect.flatMap((text) => Schema.decodeUnknown(Schema.parseJson(Session))(text)))), signal);
    },
  };
}

export function readRetry(failureCount: number, error: unknown): boolean {
  return failureCount < 2 && (error instanceof Network || (error instanceof UnexpectedResponse && error.transient));
}

export function errorMessage(error: unknown): string {
  return error instanceof Unauthenticated || error instanceof Forbidden || error instanceof Conflict ||
    error instanceof Validation || error instanceof Unsupported || error instanceof Network || error instanceof UnexpectedResponse
    ? error.message
    : "The workspace could not be loaded. Try again.";
}
