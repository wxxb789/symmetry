import { describe, expect, it } from "vitest";
import {
  attentionReadKey,
  chatOlderReadKey,
  chatReadKey,
  connectionsReadKey,
  goalEventsCatchUpReadKey,
  goalEventsReadKey,
  goalReadKey,
  invalidationKeys,
  isSessionReadKey,
  sessionReadKey,
  workItemReadKey,
  workItemTimelineReadKey,
  workspaceReadKey,
  type InvalidationPayload,
} from "./read-cache";

const scope = { scope: "run", projectId: "p1", runId: "r1" } as const;

const builderRows: readonly (readonly [string, () => readonly unknown[], string])[] = [
  ["sessionReadKey()", () => sessionReadKey(), '["session"]'],
  ["workspaceReadKey()", () => workspaceReadKey(), '["workspace"]'],
  ["workspaceReadKey(null)", () => workspaceReadKey(null), '["workspace",{"projectId":null}]'],
  ["workspaceReadKey(\"p1\")", () => workspaceReadKey("p1"), '["workspace",{"projectId":"p1"}]'],
  ["workItemReadKey()", () => workItemReadKey(), '["work-item"]'],
  ["workItemReadKey(\"w1\")", () => workItemReadKey("w1"), '["work-item",{"workItemId":"w1"}]'],
  ["attentionReadKey()", () => attentionReadKey(), '["attention"]'],
  ["attentionReadKey(\"p1\", null)", () => attentionReadKey("p1", null), '["attention",{"projectId":"p1","cursor":null}]'],
  ["attentionReadKey(null, \"c1\")", () => attentionReadKey(null, "c1"), '["attention",{"projectId":null,"cursor":"c1"}]'],
  ["goalReadKey()", () => goalReadKey(), '["goal"]'],
  ["goalReadKey(\"g1\")", () => goalReadKey("g1"), '["goal",{"goalId":"g1"}]'],
  ["goalEventsReadKey()", () => goalEventsReadKey(), '["goal-events"]'],
  ["goalEventsReadKey(\"g1\")", () => goalEventsReadKey("g1"), '["goal-events",{"goalId":"g1"}]'],
  ["goalEventsReadKey(\"g1\", 4)", () => goalEventsReadKey("g1", 4), '["goal-events",{"goalId":"g1","after":4}]'],
  ["goalEventsCatchUpReadKey()", () => goalEventsCatchUpReadKey(), '["goal-events-catch-up"]'],
  ["goalEventsCatchUpReadKey(\"g1\", 2)", () => goalEventsCatchUpReadKey("g1", 2), '["goal-events-catch-up",{"goalId":"g1","epoch":2}]'],
  ["workItemTimelineReadKey()", () => workItemTimelineReadKey(), '["work-item-timeline"]'],
  ["workItemTimelineReadKey(\"w1\")", () => workItemTimelineReadKey("w1"), '["work-item-timeline",{"workItemId":"w1"}]'],
  ["workItemTimelineReadKey(\"w1\", null)", () => workItemTimelineReadKey("w1", null), '["work-item-timeline",{"workItemId":"w1","cursor":null}]'],
  ["workItemTimelineReadKey(\"w1\", \"c1\")", () => workItemTimelineReadKey("w1", "c1"), '["work-item-timeline",{"workItemId":"w1","cursor":"c1"}]'],
  ["connectionsReadKey()", () => connectionsReadKey(), '["connections"]'],
  ["chatReadKey()", () => chatReadKey(), '["chat"]'],
  ["chatReadKey(scope)", () => chatReadKey(scope), '["chat",{"scope":"run","projectId":"p1","runId":"r1"}]'],
  ["chatOlderReadKey(scope, \"c9\")", () => chatOlderReadKey(scope, "c9"), '["chat-older",{"scope":"run","projectId":"p1","runId":"r1","before":"c9"}]'],
];

const kindRows: readonly (readonly [string, InvalidationPayload, string])[] = [
  ["work_item with an id", { kind: "work_item", id: "w1" }, '[["work-item",{"workItemId":"w1"}],["work-item-timeline"],["workspace"],["attention"]]'],
  ["work_item with only the work-item field", { kind: "work_item", work_item_id: "w2" }, '[["work-item",{"workItemId":"w2"}],["work-item-timeline"],["workspace"],["attention"]]'],
  ["work_item without an identity", { kind: "work_item" }, '[["work-item"],["work-item-timeline"],["workspace"],["attention"]]'],
  ["run", { kind: "run", id: "r1" }, '[["work-item"],["work-item-timeline"],["workspace"]]'],
  ["goal with an id", { kind: "goal", id: "g1", cursor: 4 }, '[["goal",{"goalId":"g1"}],["goal-events"],["goal-events-catch-up"],["attention"]]'],
  ["goal without an identity", { kind: "goal" }, '[["goal"],["goal-events"],["goal-events-catch-up"],["attention"]]'],
  ["chat", { kind: "chat" }, '[["chat"]]'],
  ["connection", { kind: "connection" }, '[["connections"],["workspace"]]'],
  ["resource", { kind: "resource", id: "res1" }, '[["connections"],["workspace"]]'],
  ["project", { kind: "project" }, '[["workspace"],["connections"]]'],
  ["runtime", { kind: "runtime" }, '[["workspace"]]'],
  ["the workspace spelling", { kind: "workspace" }, "[]"],
  ["an unclassified kind", { kind: "something_new" }, "[]"],
];

describe("read-cache", () => {
  it.each(builderRows)("builds the %s key with the frozen tuple", (_name, produce, expected) => {
    expect(JSON.stringify(produce())).toBe(expected);
  });

  it.each(kindRows)("maps the %s change onto its read keys", (_name, payload, expected) => {
    expect(JSON.stringify(invalidationKeys(payload))).toBe(expected);
  });

  it("marks exactly the session root as a session read", () => {
    expect(isSessionReadKey(["session"])).toBe(true);
    expect(isSessionReadKey(["workspace"])).toBe(false);
    expect(isSessionReadKey([])).toBe(false);
  });
});
