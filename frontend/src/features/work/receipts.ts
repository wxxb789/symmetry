/**
 * Durable-evidence receipts for the execution controls on the work detail.
 *
 * A receipt only ever says what the server's own records say: `applied`
 * requires an `applied` acknowledgement outcome, a command the runtime has not
 * answered stays `queued`, and evidence that cannot confirm an attempt stays
 * `unknown` — never a success. `attemptReceiptView` renders local attempts (a
 * press, or a request a reload reconstructed from its stored attempt identity)
 * with the same rule: an unconfirmed attempt is never `applied`.
 */
import type { ActionKind } from "../../services/control-actions";
import { asRecord, asText } from "../../services/served";
import { timelineItemKey, type TimelineItemLike } from "./presentation";

/**
 * The outcomes durable evidence can produce. `applied` requires the runtime's
 * own acknowledgement outcome. `deferred` is the retry the control plane
 * re-queued (moving the task into a new queued generation) without a runtime
 * acknowledgement — it is never rendered as `applied`, and it is distinct from
 * `queued`, which awaits the runtime's acknowledgement of a command delivered
 * to it. `control_applied` is the cancel the control plane applied itself on
 * queued or assigned work: the command never reached a runtime, so no
 * acknowledgement exists or is implied. `applied_unacknowledged` is a pause,
 * resume or provide_input the plane recorded `applied` from the runtime's own
 * transition report before the runtime's acknowledgement arrived; the same
 * command renders `applied` once that acknowledgement is recorded. `refused`
 * is the control plane's own typed refusal of a local attempt, and
 * `not_sent` a request this surface refused before sending it (no storable
 * attempt identity), so nothing reached the server; the runtime's rejection of
 * a delivered command stays `rejected` with its own text, and the control
 * plane's pre-dispatch rejection (recorded without an acknowledgement id) is
 * `control_rejected`, so a control-plane decision is never attributed to the
 * runtime.
 */
export type ReceiptOutcome = "applied" | "control_applied" | "applied_unacknowledged" | "rejected" | "control_rejected" | "refused" | "failed" | "not_sent" | "queued" | "deferred" | "unknown";

/** What a receipt card renders: a durable outcome, or `requested` while an attempt is unconfirmed. */
type ReceiptViewOutcome = ReceiptOutcome | "requested";

/** The served command projection members a receipt outcome is derived from. */
export interface ReceiptCommand {
  /** The served command kind; `retry` is the one kind the control plane re-queues. */
  readonly kind: string;
  readonly state: string;
  readonly acknowledgement_outcome: string | null;
  /**
   * The runtime's acknowledgement identity. A null id means no runtime
   * acknowledgement is recorded: the control plane applied the command itself
   * (a cancel on queued/assigned work), re-queued it (a retry), or recorded a
   * supervisory command from the runtime's transition report before the
   * acknowledgement arrived.
   */
  readonly acknowledgement_id: string | null;
  readonly applied_at?: string | null;
}

/**
 * The served acknowledgement outcomes. An `applied` receipt requires the
 * matching outcome: a command state alone never proves one.
 */
const outcomesByAcknowledgement: Record<string, ReceiptOutcome> = {
  applied: "applied",
  rejected: "rejected",
  failed: "failed",
};

/** Command states recorded while the runtime has not acknowledged the command yet. */
const pendingCommandStates: Record<string, true> = { pending: true };

/**
 * Derives one receipt outcome from the served command projection. A command
 * whose state is still in flight and whose acknowledgement outcome is null is
 * `queued`, never `applied`; a command the plane recorded `applied` without an
 * acknowledgement is the re-queued `retry` (`deferred`), the plane's own
 * cancel (`control_applied`), or a supervisory kind the plane recorded from
 * the runtime's transition report (`applied_unacknowledged`); anything the
 * evidence cannot place — including an `applied` state no writer produces for
 * that kind — is `unknown`.
 */
export function receiptOutcome(command: ReceiptCommand | null): ReceiptOutcome {
  if (command === null) return "unknown";
  const acknowledgement = command.acknowledgement_outcome;
  if (acknowledgement !== null) {
    if (acknowledgement === "rejected" && command.acknowledgement_id === null) return "control_rejected";
    return outcomesByAcknowledgement[acknowledgement] ?? "unknown";
  }
  if (pendingCommandStates[command.state] === true) return "queued";
  if (command.state === "applied") {
    // The command state is a closed set (pending|applied|acknowledged), and an
    // applied state without an acknowledgement is always plane-recorded: the
    // served kind names what the plane recorded and who applied it — the
    // re-queued retry, the plane's own cancel on queued/assigned work, or a
    // supervisory kind recorded from the runtime's transition report (whose
    // acknowledgement may still arrive, rendering the command `applied`).
    if (command.kind === "retry") return "deferred";
    if (command.kind === "cancel") return "control_applied";
    if (command.kind === "pause" || command.kind === "resume" || command.kind === "provide_input") return "applied_unacknowledged";
    return "unknown";
  }
  return "unknown";
}

