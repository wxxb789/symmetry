import { Link } from "@tanstack/react-router";
import { AlertCircle } from "lucide-react";
import { useState, type KeyboardEvent, type ReactNode } from "react";
import type { Project, WorkItem } from "../../services/api";
import { EmptyState, StatusBadge, statusLabel } from "../../components/workspace";
import { acceptanceLabel, ciLabel, ciResourcesOf, costHint, costLabel, noBlocker, noReportedResult, ownerKeyLabel, ownerLabel, priorityLabel, reportedResult, type CiResourceLike } from "../work/presentation";
import {
  boardColumns,
  filterWorkItems,
  filtersOf,
  ownerOptions,
  sameWorkSearch,
  stateOptions,
  summarizeWork,
  workSearch,
  workSummaryText,
  type WorkItemSearch,
  type WorkSearch,
  type WorkView,
} from "./filters";
import { useSavedViews } from "./saved-views";

export interface WorkSurfaceNavigation {
  search: WorkSearch;
  setSearch: (next: (previous: WorkSearch) => WorkSearch, options?: { replace?: boolean }) => void;
  /** The routes own their link targets; the surface owns the label, active state and target search. */
  renderViewLink: (options: ViewLinkOptions) => ReactNode;
}

export interface ViewLinkOptions {
  label: string;
  className: string;
  "aria-current": "page" | undefined;
  search: WorkSearch;
}

/** Saved list/board projection of one project's work, shared by the project route and the work-detail background. */
export function ProjectWorkSurface({ project, navigation }: { project: Project; navigation: WorkSurfaceNavigation }) {
  const { search, setSearch } = navigation;
  const items = project.work_items;
  // Both views chip CI against the project's served resources, so an item's
  // bound resource is read from one snapshot rather than re-derived per row.
  const ciResources = ciResourcesOf(project);
  const filtered = filterWorkItems(items, filtersOf(search));
  const summary = summarizeWork(items);
  const rowSearch: WorkItemSearch = { ...search, projectId: project.id };
  return <>
    <WorkToolbar project={project} navigation={navigation} />
    <p className="work-summary">{workSummaryText(summary.total, summary.blocked)}{filtered.length === summary.total ? "" : ` · showing ${filtered.length}`}</p>
    {filtered.length === 0
      ? items.length === 0
        ? <EmptyState title="No work items">This project does not have any work items yet.</EmptyState>
        : <EmptyState title="No matching work items">
          <span className="empty-actions">No work items match the current filters.
            <button className="button" type="button" onClick={() => setSearch(() => ({}), { replace: false })}>Clear filters</button>
          </span>
        </EmptyState>
      : (search.view ?? "list") === "board"
        ? <WorkBoard items={filtered} search={rowSearch} resources={ciResources} />
        : <WorkRows items={filtered} search={rowSearch} resources={ciResources} />}
  </>;
}

