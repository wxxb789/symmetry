import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Link, useRouteContext } from "@tanstack/react-router";
import { AlertCircle } from "lucide-react";
import { useLayoutEffect, useRef, useState, type KeyboardEvent, type ReactNode } from "react";
import { useSessionToken } from "../../app/session";
import type { MoveIntent, Project, WorkItem } from "../../services/api";
import { EmptyState, StatusBadge, statusLabel } from "../../components/workspace";
import { acceptanceLabel, ciLabel, ciResourcesOf, costHint, costLabel, noBlocker, noReportedResult, ownerKeyLabel, ownerLabel, priorityLabel, reportedResult, type CiResourceLike } from "../work/presentation";
import { boardMoveRequest, hasServedRevision, isGoalManaged, providerOwnedControlsOf, refusalNotice, workItemMovePolicy } from "../planning/write-policy";
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
  /** Present only on the project route: the detail's background surface stays read-only behind its dialog. */
  createWorkItem?: () => void;
  createDisabled?: boolean;
}

export interface ViewLinkOptions {
  label: string;
  className: string;
  "aria-current": "page" | undefined;
  search: WorkSearch;
}

/**
 * Focus a control that stays mounted after the update. A control that removes
 * itself on its own same-pathname update (clearing the last filter, deleting
 * the active saved view) would otherwise leave keyboard focus on `body`.
 */
function focusWorkControl(id: string) {
  document.getElementById(id)?.focus();
}

/**
 * A refresh can move the work item the operator is on: the board regroups cards
 * by state and the list re-sorts, and a re-parented node is removed and
 * re-inserted, which drops the focused link to `body` even though the same item
 * stays rendered. The work item id — not the node — is the identity that
 * survives the regroup: track the work link that holds focus and, when a commit
 * detaches it without another control taking focus, put focus back on the same
 * work item's link.
 */
function useWorkFocusRestore<T extends HTMLElement>() {
  const workRoot = useRef<T | null>(null);
  useLayoutEffect(() => {
    const root = workRoot.current;
    if (root === null) return;
    let focusedWork: { itemId: string; node: HTMLElement } | null = null;
    const track = (event: FocusEvent) => {
      const link = event.target instanceof Element ? event.target.closest<HTMLElement>("[data-work-item]") : null;
      focusedWork = link !== null && root.contains(link) ? { itemId: link.dataset["workItem"] ?? "", node: link } : null;
    };
    // The restore keys on the focused node itself being detached, so a commit
    // that only re-renders the surface never moves focus.
    const observer = new MutationObserver((records) => {
      const previous = focusedWork;
      if (previous === null) return;
      const detached = records.some((record) =>
        Array.from(record.removedNodes).some((node) => node === previous.node || node.contains(previous.node)));
      if (!detached) return;
      focusedWork = null;
      const active = document.activeElement;
      // A control that took focus in this commit (the content heading on a route
      // change, a dialog, a filter) keeps it; only a detached focus target that
      // fell back to the document is restored.
      if (active instanceof HTMLElement && active !== document.body && active !== document.documentElement && active.isConnected) return;
      for (const link of root.querySelectorAll<HTMLElement>("[data-work-item]")) {
        if (link.dataset["workItem"] === previous.itemId) {
          link.focus();
          return;
        }
      }
    });
    observer.observe(root, { childList: true, subtree: true });
    root.addEventListener("focusin", track);
    return () => { observer.disconnect(); root.removeEventListener("focusin", track); };
  }, []);
  return workRoot;
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
  const view = search.view ?? "list";
  return <>
    <WorkToolbar project={project} navigation={navigation} />
    <p className="work-summary">{workSummaryText(summary.total, summary.blocked)}{filtered.length === summary.total ? "" : ` · showing ${filtered.length}`}</p>
    {view === "board" && (project.status === "archived"
      ? <p className="board-hint muted text-small">This project is archived: the board is read-only until it is restored.</p>
      : <p className="board-hint muted text-small">Tab or the arrow keys move between cards; Enter opens one. Shift + ←/→ moves the focused card to another column; Shift + ↑/↓ reorders it inside its column.</p>)}
    {filtered.length === 0
      ? items.length === 0
        ? <EmptyState title="No work items">This project does not have any work items yet.</EmptyState>
        : <EmptyState title="No matching work items">
          <span className="empty-actions">No work items match the current filters.
            <button className="button" type="button" onClick={() => { setSearch((previous) => workSearch({ view: previous.view }), { replace: false }); focusWorkControl("work-filter-input"); }}>Clear filters</button>
          </span>
        </EmptyState>
      : view === "board"
        ? <WorkBoard items={filtered} search={rowSearch} resources={ciResources} projectStatus={project.status} />
        : <WorkRows items={filtered} search={rowSearch} resources={ciResources} />}
  </>;
}

