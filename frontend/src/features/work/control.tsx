/**
 * Execution controls for the work detail.
 *
 * Every control renders only from the server's own signal: `can_start`,
 * `can_cancel`, `can_retry` and `execution.waiting`. The surface never derives
 * a permission or transition rule from task state — `can_cancel`/`can_retry`
 * already join the attempt's state with the runtime's advertised adapter
 * operations server-side — never renders a disabled live control for an
 * operation the server did not offer, and never shows a
 * queued request as applied: an in-flight attempt renders `requested`, and a
 * settled attempt renders only what the mutation's own command or the refreshed
 * snapshot proves. Guidance, safe-boundary pause and resume are dispatched from
 * Chat; this surface renders their receipts and no control for them. Work items
 * the server marks Goal-managed render no legacy control at all — the Goal is
 * stated as the authority instead. A refusal from the control plane renders as
 * the server's own typed refusal, never as the runtime's rejection.
 */
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useRouteContext } from "@tanstack/react-router";
import { createContext, useContext, useState, type ReactNode } from "react";
import { cancelWorkMutation, provideInputMutation, retryWorkMutation, startWorkMutation } from "../../app/application";
import { useSessionToken } from "../../app/session";
import { clearActionId, readActionId, readOrMintActionId, type ActionKind } from "../../services/control-actions";
import {
  Conflict,
  Forbidden,
  ServerRefusal,
  Unsupported,
  Validation,
  errorMessage,
  type ActionId,
  type ControlReceipt,
  type ExecutionGeneration,
  type TimelineItem,
  type WorkDetail,
  type WorkTimeline,
} from "../../services/api";
import { asRecord, asText, executionStateLabel, hasDecisionPacket, timelineBoundary, timelineItemKey, timestamp, type TimelineItemLike } from "./presentation";
import {
  attemptReceiptView,
  receiptLabel,
  receiptRows,
  type AttemptExecution,
  type ControlAttempt,
  type ReceiptCommand,
} from "./receipts";

type WorkItemRow = WorkDetail["work_item"];

/** The placeholder key of an attempt the browser could not give an identity. */
const unavailableActionId = "unavailable";

interface PendingState {
  readonly pending: boolean;
  readonly paused: boolean;
}

/**
 * The older timeline pages the reader expanded, under the boundary of the first
 * page they were fetched from: the identity a served read has to still end at
 * for those pages to belong to its chain.
 */
interface RetainedPages {
  readonly workItemId: string;
  /** The boundary entry the chain starts at, when the read that fetched the pages proved one. */
  readonly boundary: string | null;
  /** The served cursor the pages were fetched with; the whole identity when no boundary entry is provable. */
  readonly cursor: string | null;
  readonly entries: readonly TimelineItem[];
}

interface ControlSurfaceValue {
  readonly workItemId: string;
  readonly attempts: readonly ControlAttempt[];
  readonly snapshotVersion: number;
  readonly execution: AttemptExecution | null;
  readonly pending: Record<ActionKind, PendingState>;
  /** The older timeline entries the served first page's boundary still chains to, shared by the history and the receipt list. */
  readonly olderEntries: readonly TimelineItem[];
  readonly retainTimelinePages: (data: { readonly pages: readonly WorkTimeline[] } | undefined) => void;
  readonly start: () => void;
  readonly cancel: (generation: ExecutionGeneration) => void;
  readonly retry: (generation: ExecutionGeneration) => void;
  readonly submitInput: (waitingTransitionId: string, answer: string, onConfirmed: () => void) => void;
}

const ControlSurfaceContext = createContext<ControlSurfaceValue | null>(null);

/** The detail's control state; the history section reads it for the retained older pages. */
export function useControlSurface(): ControlSurfaceValue {
  const value = useContext(ControlSurfaceContext);
  if (value === null) throw new Error("The work control surface must wrap its controls");
  return value;
}

/**
 * Owns the control attempts and the served snapshot for every control mount on
 * the detail, because the control bar, the waiting form and the receipt list
 * are separate mount points that must agree about one attempt's state, and the
 * receipt list also renders the older timeline pages the history section
 * expanded. A detail refresh re-keys the disabled timeline query, so those
 * pages are retained here rather than read from the current key's data — under
 * the boundary they were fetched from, so a read that ends elsewhere never
 * serves them.
 */
