import { CircleCheck } from "lucide-react";
import type { ReactNode } from "react";
import { ownValue, ServerRefusal } from "../services/api";

// The display primitives the workspace, connection, resource and planning
// surfaces share: one status badge, one field-error line, and the field row the
// planning forms are laid out from. The badge carries both status policies, so a
// surface selects one by scope instead of by which of two definitions it
// imported.

// The three status tables. Own keys only: a served status may be `constructor`,
// and a plain index would render Object's own member as a "known" label.
const knownWorkspaceStatuses: Record<string, true> = {
  active: true, archived: true, backlog: true, ready: true, in_progress: true, in_review: true, review: true,
  done: true, cancelled: true, blocked: true, todo: true,
  // Runtime lifecycle states (the orchestration check constraint allows exactly these two).
  online: true, offline: true
};
const knownResourceStatuses: Record<string, true> = { healthy: true, degraded: true, offline: true, unknown: true };
const knownSyncStatuses: Record<string, true> = { unknown: true, syncing: true, synced: true, stale: true, failed: true };

/** A served identifier as words: `in_review` reads `in review`. */
export const formatLabel = (value: string) => value.replaceAll("_", " ");

/** The workspace lifecycle states as label text; anything else is named as unknown. */
export function statusLabel(status: string): string {
  return ownValue(knownWorkspaceStatuses, status) ? formatLabel(status) : `Unknown status${status ? `: ${status}` : ""}`;
}

/** The workspace lifecycle wording, or one of the three served resource scopes. */
type StatusScope = "workspace" | "connection" | "resource" | "sync";

export function StatusBadge({ status, scope = "workspace" }: { status: string; scope?: StatusScope }) {
  if (scope === "workspace") {
    return <span className={`status-badge ${status === "done" ? "status-complete" : ""}`}>
      {status === "done" && <CircleCheck size={12} aria-hidden="true" />}
      {statusLabel(status)}
    </span>;
  }
  // Own keys only: a served status may be `constructor`, and a plain index
  // would render Object's own member as a "known" label.
  const known = scope === "sync" ? ownValue(knownSyncStatuses, status) : ownValue(knownResourceStatuses, status);
  const tone = status === "healthy" || status === "synced" ? "status-complete" : status === "degraded" || status === "failed" || status === "offline" ? "status-attention" : "";
  return <span className={`status-badge ${tone}`} data-status={status}>{known ? formatLabel(status).replace(/(^|\s)\S/g, (letter) => letter.toUpperCase()) : `Unknown ${scope} status: ${status || "unspecified"}`}</span>;
}

/** The server's field message for one control, verbatim. */
export function FieldErrors({ error, field }: { error: unknown; field: string }) {
  const messages = (error instanceof ServerRefusal && error.fields?.[field]) || [];
  if (messages.length === 0) return null;
  return <p className="field-error" role="alert">{messages.join(" ")}</p>;
}

/**
 * One `.planning-form` row: the control's label, the control, and the server's
 * own field message. The form is a CSS grid over its direct children, so this
 * renders a fragment — a wrapper element would collapse the three grid rows
 * into one cell.
 */
export function Field({ id, label, error, field, children }: { id: string; label: string; error: unknown; field: string; children: ReactNode }) {
  return <>
    <label htmlFor={id}>{label}</label>
    {children}
    <FieldErrors error={error} field={field} />
  </>;
}
