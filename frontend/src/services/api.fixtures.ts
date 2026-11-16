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
