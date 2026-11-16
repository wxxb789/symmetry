/**
 * Presentation of work-item data for the project surface and the work detail.
 * Every rendered value is either read from the decoded envelope or explicitly
 * labelled as unknown; nothing here invents acceptance, cost, or freshness.
 */

import type { SymmetryWorkspaceReadV1, SymmetryWorkTimelineReadV1 } from "@symmetry/contracts";

type Text = string | null | undefined;

/** Workspace rows are the served shape that carries assignee, delivery and execution. */
type WorkItemRecord = SymmetryWorkspaceReadV1["projects"][number]["work_items"][number];

/**
 * Presentation reads consumed members one at a time, so they are all optional
 * here: a served payload missing one must still render as a labelled view.
 */
type Loosen<T> = { readonly [K in keyof T]?: T[K] };

type AssigneeLike = Loosen<NonNullable<WorkItemRecord["assignee"]>>;
type DeliveryEntryLike = Loosen<NonNullable<WorkItemRecord["delivery"]>["ci"]>;
type DeliveryLike = Loosen<NonNullable<WorkItemRecord["delivery"]>>;
type ExecutionLike = Loosen<NonNullable<WorkItemRecord["execution"]>>;

/**
 * Free-form JSON that the wire carries inside decoded envelopes (agent payloads,
 * task result maps, evidence rows). Those values have no field contract, so
 * readers narrow them here rather than guessing DTO shapes.
 */
export function asText(value: unknown): string | null {
  return typeof value === "string" && value.trim() !== "" ? value : null;
}

export function asRecord(value: unknown): Record<string, unknown> | null {
  if (typeof value !== "object" || value === null || Array.isArray(value)) return null;
  return value as Record<string, unknown>;
}

export function asRecords(value: unknown): Record<string, unknown>[] {
  return Array.isArray(value) ? value.map(asRecord).filter((entry): entry is Record<string, unknown> => entry !== null) : [];
}

/**
 * Links in served payloads are untrusted text: only http(s) URLs become anchors,
 * so a tampered provider or agent payload cannot inject a script scheme.
 */
export function safeUrl(value: unknown): string | null {
  const raw = asText(value);
  if (raw === null) return null;
  try {
    const link = new URL(raw);
    return link.protocol === "http:" || link.protocol === "https:" ? raw : null;
  } catch {
    return null;
  }
}

// The surface serves no usage or cost records for a work item, so cost is always
// explicitly unknown and must never be rendered as a zero amount.
export const costLabel = "Cost unknown";
export const costHint = "No usage or cost records are served for this work item.";

// Acceptance needs an identity this read surface does not serve; reported
// execution output is shown separately and never relabelled as accepted.
export const acceptanceLabel = "Acceptance not recorded";
export const noAcceptedEvidence = "No accepted evidence available";
export const noReportedResult = "No reported result";
export const noReportedOutput = "No reported execution output";
export const noBlocker = "No blocker";
export const noNextAction = "No next action recorded";

export function ownerLabel(assignee: AssigneeLike | null | undefined): string {
  const type = asText(assignee?.type);
  const name = asText(assignee?.name);
  const profile = asText(assignee?.agent_profile);
  if (type === "agent") return profile ? `Agent · ${profile}` : name ? `Agent · ${name}` : "Agent";
  if (type === "human") return name ?? "Human owner not recorded";
  if (type === "unassigned" || type === null) return "Unassigned";
  return name ?? `Unknown owner type: ${type}`;
}

export function ownerKey(assignee: AssigneeLike | null | undefined): string {
  const type = asText(assignee?.type) ?? "unassigned";
  const name = asText(assignee?.name);
  const profile = asText(assignee?.agent_profile);
  if (type === "agent") return `agent:${profile ?? name ?? "unrecorded"}`;
  if (type === "human") return `human:${name ?? "unrecorded"}`;
  if (type === "unassigned") return "unassigned";
  return `type:${type}`;
}

/** Inverse of `ownerKey`, so a filter carried in the URL can be labelled before its rows load. */
export function ownerKeyLabel(key: string): string {
  if (key === "unassigned") return "Unassigned";
  const separator = key.indexOf(":");
  const kind = separator === -1 ? key : key.slice(0, separator);
  const value = separator === -1 ? "" : key.slice(separator + 1);
  if (kind === "agent") return `Agent · ${value}`;
  if (kind === "human") return value;
  return `Unknown owner type: ${value || kind}`;
}

export function priorityLabel(priority: Text): string | null {
  const value = asText(priority);
  if (value === null) return null;
  return value === "no_priority" ? "no priority" : value.replaceAll("_", " ");
}