function WorkToolbar({ project, navigation }: { project: Project; navigation: WorkSurfaceNavigation }) {
  const { search, setSearch, renderViewLink } = navigation;
  const view = search.view ?? "list";
  const saved = useSavedViews(project.id);
  const [name, setName] = useState("");
  const activeView = saved.views.find((entry) => sameWorkSearch(entry.search, search))?.name ?? "";
  const stateChoices = stateOptions(project.work_items);
  const ownerChoices = ownerOptions(project.work_items);
  // A filter carried in the URL stays visible and truthful even when the current
  // snapshot no longer contains that state or owner.
  const absentState = search.state !== undefined && !stateChoices.includes(search.state) ? search.state : undefined;
  const absentOwner = search.owner !== undefined && !ownerChoices.some((option) => option.value === search.owner) ? search.owner : undefined;
  const viewLink = (target: WorkView) => renderViewLink({
    label: target === "list" ? "List" : "Board",
    className: view === target ? "view-option active" : "view-option",
    "aria-current": view === target ? "page" : undefined,
    search: workSearch({ ...search, view: target }),
  });
  return <section className="work-toolbar" aria-label="Work views and filters">
    <div className="toolbar-row">
      <nav className="view-switch" aria-label="Work views">
        {viewLink("list")}
        {viewLink("board")}
      </nav>
      <span className="muted text-small">Read-only</span>
    </div>
    <div className="toolbar-row">
      <input className="filter-input" type="search" aria-label="Filter work items" placeholder="Search title, key or owner" value={search.q ?? ""}
        onChange={(event) => { const value = event.currentTarget.value; setSearch((previous) => workSearch({ ...previous, q: value })); }} />
      <select aria-label="Work state" value={search.state ?? ""} onChange={(event) => { const value = event.currentTarget.value; setSearch((previous) => workSearch({ ...previous, state: value })); }}>
        <option value="">All states</option>
        {absentState !== undefined && <option value={absentState}>{statusLabel(absentState)} (not in this project)</option>}
        {stateChoices.map((state) => <option key={state} value={state}>{statusLabel(state)}</option>)}
      </select>
      <select aria-label="Work owner" value={search.owner ?? ""} onChange={(event) => { const value = event.currentTarget.value; setSearch((previous) => workSearch({ ...previous, owner: value })); }}>
        <option value="">All owners</option>
        {absentOwner !== undefined && <option value={absentOwner}>{ownerKeyLabel(absentOwner)} (not in this project)</option>}
        {ownerChoices.map((option) => <option key={option.value} value={option.value}>{option.label}</option>)}
      </select>
      <label className="checkbox-field"><input type="checkbox" aria-label="Blocked only" checked={search.blocked === "1"}
        onChange={(event) => { const checked = event.currentTarget.checked; setSearch((previous) => workSearch({ ...previous, blocked: checked ? "1" : undefined })); }} />Blocked only</label>
    </div>
    <div className="toolbar-row">
      <select aria-label="Saved views" value={activeView} onChange={(event) => {
        const entry = saved.views.find((candidate) => candidate.name === event.currentTarget.value);
        if (entry) setSearch(() => entry.search, { replace: false });
      }}>
        <option value="">{saved.views.length === 0 ? "No saved views" : "Saved views"}</option>
        {saved.views.map((entry) => <option key={entry.name} value={entry.name}>{entry.name}</option>)}
      </select>
      <input type="text" aria-label="Saved view name" placeholder="View name" value={name} onChange={(event) => setName(event.currentTarget.value)} />
      <button className="button" type="button" disabled={name.trim() === ""} onClick={() => { saved.save(name, search); setName(""); }}>Save view</button>
      {activeView !== "" && <button className="button" type="button" onClick={() => saved.remove(activeView)}>Delete saved view</button>}
    </div>
  </section>;
}

function WorkRows({ items, search, resources }: { items: readonly WorkItem[]; search: WorkItemSearch; resources: ReadonlyMap<string, CiResourceLike> }) {
  return <ul className="work-list" aria-label="Work items">
    {items.map((item) => <li key={item.id} className="work-row">
      <Link className="work-row-link" to="/work/$workItemId" params={{ workItemId: item.id }} search={search} aria-label={item.title}>
        <WorkRowIdentity item={item} />
        <StatusBadge status={item.status} />
        <WorkRowResult item={item} />
        <WorkRowBlocker item={item} />
        <span className="chip chip-unknown" title={costHint}>{costLabel}</span>
        <CiChip item={item} resources={resources} />
      </Link>
    </li>)}
  </ul>;
}

