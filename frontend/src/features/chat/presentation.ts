/**
 * Presentation of the scoped engineering conversation.
 *
 * Every rendered value is the server's own: the intent vocabulary is the server's, the
 * scope label names the conversation read's answer rather than the URL that asked for
 * it, receipts are derived by `features/work/receipts.ts` (applied only from a served
 * acknowledgement outcome) and a served value becomes an anchor only through the one
 * safe-URL helper http(s) admits — `services/served.ts`; the work vocabulary stays in
 * `features/work/presentation.ts`. Raw execution data is never a source here: evidence
 * rows read the read's semantic timeline only.
 */
import { decisionRecord, executionStateLabel } from "../work/presentation";
import { receiptLabel, receiptOutcome, type ReceiptCommand, type ReceiptOutcome } from "../work/receipts";
import type { ChatConversationMessage, ChatConversationRun, ChatScope, Runtime } from "../../services/api";
import { asRecord, asRecords, asText, safeUrl } from "../../services/served";
import type { ChatHistoryPage, ChatIntent, ChatSendStatus } from "./scopes";

/** The runtime row's adapter record as served; absent for a legacy registration. */
export type ChatAdapterRecord = NonNullable<NonNullable<Runtime["capabilities"]>["adapter"]>;

const intentLabels: Record<ChatIntent, string> = {
  discuss: "Discuss",
  status: "Status",
  start_work: "Start work",
  guidance: "Guidance",
  decision: "Decision",
  pause: "Pause",
  resume: "Resume",
  cancel: "Cancel",
};

/** The readable label of one server intent; an unknown served intent renders verbatim. */
export function chatIntentLabel(intent: string): string {
  return intent in intentLabels ? intentLabels[intent as ChatIntent] : intent;
}

/** The served run members the intent list is filtered by. */
export interface ChatRunSignals {
  readonly canGuide: boolean;
  readonly canPause: boolean;
  readonly canResume: boolean;
  /** True when the resolved runtime row advertises no safe-boundary pause. */
  readonly pauseUnsupported: boolean;
}

/**
 * The intents this scope may propose: the server's vocabulary filtered by the
 * scope and the served state, never invented. A run offers guidance, pause and
 * resume only while its own served flags allow them (and no pause is offered
 * when the runtime's adapter says it cannot pause). A waiting decision is
 * never offered here: its packet's own buttons are the only path that carries
 * the waiting transition identity the server requires. Cancel and retry stay
 * on the work detail, which owns them.
 */
export function applicableChatIntents(options: {
  readonly scope: ChatScope;
  readonly run: ChatRunSignals | null;
  readonly hasProjects: boolean;
}): readonly ChatIntent[] {
  if (options.scope === "project") return ["discuss", "status", "start_work"];
  if (options.scope === "run") {
    const run = options.run;
    return [
      "discuss",
      "status",
      ...(run !== null && run.canGuide ? (["guidance"] as const) : []),
      ...(run !== null && run.canPause && !run.pauseUnsupported ? (["pause"] as const) : []),
      ...(run !== null && run.canResume ? (["resume"] as const) : []),
    ];
  }
  return options.hasProjects ? ["discuss", "status", "start_work"] : ["discuss", "status"];
}

/** The URL-derived scope, shown with a pending marker until the read answers. */
function requestedScopeLabel(scope: ChatScope, projectId: string | null, runId: string | null): string {
  switch (scope) {
    case "project": return projectId === null ? "Project conversation — no project selected" : `Project conversation — ${projectId}`;
    case "run": return runId === null ? "Run conversation — no run selected" : `Run conversation — ${runId}`;
    case "workspace": return "Workspace conversation";
  }
}

