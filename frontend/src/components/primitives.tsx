import { CircleCheck } from "lucide-react";
import type { ReactNode } from "react";
import { ownValue, ServerRefusal } from "../services/api";

// Own keys only: a served status may be `constructor`, and a plain index would
// render Object's own member as a "known" label.
const knownWorkspaceStatuses: Record<string, true> = {
  active: true, archived: true, backlog: true, ready: true, in_progress: true, in_review: true, review: true,
  done: true, cancelled: true, blocked: true, todo: true,
  // Runtime states: the orchestration check constraint allows exactly these two.
  online: true, offline: true
};
const knownResourceStatuses: Record<string, true> = { healthy: true, degraded: true, offline: true, unknown: true };
const knownSyncStatuses: Record<string, true> = { unknown: true, syncing: true, synced: true, stale: true, failed: true };

export const formatLabel = (value: string) => value.replaceAll("_", " ");

export function statusLabel(status: string): string {
  return ownValue(knownWorkspaceStatuses, status) ? formatLabel(status) : `Unknown status${status ? `: ${status}` : ""}`;
}

type StatusScope = "workspace" | "connection" | "resource" | "sync";

export function StatusBadge({ status, scope = "workspace" }: { status: string; scope?: StatusScope }) {
  if (scope === "workspace") {
    return <span className={`status-badge ${status === "done" ? "status-complete" : ""}`}>
      {status === "done" && <CircleCheck size={12} aria-hidden="true" />}
      {statusLabel(status)}
    </span>;
  }
  const known = scope === "sync" ? ownValue(knownSyncStatuses, status) : ownValue(knownResourceStatuses, status);
  const tone = status === "healthy" || status === "synced" ? "status-complete" : status === "degraded" || status === "failed" || status === "offline" ? "status-attention" : "";
  return <span className={`status-badge ${tone}`} data-status={status}>{known ? formatLabel(status).replace(/(^|\s)\S/g, (letter) => letter.toUpperCase()) : `Unknown ${scope} status: ${status || "unspecified"}`}</span>;
}

export function FieldErrors({ error, field }: { error: unknown; field: string }) {
  const messages = (error instanceof ServerRefusal && error.fields?.[field]) || [];
  if (messages.length === 0) return null;
  return <p className="field-error" role="alert">{messages.join(" ")}</p>;
}

// `.planning-form`/`.connection-form` are CSS grids over their direct children: this
// renders a fragment — a wrapper element would collapse the three grid rows into one.
export function Field({ id, label, error, field, children }: { id: string; label: string; error: unknown; field: string; children: ReactNode }) {
  return <>
    <label htmlFor={id}>{label}</label>
    {children}
    <FieldErrors error={error} field={field} />
  </>;
}
