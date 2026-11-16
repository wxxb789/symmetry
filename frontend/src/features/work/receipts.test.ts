import { describe, expect, it } from "vitest";
import type { TimelineItemLike } from "./presentation";
import {
  attemptReceiptView,
  receiptLabel,
  receiptOutcome,
  receiptRowLimit,
  receiptRows,
  type AttemptContext,
  type ControlAttempt,
} from "./receipts";

function timelineCommand(kind: string, options: {
  commandId?: string;
  generation?: number | null;
  recordedAt?: string | null;
  state?: string;
  acknowledgementOutcome?: string | null;
} = {}): TimelineItemLike {
  return {
    source: "command",
    generation: options.generation === undefined ? 1 : options.generation,
    recorded_at: options.recordedAt ?? "2026-11-16T10:00:00Z",
    data: {
      command_id: options.commandId ?? `command-${kind}`,
      task_id: "task-1",
      kind,
      payload: {},
      state: options.state ?? "pending",
      issued_at: "2026-11-16T10:00:00Z",
      applied_at: null,
      acknowledgement_id: null,
      acknowledgement_outcome: options.acknowledgementOutcome ?? null,
      acknowledged_at: null,
    },
  };
}

describe("receipt outcomes", () => {
  it("requires the applied acknowledgement outcome for an applied receipt", () => {
    expect(receiptOutcome({ state: "acknowledged", acknowledgement_outcome: "applied" })).toBe("applied");
    // A command the control plane recorded as applied while the runtime has not
    // acknowledged it is a deferred request, never applied.
    expect(receiptOutcome({ state: "applied", acknowledgement_outcome: null })).toBe("deferred");
    expect(receiptOutcome({ state: "pending", acknowledgement_outcome: null })).toBe("queued");
    expect(receiptOutcome(null)).toBe("unknown");
  });

  it("maps the rejected and failed acknowledgements and the terminal cancelled states", () => {
    expect(receiptOutcome({ state: "acknowledged", acknowledgement_outcome: "rejected" })).toBe("rejected");
    expect(receiptOutcome({ state: "acknowledged", acknowledgement_outcome: "failed" })).toBe("failed");
    expect(receiptOutcome({ state: "cancelled", acknowledgement_outcome: null })).toBe("cancelled");
    expect(receiptOutcome({ state: "superseded", acknowledgement_outcome: null })).toBe("cancelled");
  });

  it("renders an unrecognised state/outcome pair as unknown, never as success", () => {
    expect(receiptOutcome({ state: "acknowledged", acknowledgement_outcome: "exploded" })).toBe("unknown");
    expect(receiptOutcome({ state: "acknowledged", acknowledgement_outcome: null })).toBe("unknown");
    expect(receiptOutcome({ state: "teleporting", acknowledgement_outcome: null })).toBe("unknown");
  });

  it("labels every durable outcome with its exact text", () => {
    expect(receiptLabel("applied")).toBe("Applied");
    expect(receiptLabel("rejected")).toBe("Rejected by the runtime");
    expect(receiptLabel("failed")).toBe("Failed");
    expect(receiptLabel("queued")).toBe("Awaiting agent acknowledgement");
    expect(receiptLabel("deferred")).toBe("Queued for the runtime");
    expect(receiptLabel("cancelled")).toBe("Cancelled");
    expect(receiptLabel("unknown")).toBe("Outcome unknown");
    // A control-plane refusal is the server's own decision; it is never
    // rendered with the runtime's rejection text.
    expect(receiptLabel("refused")).toBe("Refused by the server");
  });
});

describe("receipt history rows", () => {
  it("keeps the served order and each row's own kind, generation and outcome", () => {
    const history = receiptRows([
      timelineCommand("guidance", { commandId: "c-guidance", generation: 2, state: "applied" }),
      timelineCommand("pause", { commandId: "c-pause", generation: 2 }),
      timelineCommand("cancel", { commandId: "c-cancel", generation: null, state: "applied" }),
      timelineCommand("retry", { commandId: "c-retry", generation: 1 }),
    ], []);
    // A control-plane-applied command the runtime has not acknowledged is a
    // deferred request, distinct from one still awaiting acknowledgement.
    expect(history.rows.map((row) => [row.kind, row.generation, row.outcome])).toEqual([
      ["guidance", 2, "deferred"],
      ["pause", 2, "queued"],
      ["cancel", null, "deferred"],
      ["retry", 1, "queued"],
    ]);
    expect(history.hidden).toBe(0);
  });

  it("renders acknowledged and rejected guidance receipts with their own kind and outcome", () => {
    const history = receiptRows([
      timelineCommand("guidance", { commandId: "c-applied", state: "acknowledged", acknowledgementOutcome: "applied" }),
      timelineCommand("pause", { commandId: "c-rejected", state: "acknowledged", acknowledgementOutcome: "rejected" }),
      timelineCommand("resume", { commandId: "c-failed", state: "acknowledged", acknowledgementOutcome: "failed" }),
    ], []);
    expect(history.rows.map((row) => [row.kind, row.outcome])).toEqual([
      ["guidance", "applied"],
      ["pause", "rejected"],
      ["resume", "failed"],
    ]);
  });

  it("excludes timeline entries that are not commands", () => {
    const event: TimelineItemLike = { source: "event", generation: 1, recorded_at: "2026-11-16T10:00:00Z", data: { event_id: "e-1", sequence: 1, kind: "progress", payload: {}, occurred_at: null } };
    const transition: TimelineItemLike = { source: "transition", generation: 1, recorded_at: "2026-11-16T10:00:00Z", data: { transition_id: "t-1", state: "completed", payload: {} } };
    expect(receiptRows([event, transition], []).rows).toEqual([]);
  });

  it("deduplicates a command served by the detail and an older page", () => {
    const command = timelineCommand("cancel", { commandId: "c-cancel", generation: null, state: "applied" });
    const history = receiptRows([command], [[command]]);
    expect(history.rows).toHaveLength(1);
    expect(history.hidden).toBe(0);
  });

  it("caps the rendered rows and counts the remainder", () => {
    const commands = Array.from({ length: receiptRowLimit + 3 }, (_unused, index) => timelineCommand("guidance", {
      commandId: `c-${index}`,
      recordedAt: `2026-11-16T10:0${index}:00Z`,
    }));
    const history = receiptRows(commands, []);
    expect(history.rows).toHaveLength(receiptRowLimit);
    expect(history.hidden).toBe(3);
    expect(history.rows[0]?.commandId).toBe("c-0");
    expect(history.rows[receiptRowLimit - 1]?.commandId).toBe(`c-${receiptRowLimit - 1}`);
  });
});