interface ChatScopeContextInput {
  /** The URL's own scope members. */
  readonly urlScope: ChatScope;
  readonly urlProjectId: string | null;
  readonly urlRunId: string | null;
  /** The conversation read's answer; null while no read has answered. */
  readonly served: { readonly scope: ChatScope; readonly projectId: string | null; readonly runId: string | null } | null;
  /** Whether a read is in flight for a complete scope. */
  readonly pending: boolean;
  /** The served project row the read's `project_id` resolves to, when loaded. */
  readonly project: { readonly name: string; readonly key: string } | null;
  /** The served run projection's identity, when the read carries it. */
  readonly run: { readonly workItemKey: string | null; readonly generation: number | null } | null;
}

/**
 * The conversation's scope as the server resolved it. Until the read answers
 * this names the URL's request with a pending marker; afterwards it names the
 * read's own `scope`/`project_id`/`run_id` members and never the URL's claim.
 */
export function chatScopeContext(input: ChatScopeContextInput): string {
  if (input.served === null) {
    const requested = requestedScopeLabel(input.urlScope, input.urlProjectId, input.urlRunId);
    return input.pending ? `${requested} · pending` : requested;
  }
  switch (input.served.scope) {
    case "workspace":
      return "Workspace conversation";
    case "project": {
      const projectId = input.served.projectId;
      if (projectId === null) return "Project conversation";
      const project = input.project;
      return `Project conversation — ${project === null ? projectId : `${project.name} (${project.key})`}`;
    }
    case "run": {
      const runId = input.served.runId;
      const key = input.run?.workItemKey ?? runId;
      const attempt = input.run?.generation === null || input.run?.generation === undefined ? "" : ` · Attempt ${input.run.generation}`;
      return `Run conversation — ${key ?? "run not recorded"}${attempt}`;
    }
  }
}

/**
 * How the resolved runtime delivers guidance. The adapter's own operation value
 * is the only authority; a runtime without an adapter record is a legacy
 * registration whose guidance arrives at the next safe execution boundary.
 * A generic adapter is a legacy registration too — its `guidance:
 * "unsupported"` describes native steering, while the supervisory protocol this
 * runtime advertises with `supervisory_control` delivers guidance at the
 * worker's next safe execution boundary, which is what the server's own reply
 * and `can_guide` report.
 */
export function guidanceDeliveryLabel(adapter: ChatAdapterRecord | null | undefined): string {
  if (adapter === null || adapter === undefined) return "Guidance is delivered at the worker's next safe execution boundary";
  if (adapter.kind === "generic") return "Guidance is delivered at the worker's next safe execution boundary";
  switch (adapter.operations.guidance) {
    case "native_steer": return "Guidance is native steering for this runtime and is not dispatched from this conversation";
    case "next_turn": return "Guidance is delivered on the worker's next turn";
    case "unsupported": return "Guidance is unsupported by this runtime's adapter";
    default: return "Guidance delivery is not advertised by this runtime's adapter record";
  }
}

/** The explanation rendered when the runtime cannot pause at a safe boundary. */
export const pauseUnsupportedLabel = "This runtime cannot pause at a safe boundary; stop the run from the work detail.";

/**
 * True when the runtime's adapter advertises no pause operation. A generic
 * adapter's `pause: "unsupported"` means "no native pause": the legacy
 * supervisory protocol this runtime advertises with `supervisory_control`
 * pauses at a safe boundary, which is what the server's own `can_pause` and
 * reply report, so a generic registration is never labelled unsupported.
 */
export function pauseUnsupported(adapter: ChatAdapterRecord | null | undefined): boolean {
  if (adapter === null || adapter === undefined) return false;
  if (adapter.kind === "generic") return false;
  return adapter.operations.pause === "unsupported";
}

/** A message's receipt outcome: a durable outcome, or the retained-page provenance. */
type ChatReceiptOutcome = ReceiptOutcome | "retained";

/** The served execution state as a sentence-cased label; an unknown state keeps its honest name. */
export function chatStateLabel(state: string): string {
  const label = executionStateLabel(state) ?? state;
  return `${label.slice(0, 1).toUpperCase()}${label.slice(1)}`;
}

export interface ChatReceiptView {
  readonly outcome: ChatReceiptOutcome;
  readonly headline: string;
}

