import { Schema } from "effect";

/**
 * Goal commands and their canonical identities.
 *
 * The control plane hashes a plan proposal with `RequestHash.canonical/1`: one
 * JSON document with lexicographically sorted object keys, array order
 * preserved, Elixir `Jason` scalar encoding. `canonicalJson`/`canonicalDigest`
 * reproduce exactly that encoding so the surface can present the server's own
 * proposal unchanged; the server still validates the hash and refuses a
 * mismatch, so nothing here grants authority.
 */

export const GoalCommandEnvelope = Schema.Struct({
  schema_version: Schema.Literal("symmetry.goal_command.v1"),
  mutation_id: Schema.String,
  expected_version: Schema.Number,
  expected_revision: Schema.Number,
  kind: Schema.String,
  payload: Schema.Unknown,
});
export type GoalCommandEnvelope = typeof GoalCommandEnvelope.Type;

export type CommandKind =
  | "request_plan"
  | "accept_plan"
  | "activate"
  | "pause"
  | "resume"
  | "cancel"
  | "amend"
  | "resolve_decision"
  | "achieve";

/**
 * Canonical JSON for request hashing: object keys sorted by UTF-16 code unit
 * order, arrays preserved, no insignificant whitespace. Values that this
 * surface does not send (functions, symbols, `undefined`) throw instead of
 * silently hashing a different document.
 */
export function canonicalJson(value: unknown): string {
  if (value === null || typeof value === "boolean") return JSON.stringify(value);
  if (typeof value === "number") {
    if (!Number.isFinite(value)) throw new TypeError("canonical JSON rejects non-finite numbers");
    if (!Number.isInteger(value) && Math.abs(value) < 1e-6 && value !== 0) return value.toExponential();
    return JSON.stringify(value);
  }
  if (typeof value === "string") return JSON.stringify(value);
  if (Array.isArray(value)) return `[${value.map(canonicalJson).join(",")}]`;
  if (typeof value === "object") {
    const entries = Object.entries(value as Record<string, unknown>);
    if (entries.some(([, nested]) => nested === undefined)) {
      throw new TypeError("canonical JSON rejects undefined members");
    }
    const sorted = entries.sort(([left], [right]) => (left < right ? -1 : left > right ? 1 : 0));
    return `{${sorted.map(([key, nested]) => `${JSON.stringify(key)}:${canonicalJson(nested)}`).join(",")}}`;
  }
  throw new TypeError(`canonical JSON rejects ${typeof value} values`);
}

/** `sha256:<64 lowercase hex>` over the canonical JSON document. */
export async function canonicalDigest(value: unknown): Promise<string> {
  const bytes = new TextEncoder().encode(canonicalJson(value));
  const digest = await crypto.subtle.digest("SHA-256", bytes);
  return `sha256:${Array.from(new Uint8Array(digest), (byte) => byte.toString(16).padStart(2, "0")).join("")}`;
}

/**
 * A refusal from the command surface. `details` carries exactly what the server
 * sent (for example the stale conflict's `current_version`, `current_revision`
 * and `allowed_actions`); the surface never invents a missing member.
 */
export class MutationRefused extends Error {
  readonly status: number;
  readonly code: string;
  readonly details: Record<string, unknown>;

  constructor(status: number, code: string, message: string, details: Record<string, unknown>) {
    super(message);
    this.name = "MutationRefused";
    this.status = status;
    this.code = code;
    this.details = details;
  }

  get stale(): boolean {
    return this.status === 409 && (this.code === "stale" || this.code === "stale_revision");
  }
}

export function refuseMessage(error: unknown): string {
  return error instanceof MutationRefused
    ? error.message
    : "The server could not complete this action. Try again.";
}

/** UUIDv4 mutation identity, one per logical action attempt. */
export function newMutationId(): string {
  return crypto.randomUUID();
}

export function commandEnvelope(
  kind: CommandKind,
  payload: Record<string, unknown>,
  goal: { version: number; current_revision: number },
  mutationId: string = newMutationId(),
): GoalCommandEnvelope {
  return {
    schema_version: "symmetry.goal_command.v1",
    mutation_id: mutationId,
    expected_version: goal.version,
    expected_revision: goal.current_revision,
    kind,
    payload,
  };
}
