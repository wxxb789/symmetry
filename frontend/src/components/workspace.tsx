import { AlertCircle, CircleCheck, Inbox, RefreshCw } from "lucide-react";
import type { ReactNode } from "react";
import { errorMessage, type WorkItem } from "../services/api";

export function PageHeading({ title, description, children }: { title: string; description: string; children?: ReactNode }) {
  return <header className="page-heading">
    <div><h1 id="content-heading" tabIndex={-1}>{title}</h1><p className="muted">{description}</p></div>
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

export function StatusBadge({ status }: { status: string }) {
  const known = knownStatuses.has(status);
  return <span className={`status-badge ${status === "done" ? "status-complete" : ""}`}>
    {status === "done" && <CircleCheck size={12} aria-hidden="true" />}
    {known ? status.replaceAll("_", " ") : `Unknown status${status ? `: ${status}` : ""}`}
  </span>;
}

export function WorkList({ workItems }: { workItems: readonly WorkItem[] }) {
  if (workItems.length === 0) return <EmptyState title="No work items">This project does not have any work items yet.</EmptyState>;
  return <ul className="work-list" aria-label="Work items">
    {workItems.map((item) => <li key={item.id} className="work-row">
      <div className="work-main"><span className="item-key">{item.key}</span><h3>{item.title}</h3>
        {item.blocked && <p className="blocker"><AlertCircle size={14} aria-hidden="true" />Blocked: {item.blocker?.trim() || "Blocking reason unknown"}</p>}
      </div>
      <StatusBadge status={item.status} />
    </li>)}
  </ul>;
}