/**
 * The receipt one message renders, or null when the message carries no command.
 * `applied` requires the served acknowledgement outcome; a command the control
 * plane applied or rejected itself renders the plane's own outcome, and a
 * supervisory command the plane recorded applied from the runtime's transition
 * report renders applied-unacknowledged until the runtime acknowledges it; a
 * command only an older retained page carries is never refreshed against the
 * current page, so it says exactly that instead of claiming an outcome.
 */
export function chatReceiptView(command: ReceiptCommand | null, carriedByCurrentPage: boolean): ChatReceiptView | null {
  if (command === null) return null;
  if (!carriedByCurrentPage) return { outcome: "retained", headline: "Acknowledgement not refreshed with this page" };
  const outcome = receiptOutcome(command);
  // The conversation's two phrasings; every other outcome renders the work
  // surface's own label verbatim.
  const headline = outcome === "queued" ? "Saved · awaiting agent acknowledgement" : receiptLabel(outcome);
  return { outcome, headline };
}

/** The status region's exact text for the scope's send state. */
export function chatSendStatusText(status: ChatSendStatus): string {
  switch (status.kind) {
    case "idle": return "Messages are saved to this context.";
    case "sending": return "Sending…";
    case "saved-question": return "Saved to this context.";
    case "saved-command": return "Message saved; the transcript receipt tracks the acknowledgement.";
    case "refused": return `Refused: ${status.message}`;
    // A decision attempt keeps no composer draft: its retry is the option
    // button, which reuses the frozen identity exactly for the same option.
    case "unconfirmed": return status.source === "decision"
      ? "Decision not confirmed. Choosing the same option again reuses the same action identity."
      : "Send not confirmed. The draft is kept; retry reuses the same action identity.";
    case "notice": return status.message;
  }
}

/** One served value rendered as a link only when its scheme is http(s). */
interface ServedLink {
  readonly text: string;
  readonly href: string | null;
}

export function servedLink(value: unknown): ServedLink | null {
  const text = asText(value);
  return text === null ? null : { text, href: safeUrl(text) };
}

/** The scope-context links: the work item's pull request and its CI build. */
export function chatRunLinks(run: { readonly pull_request_url?: string | null; readonly delivery?: { readonly ci?: { readonly url?: string | null } | null } | null } | null): readonly { readonly label: string; readonly link: ServedLink }[] {
  if (run === null) return [];
  const links: { label: string; link: ServedLink }[] = [];
  const pullRequest = servedLink(run.pull_request_url);
  if (pullRequest !== null) links.push({ label: "Pull request", link: pullRequest });
  const ci = servedLink(run.delivery?.ci?.url);
  if (ci !== null) links.push({ label: "CI", link: ci });
  return links;
}

interface ChatHistoryGap {
  /** The oldest message id of the chain the served read reached. */
  readonly current: string;
  /** The newest message id among the retained pages the served read's chain did not reach. */
  readonly retained: string;
}

interface ChatHistoryView {
  /** Oldest first: the retained older entries, then the current page in its served order. */
  readonly messages: readonly ChatConversationMessage[];
  /** The ids the current page carries; only their receipts are refreshed. */
  readonly currentIds: ReadonlySet<string>;
  readonly gap: ChatHistoryGap | null;
  readonly loadCursor: string | null;
}

const messageOrder = (left: ChatConversationMessage, right: ChatConversationMessage): number => {
  const leftTime = left.inserted_at;
  const rightTime = right.inserted_at;
  if (leftTime !== null && rightTime !== null && leftTime !== rightTime) return leftTime < rightTime ? -1 : 1;
  if (left.id === right.id) return 0;
  return left.id < right.id ? -1 : 1;
};