const executionStates = [
  "queued",
  "assigned",
  "claimed",
  "running",
  "paused",
  "waiting_for_input",
  "completed",
  "failed",
  "cancelled",
] as const;

export function executionStateLabel(state: Text): string | null {
  const value = asText(state);
  if (value === null) return null;
  return executionStates.includes(value as (typeof executionStates)[number])
    ? value.replaceAll("_", " ")
    : `Unknown execution state: ${value}`;
}

export interface ReportedResult {
  text: string | null;
  kind: string | null;
}

export function failureText(failure: unknown): string | null {
  const record = asRecord(failure);
  if (record === null) return null;
  const reason = asText(record["reason"]) ?? asText(record["message"]) ?? asText(record["summary"]);
  return reason === null ? null : `Failed: ${reason}`;
}

/**
 * Reported output is served either as text or as structured data; structured
 * values render as their JSON text so nothing is silently dropped, and values
 * with no renderable form stay null.
 */
export function reportedText(value: unknown): string | null {
  const text = asText(value);
  if (text !== null) return text;
  if (Array.isArray(value)) return value.length === 0 ? null : JSON.stringify(value);
  const record = asRecord(value);
  if (record !== null) return Object.keys(record).length === 0 ? null : JSON.stringify(record);
  if (typeof value === "number" || typeof value === "boolean") return String(value);
  return null;
}

export function reportedResult(execution: ExecutionLike | null | undefined): ReportedResult {
  const result = asRecord(execution?.result);
  return {
    text: reportedText(result?.["summary"]) ?? reportedText(result?.["message"]) ?? failureText(execution?.failure),
    kind: result === null ? null : asText(result["kind"]),
  };
}

export interface EvidenceAttribution {
  source: string;
  generation: number | null;
  recordedAt: string | null;
}

export function attributionOf(value: unknown): EvidenceAttribution | null {
  const record = asRecord(value);
  if (record === null) return null;
  const source = asText(record["source"]);
  const generation = typeof record["generation"] === "number" ? record["generation"] : null;
  const recordedAt = asText(record["recorded_at"]);
  if (source === null && generation === null && recordedAt === null) return null;
  return { source: source ?? "unlabelled", generation, recordedAt };
}

export interface EvidenceRow {
  text: string;
  attribution: EvidenceAttribution | null;
}

export function evidenceRows(values: unknown): EvidenceRow[] {
  const entries = Array.isArray(values) ? values : [values];
  return entries.flatMap((value) => {
    const record = asRecord(value);
    if (record === null) {
      const text = reportedText(value);
      return text === null ? [] : [{ text, attribution: null }];
    }
    const name = asText(record["name"]);
    const status = asText(record["status"]);
    const text = name !== null && status !== null
      ? `${name} · ${status}`
      : asText(record["message"]) ?? asText(record["title"]) ?? name ?? reportedText(record["summary"]) ?? asText(record["path"]) ?? asText(record["url"]) ?? asText(record["text"]);
    return text === null ? [] : [{ text, attribution: attributionOf(record) }];
  });
}

export interface CiLabel {
  text: string;
  tone: "unknown" | "pending" | "failed" | "passed" | "stale";
  title: string;
}

/** Bound-resource fields the CI chip reads; every other served member is additive. */
export interface CiResourceLike {
  readonly status?: Text;
  readonly sync_status?: Text;
}

/** A CI status is only rendered as fresh green while its recorded time is known and recent. */
const ciFreshnessWindowMs = 24 * 60 * 60 * 1000;

/**
 * Only a timestamp that carries its own zone names an instant; looser served
 * text cannot support a freshness claim.
 */
const strictAbsoluteTimestamp = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}(:\d{2}(\.\d+)?)?(Z|[+-]\d{2}:\d{2})$/;

/** Served bound-resource states that must override a passing CI signal. */
const problemResourceStatuses = ["failed", "offline", "degraded"];

/** The first bound-resource problem in served order, or null when the binding is clean. */
function boundResourceSignal(resource: CiResourceLike | null | undefined): string | null {
  if (resource === null || resource === undefined) return null;
  const syncStatus = asText(resource.sync_status);
  if (syncStatus !== null && syncStatus !== "synced") return syncStatus;
  const status = asText(resource.status);
  return status !== null && problemResourceStatuses.includes(status) ? status : null;
}

