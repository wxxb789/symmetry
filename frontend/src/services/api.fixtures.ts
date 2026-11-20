/**
 * Served read payloads for boundary tests. Test-only data: application code never
 * imports this module, and the shapes mirror what the running control plane serves.
 */

import servedAttention from "../../../contracts/fixtures/valid/attention-read.basic.json";
import servedGoalRead from "../../../contracts/fixtures/valid/goal-read.active.json";

/**
 * The imported documents are captures of a real portal server's own responses
 * (sanitized identifiers only) validated against the contract bundle by the
 * portal controller gate and the Elixir fixture-manifest test. The served-read
 * fixtures below derive from them: overrides replace values, never member
 * names, so a test can never inject a spelling the server does not send.
 */
const clone = <T>(value: T): T => JSON.parse(JSON.stringify(value)) as T;

export interface ReadWorkItemOptions {
  id: string;
  projectId: string;
  title: string;
  key?: string;
  status?: string;
  blocked?: boolean;
  blocker?: string | null;
  /**
   * Fields merged over the baseline execution below, so a test names only the
   * values it reads; nested values (result, failure, waiting) replace wholesale.
   */
  execution?: Record<string, unknown>;
  /** Fields merged over the default delivery; a replaced entry must be a complete served value. */
  delivery?: Record<string, unknown>;
}

export interface ReadWorkDetailOptions extends ReadWorkItemOptions {
  nextBefore?: string | null;
  timeline?: unknown[];
  /** Served outcome summary (open JSON); null by default. */
  outcomeSummary?: unknown;
  /** Served outcome tests (open JSON value); empty by default. */
  outcomeTests?: unknown;
}

// The unrecorded delivery projection exactly as `effective_delivery` serves
// it: the `:url` fallback stays null, while the `:status` fallback is served
// as the value too ("unknown" for CI, "none" for review).
const readDelivery = () => ({
  pull_request: { value: null, source: "none", generation: null, url: null, status: null },
  ci: { value: "unknown", source: "none", generation: null, url: null, status: "unknown" },
  review: { value: "none", source: "none", generation: null, url: null, status: "none" },
});

/**
 * Baseline task projection for an item with a recorded execution: the read
 * boundary decodes the served execution whole, so a partial override must still
 * arrive inside a complete shape.
 */
const readExecution = () => ({
  task_id: "55555555-5555-4555-8555-555555555555",
  run_id: null,
  runtime_id: null,
  generation: 1,
  state: "running",
  waiting: null,
  latest_command: null,
  result: null,
  failure: null,
  timing: { state_since: null, started_at: null, finished_at: null },
  supervisory_control: false,
  can_guide: false,
  can_pause: false,
  can_resume: false,
  can_cancel: false,
  can_retry: false,
  intent_locked: false,
});

export function readWorkItem(options: ReadWorkItemOptions): Record<string, unknown> {
  return {
    id: options.id,
    key: options.key ?? "DEMO-1",
    project_id: options.projectId,
    title: options.title,
    description: null,
    status: options.status ?? "ready",
    priority: "high",
    blocked: options.blocked ?? false,
    blocker: options.blocker ?? null,
    assignee: { type: "agent", name: null, agent_profile: "default" },
    delivery: options.delivery === undefined ? readDelivery() : { ...readDelivery(), ...options.delivery },
    execution: options.execution === undefined ? null : { ...readExecution(), ...options.execution },
    external: null,
    updated_at: null,
  };
}

export function readWorkDetail(options: ReadWorkDetailOptions): Record<string, unknown> {
  const workItem = readWorkItem(options);
  return {
    work_item: workItem,
    outcome: {
      goal: options.title,
      phase: options.status ?? "ready",
      owner: workItem["assignee"],
      summary: options.outcomeSummary ?? null,
      findings: [],
      changed_artifacts: [],
      tests: options.outcomeTests ?? [],
      blocker: null,
      pull_request_url: null,
      ci_status: null,
      review_status: null,
      delivery: workItem["delivery"],
      result: null,
      failure: null,
    },
    timeline: [],
    raw: { task: null, timeline: options.timeline ?? [], next_before: options.nextBefore ?? null },
  };
}

export interface ReadAttentionOptions {
  selectedProjectId: string | null;
  blocked?: Array<{ projectId: string; projectName?: string | null; projectKey?: string | null; workItemId: string; key?: string; title: string; status?: string; blocker?: string | null }>;
  goals?: Array<{ goalId: string; projectId: string; title: string; state?: string; currentRevision?: number; blockerReasons?: string[]; attentionReasons?: string[]; activeTasks?: number; nonterminalTasks?: number; readyToAchieve?: boolean }>;
  nextCursor?: string | null;
  goalUnavailable?: { code: string; message: string } | null;
  projectUnavailable?: { code: string; message: string } | null;
}

