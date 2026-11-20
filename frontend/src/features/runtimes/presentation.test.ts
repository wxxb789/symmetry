import { describe, expect, it } from "vitest";
import { booleanCapability, capacityLabel, guidanceCapability, operationRows, pauseCapability, runtimesEmptyState, usageCapability } from "./presentation";

describe("boolean capability values", () => {
  it("renders true as Supported and false as Not supported", () => {
    expect(booleanCapability(true)).toEqual({ label: "Supported", tone: "passed" });
    expect(booleanCapability(false)).toEqual({ label: "Not supported", tone: "failed" });
  });

  it("renders a member the record does not carry as Not advertised, never as a boolean value", () => {
    expect(booleanCapability(undefined)).toEqual({ label: "Not advertised", tone: "unknown" });
  });
});

describe("guidance capability values", () => {
  it("distinguishes native steering from next-turn delivery", () => {
    expect(guidanceCapability("native_steer")).toEqual({ label: "Native steering", tone: "passed" });
    expect(guidanceCapability("next_turn")).toEqual({ label: "Delivered with the next turn", tone: "pending" });
  });

  it("never presents next-turn delivery as steering", () => {
    expect(guidanceCapability("next_turn").label).not.toContain("steering");
  });

  it("renders unsupported guidance as Not supported and an absent member as Not advertised", () => {
    expect(guidanceCapability("unsupported")).toEqual({ label: "Not supported", tone: "failed" });
    expect(guidanceCapability(undefined)).toEqual({ label: "Not advertised", tone: "unknown" });
  });
});

describe("pause capability values", () => {
  it("labels a safe-boundary pause explicitly, never as a bare Pause", () => {
    const value = pauseCapability("safe_boundary");
    expect(value).toEqual({ label: "Pause at a safe boundary", tone: "passed" });
    expect(value.label).not.toBe("Pause");
  });

  it("renders unsupported pause as Not supported and an absent member as Not advertised", () => {
    expect(pauseCapability("unsupported")).toEqual({ label: "Not supported", tone: "failed" });
    expect(pauseCapability(undefined)).toEqual({ label: "Not advertised", tone: "unknown" });
  });
});

describe("usage capability values", () => {
  it("distinguishes reported and estimated usage", () => {
    expect(usageCapability("reported")).toEqual({ label: "Reported", tone: "passed" });
    expect(usageCapability("estimated")).toEqual({ label: "Estimated", tone: "pending" });
  });

  it("never renders advertised-unknown usage as a measured value", () => {
    const value = usageCapability("unknown");
    expect(value.label).toBe("Unknown");
    expect(value.label).not.toBe("Reported");
    expect(value.label).not.toBe("Estimated");
    expect(value.tone).toBe("unknown");
  });

  it("renders an absent usage member as Not advertised, not as Unknown", () => {
    expect(usageCapability(undefined)).toEqual({ label: "Not advertised", tone: "unknown" });
  });
});