/** The retained older pages as one oldest-first list; the current page's copies win. */
function retainedMessages(pages: readonly ChatHistoryPage[], currentIds: ReadonlySet<string>): readonly ChatConversationMessage[] {
  const seen = new Set<string>();
  const union: ChatConversationMessage[] = [];
  // Most recently loaded page first, so a message two pages both carry keeps the
  // copy from the newer fetch; a copy the current page owns is never repeated.
  for (const page of [...pages].reverse()) {
    for (const message of page.messages) {
      if (seen.has(message.id) || currentIds.has(message.id)) continue;
      seen.add(message.id);
      union.push(message);
    }
  }
  return union.sort(messageOrder);
}

/**
 * The retained history as rendered: the current page keeps its own order, older
 * messages precede it oldest first and duplicate ids render once (from the
 * refreshed copy). The gap is reported only while a retained page is off the
 * chain the served page's boundary reaches: every page on that chain renders as
 * one contiguous history, and the region between the chain and a page it did
 * not reach is exactly what the note names.
 */
export function chatHistoryView(pages: readonly ChatHistoryPage[], nextCursor: string | null): ChatHistoryView {
  const newest = pages[0];
  if (newest === undefined) return { messages: [], currentIds: new Set(), gap: null, loadCursor: nextCursor };
  const older = pages.slice(1);
  const currentIds = new Set(newest.messages.map((message) => message.id));
  const retained = retainedMessages(older, currentIds);
  const gap = historyGap(newest, older);
  return { messages: [...retained, ...newest.messages], currentIds, gap, loadCursor: nextCursor };
}

function historyGap(newest: ChatHistoryPage, older: readonly ChatHistoryPage[]): ChatHistoryGap | null {
  if (older.length === 0 || newest.nextBefore === null || newest.messages.length === 0) return null;
  // The chain the served page's boundary reaches, followed the way a load
  // continues: it starts at the current page and gains every retained page that
  // overlaps a message it carries or was fetched from one — such a page holds
  // the messages just below that boundary. Reachability assumes a serve lists
  // history in commit order: a message committed after a page was fetched can
  // carry an earlier inserted_at than a visible message and then hide between
  // two chained pages without a note (the server orders by inserted_at while
  // its per-action lock does not serialize the scope). The comparison is by
  // message id: a served cursor is
  // re-signed per response, so token bytes are not a stable identity across
  // reads (two signs in the same millisecond can even coincide).
  //
  // Every retained page has to be reached. A chain a moved boundary left behind
  // must not be excused by a later page that does reach the served boundary —
  // that page proves only its own region, and rendering the pages below it
  // beside the chain would present the region between the two as contiguous. A
  // page from the moved boundary joins the chain when Load older loads that
  // region into it.
  const attached = new Set<string>(newest.messages.map((message) => message.id));
  const attachedMessages: ChatConversationMessage[] = [...newest.messages];
  const remaining = [...older];
  for (let reached = true; reached;) {
    reached = false;
    for (let index = remaining.length - 1; index >= 0; index -= 1) {
      const page = remaining[index]!;
      const chained = page.fetchBeforeId !== null && attached.has(page.fetchBeforeId);
      if (!chained && !page.messages.some((message) => attached.has(message.id))) continue;
      for (const message of page.messages) attached.add(message.id);
      attachedMessages.push(...page.messages);
      remaining.splice(index, 1);
      reached = true;
    }
  }
  const unreached = remaining.flatMap((page) => page.messages);
  if (unreached.length === 0) return null;
  // The note names the region between the two: the chain's own oldest message
  // and the newest point of the history it did not reach.
  const current = attachedMessages.reduce((oldest, message) => (messageOrder(message, oldest) < 0 ? message : oldest));
  const retained = unreached.reduce((newestMessage, message) => (messageOrder(message, newestMessage) > 0 ? message : newestMessage));
  return { current: current.id, retained: retained.id };
}

/** One recorded semantic execution entry, rendered as inert text. */
interface ChatEvidenceRow {
  readonly key: string;
  readonly source: "event" | "transition" | "command";
  readonly kind: string;
  readonly summary: string | null;
}

/**
 * The run's recorded execution evidence, oldest first. Only the read's semantic
 * timeline is a source; `raw.timeline` is blank in a chat read and is never
 * touched. An entry with no readable payload still names its kind and state.
 */