export function WorkControlSurface({ detail, cursor, children }: { detail: WorkDetail; cursor: string | null; children: ReactNode }) {
  const { application } = useRouteContext({ from: "__root__" });
  const queryClient = useQueryClient();
  const csrf = useSessionToken();
  const item = detail.work_item;
  const workItemId = item.id;
  const executionRow = item.execution ?? null;
  const goalManaged = asText(item.goal_id) !== null;

  const snapshotVersion = () => queryClient.getQueryState(["work-item", { workItemId }])?.dataUpdatedAt ?? 0;
  const [attempts, setAttempts] = useState<ControlAttempt[]>(() => (goalManaged ? [] : mountAttempts(item, snapshotVersion())));
  // The boundary this detail read's first page ends at. The pages a reader loads
  // are fetched from exactly this boundary, and the server signs a fresh cursor
  // for it on every read, so the boundary entry — not the token — is the identity
  // they are retained under: a served read that ends somewhere else does not
  // chain where they begin, and serving them beside it would present a gap as
  // contiguous history.
  const firstPage = detail.raw.timeline;
  const servedBoundary = cursor === null ? null : timelineBoundary(firstPage[firstPage.length - 1]);
  // Reopening the detail inside the freshness window remounts this component
  // while the disabled query still holds the pages the reader expanded.
  const [older, setOlder] = useState<RetainedPages>(() => ({
    workItemId,
    boundary: servedBoundary,
    cursor,
    entries: queryClient.getQueryData<{ pages: readonly WorkTimeline[] }>(["work-item-timeline", { workItemId, cursor }])?.pages.flatMap((page) => page.items) ?? [],
  }));
  // Every older page is served by a reader action, so the merge runs when that
  // fetch resolves, under the boundary the render that issued the fetch served.
  const retainTimelinePages = (data: { readonly pages: readonly WorkTimeline[] } | undefined) => {
    const fetched = data?.pages.flatMap((page) => page.items) ?? [];
    if (fetched.length === 0) return;
    setOlder((previous) => {
      const bound = retainedForServedChain(previous, workItemId, servedBoundary, cursor);
      const entries = mergeOlderEntries(fetched, bound ? previous.entries : []);
      return bound && entries === previous.entries ? previous : { workItemId, boundary: servedBoundary, cursor, entries };
    });
  };
  const olderEntries = retainedForServedChain(older, workItemId, servedBoundary, cursor) ? older.entries : [];

  const replace = (kind: ActionKind, attempt: ControlAttempt) => {
    setAttempts((previous) => [...previous.filter((candidate) => candidate.kind !== kind), attempt]);
  };
  const settle = (kind: ActionKind, patch: (attempt: ControlAttempt) => ControlAttempt) => {
    setAttempts((previous) => previous.map((attempt) => (attempt.kind === kind ? patch(attempt) : attempt)));
  };
  const invalidate = () => {
    void queryClient.invalidateQueries({ queryKey: ["work-item", { workItemId }] });
    // The timeline is only invalidated when no page read is in flight: a
    // settle must never cancel the older page a reader asked for.
    if (queryClient.isFetching({ queryKey: ["work-item-timeline", { workItemId }] }) === 0) {
      void queryClient.invalidateQueries({ queryKey: ["work-item-timeline", { workItemId }] });
    }
  };

  /**
   * Records one attempt before it is sent. The attempt identity survives a
   * reload in session storage; a browser without one refuses the request
   * instead of sending it without an idempotency identity.
   */
  const track = (kind: ActionKind, scope: string, generation: number | null): ActionId | null => {
    const actionId = readOrMintActionId(kind, workItemId, scope);
    if (actionId === null) {
      replace(kind, {
        ...emptyAttempt(kind, unavailableActionId, scope, generation, snapshotVersion()),
        refusal: { outcome: "not_sent", message: "This browser cannot store an attempt identity, so the request was not sent." },
      });
      return null;
    }
    replace(kind, emptyAttempt(kind, actionId, scope, generation, snapshotVersion()));
    return actionId;
  };

  const startMutation = useMutation(startWorkMutation(application.api));
  const cancelMutation = useMutation(cancelWorkMutation(application.api));
  const retryMutation = useMutation(retryWorkMutation(application.api));
  const inputMutation = useMutation(provideInputMutation(application.api));

  const start = () => {
    const actionId = track("start", "initial", null);
    if (actionId === null) return;
    startMutation.mutate({ workItemId, actionId, csrf }, {
      onSuccess: (outcome) => {
        // The accepted start's execution is the only durable evidence it has;
        // its receipt renders that run, never an acknowledgement it did not get.
        settle("start", (attempt) => ({
          ...attempt,
          baseline: snapshotVersion(),
          executionTaskId: outcome.execution.task_id,
          generation: outcome.execution.generation,
          responseState: outcome.execution.state,
        }));
        clearActionId("start", workItemId, "initial");
      },
      onError: (error) => {
        settle("start", (attempt) => ({ ...attempt, baseline: snapshotVersion(), ...failurePatch(error) }));
        if (definitiveRefusal(error)) clearActionId("start", workItemId, "initial");
      },
      onSettled: invalidate,
    });
  };

  const cancel = (generation: ExecutionGeneration) => {
    const scope = String(generation);
    const actionId = track("cancel", scope, generation);
    if (actionId === null) return;
    cancelMutation.mutate({ workItemId, generation, actionId, csrf }, {
      onSuccess: (outcome) => {
        settle("cancel", (attempt) => ({ ...attempt, baseline: snapshotVersion(), commandId: outcome.command.command_id, command: commandRecord(outcome.command) }));
        clearActionId("cancel", workItemId, scope);
      },
      onError: (error) => {
        settle("cancel", (attempt) => ({ ...attempt, baseline: snapshotVersion(), ...failurePatch(error) }));
        if (definitiveRefusal(error)) clearActionId("cancel", workItemId, scope);
      },
      onSettled: invalidate,
    });
  };

  const retry = (generation: ExecutionGeneration) => {
    const scope = String(generation);
    const actionId = track("retry", scope, generation);
    if (actionId === null) return;
    retryMutation.mutate({ workItemId, generation, actionId, csrf }, {
      onSuccess: (outcome) => {
        settle("retry", (attempt) => ({ ...attempt, baseline: snapshotVersion(), commandId: outcome.command.command_id, command: commandRecord(outcome.command) }));
        clearActionId("retry", workItemId, scope);
      },
      onError: (error) => {
        settle("retry", (attempt) => ({ ...attempt, baseline: snapshotVersion(), ...failurePatch(error) }));
        if (definitiveRefusal(error)) clearActionId("retry", workItemId, scope);
      },
      onSettled: invalidate,
    });
  };

  const submitInput = (waitingTransitionId: string, answer: string, onConfirmed: () => void) => {
    const actionId = track("input", waitingTransitionId, null);
    if (actionId === null) return;
    inputMutation.mutate({ workItemId, input: { answer }, waitingTransitionId, actionId, csrf }, {
      onSuccess: (outcome) => {
        settle("input", (attempt) => ({ ...attempt, baseline: snapshotVersion(), commandId: outcome.command.command_id, command: commandRecord(outcome.command) }));
        clearActionId("input", workItemId, waitingTransitionId);
        // The draft is cleared only here: a refusal keeps it for the reader.
        onConfirmed();
      },
      onError: (error) => {
        settle("input", (attempt) => ({ ...attempt, baseline: snapshotVersion(), ...failurePatch(error) }));
        if (definitiveRefusal(error)) clearActionId("input", workItemId, waitingTransitionId);
      },
      onSettled: invalidate,
    });
  };

  const pending: Record<ActionKind, PendingState> = {
    start: { pending: startMutation.isPending, paused: startMutation.isPaused },
    cancel: { pending: cancelMutation.isPending, paused: cancelMutation.isPaused },
    retry: { pending: retryMutation.isPending, paused: retryMutation.isPaused },
    input: { pending: inputMutation.isPending, paused: inputMutation.isPaused },
  };

  const value: ControlSurfaceValue = {
    workItemId,
    attempts,
    snapshotVersion: snapshotVersion(),
    execution: executionRow === null ? null : { task_id: executionRow.task_id, generation: executionRow.generation, state: executionRow.state },
    pending,
    olderEntries,
    retainTimelinePages,
    start,
    cancel,
    retry,
    submitInput,
  };
  return <ControlSurfaceContext.Provider value={value}>{children}</ControlSurfaceContext.Provider>;
}