function WorkToolbar({ project, navigation }: { project: Project; navigation: WorkSurfaceNavigation }) {
  const { search, setSearch, renderViewLink, createWorkItem, createDisabled = false } = navigation;
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
      {project.status === "archived"
        ? <span className="muted text-small">Archived project · read-only until restored</span>
        : createWorkItem !== undefined
          ? <button className="button button-primary" type="button" disabled={createDisabled} onClick={createWorkItem}>New work item</button>
          : <span className="muted text-small">Read-only in this context</span>}
    </div>
    <div className="toolbar-row">
      <input id="work-filter-input" className="filter-input" type="search" aria-label="Filter work items" placeholder="Search title, key or owner" value={search.q ?? ""}
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
      <select id="work-saved-view-select" aria-label="Saved views" value={activeView} onChange={(event) => {
        const entry = saved.views.find((candidate) => candidate.name === event.currentTarget.value);
        if (entry) setSearch(() => entry.search, { replace: false });
      }}>
        <option value="">{saved.views.length === 0 ? "No saved views" : "Saved views"}</option>
        {saved.views.map((entry) => <option key={entry.name} value={entry.name}>{entry.name}</option>)}
      </select>
      <input type="text" aria-label="Saved view name" placeholder="View name" value={name} onChange={(event) => setName(event.currentTarget.value)} />
      <button className="button" type="button" disabled={name.trim() === ""} onClick={() => { saved.save(name, search); setName(""); }}>Save view</button>
      {activeView !== "" && <button className="button" type="button" onClick={() => { saved.remove(activeView); focusWorkControl("work-saved-view-select"); }}>Delete saved view</button>}
    </div>
  </section>;
}

