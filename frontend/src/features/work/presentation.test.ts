import { describe, expect, it } from "vitest";
import { asRecord, asText, attributionOf, ciLabel, costLabel, decisionScopeLabel, decisionView, evidenceRows, executionStateLabel, failureText, nextActionLabel, ownerKey, ownerLabel, reportedResult, timelineItemKey, timelineItemView, timestamp } from "./presentation";

const workItemId = "11111111-1111-4111-8111-111111111111";
const taskId = "22222222-2222-4222-8222-222222222222";
const runId = "33333333-3333-4333-8333-333333333333";

describe("free-form reads", () => {
  it("rejects blank text and non-records without inventing values", () => {
    expect(asText("  ")).toBeNull();
    expect(asText(12)).toBeNull();
    expect(asRecord([])).toBeNull();
    expect(asRecord({ payload: {} })).toEqual({ payload: {} });
  });

  it("keeps an unparseable timestamp visible instead of assuming a date", () => {
    expect(timestamp("not-a-time")).toEqual({ exact: "not-a-time", readable: "not-a-time" });
    expect(timestamp(null)).toBeNull();
  });
});

describe("cost and acceptance truthfulness", () => {
  it("labels unknown cost explicitly and never as an amount", () => {
    expect(costLabel).toBe("Cost unknown");
    expect(costLabel).not.toMatch(/\$|0/);
  });

  it("reports execution output separately from acceptance", () => {
    const reported = reportedResult({ generation: 2, result: { summary: "Touched the read path", kind: "candidate_completion" } });
    expect(reported).toEqual({ text: "Touched the read path", kind: "candidate_completion" });
    expect(reportedResult({ failure: { reason: "network" } }).text).toBe("Failed: network");
    expect(failureText(null)).toBeNull();
  });

  it("reads owner labels and owner filter keys from the served assignee", () => {
    expect(ownerLabel({ type: "unassigned", name: null, agent_profile: null })).toBe("Unassigned");
    expect(ownerLabel({ type: "agent", name: "Worker", agent_profile: "reviewer" })).toBe("Agent · reviewer");
    expect(ownerLabel({ type: "human", name: "Ada", agent_profile: null })).toBe("Ada");
    expect(ownerLabel({ type: "robot", name: null, agent_profile: null })).toBe("Unknown owner type: robot");
    expect(ownerLabel(undefined)).toBe("Unassigned");
    expect(ownerKey({ type: "agent", name: "Worker", agent_profile: "reviewer" })).toBe("agent:reviewer");
    expect(ownerKey({ type: "human", name: "Ada", agent_profile: null })).toBe("human:Ada");
  });
});

describe("CI freshness", () => {
  const now = Date.parse("2026-03-01T12:00:00Z");
  const ci = (status: string, source: string, recorded?: string) => ({
    ci: { status, source, ...(recorded === undefined ? {} : { recorded_at: recorded }) },
  });

  it("never renders unknown or unrecorded CI as fresh", () => {
    expect(ciLabel(null, now)).toMatchObject({ text: "CI unknown", tone: "unknown" });
    expect(ciLabel(ci("unknown", "none"), now)).toMatchObject({ text: "CI unknown", tone: "unknown" });
    expect(ciLabel(ci("passed", "manual"), now)).toMatchObject({ text: "CI passed · freshness unknown", tone: "unknown" });
  });

  it("marks provider CI stale outside the freshness window and fresh inside it", () => {
    expect(ciLabel(ci("passed", "provider", "2026-03-01T09:00:00Z"), now)).toMatchObject({ text: "CI passed", tone: "passed" });
    expect(ciLabel(ci("passed", "provider", "2026-02-20T09:00:00Z"), now)).toMatchObject({ text: "CI passed · stale", tone: "stale" });
    expect(ciLabel(ci("failed", "provider", "2026-03-01T09:00:00Z"), now)).toMatchObject({ text: "CI failed", tone: "failed" });
    expect(ciLabel(ci("pending", "provider", "2026-03-01T09:00:00Z"), now)).toMatchObject({ text: "CI pending", tone: "pending" });
  });
});