const receiptLabels: Record<ReceiptOutcome, string> = {
  applied: "Applied",
  control_applied: "Applied by the control plane",
  applied_unacknowledged: "Applied by the runtime; acknowledgement not recorded",
  rejected: "Rejected by the runtime",
  control_rejected: "Rejected by the control plane",
  refused: "Refused by the server",
  failed: "Failed",
  not_sent: "Not sent",
  queued: "Awaiting agent acknowledgement",
  deferred: "Queued for the runtime",
  unknown: "Outcome unknown",
};

/** The exact text each durable outcome renders. */
export function receiptLabel(outcome: ReceiptOutcome): string {
  return receiptLabels[outcome];
}

/** The in-flight receipt text; it is never `Applied` while the server has not answered. */
const requestSentLabel = "Request sent…";
const requestQueuedLabel = "Request queued…";

/** The newest receipts a list renders; older ones are counted instead of dropped silently. */
export const receiptRowLimit = 8;

interface ReceiptRow {
  readonly key: string;
  readonly commandId: string | null;
  readonly kind: string;
  readonly generation: number | null;
  readonly recordedAt: string | null;
  readonly outcome: ReceiptOutcome;
}

interface ReceiptHistory {
  readonly rows: readonly ReceiptRow[];
  readonly hidden: number;
}

/**
 * The rendered receipt history: one row per served timeline `command` entry,
 * newest first in served order. Guidance, pause and resume receipts render with
 * their own served kind and outcome and are never turned into controls;
 * task-scoped cancel/retry entries keep their served null generation.
 */
export function receiptRows(
  detailTimeline: readonly TimelineItemLike[],
  olderPages: readonly (readonly TimelineItemLike[])[],
): ReceiptHistory {
  const rows: ReceiptRow[] = [];
  const seen = new Set<string>();
  for (const item of [...detailTimeline, ...olderPages.flat()]) {
    if (item.source !== "command") continue;
    const command = asRecord(item.data);
    if (command === null) continue;
    const commandId = asText(command.command_id);
    // The same command can be served by the detail read and an older page; the
    // command id is its durable identity, and the timeline key the fallback.
    const key = commandId ?? timelineItemKey(item);
    if (seen.has(key)) continue;
    seen.add(key);
    const kind = asText(command.kind) ?? "command";
    const state = asText(command.state) ?? "unknown";
    const acknowledgementOutcome = asText(command.acknowledgement_outcome);
    rows.push({
      key,
      commandId,
      kind,
      generation: typeof item.generation === "number" ? item.generation : null,
      recordedAt: asText(item.recorded_at),
      outcome: receiptOutcome({
        kind,
        state,
        acknowledgement_outcome: acknowledgementOutcome,
        acknowledgement_id: asText(command.acknowledgement_id),
        applied_at: asText(command.applied_at),
      }),
    });
  }
  return { rows: rows.slice(0, receiptRowLimit), hidden: Math.max(rows.length - receiptRowLimit, 0) };
}

/** A local failure of one attempt: a definitive server refusal, or an unconfirmed one. */
interface AttemptFailure {
  readonly outcome: ReceiptOutcome;
  readonly message: string | null;
}

/**
 * One control attempt. It is keyed by the action id the reader pressed, and by
 * the command id from the moment an accepted response carried one.
 */
export interface ControlAttempt {
  readonly kind: ActionKind;
  readonly actionId: string;
  /** The session-storage scope the action id belongs to: "initial", a generation, or a waiting transition. */
  readonly scope: string;
  /** Set once an accepted response carried a command; the attempt's durable identity from then on. */
  readonly commandId: string | null;
  /** The command state an accepted response reported. */
  readonly command: ReceiptCommand | null;
  /** Set once an accepted start response carried an execution. */
  readonly executionTaskId: string | null;
  /** The run generation the attempt is aimed at (cancel/retry) or produced (start), when served. */
  readonly generation: number | null;
  /** The detail snapshot version the attempt was recorded against; a later snapshot is a refresh of it. */
  readonly baseline: number | null;
  /** The execution state an accepted start response reported. */
  readonly responseState: string | null;
  /** A definitive server refusal; it survives refetches untouched. */
  readonly refusal: AttemptFailure | null;
  /** A non-definitive local failure; a refreshed snapshot that cannot confirm it renders `unknown`. */
  readonly unconfirmed: AttemptFailure | null;
}