describe("local attempt receipts", () => {
  const baseAttempt: ControlAttempt = {
    kind: "cancel",
    actionId: "action-1",
    scope: "2",
    commandId: null,
    command: null,
    executionTaskId: null,
    generation: 2,
    baseline: 4,
    responseState: null,
    refusal: null,
    unconfirmed: null,
  };
  const baseContext: AttemptContext = {
    snapshotVersion: 4,
    carried: new Set<string>(),
    execution: null,
    pending: false,
    paused: false,
  };

  it("renders an in-flight attempt as requested, never applied", () => {
    const sent = attemptReceiptView(baseAttempt, { ...baseContext, pending: true });
    expect(sent?.outcome).toBe("requested");
    expect(sent?.headline).toBe("Request sent…");
    const queued = attemptReceiptView(baseAttempt, { ...baseContext, pending: true, paused: true });
    expect(queued?.outcome).toBe("requested");
    expect(queued?.headline).toBe("Request queued…");
  });

  it("never renders an unconfirmed command attempt as applied after a refresh", () => {
    const attempt: ControlAttempt = { ...baseAttempt, commandId: "command-9", command: { state: "applied", acknowledgement_outcome: null } };
    expect(attemptReceiptView(attempt, baseContext)?.outcome).toBe("deferred");
    expect(attemptReceiptView(attempt, baseContext)?.headline).toBe("Queued for the runtime");
    // The refreshed snapshot carries the command: the durable row owns it.
    expect(attemptReceiptView(attempt, { ...baseContext, snapshotVersion: 5, carried: new Set(["command-9"]) })).toBeNull();
    // A refresh that cannot show the attempt renders unknown, never applied.
    const refreshed = attemptReceiptView(attempt, { ...baseContext, snapshotVersion: 5 });
    expect(refreshed?.outcome).toBe("unknown");
    expect(refreshed?.headline).toBe("Outcome unknown");
    expect(refreshed?.note).toContain("authority");
  });

  it("keeps a definitive server refusal after a refresh", () => {
    const attempt: ControlAttempt = { ...baseAttempt, kind: "retry", refusal: { outcome: "refused", message: "The attempt has already moved on." } };
    const view = attemptReceiptView(attempt, { ...baseContext, snapshotVersion: 9 });
    expect(view?.outcome).toBe("refused");
    expect(view?.headline).toBe("Refused by the server");
    expect(view?.message).toContain("already moved on");
  });

  it("reports an accepted start while no runtime has claimed the run and drops it once the run moves on", () => {
    const attempt: ControlAttempt = { ...baseAttempt, kind: "start", scope: "initial", executionTaskId: "task-1", generation: 1, responseState: "queued" };
    const queued = attemptReceiptView(attempt, { ...baseContext, snapshotVersion: 5, execution: { task_id: "task-1", generation: 1, state: "queued" } });
    expect(queued?.outcome).toBe("queued");
    // The run is recorded and no runtime has claimed it: the headline names
    // that pre-runtime state instead of a command acknowledgement that cannot
    // exist yet.
    expect(queued?.headline).toBe("Awaiting a runtime");
    expect(queued?.headline).not.toBe(receiptLabel("queued"));
    expect(attemptReceiptView(attempt, { ...baseContext, snapshotVersion: 5, execution: { task_id: "task-1", generation: 1, state: "running" } })).toBeNull();
    const lostRun = attemptReceiptView(attempt, { ...baseContext, snapshotVersion: 5 });
    expect(lostRun?.outcome).toBe("unknown");
  });

  it("reconstructs a stored attempt as a pending request until a refresh cannot confirm it", () => {
    const probe: ControlAttempt = { ...baseAttempt, kind: "input", scope: "transition-1", generation: null };
    const pending = attemptReceiptView(probe, baseContext);
    expect(pending?.outcome).toBe("requested");
    expect(pending?.headline).toBe("Request sent…");
    expect(attemptReceiptView(probe, { ...baseContext, snapshotVersion: 5 })?.outcome).toBe("unknown");
  });
});