export function chatEvidenceRows(run: ChatConversationRun | null): readonly ChatEvidenceRow[] {
  if (run === null) return [];
  const rows: ChatEvidenceRow[] = [];
  for (const [index, item] of [...run.timeline].reverse().entries()) {
    if (item.source === "event") {
      const kind = item.data.kind;
      const payload = item.data.payload;
      const state = asText(payload.state);
      const detail = asText(payload.message) ?? asText(payload.summary) ?? asText(payload.rationale);
      rows.push({ key: `event:${item.data.event_id}:${index}`, source: "event", kind, summary: detail ?? state });
    } else if (item.source === "transition") {
      const from = asText(item.data.payload.from);
      rows.push({
        key: `transition:${item.data.transition_id}:${index}`,
        source: "transition",
        kind: item.data.state,
        summary: from === null ? null : `From ${from}`,
      });
    } else {
      const kind = item.data.kind;
      const outcome = receiptOutcome({
        kind,
        state: item.data.state,
        acknowledgement_outcome: item.data.acknowledgement_outcome,
        acknowledgement_id: asText(item.data.acknowledgement_id),
        applied_at: item.data.applied_at,
      });
      rows.push({ key: `command:${item.data.command_id}:${index}`, source: "command", kind, summary: receiptLabel(outcome) });
    }
  }
  return rows;
}

/** The served waiting projection members the decision packet reads. */
interface ChatWaitingProjection {
  readonly transition_id: string;
  readonly generation: number;
  readonly question: string | null;
  readonly decision?: unknown;
  readonly payload?: unknown;
}

export interface ChatDecisionOption {
  readonly id: string;
  readonly label: string | null;
  readonly consequence: string | null;
  readonly recommended: boolean;
}

interface ChatDecisionPacket {
  readonly context: string | null;
  readonly question: string | null;
  readonly transitionId: string;
  readonly generation: number;
  readonly options: readonly ChatDecisionOption[];
}

/**
 * The waiting transition's decision packet, or null when the served snapshot
 * carries none. The agent-supplied packet stays open JSON, so its members are
 * narrowed here; both the extraction result and the raw payload are read,
 * exactly as the work surface reads a decision packet.
 */
export function chatDecisionPacket(waiting: ChatWaitingProjection | null): ChatDecisionPacket | null {
  if (waiting === null) return null;
  const packet = decisionRecord(waiting);
  if (packet === null) return null;
  const recommended = asText(packet.recommended_option_id);
  const options = asRecords(packet.options).flatMap((option): ChatDecisionOption[] => {
    const id = asText(option.id);
    if (id === null) return [];
    return [{ id, label: asText(option.label), consequence: asText(option.consequence), recommended: recommended === id }];
  });
  return {
    context: asText(packet.context),
    question: asText(waiting.question),
    transitionId: waiting.transition_id,
    generation: waiting.generation,
    options,
  };
}

/** The chat surface's failure policy: `message`, then `summary`, then `error`, verbatim (no prefix). */
function chatFailureText(failure: unknown): string | null {
  const record = asRecord(failure);
  if (record === null) return null;
  return asText(record.message) ?? asText(record.summary) ?? asText(record.error);
}

interface ChatFailureView {
  readonly state: "failed" | "cancelled";
  readonly text: string;
}

/**
 * The served failure of a run that ended, bounded the way the classic
 * conversation bounded it: the first readable string, cut at 2,000 characters,
 * or the generic sentence when the records carry none. Never a raw object.
 */
export function chatFailureView(state: unknown, failure: unknown, outcomeFailure: unknown): ChatFailureView | null {
  const executionState = asText(state);
  if (executionState !== "failed" && executionState !== "cancelled") return null;
  const text = chatFailureText(failure) ?? chatFailureText(outcomeFailure);
  if (text === null) return { state: executionState, text: "The run ended without a recorded failure message." };
  return { state: executionState, text: text.length > 2000 ? `${text.slice(0, 2000)}…` : text };
}