describe("decision and next action reads", () => {
  it("reads the decision packet from the waiting context or its payload", () => {
    const waiting = {
      question: "Approve the migration?",
      transition_id: "44444444-4444-4444-8444-444444444444",
      generation: 3,
      payload: {
        decision: {
          reason: "blocking_ambiguity",
          context: "Two equally valid paths exist.",
          recommended_option_id: "b",
          options: [
            { id: "a", label: "Migrate now", consequence: "Downtime" },
            { id: "b", label: "Migrate later", consequence: "Delay" },
          ],
        },
      },
    };
    const decision = decisionView(waiting, null);
    expect(decision?.question).toBe("Approve the migration?");
    expect(decision?.options).toEqual([
      { label: "Migrate now", consequence: "Downtime", recommended: false },
      { label: "Migrate later", consequence: "Delay", recommended: true },
    ]);
    expect(decisionScopeLabel(waiting)).toBe("Scope: transition 44444444-4444-4444-8444-444444444444 · generation 3");
    expect(decisionView(null, null)).toBeNull();
  });

  it("renders the recorded next action of each served kind", () => {
    expect(nextActionLabel({ proposed_next_action: { kind: "validate", producing_task_id: taskId } })).toBe(`Validate the produced work (task ${taskId.slice(0, 8)}…)`);
    expect(nextActionLabel({ proposed_next_action: { kind: "repair", work_item_id: workItemId, reason: "tests failed" } })).toBe(`Repair work item ${workItemId.slice(0, 8)}…: tests failed`);
    expect(nextActionLabel({ proposed_next_action: { kind: "observe", resource_id: runId, external_ref: "pr-7" } })).toBe(`Observe resource ${runId.slice(0, 8)}… (pr-7)`);
    expect(nextActionLabel({ proposed_next_action: { kind: "replan", reason: "new scope" } })).toBe("Replan: new scope");
    expect(nextActionLabel({ proposed_next_action: { kind: "wait", blocker: { kind: "decision", decision_id: runId } } })).toBe("Wait for the recorded blocker to clear");
    expect(nextActionLabel({})).toBeNull();
  });

  it("labels served execution states and flags unknown ones", () => {
    expect(executionStateLabel("waiting_for_input")).toBe("waiting for input");
    expect(executionStateLabel("teleporting")).toBe("Unknown execution state: teleporting");
    expect(executionStateLabel(null)).toBeNull();
  });
});

describe("evidence and timeline presentation", () => {
  it("keeps evidence attribution when the row carries it", () => {
    const rows = evidenceRows([
      "plain finding",
      { message: "unsafe write", severity: "error", source: "agent", generation: 2, event_id: taskId, recorded_at: "2026-03-01T09:00:00Z" },
      { name: "mix test", status: "passed" },
      { unrelated: true },
    ]);
    expect(rows.map((row) => row.text)).toEqual(["plain finding", "unsafe write", "mix test · passed"]);
    expect(rows[1]?.attribution).toEqual({ source: "agent", generation: 2, recordedAt: "2026-03-01T09:00:00Z" });
    expect(attributionOf({ severity: "error" })).toBeNull();
    expect(evidenceRows(undefined)).toEqual([]);
  });

  it("labels timeline items by their served source without dropping unknown payloads", () => {
    const event = { source: "event", run_id: runId, generation: 1, recorded_at: "2026-03-01T09:00:00Z", data: { event_id: taskId, sequence: 4, kind: "artifact", payload: { path: "frontend/src/a.ts" }, occurred_at: null } };
    expect(timelineItemView(event)).toEqual({ label: "event · artifact", headline: "frontend/src/a.ts", link: null });
    expect(timelineItemKey(event)).toBe(`event:${taskId}:1`);
    expect(timelineItemView({ source: "transition", generation: 1, data: { transition_id: taskId, state: "failed", payload: { reason: "boom" } } }))
      .toEqual({ label: "transition · failed", headline: "boom", link: null });
    expect(timelineItemView({ source: "command", generation: 1, data: { command_id: taskId, kind: "pause", state: "acknowledged", acknowledgement_outcome: "applied" } }))
      .toEqual({ label: "command · pause", headline: "Command acknowledged · acknowledged applied", link: null });
  });
});