export function ciLabel(delivery: DeliveryLike | null | undefined, resource?: CiResourceLike | null, now: number = Date.now()): CiLabel {
  const ci: DeliveryEntryLike | null | undefined = delivery?.ci;
  const status = (asText(ci?.status) ?? "unknown").toLocaleLowerCase();
  const recordedAt = asText(ci?.recorded_at);
  const signal = boundResourceSignal(resource);
  const title = `CI source: ${asText(ci?.source) ?? "none"}${recordedAt === null ? " · no recorded time" : ` · recorded ${recordedAt}`}${status === "passed" && signal !== null ? ` · bound resource ${signal}` : ""}`;
  if (status === "unknown") return { text: "CI unknown", tone: "unknown", title };
  if (status !== "passed") return { text: `CI ${status}`, tone: status === "failed" ? "failed" : "pending", title };
  if (signal !== null) return { text: `CI passed · bound resource ${signal}`, tone: "stale", title };
  const recorded = recordedAt === null || !strictAbsoluteTimestamp.test(recordedAt) ? Number.NaN : Date.parse(recordedAt);
  if (Number.isNaN(recorded)) return { text: "CI passed · freshness unknown", tone: "unknown", title };
  if (asText(ci?.source) !== "provider") return { text: "CI passed · unverified source", tone: "unknown", title };
  if (resource === null || resource === undefined) return { text: "CI passed · binding not confirmed", tone: "unknown", title };
  if (now - recorded > ciFreshnessWindowMs) return { text: "CI passed · stale", tone: "stale", title };
  return { text: "CI passed", tone: "passed", title };
}

/** The project read's bound resources, addressable by id for the CI chip on each row. */
export function ciResourcesOf(project: unknown): ReadonlyMap<string, CiResourceLike> {
  const resources = new Map<string, CiResourceLike>();
  for (const record of asRecords(asRecord(project)?.["resources"])) {
    const id = asText(record["id"]);
    if (id !== null) resources.set(id, { status: asText(record["status"]), sync_status: asText(record["sync_status"]) });
  }
  return resources;
}

export interface DeliveryRow {
  label: string;
  value: string;
  source: string;
  recordedAt: string | null;
  url: string | null;
}

export function deliveryRows(delivery: DeliveryLike | null | undefined, resource?: CiResourceLike | null, now: number = Date.now()): DeliveryRow[] {
  const rows: DeliveryRow[] = [];
  const pullRequest = delivery?.pull_request;
  const review = delivery?.review;
  const pullStatus = asText(pullRequest?.status);
  const pullUrl = asText(pullRequest?.url);
  rows.push({
    label: "Pull request",
    value: pullStatus ?? (pullUrl === null ? "not recorded" : "linked"),
    source: asText(pullRequest?.source) ?? "none",
    recordedAt: asText(pullRequest?.recorded_at),
    url: safeUrl(pullUrl),
  });
  rows.push({
    label: "CI",
    value: ciLabel(delivery, resource, now).text,
    source: asText(delivery?.ci?.source) ?? "none",
    recordedAt: asText(delivery?.ci?.recorded_at),
    url: safeUrl(delivery?.ci?.url),
  });
  rows.push({
    label: "Review",
    value: asText(review?.status) ?? "none",
    source: asText(review?.source) ?? "none",
    recordedAt: asText(review?.recorded_at),
    url: safeUrl(review?.url),
  });
  return rows;
}

export interface DecisionOption {
  label: string;
  consequence: string;
  recommended: boolean;
}

export interface DecisionView {
  question: string;
  reason: string | null;
  context: string | null;
  options: DecisionOption[];
}

/** Exact scope of a waiting decision: the transition that recorded it and the execution generation. */
export function decisionScopeLabel(waiting: unknown): string | null {
  const record = asRecord(waiting);
  if (record === null) return null;
  const transitionId = asText(record["transition_id"]);
  const generation = typeof record["generation"] === "number" ? record["generation"] : null;
  if (transitionId === null && generation === null) return null;
  return `Scope: ${transitionId === null ? "transition not recorded" : `transition ${transitionId}`}${generation === null ? "" : ` · generation ${generation}`}`;
}

export function decisionView(waiting: unknown, fallbackQuestion: string | null): DecisionView | null {
  const record = asRecord(waiting);
  if (record === null) return null;
  const payload = asRecord(record["payload"]);
  const decision = asRecord(record["decision"]) ?? (payload === null ? null : asRecord(payload["decision"]));
  const question = asText(record["question"]) ?? (payload === null ? null : asText(payload["question"])) ?? fallbackQuestion;
  if (question === null && decision === null) return null;
  const recommendation = decision === null ? null : asText(decision["recommended_option_id"]);
  const options = asRecords(decision?.["options"]).flatMap((option) => {
    const id = asText(option["id"]);
    const label = asText(option["label"]);
    if (label === null) return [];
    return [{ label, consequence: asText(option["consequence"]) ?? "No consequence recorded", recommended: id !== null && id === recommendation }];
  });
  return {
    question: question ?? "A decision is needed to continue.",
    reason: decision === null ? null : asText(decision["reason"]),
    context: decision === null ? null : asText(decision["context"]),
    options,
  };
}