/** The merged attention read as the portal API serves it, overridden per test. */
export function readAttention(options: ReadAttentionOptions): Record<string, unknown> {
  const document = clone(servedAttention);
  const goalTemplate = document.goal_attention.entries[0]!;
  const blockedTemplate = document.project_blocked.entries[0]!;

  return {
    ...document,
    selected_project_id: options.selectedProjectId,
    goal_attention: {
      ...document.goal_attention,
      entries: (options.goals ?? []).map((goal) => ({
        ...goalTemplate,
        goal_id: goal.goalId,
        project_id: goal.projectId,
        title: goal.title,
        state: goal.state ?? goalTemplate.state,
        current_revision: goal.currentRevision ?? goalTemplate.current_revision,
        blockers: [],
        blocker_reasons: goal.blockerReasons ?? [],
        settlement_attention: [],
        attention_reasons: goal.attentionReasons ?? [],
        active_tasks: goal.activeTasks ?? 0,
        nonterminal_tasks: goal.nonterminalTasks ?? 0,
        "ready_to_achieve?": goal.readyToAchieve ?? false,
      })),
      next_cursor: options.nextCursor ?? null,
      unavailable: options.goalUnavailable ?? null,
    },
    project_blocked: {
      ...document.project_blocked,
      entries: (options.blocked ?? []).map((entry) => ({
        ...blockedTemplate,
        project_id: entry.projectId,
        project_key: entry.projectKey ?? blockedTemplate.project_key,
        project_name: entry.projectName ?? blockedTemplate.project_name,
        work_item_id: entry.workItemId,
        work_item_key: entry.key ?? blockedTemplate.work_item_key,
        title: entry.title,
        status: entry.status ?? blockedTemplate.status,
        blocker: entry.blocker ?? null,
      })),
      unavailable: options.projectUnavailable ?? null,
    },
  };
}

export interface ReadGoalOptions {
  id: string;
  projectId: string;
  projectKey?: string | null;
  projectName?: string | null;
  title: string;
  state?: string;
  currentRevision?: number;
  version?: number;
  objective?: string;
  allowedActions?: string[];
  workItems?: Array<Record<string, unknown>>;
  decisions?: Array<Record<string, unknown>>;
  graph?: { nodes?: Array<Record<string, unknown>>; edges?: Array<Record<string, unknown>>; "cycle_detected?"?: boolean };
  accounting?: Record<string, unknown>;
  externalWaits?: Array<Record<string, unknown>>;
  acceptedOutcomes?: Array<Record<string, unknown>>;
  blockers?: Array<Record<string, unknown>>;
  settlementAttention?: Array<Record<string, unknown>>;
}

/** One Goal read as the portal API serves it, overridden per test. */
export function readGoalProjection(options: ReadGoalOptions): Record<string, unknown> {
  const document = clone(servedGoalRead);
  const state = options.state ?? document.state;
  const currentRevision = options.currentRevision ?? document.current_revision;
  const workItems = options.workItems ?? [];
  const requiredItems = workItems.filter((item) => item["required?"] !== false);
  const acceptedRequired = requiredItems.filter((item) => item["accepted?"] === true);

  return {
    ...document,
    id: options.id,
    project_id: options.projectId,
    project_key: options.projectKey ?? document.project_key,
    project_name: options.projectName ?? document.project_name,
    title: options.title,
    state,
    current_revision: currentRevision,
    version: options.version ?? document.version,
    lifecycle: { ...document.lifecycle, state },
    revision: {
      ...document.revision,
      revision: currentRevision,
      objective: options.objective ?? options.title,
    },
    blockers: options.blockers ?? [],
    blocker_reasons: (options.blockers ?? []).map((blocker) => (typeof blocker["reason"] === "string" ? blocker["reason"] : "")),
    settlement_attention: options.settlementAttention ?? [],
    completion: {
      ...document.completion,
      required_items: requiredItems.length,
      accepted_required_items: acceptedRequired.length,
      accepted_outcomes: (options.acceptedOutcomes ?? []).length,
    },
    allowed_actions: options.allowedActions ?? [],
    work_items: workItems,
    graph: options.graph === undefined ? { ...document.graph, nodes: [], edges: [] } : {
      ...document.graph,
      nodes: options.graph.nodes ?? [],
      edges: options.graph.edges ?? [],
      "cycle_detected?": options.graph["cycle_detected?"] ?? false,
    },
    decisions: options.decisions ?? [],
    accepted_outcomes: options.acceptedOutcomes ?? [],
    accounting: options.accounting ?? document.accounting,
    external_waits: options.externalWaits ?? [],
  };
}

/** One Goal audit receipt page as the portal event read serves it. */
export function readGoalEvents(entries: Array<{ sequence: number; kind: string; revision?: number | null }>, nextAfter: number | null): Record<string, unknown> {
  return {
    entries: entries.map((entry) => ({
      id: `event-${entry.sequence}`,
      sequence: entry.sequence,
      kind: entry.kind,
      actor_ref: "portal_operator",
      revision: entry.revision ?? 1,
      payload: {},
      response: {},
      inserted_at: "2026-11-16T10:00:00Z",
    })),
    next_after: nextAfter,
  };
}
