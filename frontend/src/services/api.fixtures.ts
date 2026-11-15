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
}

export interface ReadWorkDetailOptions extends ReadWorkItemOptions {
  nextBefore?: string | null;
  timeline?: unknown[];
}

const readDelivery = () => ({
  pull_request: { value: null, source: "none", generation: null, url: null, status: null },
  ci: { value: null, source: "none", generation: null, url: null, status: "unknown" },
  review: { value: null, source: "none", generation: null, url: null, status: "none" },
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
    delivery: readDelivery(),
    execution: null,
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
      summary: null,
      findings: [],
      changed_artifacts: [],
      tests: [],
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
