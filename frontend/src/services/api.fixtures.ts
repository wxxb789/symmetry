/**
 * Served read payloads for boundary tests. Test-only data: application code never
 * imports this module, and the shapes mirror what the running control plane serves.
 */

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

const readDelivery = () => ({
  pull_request: { value: null, source: "none", generation: null, url: null, status: null },
  ci: { value: null, source: "none", generation: null, url: null, status: "unknown" },
  review: { value: null, source: "none", generation: null, url: null, status: "none" },
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

/** The merged attention read exactly as the portal API serves it. */
export function readAttention(options: ReadAttentionOptions): Record<string, unknown> {
  return {
    selected_project_id: options.selectedProjectId,
    goal_attention: {
      entries: (options.goals ?? []).map((goal) => ({
        goal_id: goal.goalId,
        project_id: goal.projectId,
        title: goal.title,
        state: goal.state ?? "draft",
        current_revision: goal.currentRevision ?? 1,
        blockers: [],
        blocker_reasons: goal.blockerReasons ?? [],
        settlement_attention: [],
        attention_reasons: goal.attentionReasons ?? [],
        active_tasks: goal.activeTasks ?? 0,
        nonterminal_tasks: goal.nonterminalTasks ?? 0,
        "ready_to_achieve?": goal.readyToAchieve ?? false,
        updated_at: null,
      })),
      next_cursor: options.nextCursor ?? null,
      unavailable: options.goalUnavailable ?? null,
    },
    project_blocked: {
      entries: (options.blocked ?? []).map((entry) => ({
        project_id: entry.projectId,
        project_key: entry.projectKey ?? "DEMO",
        project_name: entry.projectName ?? "Demo project",
        project_status: "active",
        work_item_id: entry.workItemId,
        work_item_key: entry.key ?? "DEMO-1",
        title: entry.title,
        status: entry.status ?? "ready",
        blocked: true,
        blocker: entry.blocker ?? null,
        updated_at: null,
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
  graph?: { nodes?: Array<Record<string, unknown>>; edges?: Array<Record<string, unknown>>; cycleDetected?: boolean };
  accounting?: Record<string, unknown>;
  externalWaits?: Array<Record<string, unknown>>;
  acceptedOutcomes?: Array<Record<string, unknown>>;
  blockers?: Array<Record<string, unknown>>;
  settlementAttention?: Array<Record<string, unknown>>;
}

/** One Goal read exactly as the portal API serves it (consumed members only). */
export function readGoalProjection(options: ReadGoalOptions): Record<string, unknown> {
  return {
    id: options.id,
    project_id: options.projectId,
    project_key: options.projectKey ?? "DEMO",
    project_name: options.projectName ?? "Demo project",
    title: options.title,
    state: options.state ?? "active",
    current_revision: options.currentRevision ?? 1,
    version: options.version ?? 1,
    updated_at: "2026-11-16T10:00:00Z",
    lifecycle: { state: options.state ?? "active", "terminal?": false, "scheduling_paused?": false, "immutable?": false },
    revision: {
      revision: options.currentRevision ?? 1,
      objective: options.objective ?? options.title,
      non_goals: [],
      acceptance_contract: { predicates: [{ id: "operator", kind: "operator_acceptance" }] },
      authority_policy: { operator_required_for_scope_change: true, operator_required_for_completion: true, publication_allowed: false, allowed_actions: [] },
      execution_policy: { final_acceptance: "operator", allowed_actions: [], allowed_resource_ids: [] },
      context_manifest: { byte_budget: 32768, required_source_kinds: ["repository"], include_advisory_recall: false },
      inserted_at: "2026-11-16T09:00:00Z",
    },
    blockers: options.blockers ?? [],
    blocker_reasons: (options.blockers ?? []).map((blocker) => (typeof blocker["reason"] === "string" ? blocker["reason"] : "")),
    settlement_attention: options.settlementAttention ?? [],
    attention_reasons: [],
    completion: {
      "current_revision?": true,
      required_items: (options.workItems ?? []).filter((item) => item["required?"] !== false).length,
      accepted_required_items: (options.workItems ?? []).filter((item) => item["accepted?"] === true).length,
      "all_required_accepted?": false,
      no_open_decisions: true,
      no_nonterminal_tasks: true,
      "ready?": false,
      accepted_outcomes: (options.acceptedOutcomes ?? []).length,
      rejected_outcomes: 0,
    },
    allowed_actions: options.allowedActions ?? [],
    work_items: options.workItems ?? [],
    graph: {
      nodes: options.graph?.nodes ?? [],
      edges: options.graph?.edges ?? [],
      cycle_detected: options.graph?.cycleDetected ?? false,
    },
    decisions: options.decisions ?? [],
    accepted_outcomes: options.acceptedOutcomes ?? [],
    accounting: options.accounting ?? {
      held_microusd: null,
      unknown_reservation_count: 0,
      settled_reservation_count: 0,
      released_reservation_count: 0,
      usage_records: 0,
      usage_leaf_count: 0,
      known_usage_cost_microusd: null,
      usage_cost_microusd: null,
      usage_cost_unknown: false,
      unknown_usage_count: 0,
    },
    external_waits: options.externalWaits ?? [],
    history: { work_items: [], dependencies: [], outcomes: [], external_waits: [] },
    execution: { task_count: 0, active_task_count: 0, nonterminal_task_count: 0 },
    planning: null,
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
