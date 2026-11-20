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
  acknowledgementId?: string | null;
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
      acknowledgement_id: options.acknowledgementId ?? null,
      acknowledgement_outcome: options.acknowledgementOutcome ?? null,
      acknowledged_at: null,
    },
  };
}

describe("receipt outcomes", () => {
  it("requires the applied acknowledgement outcome for an applied receipt", () => {
    expect(receiptOutcome({ kind: "guidance", state: "acknowledged", acknowledgement_outcome: "applied", acknowledgement_id: "ack-1" })).toBe("applied");
    // The retry the control plane re-queued into a fresh generation while no
    // runtime has acknowledged it is a deferred request, never applied.
    expect(receiptOutcome({ kind: "retry", state: "applied", acknowledgement_outcome: null, acknowledgement_id: null })).toBe("deferred");
    // A command the control plane applied itself and never dispatched to a
    // runtime cannot carry the runtime's acknowledgement; it renders the
    // plane's own outcome.
    expect(receiptOutcome({ kind: "cancel", state: "applied", acknowledgement_outcome: null, acknowledgement_id: null })).toBe("control_applied");
    expect(receiptOutcome({ kind: "pause", state: "applied", acknowledgement_outcome: null, acknowledgement_id: null })).toBe("control_applied");
    expect(receiptOutcome({ kind: "guidance", state: "pending", acknowledgement_outcome: null, acknowledgement_id: null })).toBe("queued");
    expect(receiptOutcome(null)).toBe("unknown");
  });

  it("maps the runtime's and the control plane's rejections and the failed acknowledgement", () => {
    expect(receiptOutcome({ kind: "guidance", state: "acknowledged", acknowledgement_outcome: "rejected", acknowledgement_id: "ack-2" })).toBe("rejected");
    expect(receiptOutcome({ kind: "guidance", state: "acknowledged", acknowledgement_outcome: "failed", acknowledgement_id: "ack-3" })).toBe("failed");
    // The control plane's own pre-dispatch rejection records the outcome
    // without a runtime acknowledgement id; the rejection is never attributed
    // to a runtime that never saw the command.
    expect(receiptOutcome({ kind: "guidance", state: "acknowledged", acknowledgement_outcome: "rejected", acknowledgement_id: null })).toBe("control_rejected");
  });

  it("renders an unrecognised state/outcome pair as unknown, never as success", () => {
    expect(receiptOutcome({ kind: "guidance", state: "acknowledged", acknowledgement_outcome: "exploded", acknowledgement_id: "ack-4" })).toBe("unknown");
    expect(receiptOutcome({ kind: "guidance", state: "acknowledged", acknowledgement_outcome: null, acknowledgement_id: null })).toBe("unknown");
    // The command state is a closed set (pending|applied|acknowledged), so the
    // terminal states a cancelled attempt ends in cannot be served as command
    // states; any state outside the set falls through to unknown.
    expect(receiptOutcome({ kind: "cancel", state: "cancelled", acknowledgement_outcome: null, acknowledgement_id: null })).toBe("unknown");
    expect(receiptOutcome({ kind: "cancel", state: "superseded", acknowledgement_outcome: null, acknowledgement_id: null })).toBe("unknown");
    expect(receiptOutcome({ kind: "guidance", state: "teleporting", acknowledgement_outcome: null, acknowledgement_id: null })).toBe("unknown");
  });

  it("labels every durable outcome with its exact text", () => {
    expect(receiptLabel("applied")).toBe("Applied");
    expect(receiptLabel("control_applied")).toBe("Applied by the control plane");
    expect(receiptLabel("rejected")).toBe("Rejected by the runtime");
    expect(receiptLabel("control_rejected")).toBe("Rejected by the control plane");
    expect(receiptLabel("failed")).toBe("Failed");
    expect(receiptLabel("not_sent")).toBe("Not sent");
    expect(receiptLabel("queued")).toBe("Awaiting agent acknowledgement");
    expect(receiptLabel("deferred")).toBe("Queued for the runtime");
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
      timelineCommand("retry", { commandId: "c-retry", generation: 1, state: "applied" }),
    ], []);
    // Each row's served kind decides: the retry the control plane re-queued is
    // deferred, every other command it applied itself renders the plane's own
    // outcome, and one still awaiting the runtime's acknowledgement is queued.
    expect(history.rows.map((row) => [row.kind, row.generation, row.outcome])).toEqual([
      ["guidance", 2, "control_applied"],
      ["pause", 2, "queued"],
      ["cancel", null, "control_applied"],
      ["retry", 1, "deferred"],
    ]);
    expect(history.hidden).toBe(0);
  });

  it("renders acknowledged, runtime-rejected and control-plane-rejected receipts with their own kind and outcome", () => {
    const history = receiptRows([
      timelineCommand("guidance", { commandId: "c-applied", state: "acknowledged", acknowledgementOutcome: "applied", acknowledgementId: "ack-5" }),
      timelineCommand("pause", { commandId: "c-rejected", state: "acknowledged", acknowledgementOutcome: "rejected", acknowledgementId: "ack-6" }),
      timelineCommand("resume", { commandId: "c-failed", state: "acknowledged", acknowledgementOutcome: "failed", acknowledgementId: "ack-7" }),
      timelineCommand("guidance", { commandId: "c-control-rejected", state: "acknowledged", acknowledgementOutcome: "rejected" }),
    ], []);
    expect(history.rows.map((row) => [row.kind, row.outcome])).toEqual([
      ["guidance", "applied"],
      ["pause", "rejected"],
      ["resume", "failed"],
      ["guidance", "control_rejected"],
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

  it("renders an unconfirmed command attempt with the control plane's own outcome, never applied, and unknown after a refresh", () => {
    const attempt: ControlAttempt = { ...baseAttempt, commandId: "command-9", command: { kind: "cancel", state: "applied", acknowledgement_outcome: null, acknowledgement_id: null } };
    expect(attemptReceiptView(attempt, baseContext)?.outcome).toBe("control_applied");
    expect(attemptReceiptView(attempt, baseContext)?.headline).toBe("Applied by the control plane");
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
