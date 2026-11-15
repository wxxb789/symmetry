import { AlertCircle, CircleCheck, Inbox, RefreshCw } from "lucide-react";
import type { ReactNode } from "react";
import { errorMessage } from "../services/api";

export function PageHeading({ title, description, children, level = 1, id = "content-heading" }: {
  title: string;
  description: string;
  children?: ReactNode;
  /** The drawer background demotes its project heading so the open detail keeps the single page heading. */
  level?: 1 | 2;
  id?: string;
}) {
  return <header className="page-heading">
    <div>
      {level === 1
        ? <h1 id={id} tabIndex={-1}>{title}</h1>
        : <h2 id={id} tabIndex={-1}>{title}</h2>}
      <p className="muted">{description}</p>
    </div>
    {children}
  </header>;
}

export function LoadingState({ label = "Loading workspace…" }: { label?: string }) {
  return <div className="empty-state" role="status"><RefreshCw className="loading-icon" aria-hidden="true" /><p>{label}</p></div>;
}

export function EmptyState({ title, children }: { title: string; children: ReactNode }) {
  return <section className="empty-state"><Inbox size={28} aria-hidden="true" /><h2>{title}</h2><p className="muted">{children}</p></section>;
}

export function ErrorState({ error, retry, stale = false }: { error: unknown; retry: () => void; stale?: boolean }) {
  return <section className="notice notice-error" role="alert">
    <AlertCircle size={20} aria-hidden="true" />
    <div><h2>{stale ? "Refresh failed — showing last loaded data" : "Unable to load workspace"}</h2><p>{errorMessage(error)}</p>
      <button className="button" type="button" onClick={retry}>Try again</button>
    </div>
  </section>;
}

export function RefreshButton({ fetching, refresh }: { fetching: boolean; refresh: () => void }) {
  return <button className="button" type="button" onClick={refresh} aria-disabled={fetching}>
    <RefreshCw size={16} aria-hidden="true" className={fetching ? "loading-icon" : ""} />
    {fetching ? "Refreshing…" : "Refresh"}
  </button>;
}

const knownStatuses = new Set(["active", "archived", "backlog", "ready", "in_progress", "in_review", "review", "done", "cancelled", "blocked", "todo"]);

export function statusLabel(status: string): string {
  return knownStatuses.has(status) ? status.replaceAll("_", " ") : `Unknown status${status ? `: ${status}` : ""}`;
}

export function StatusBadge({ status }: { status: string }) {
  return <span className={`status-badge ${status === "done" ? "status-complete" : ""}`}>
    {status === "done" && <CircleCheck size={12} aria-hidden="true" />}
    {statusLabel(status)}
  </span>;
}