describe("operation rows", () => {
  const booleanOperations = ["start", "events", "cancel", "resume", "handoff", "approval_response", "hard_cost_limit"] as const;

  it("covers all ten advertised operations in the record's own order", () => {
    const rows = operationRows({
      // The booleans alternate in pairs, so a row that read its neighbour would
      // show a different label; the flip test below pins the members that still
      // share a value (two labels cannot make seven booleans all distinct).
      start: true,
      events: false,
      cancel: false,
      resume: true,
      handoff: false,
      approval_response: true,
      guidance: "next_turn",
      pause: "unsupported",
      usage: "unknown",
      hard_cost_limit: false,
    });
    expect(rows.map((row) => row.key)).toEqual([
      "start",
      "events",
      "cancel",
      "resume",
      "handoff",
      "guidance",
      "pause",
      "approval_response",
      "usage",
      "hard_cost_limit",
    ]);
    expect(rows.map((row) => row.label)).toEqual([
      "Supported",
      "Not supported",
      "Not supported",
      "Supported",
      "Not supported",
      "Delivered with the next turn",
      "Not supported",
      "Supported",
      "Unknown",
      "Not supported",
    ]);
  });

  it("reads every boolean row from its own member", () => {
    // Flip one member at a time: under any swap of two members exactly the
    // wrong row would turn Supported, so this pins the member each row reads.
    for (const member of booleanOperations) {
      const supported = operationRows({
        start: member === "start",
        events: member === "events",
        cancel: member === "cancel",
        resume: member === "resume",
        handoff: member === "handoff",
        approval_response: member === "approval_response",
        hard_cost_limit: member === "hard_cost_limit",
      }).filter((row) => row.label === "Supported").map((row) => row.key);
      expect(supported, `flipped ${member}`).toEqual([member]);
    }
  });

  it("keeps explicit negatives and unmeasured usage distinct", () => {
    const rows = operationRows({ resume: false, pause: "unsupported", usage: "unknown" });
    const byKey = new Map(rows.map((row) => [row.key, row]));
    expect(byKey.get("resume")).toEqual({ key: "resume", name: "Resume", label: "Not supported", tone: "failed" });
    expect(byKey.get("pause")).toEqual({ key: "pause", name: "Pause", label: "Not supported", tone: "failed" });
    expect(byKey.get("usage")).toEqual({ key: "usage", name: "Usage", label: "Unknown", tone: "unknown" });
  });

  it("renders every operation as Not advertised when the record carries no operations", () => {
    const rows = operationRows(undefined);
    expect(rows).toHaveLength(10);
    for (const row of rows) {
      expect(row.label).toBe("Not advertised");
      expect(row.tone).toBe("unknown");
    }
  });
});

describe("capacity label", () => {
  it("names reserved capacity out of the total", () => {
    expect(capacityLabel(2, 8)).toBe("2 of 8 reserved");
  });

  it("never substitutes a number for a capacity member the record does not carry", () => {
    expect(capacityLabel(undefined, 8)).toBe("Not recorded");
    expect(capacityLabel(2, undefined)).toBe("Not recorded");
  });
});

describe("runtimes empty state", () => {
  it("names the compatibility filter instead of claiming the registry is empty", () => {
    const scoped = runtimesEmptyState({ scoped: true, selectedProjectId: "project-1", registeredCount: 3 });
    expect(scoped.title).toBe("No compatible runtimes");
    expect(scoped.body).toContain("3 runtimes are registered with this deployment");
    expect(scoped.body).toContain("none is bound to this project");
    const scopedBody = scoped.body;
    expect(scopedBody).not.toContain("No runtime is registered");
  });

  it("says the registry is empty only when the served registered count is zero", () => {
    const empty = runtimesEmptyState({ scoped: true, selectedProjectId: "project-1", registeredCount: 0 });
    expect(empty.title).toBe("No compatible runtimes");
    expect(empty.body).toContain("No runtime is registered with this deployment");
  });

  it("counts the registry for the unscoped read of the server-selected project", () => {
    const unscoped = runtimesEmptyState({ scoped: false, selectedProjectId: "project-1", registeredCount: 1 });
    expect(unscoped.title).toBe("No compatible runtimes");
    expect(unscoped.body).toContain("1 runtime is registered with this deployment");
    expect(unscoped.body).toContain("none is bound to the selected project");
  });

  it("states that no project scope exists when the read selects no project", () => {
    const noProject = runtimesEmptyState({ scoped: false, selectedProjectId: null, registeredCount: 2 });
    expect(noProject.title).toBe("No project selected");
    expect(noProject.body).toContain("2 runtimes are registered with this deployment");
    const none = runtimesEmptyState({ scoped: false, selectedProjectId: null, registeredCount: 0 });
    expect(none.title).toBe("No project selected");
    expect(none.body).toContain("no runtime is registered");
  });
});