export interface AttemptExecution {
  readonly task_id: string;
  readonly generation: number;
  readonly state: string;
}

export interface AttemptContext {
  /** The current detail snapshot version (`dataUpdatedAt` of the detail query). */
  readonly snapshotVersion: number;
  /** Command ids the current detail and timeline snapshots carry. */
  readonly carried: ReadonlySet<string>;
  /** The execution the current detail snapshot serves, if any. */
  readonly execution: AttemptExecution | null;
  /** Whether the attempt's mutation is still in flight. */
  readonly pending: boolean;
  /** Whether the in-flight mutation is paused because the browser is offline. */
  readonly paused: boolean;
}

interface AttemptView {
  readonly outcome: ReceiptViewOutcome;
  readonly headline: string;
  readonly message: string | null;
  readonly note: string | null;
}

/** Execution states in which no runtime has claimed the run yet. */
const preRuntimeStates: Record<string, true> = { queued: true, assigned: true };

const durableAuthorityNote = "Durable execution history is the authority for this attempt; it does not record a confirmation.";
const pendingNote = "The server has not confirmed this request yet.";
const queuedRunNote = "The run is recorded; no runtime has claimed it yet.";
/**
 * A recorded run no runtime has claimed is not a command awaiting an
 * acknowledgement: the pre-runtime view carries its own headline, distinct
 * from `requested` and from a runtime acknowledgement that cannot exist yet.
 */
const queuedRunLabel = "Awaiting a runtime";

const unknownView = (message: string | null): AttemptView => ({
  outcome: "unknown",
  headline: receiptLabel("unknown"),
  message,
  note: durableAuthorityNote,
});

const queuedRunView: AttemptView = {
  outcome: "queued",
  headline: queuedRunLabel,
  message: null,
  note: queuedRunNote,
};

/**
 * The receipt one local attempt renders, or null once durable evidence owns it
 * (after a refreshed snapshot carries the attempt's command) and there is
 * nothing left to say. A null return means the attempt no longer renders.
 */
export function attemptReceiptView(attempt: ControlAttempt, context: AttemptContext): AttemptView | null {
  if (context.pending) {
    return {
      outcome: "requested",
      headline: context.paused ? requestQueuedLabel : requestSentLabel,
      message: null,
      note: pendingNote,
    };
  }
  if (attempt.refusal !== null) {
    return {
      outcome: attempt.refusal.outcome,
      headline: receiptLabel(attempt.refusal.outcome),
      message: attempt.refusal.message,
      note: null,
    };
  }
  const refreshed = attempt.baseline !== null && context.snapshotVersion > attempt.baseline;
  if (attempt.commandId !== null) {
    // A refreshed snapshot that carries the command owns the receipt; one that
    // does not can never prove it, so the attempt renders unknown.
    if (context.carried.has(attempt.commandId)) return null;
    if (refreshed) return unknownView(null);
    const outcome = receiptOutcome(attempt.command);
    return { outcome, headline: receiptLabel(outcome), message: null, note: null };
  }
  if (attempt.executionTaskId !== null) {
    const served = context.execution !== null && context.execution.task_id === attempt.executionTaskId ? context.execution : null;
    if (served !== null) {
      // A later generation supersedes this attempt; the detail owns the state.
      if (attempt.generation !== null && served.generation > attempt.generation) return null;
      return preRuntimeStates[served.state] === true ? queuedRunView : null;
    }
    if (refreshed) return unknownView(attempt.unconfirmed?.message ?? null);
    return attempt.responseState !== null && preRuntimeStates[attempt.responseState] === true ? queuedRunView : null;
  }
  if (attempt.unconfirmed !== null && !refreshed) {
    return {
      outcome: attempt.unconfirmed.outcome,
      headline: receiptLabel(attempt.unconfirmed.outcome),
      message: attempt.unconfirmed.message,
      note: null,
    };
  }
  if (refreshed) return unknownView(attempt.unconfirmed?.message ?? null);
  return { outcome: "requested", headline: requestSentLabel, message: null, note: pendingNote };
}
