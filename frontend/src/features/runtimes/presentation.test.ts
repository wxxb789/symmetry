import { describe, expect, it } from "vitest";
import { booleanCapability, capacityLabel, guidanceCapability, operationRows, pauseCapability, usageCapability } from "./presentation";

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
  it("covers all ten advertised operations in the record's own order", () => {
    const rows = operationRows({
      start: true,
      events: true,
      cancel: true,
      resume: false,
      handoff: false,
      guidance: "next_turn",
      pause: "unsupported",
      approval_response: false,
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
      "Supported",
      "Supported",
      "Not supported",
      "Not supported",
      "Delivered with the next turn",
      "Not supported",
      "Not supported",
      "Unknown",
      "Not supported",
    ]);
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