const shortId = (value: string): string => (value.length > 13 ? `${value.slice(0, 8)}…` : value);

/** Reads the reported task result's proposed next action; advisory text only, never an executable command. */
export function nextActionLabel(result: unknown): string | null {
  const next = asRecord(asRecord(result)?.["proposed_next_action"]);
  if (next === null) return null;
  const kind = asText(next["kind"]);
  switch (kind) {
    case "validate": {
      const taskId = asText(next["producing_task_id"]);
      return taskId === null ? "Validate the produced work" : `Validate the produced work (task ${shortId(taskId)})`;
    }
    case "repair": {
      const workItemId = asText(next["work_item_id"]);
      const reason = asText(next["reason"]);
      return `Repair ${workItemId === null ? "the affected work item" : `work item ${shortId(workItemId)}`}${reason === null ? "" : `: ${reason}`}`;
    }
    case "observe": {
      const resourceId = asText(next["resource_id"]);
      const reference = asText(next["external_ref"]);
      return `Observe ${resourceId === null ? "the bound resource" : `resource ${shortId(resourceId)}`}${reference === null ? "" : ` (${reference})`}`;
    }
    case "replan": {
      const reason = asText(next["reason"]);
      return reason === null ? "Replan the remaining work" : `Replan: ${reason}`;
    }
    case "wait":
      return "Wait for the recorded blocker to clear";
    case null:
      return null;
    default:
      return `Recorded next action: ${kind.replaceAll("_", " ")}`;
  }
}

export function resultKindLabel(kind: string | null): string | null {
  return kind === null ? null : kind.replaceAll("_", " ");
}

/** Timeline rows arrive from both the detail read and the timeline page; only `data` stays free-form. */
export type TimelineItemLike = Omit<Loosen<SymmetryWorkTimelineReadV1["items"][number]>, "data"> & { readonly data?: unknown };

export interface TimelineView {
  label: string;
  headline: string | null;
  link: string | null;
}

const payloadKeys = ["summary", "message", "rationale", "question", "reason", "detail", "text", "path", "url", "status", "state", "kind"];

function payloadText(payload: Record<string, unknown> | null): string | null {
  if (payload === null) return null;
  for (const key of payloadKeys) {
    const value = asText(payload[key]);
    if (value !== null) return value;
  }
  return null;
}

export function timelineItemView(item: TimelineItemLike): TimelineView {
  const data = asRecord(item.data) ?? {};
  const payload = asRecord(data["payload"]);
  const source = asText(item.source) ?? "event";
  if (source === "event") {
    const kind = asText(data["kind"]) ?? "event";
    return {
      label: `event · ${kind.replaceAll("_", " ")}`,
      headline: kind === "test" ? [asText(payload?.["name"]), asText(payload?.["status"])].filter((part): part is string => part !== null).join(" · ") || payloadText(payload) : payloadText(payload),
      link: safeUrl(payload?.["url"]),
    };
  }
  if (source === "transition") {
    const state = asText(data["state"]) ?? "unknown";
    return { label: `transition · ${state.replaceAll("_", " ")}`, headline: payloadText(payload) ?? asText(payload?.["failure"]) ?? null, link: safeUrl(payload?.["url"]) };
  }
  if (source === "command") {
    const kind = asText(data["kind"]) ?? "command";
    const state = asText(data["state"]) ?? "unknown";
    const acknowledgement = asText(data["acknowledgement_outcome"]);
    return { label: `command · ${kind.replaceAll("_", " ")}`, headline: `Command ${state}${acknowledgement === null ? "" : ` · acknowledged ${acknowledgement}`}`, link: null };
  }
  return { label: source, headline: payloadText(payload), link: null };
}

export function timelineItemKey(item: TimelineItemLike): string {
  const data = asRecord(item.data) ?? {};
  const identity = asText(data["event_id"]) ?? asText(data["transition_id"]) ?? asText(data["command_id"]) ?? asText(item.recorded_at) ?? "recorded";
  return `${asText(item.source) ?? "event"}:${identity}:${typeof item.generation === "number" ? item.generation : ""}`;
}

const timestampFormat = new Intl.DateTimeFormat(undefined, { dateStyle: "medium", timeStyle: "short" });

export interface TimestampView {
  exact: string;
  readable: string;
}

export function timestamp(value: Text): TimestampView | null {
  const exact = asText(value);
  if (exact === null) return null;
  const parsed = Date.parse(exact);
  return { exact, readable: Number.isNaN(parsed) ? exact : timestampFormat.format(new Date(parsed)) };
}
