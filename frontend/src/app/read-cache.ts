import type { ChatScopeKey } from "./application";

export interface InvalidationPayload {
  kind: string;
  id?: string;
  project_id?: string;
  work_item_id?: string;
  goal_id?: string;
  run_id?: string;
  version?: number;
  cursor?: number;
}

export function sessionReadKey(): readonly ["session"] {
  return ["session"];
}

export function workspaceReadKey(): readonly ["workspace"];
export function workspaceReadKey(projectId: string | null): readonly ["workspace", { projectId: string | null }];
export function workspaceReadKey(projectId?: string | null): readonly unknown[] {
  return projectId === undefined ? ["workspace"] : ["workspace", { projectId }];
}

export function workItemReadKey(): readonly ["work-item"];
export function workItemReadKey(workItemId: string): readonly ["work-item", { workItemId: string }];
export function workItemReadKey(workItemId?: string): readonly unknown[] {
  return workItemId === undefined ? ["work-item"] : ["work-item", { workItemId }];
}

export function attentionReadKey(): readonly ["attention"];
export function attentionReadKey(projectId: string | null, cursor: string | null): readonly ["attention", { projectId: string | null; cursor: string | null }];
export function attentionReadKey(projectId?: string | null, cursor?: string | null): readonly unknown[] {
  return projectId === undefined ? ["attention"] : ["attention", { projectId, cursor }];
}

export function goalReadKey(): readonly ["goal"];
export function goalReadKey(goalId: string): readonly ["goal", { goalId: string }];
export function goalReadKey(goalId?: string): readonly unknown[] {
  return goalId === undefined ? ["goal"] : ["goal", { goalId }];
}

export function goalEventsReadKey(): readonly ["goal-events"];
export function goalEventsReadKey(goalId: string): readonly ["goal-events", { goalId: string }];
export function goalEventsReadKey(goalId: string, after: number): readonly ["goal-events", { goalId: string; after: number }];
export function goalEventsReadKey(goalId?: string, after?: number): readonly unknown[] {
  if (goalId === undefined) return ["goal-events"];
  return after === undefined ? ["goal-events", { goalId }] : ["goal-events", { goalId, after }];
}

export function goalEventsCatchUpReadKey(): readonly ["goal-events-catch-up"];
export function goalEventsCatchUpReadKey(goalId: string, epoch: number): readonly ["goal-events-catch-up", { goalId: string; epoch: number }];
export function goalEventsCatchUpReadKey(goalId?: string, epoch?: number): readonly unknown[] {
  if (goalId === undefined) return ["goal-events-catch-up"];
  return ["goal-events-catch-up", { goalId, epoch }];
}

export function workItemTimelineReadKey(): readonly ["work-item-timeline"];
export function workItemTimelineReadKey(workItemId: string): readonly ["work-item-timeline", { workItemId: string }];
export function workItemTimelineReadKey(workItemId: string, cursor: string | null): readonly ["work-item-timeline", { workItemId: string; cursor: string | null }];
export function workItemTimelineReadKey(workItemId?: string, cursor?: string | null): readonly unknown[] {
  if (workItemId === undefined) return ["work-item-timeline"];
  if (cursor === undefined) return ["work-item-timeline", { workItemId }];
  return ["work-item-timeline", { workItemId, cursor }];
}

export function connectionsReadKey(): readonly ["connections"] {
  return ["connections"];
}

export function chatReadKey(): readonly ["chat"];
export function chatReadKey(scope: ChatScopeKey): readonly ["chat", ChatScopeKey];
export function chatReadKey(scope?: ChatScopeKey): readonly unknown[] {
  return scope === undefined ? ["chat"] : ["chat", scope];
}

export function chatOlderReadKey(scope: ChatScopeKey, before: string): readonly ["chat-older", ChatScopeKey & { readonly before: string }] {
  return ["chat-older", { ...scope, before }];
}

/** The query-key roots one invalidation affects; an empty list means every read, never a drop. */
export function invalidationKeys(payload: InvalidationPayload): readonly (readonly unknown[])[] {
  const identity = payload.id ?? payload.work_item_id;
  switch (payload.kind) {
    case "work_item":
      // Attention merges the Goal feed with blocked work items, so a work item's change changes it too.
      return [...(identity === undefined ? [workItemReadKey()] : [workItemReadKey(identity)]), workItemTimelineReadKey(), workspaceReadKey(), attentionReadKey()];
    case "run":
      // The payload names the run, but surfaces key by work item.
      return [workItemReadKey(), workItemTimelineReadKey(), workspaceReadKey()];
    case "goal":
      // `goal-events` pages and the `goal-events-catch-up` continuation read the same served log.
      return [...(identity === undefined ? [goalReadKey()] : [goalReadKey(identity)]), goalEventsReadKey(), goalEventsCatchUpReadKey(), attentionReadKey()];
    case "chat":
      return [chatReadKey()];
    case "connection":
    case "resource":
      return [connectionsReadKey(), workspaceReadKey()];
    case "project":
      return [workspaceReadKey(), connectionsReadKey()];
    case "runtime":
      return [workspaceReadKey()];
    default:
      return [];
  }
}

export const isSessionReadKey = (key: readonly unknown[]): boolean => key[0] === "session";