/** The control bar under the detail chips: served capabilities only. */
export function WorkControls({ detail }: { detail: WorkDetail }) {
  const surface = useControlSurface();
  const item = detail.work_item;
  const goalId = asText(item.goal_id);
  if (goalId !== null) {
    return <section className="goal-managed" aria-labelledby="goal-managed-heading">
      <h3 id="goal-managed-heading">Goal-managed work</h3>
      <p>
        This work item's work is admitted by its Goal {goalId}; the Goal is the authority for starting, cancelling or
        retrying it. This surface offers no legacy run controls for Goal-managed work.
      </p>
    </section>;
  }
  const execution = item.execution ?? null;
  const generation = execution !== null && typeof execution.generation === "number" ? execution.generation : null;
  const state = execution === null ? null : executionStateLabel(execution.state);
  // Sentence case matches how the classic surface named execution states.
  const stateLabel = state === null ? null : `${state.slice(0, 1).toUpperCase()}${state.slice(1)}`;
  return <section className="control-bar" aria-label="Execution controls">
    {stateLabel !== null && <span className="chip">{stateLabel}</span>}
    {item.can_start === true && <button className="button control-button-primary" type="button" disabled={surface.pending.start.pending} onClick={surface.start}>Start run</button>}
    {execution?.can_cancel === true && generation !== null && <button className="button control-button-danger" type="button" disabled={surface.pending.cancel.pending} onClick={() => { surface.cancel(generation as ExecutionGeneration); }}>Cancel run</button>}
    {execution?.can_retry === true && generation !== null && <button className="button" type="button" disabled={surface.pending.retry.pending} onClick={() => { surface.retry(generation as ExecutionGeneration); }}>Retry</button>}
    <p className="control-note">
      <strong>Guidance, safe-boundary pause and resume are dispatched from Chat.</strong> This surface records their
      receipts and never renders them as live controls.
    </p>
  </section>;
}

