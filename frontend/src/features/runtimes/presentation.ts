/**
 * Presentation of the runtime's own advertised capability record. Every value
 * is either the served member or an explicit absence label; nothing here
 * substitutes a default for a member the record does not carry.
 */

import type { Runtime } from "../../services/api";

/** The adapter operations object as served; every member stays optional so an older partial record renders honestly. */
export type AdapterOperationsRecord = Partial<NonNullable<Runtime["capabilities"]["adapter"]>["operations"]>;

/** Chip tones the operations list can carry. `unknown` renders dashed and never reads as a measured value. */
export type CapabilityTone = "passed" | "failed" | "pending" | "unknown";

export interface CapabilityPresentation {
  readonly label: string;
  readonly tone: CapabilityTone;
}

/** One operations row: the advertised key, its readable label and the served value's presentation. */
export interface OperationRow extends CapabilityPresentation {
  readonly key: string;
  readonly name: string;
}

const notAdvertised: CapabilityPresentation = { label: "Not advertised", tone: "unknown" };
const supported: CapabilityPresentation = { label: "Supported", tone: "passed" };
const notSupported: CapabilityPresentation = { label: "Not supported", tone: "failed" };

export function booleanCapability(value: boolean | undefined): CapabilityPresentation {
  if (value === undefined) return notAdvertised;
  return value ? supported : notSupported;
}

export function guidanceCapability(value: AdapterOperationsRecord["guidance"]): CapabilityPresentation {
  switch (value) {
    case undefined: return notAdvertised;
    case "native_steer": return { label: "Native steering", tone: "passed" };
    case "next_turn": return { label: "Delivered with the next turn", tone: "pending" };
    case "unsupported": return notSupported;
  }
}

export function pauseCapability(value: AdapterOperationsRecord["pause"]): CapabilityPresentation {
  switch (value) {
    case undefined: return notAdvertised;
    case "safe_boundary": return { label: "Pause at a safe boundary", tone: "passed" };
    case "unsupported": return notSupported;
  }
}

export function usageCapability(value: AdapterOperationsRecord["usage"]): CapabilityPresentation {
  switch (value) {
    case undefined: return notAdvertised;
    case "reported": return { label: "Reported", tone: "passed" };
    case "estimated": return { label: "Estimated", tone: "pending" };
    case "unknown": return { label: "Unknown", tone: "unknown" };
  }
}

/** Reserved out of total capacity, or an explicit absence label — never a substituted number. */
export function capacityLabel(reserved: number | undefined, capacity: number | undefined): string {
  if (reserved === undefined || capacity === undefined) return "Not recorded";
  return `${reserved} of ${capacity} reserved`;
}

/** All ten operations an adapter record can carry, in the record's own order. */
export function operationRows(operations: AdapterOperationsRecord | null | undefined): readonly OperationRow[] {
  const record: AdapterOperationsRecord = operations ?? {};
  const rows: readonly (readonly [keyof AdapterOperationsRecord, string, CapabilityPresentation])[] = [
    ["start", "Start", booleanCapability(record.start)],
    ["events", "Events", booleanCapability(record.events)],
    ["cancel", "Cancel", booleanCapability(record.cancel)],
    ["resume", "Resume", booleanCapability(record.resume)],
    ["handoff", "Handoff", booleanCapability(record.handoff)],
    ["guidance", "Guidance", guidanceCapability(record.guidance)],
    ["pause", "Pause", pauseCapability(record.pause)],
    ["approval_response", "Approval response", booleanCapability(record.approval_response)],
    ["usage", "Usage", usageCapability(record.usage)],
    ["hard_cost_limit", "Hard cost limit", booleanCapability(record.hard_cost_limit)],
  ];
  return rows.map(([key, name, presentation]) => ({ key, name, label: presentation.label, tone: presentation.tone }));
}