function WorkBoard({ items, search, resources }: { items: readonly WorkItem[]; search: WorkItemSearch; resources: ReadonlyMap<string, CiResourceLike> }) {
  return <section className="work-board" aria-label="Work board" onKeyDown={moveBoardFocus}>
    {boardColumns(items).map((column) => {
      const headingId = `board-column-${column.state.replaceAll("_", "-")}`;
      return <section className="board-column" key={column.state} aria-labelledby={headingId}>
        <div className="board-heading"><h2 id={headingId}>{statusLabel(column.state)}</h2><span className="count">{column.items.length}</span></div>
        <ul className="board-cards">
          {column.items.map((item, index) => <li key={item.id} data-board-card="" data-board-column={column.state} data-board-index={index}>
            <Link className="board-card" to="/work/$workItemId" params={{ workItemId: item.id }} search={search} aria-label={item.title}>
              <span className="item-key">{item.key}</span>
              <span className="work-title">{item.title}</span>
              <span className="board-meta">{ownerLabel(item.assignee)}{priorityLabel(item.priority) === null ? "" : ` · ${priorityLabel(item.priority)}`}</span>
              <WorkRowResult item={item} />
              <WorkRowBlocker item={item} />
              <span className="board-chips"><span className="chip chip-unknown" title={costHint}>{costLabel}</span><CiChip item={item} resources={resources} /></span>
            </Link>
          </li>)}
        </ul>
      </section>;
    })}
  </section>;
}

function WorkRowIdentity({ item }: { item: WorkItem }) {
  const priority = priorityLabel(item.priority);
  return <span className="work-main">
    <span className="work-identity"><span className="item-key">{item.key}</span><span className="work-title">{item.title}</span></span>
    <span className="work-context">
      <span className="work-cell">{ownerLabel(item.assignee)}</span>
      {priority !== null && <span className="work-cell muted">Priority {priority}</span>}
    </span>
  </span>;
}

function WorkRowResult({ item }: { item: WorkItem }) {
  const reported = reportedResult(item.execution);
  return <span className="work-result">
    <span>{acceptanceLabel}</span>
    <span className="muted">{reported.text === null ? noReportedResult : `Reported: ${reported.text}`}</span>
  </span>;
}

function WorkRowBlocker({ item }: { item: WorkItem }) {
  if (!item.blocked) return <span className="work-blocker muted">{noBlocker}</span>;
  return <span className="work-blocker"><AlertCircle size={14} aria-hidden="true" />Blocked: {item.blocker?.trim() || "Blocking reason unknown"}</span>;
}

function CiChip({ item, resources }: { item: WorkItem; resources: ReadonlyMap<string, CiResourceLike> }) {
  const resourceId = item.ci_resource_id ?? null;
  const ci = ciLabel(item.delivery, resourceId === null ? undefined : resources.get(resourceId) ?? null);
  return <span className={`chip chip-${ci.tone}`} title={ci.title}>{ci.text}</span>;
}

/** Arrow keys move focus between board cards; the cards themselves stay links so Enter opens them. */
function moveBoardFocus(event: KeyboardEvent<HTMLElement>) {
  const vertical = event.key === "ArrowUp" ? -1 : event.key === "ArrowDown" ? 1 : 0;
  const horizontal = event.key === "ArrowLeft" ? -1 : event.key === "ArrowRight" ? 1 : 0;
  if (vertical === 0 && horizontal === 0) return;
  const target = event.target;
  if (!(target instanceof HTMLElement)) return;
  const card = target.closest("[data-board-card]");
  if (!(card instanceof HTMLElement)) return;
  const cards = Array.from(event.currentTarget.querySelectorAll<HTMLElement>("[data-board-card]"));
  const column = card.dataset["boardColumn"] ?? "";
  const index = Number(card.dataset["boardIndex"] ?? "0");
  const next = vertical === 0
    ? neighbourCard(cards, column, index, horizontal)
    : cards.find((entry) => entry.dataset["boardColumn"] === column && Number(entry.dataset["boardIndex"]) === index + vertical);
  if (next) {
    event.preventDefault();
    next.querySelector("a")?.focus();
  }
}

/** Nearest card in the neighbouring column, clamped to that column's length. */
function neighbourCard(cards: readonly HTMLElement[], column: string, index: number, step: number): HTMLElement | undefined {
  const columns = [...new Set(cards.map((entry) => entry.dataset["boardColumn"] ?? ""))];
  const neighbour = cards.filter((entry) => entry.dataset["boardColumn"] === columns[columns.indexOf(column) + step]);
  return neighbour[Math.min(index, neighbour.length - 1)];
}