/** The waiting form, rendered only for a waiting request without a decision packet. */
export function WorkWaitingInput({ detail }: { detail: WorkDetail }) {
  const surface = useControlSurface();
  const [draft, setDraft] = useState("");
  const item = detail.work_item;
  const waiting = item.execution?.waiting ?? null;
  const suppressed = asText(item.goal_id) !== null || waiting === null || hasDecisionPacket(waiting);
  if (suppressed || waiting === null) return null;
  return <section className="waiting-input" aria-labelledby="waiting-input-heading">
    <h3 id="waiting-input-heading">Agent needs a decision</h3>
    <p>{asText(waiting.question) ?? "Provide the requested input to continue."}</p>
    <form onSubmit={(event) => {
      event.preventDefault();
      // The served waiting transition from the snapshot that rendered this form.
      surface.submitInput(waiting.transition_id, draft, () => { setDraft(""); });
    }}>
      <input aria-label="Response" name="answer" required value={draft} onChange={(event) => { setDraft(event.target.value); }} disabled={surface.pending.input.pending} />
      <button className="button control-button-primary" type="submit" disabled={surface.pending.input.pending}>Send</button>
    </form>
  </section>;
}

/** The receipt list beside the execution history. */
export function WorkReceipts({ firstPage }: { firstPage: readonly TimelineItemLike[] }) {
  const surface = useControlSurface();
  // The history section owns the timeline query and the pages the reader
  // expanded; the receipt list renders that same retained state, bounded the
  // same way, so no row is served from a page the detail read no longer chains
  // to and none is dropped while the boundary still serves it.
  const olderPages = surface.olderEntries.length === 0 ? [] : [surface.olderEntries];
  // Only a rendered page marks a command carried: the served `latest_command`
  // can be absent from the capped page, and treating it as carried would drop
  // its attempt's receipt without the durable row replacing it.
  const carried = new Set<string>();
  for (const entry of [...firstPage, ...olderPages.flat()]) {
    if (entry.source !== "command") continue;
    const command = asRecord(entry.data);
    const commandId = command === null ? null : asText(command.command_id);
    if (commandId !== null) carried.add(commandId);
  }

  const history = receiptRows(firstPage, olderPages);
  const local = [...surface.attempts].reverse().flatMap((attempt) => {
    const view = attemptReceiptView(attempt, {
      snapshotVersion: surface.snapshotVersion,
      carried,
      execution: surface.execution,
      pending: surface.pending[attempt.kind].pending,
      paused: surface.pending[attempt.kind].paused,
    });
    if (view === null) return [];
    const scope = attempt.kind === "input" ? "waiting response" : attempt.generation === null ? "attempt" : `generation ${attempt.generation}`;
    return [{ key: `${attempt.kind}:${attempt.actionId}`, kind: attempt.kind, scope, view }];
  });
  if (local.length === 0 && history.rows.length === 0) return null;
  return <>
    <h3>Control receipts</h3>
    <ul className="receipt-list">
      {local.map(({ key, kind, scope, view }) => <li className="receipt" data-outcome={view.outcome} key={key}>
        <p className="receipt-outcome">{view.headline}</p>
        <p className="muted text-small">{kind} · {scope}</p>
        {view.message !== null && <p className="text-small">{view.message}</p>}
        {view.note !== null && <p className="muted text-small">{view.note}</p>}
      </li>)}
      {history.rows.map((row) => {
        const scope = row.generation === null ? "task-scoped" : `generation ${row.generation}`;
        const recorded = row.recordedAt === null ? null : timestamp(row.recordedAt)?.readable ?? row.recordedAt;
        return <li className="receipt" data-outcome={row.outcome} key={row.key}>
          <p className="receipt-outcome">{receiptLabel(row.outcome)}</p>
          <p className="muted text-small">{row.kind.replaceAll("_", " ")} · {scope}{recorded === null ? "" : ` · ${recorded}`}</p>
        </li>;
      })}
    </ul>
    {history.hidden > 0 && <p className="muted text-small">
      {history.hidden} older {history.hidden === 1 ? "receipt is" : "receipts are"} recorded in the execution history.
    </p>}
  </>;
}