function WorkRows({ items, search, resources }: { items: readonly WorkItem[]; search: WorkItemSearch; resources: ReadonlyMap<string, CiResourceLike> }) {
  const workRoot = useWorkFocusRestore<HTMLUListElement>();
  return <ul className="work-list" aria-label="Work items" ref={workRoot}>
    {items.map((item) => <li key={item.id} className="work-row">
      <Link className="work-row-link" to="/work/$workItemId" params={{ workItemId: item.id }} search={search} aria-label={item.title} data-work-item={item.id}>
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

export function OwnershipChip({ item }: { item: WorkItem }) {
  if (isGoalManaged(item)) return <span className="chip chip-owned" title="This work item's changes go through Goal admission, not this surface">Goal-managed</span>;
  if (providerOwnedControlsOf(item).size > 0) return <span className="chip chip-owned" title="Its external work tracker owns the fields listed in the editor">Provider-owned</span>;
  return null;
}

function WorkBoard({ items, search, resources, projectStatus }: { items: readonly WorkItem[]; search: WorkItemSearch; resources: ReadonlyMap<string, CiResourceLike>; projectStatus: string }) {
  const { application } = useRouteContext({ from: "__root__" });
  const csrf = useSessionToken();
  const queryClient = useQueryClient();
  const workRoot = useWorkFocusRestore<HTMLElement>();
  const [announcement, setAnnouncement] = useState("");
  const [moveFailure, setMoveFailure] = useState<string | null>(null);
  const columns = boardColumns(items);
  const move = useMutation({
    mutationFn: (request: { itemId: string; intent: MoveIntent; announcement: string }) => application.api.moveWorkItem(request.itemId, request.intent, csrf, new AbortController().signal),
    // The move is applied by the server; the board re-reads it and announces the
    // confirmed position. Focus follows the work item id through the regroup.
    onSuccess: async (_receipt, request) => {
      setMoveFailure(null);
      setAnnouncement(request.announcement);
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["workspace"] }),
        queryClient.invalidateQueries({ queryKey: ["work-item", { workItemId: request.itemId }] }),
      ]);
    },
    onError: async (error) => {
      setAnnouncement("");
      setMoveFailure(refusalNotice(error));
      await queryClient.invalidateQueries({ queryKey: ["workspace"] });
    },
  });
  const moveFocusedCard = (event: KeyboardEvent<HTMLElement>, item: WorkItem, direction: "left" | "right" | "up" | "down") => {
    event.preventDefault();
    if (move.isPending) return;
    if (!hasServedRevision(item)) {
      setAnnouncement("This card's revision was not served; refresh the project before moving it.");
      return;
    }
    const policy = workItemMovePolicy(projectStatus, item);
    const applies = direction === "left" || direction === "right" ? policy.statusChange : policy.reorder;
    if (!applies) {
      setAnnouncement(isGoalManaged(item)
        ? "Goal-managed work has no board movement here; its changes go through Goal admission."
        : projectStatus === "archived"
          ? "This project is archived; restore it to move work."
          : "This work item's state column is owned by the external work tracker.");
      return;
    }
    const request = boardMoveRequest({ direction, item, columns, stateLabel: statusLabel, version: item.version });
    if (request === null) return;
    setAnnouncement("");
    setMoveFailure(null);
    move.mutate({ itemId: item.id, intent: request.intent, announcement: request.announcement });
  };
  const handleKeyDown = (event: KeyboardEvent<HTMLElement>) => {
    const direction = event.key === "ArrowLeft" ? "left" : event.key === "ArrowRight" ? "right" : event.key === "ArrowUp" ? "up" : event.key === "ArrowDown" ? "down" : null;
    if (direction === null) return;
    const target = event.target;
    if (!(target instanceof HTMLElement)) return;
    const card = target.closest<HTMLElement>("[data-board-card]");
    if (card === null) return;
    if (!event.shiftKey) {
      moveBoardFocus(event);
      return;
    }
    const itemId = card.querySelector<HTMLElement>("[data-work-item]")?.dataset["workItem"];
    const item = items.find((candidate) => candidate.id === itemId);
    if (item === undefined) return;
    moveFocusedCard(event, item, direction);
  };
  return <section className="work-board" aria-label="Work board" onKeyDown={handleKeyDown} ref={workRoot}>
    <p className="sr-only" role="status">{move.isPending ? "Moving the focused card…" : announcement}</p>
    {moveFailure !== null && <p className="notice notice-error" role="alert">{moveFailure}</p>}
    {columns.map((column) => {
      const headingId = `board-column-${column.state.replaceAll("_", "-")}`;
      return <section className="board-column" key={column.state} aria-labelledby={headingId}>
        <div className="board-heading"><h2 id={headingId}>{statusLabel(column.state)}</h2><span className="count">{column.items.length}</span></div>
        <ul className="board-cards">
          {column.items.map((item, index) => <li key={item.id} data-board-card="" data-board-column={column.state} data-board-index={index}>
            <Link className="board-card" to="/work/$workItemId" params={{ workItemId: item.id }} search={search} aria-label={item.title} data-work-item={item.id}>
              <span className="item-key">{item.key}</span>
              <span className="work-title">{item.title}</span>
              <span className="board-meta">{ownerLabel(item.assignee)}{priorityLabel(item.priority) === null ? "" : ` · ${priorityLabel(item.priority)}`}</span>
              <WorkRowResult item={item} />
              <WorkRowBlocker item={item} />
              <span className="board-chips"><OwnershipChip item={item} /><span className="chip chip-unknown" title={costHint}>{costLabel}</span><CiChip item={item} resources={resources} /></span>
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
    <span className="work-identity"><span className="item-key">{item.key}</span><span className="work-title">{item.title}</span><OwnershipChip item={item} /></span>
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