function emptyAttempt(kind: ActionKind, actionId: string, scope: string, generation: number | null, baseline: number): ControlAttempt {
  return {
    kind,
    actionId,
    scope,
    commandId: null,
    command: null,
    executionTaskId: null,
    generation,
    baseline,
    responseState: null,
    refusal: null,
    unconfirmed: null,
  };
}

/**
 * Merges newly served older pages into the entries already expanded: a page
 * re-served for the same entry replaces its own text while everything the
 * reader already expanded stays rendered.
 */
function mergeOlderEntries(fetched: readonly TimelineItem[], retained: readonly TimelineItem[]): readonly TimelineItem[] {
  const fetchedKeys = new Set(fetched.map(timelineItemKey));
  const merged = [...fetched, ...retained.filter((item) => !fetchedKeys.has(timelineItemKey(item)))];
  return merged.length === retained.length && merged.every((item, index) => item === retained[index]) ? retained : merged;
}

/**
 * Whether retained pages were fetched for the chain the served read is on. Two
 * reads prove they are on the same chain by ending at the same boundary entry —
 * the entry the cursor's own position names. When neither read proves one, only
 * the very same served cursor does: the server mints a fresh token per read, so
 * an equal token means the detail was not re-served at all.
 */
function retainedForServedChain(retained: RetainedPages, workItemId: string, servedBoundary: string | null, cursor: string | null): boolean {
  return retained.workItemId === workItemId
    && (retained.boundary === null ? servedBoundary === null && retained.cursor === cursor : retained.boundary === servedBoundary);
}

/**
 * Reconstructs the attempts a reload kept: an action id still in session
 * storage marks a request this surface sent and never saw confirmed, and the
 * operation's own served signal must still offer it.
 */
function mountAttempts(item: WorkItemRow, baseline: number): ControlAttempt[] {
  const attempts: ControlAttempt[] = [];
  const execution = item.execution ?? null;
  const probe = (kind: ActionKind, scope: string, generation: number | null) => {
    const actionId = readActionId(kind, item.id, scope);
    if (actionId !== null) attempts.push(emptyAttempt(kind, actionId, scope, generation, baseline));
  };
  if (item.can_start === true) probe("start", "initial", null);
  if (execution?.can_cancel === true && typeof execution.generation === "number") probe("cancel", String(execution.generation), execution.generation);
  if (execution?.can_retry === true && typeof execution.generation === "number") probe("retry", String(execution.generation), execution.generation);
  const waiting = execution?.waiting ?? null;
  if (waiting !== null && !hasDecisionPacket(waiting)) probe("input", waiting.transition_id, null);
  return attempts;
}

function commandRecord(command: ControlReceipt): ReceiptCommand {
  return {
    kind: command.kind,
    state: command.state,
    acknowledgement_outcome: command.acknowledgement_outcome,
    acknowledgement_id: command.acknowledgement_id ?? null,
    applied_at: command.applied_at ?? null,
  };
}

/** A definitive server refusal: it is a refusal receipt and its attempt id is dead. */
function definitiveRefusal(error: unknown): boolean {
  return error instanceof Conflict || error instanceof Validation || error instanceof Forbidden || error instanceof Unsupported ||
    (error instanceof ServerRefusal && error.status >= 400 && error.status < 500);
}

function failurePatch(error: unknown): Pick<ControlAttempt, "refusal" | "unconfirmed"> {
  const message = errorMessage(error);
  // A control-plane refusal is the server's own decision: it renders as a
  // refusal, never as the runtime's rejection or as a local failure.
  if (definitiveRefusal(error)) return { refusal: { outcome: "refused", message }, unconfirmed: null };
  // A lost response or an unproven fault: the request may have been applied, so
  // it stays unknown until the server's own records place it.
  return { refusal: null, unconfirmed: { outcome: "unknown", message } };
}

