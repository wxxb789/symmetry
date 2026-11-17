import * as Dialog from "@radix-ui/react-dialog";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useParams, useRouteContext, useSearch } from "@tanstack/react-router";
import { X } from "lucide-react";
import { useEffect, useState } from "react";
import { useSessionToken } from "../../app/session";
import { workDetailOptions, workspaceOptions } from "../../app/application";
import { EmptyState, ErrorState, LoadingState, PageHeading, RefreshButton, StatusBadge } from "../../components/workspace";
import { errorMessage, type Api, type Project, type WorkDetail } from "../../services/api";
import { workSearch, type WorkSearch } from "../projects/filters";
import { OwnershipChip, ProjectWorkSurface, type WorkSurfaceNavigation } from "../projects/work-surface";
import { WorkItemEditor } from "../planning/editors";
import { hasServedRevision, isGoalManaged, workItemEditable } from "../planning/write-policy";
import { AssignmentSection } from "./assignment";
import { WorkControlSurface, WorkControls, WorkReceipts, WorkWaitingInput } from "./control";
import { WorkHistory } from "./history";
import {
  acceptanceLabel,
  asText,
  ciLabel,
  costHint,
  costLabel,
  decisionScopeLabel,
  decisionView,
  deliveryRows,
  evidenceRows,
  executionStateLabel,
  failureText,
  nextActionLabel,
  noAcceptedEvidence,
  noBlocker,
  noNextAction,
  noReportedOutput,
  ownerLabel,
  priorityLabel,
  reportedResult,
  reportedText,
  resultKindLabel,
  safeUrl,
  timestamp,
  type EvidenceAttribution,
  type EvidenceRow,
} from "./presentation";

/**
 * `/portal/work/:workItemId` — outcome, reported evidence, blocker/decision, next
 * action and execution history, in that order. Desktop renders a Radix drawer and
 * narrow widths render the same content as a page. The drawer stays mounted while
 * the detail loads, fails or resolves its project scope, so keyboard focus is
 * never dropped; the project work behind it appears only once the served
 * projection confirms which project the item belongs to.
 */
export function WorkDetailPage() {
  const { workItemId } = useParams({ from: "/work/$workItemId" });
  const search = useSearch({ from: "/work/$workItemId" });
  const { application } = useRouteContext({ from: "__root__" });
  const navigate = useNavigate();
  const desktop = useDesktopDrawer();
  const csrf = useSessionToken();
  const queryClient = useQueryClient();
  // Shares the detail cache with the body below: no second request, and the
  // query's project scope stays a hint until the served projection confirms it.
  const detail = useQuery(workDetailOptions(application.api, workItemId)).data;
  const item = detail?.work_item;
  const listState = workSearch({ ...search });
  const queryProjectId = search.projectId;
  const itemProjectId = item?.project_id;
  const foreignScope = queryProjectId !== undefined && itemProjectId !== undefined && queryProjectId !== itemProjectId;
  const scopedProjectId = itemProjectId ?? queryProjectId;
  // The project snapshot drives the editor's resource selects and the archived
  // read-only gate. It is read from the cache the drawer background already
  // fills on desktop; a cold deep link fetches it only when the operator opens
  // the editor (see openEditor), so no read path pays for it.
  const cachedProject = useQuery({
    ...workspaceOptions(application.api, scopedProjectId ?? ""),
    enabled: false,
  }).data?.projects.find((candidate) => candidate.id === scopedProjectId);
  const editable = item !== undefined && workItemEditable(cachedProject?.status, item);
  const versionedItem = item !== undefined && hasServedRevision(item) ? item : null;
  // The editor and its draft owner live above the responsive branches: the
  // 1024 px breakpoint swaps the detail's presentation tree (drawer above,
  // page below), which remounts that tree but never this state, the open
  // editor or its form values.
  const [editing, setEditing] = useState<Project | null>(null);
  const [opening, setOpening] = useState(false);
  const [pendingWrites, setPendingWrites] = useState(0);
  const [notice, setNotice] = useState<{ tone: "status" | "alert"; text: string } | null>(null);
  // An editor whose row stops carrying a served revision has no writable path:
  // the form is not rendered and the surface says so instead of dropping the
  // draft silently.
  const revisionLost = editing !== null && item !== undefined && !hasServedRevision(item);
  const shownNotice = revisionLost
    ? { tone: "alert" as const, text: "This work item's revision is no longer served, so the open form was closed without writing anything." }
    : notice;
  // The node inside the drawer's dialog that hosts the open editor's DOM: the
  // drawer becomes an ancestor of the editor, so Radix's aria-hidden bookkeeping
  // keeps the editor visible whichever of the two dialogs mounted first.
  const [drawerHost, setDrawerHost] = useState<HTMLElement | null>(null);
  const reRead = () => Promise.all([
    queryClient.invalidateQueries({ queryKey: ["work-item", { workItemId }] }),
    queryClient.invalidateQueries({ queryKey: ["workspace"] }),
  ]);
  useEffect(() => {
    if (!foreignScope || itemProjectId === undefined) return;
    // Canonical URL for the actual subject: the other project's list filters do not apply.
    void navigate({ to: "/work/$workItemId", params: { workItemId }, search: { projectId: itemProjectId }, replace: true });
  }, [foreignScope, itemProjectId, workItemId, navigate]);
  const setSearch = (next: (previous: WorkSearch) => WorkSearch, options?: { replace?: boolean }) =>
    void navigate({
      from: "/work/$workItemId",
      to: "/work/$workItemId",
      params: { workItemId },
      // Functional form: the router hands the latest search, so rapid compound
      // updates cannot drop an earlier change, and the verified scope is kept.
      search: (previous) => ({ ...next(previous), ...(previous.projectId ? { projectId: previous.projectId } : {}) }),
      replace: options?.replace ?? true,
    });
  // The editor needs the project snapshot for its resource selects and its
  // archived gate; a cold deep link fetches it here, once, when the operator
  // actually opens the editor.
  const openEditor = async () => {
    if (item === undefined || opening) return;
    const projectId = item.project_id ?? scopedProjectId;
    if (projectId === null || projectId === undefined) {
      setNotice({ tone: "alert", text: "This work item's project was not served, so its editor cannot load the project's resources." });
      return;
    }
    setOpening(true);
    try {
      const workspace = cachedProject === undefined
        ? await queryClient.fetchQuery(workspaceOptions(application.api, projectId))
        : null;
      const project = cachedProject ?? workspace?.projects.find((candidate) => candidate.id === projectId);
      if (project === undefined) {
        setNotice({ tone: "alert", text: "This work item's project is unavailable, so its editor cannot load the project's resources." });
        return;
      }
      if (project.status === "archived") {
        setNotice({ tone: "alert", text: "This project is archived. Restore it before editing its work." });
        return;
      }
      setNotice(null);
      setEditing(project);
    } catch (error) {
      setNotice({ tone: "alert", text: errorMessage(error) });
    } finally {
      setOpening(false);
    }
  };
  const close = () => {
    const projectId = itemProjectId ?? queryProjectId;
    if (projectId === undefined) {
      void navigate({ to: "/projects", replace: true });
      return;
    }
    void navigate({ to: "/projects/$projectId", params: { projectId }, search: foreignScope ? {} : listState, replace: true });
  };
  const backgroundProjectId = foreignScope ? undefined : itemProjectId;
  const detailBody = (bodyScopedProjectId: string | null, drawer = false) =>
    <WorkDetailBody workItemId={workItemId} scopedProjectId={bodyScopedProjectId} backSearch={foreignScope ? {} : listState}
      editable={editable} opening={opening} pendingWrites={pendingWrites} notice={shownNotice} onDismissNotice={() => setNotice(null)}
      openEditor={() => { void openEditor(); }} drawer={drawer} />;
  // Filters of another project never describe this subject, so both detail branches
  // drop them once the served scope is known to be foreign.
  const presentation = desktop
    ? <>
      {backgroundProjectId !== undefined && <ProjectWorkBackground projectId={backgroundProjectId} navigation={{
        search: listState,
        setSearch,
        renderViewLink: (options) => <Link className={options.className} aria-current={options["aria-current"]} to="/work/$workItemId" params={{ workItemId }} search={{ ...options.search, projectId: backgroundProjectId }}>{options.label}</Link>,
      }} />}
      {/* Only the operator's own dismissals close the drawer: Escape, the overlay
          and the close control. An unattributable layer request (a window resize)
          is ignored, so a resize cannot navigate out from under an open form. */}
      <Dialog.Root open>
        <Dialog.Portal>
          <Dialog.Overlay className="dialog-overlay" />
          <Dialog.Content className="work-drawer" onCloseAutoFocus={(event) => event.preventDefault()}
            onEscapeKeyDown={close} onPointerDownOutside={close}>
            <button className="icon-button work-drawer-close" type="button" aria-label="Close work detail" onClick={close}><X size={20} aria-hidden="true" /></button>
            {detailBody(queryProjectId ?? itemProjectId ?? null, true)}
            <div ref={setDrawerHost} data-work-editor-host="" />
          </Dialog.Content>
        </Dialog.Portal>
      </Dialog.Root>
    </>
    : detailBody(queryProjectId ?? null);
  // The editor and its draft owner stay in one stable position across the
  // branch swap; only its dialog shell follows the presentation.
  return <>{presentation}{editing !== null && versionedItem !== null && <WorkItemEditor key={versionedItem.id} presentation={desktop ? "drawer" : "page"} drawerHost={drawerHost} project={editing} item={versionedItem} api={application.api} csrf={csrf}
    onWriteStart={() => setPendingWrites((count) => count + 1)}
    onWriteSettled={() => setPendingWrites((count) => Math.max(0, count - 1))}
    onSaved={(_receipt, text) => { setEditing(null); setNotice({ tone: "status", text }); void reRead(); }}
    onRefused={(message) => { setEditing(null); setNotice({ tone: "alert", text: message }); void reRead(); }}
    onClose={() => setEditing(null)} />}</>;
}

/** The project list/board behind the desktop drawer, so the detail URL reloads into the same view. */
function ProjectWorkBackground({ projectId, navigation }: { projectId: string; navigation: WorkSurfaceNavigation }) {
  const { application } = useRouteContext({ from: "__root__" });
  const query = useQuery(workspaceOptions(application.api, projectId));
  const project = query.data?.projects.find((item) => item.id === projectId);
  const refresh = () => { if (!query.isFetching) void query.refetch(); };
  return <div className="work-background">
    {query.isPending && <LoadingState label="Loading project…" />}
    {query.isError && <ErrorState error={query.error} retry={refresh} stale={query.data !== undefined} />}
    {query.data !== undefined && project === undefined && <EmptyState title="Project not found">This project is unavailable or you no longer have access. <Link to="/projects">View all projects</Link>.</EmptyState>}
    {project !== undefined && <>
      <PageHeading level={2} id="project-heading" title={project.name} description={project.description?.trim() || "Work and current blockers in this project."} />
      {project.status === "archived" && <p className="notice" role="status">This project is archived. Its existing work remains visible here.</p>}
      <ProjectWorkSurface project={project} navigation={navigation} />
    </>}
  </div>;
}

function WorkDetailBody({ workItemId, scopedProjectId, backSearch, editable, opening, pendingWrites, notice, onDismissNotice, openEditor, drawer = false }: {
  workItemId: string;
  scopedProjectId: string | null;
  backSearch: WorkSearch;
  /** Whether the item offers a writable path; the page owns the rule and the editor. */
  editable: boolean;
  opening: boolean;
  pendingWrites: number;
  /** The notice the page owns above the responsive branches. */
  notice: { tone: "status" | "alert"; text: string } | null;
  onDismissNotice: () => void;
  openEditor: () => void;
  drawer?: boolean;
}) {
  const { application } = useRouteContext({ from: "__root__" });
  const queryClient = useQueryClient();
  const query = useQuery(workDetailOptions(application.api, workItemId));
  const detail = query.data;
  const item = detail?.work_item;
  const backToProjectId = item?.project_id ?? scopedProjectId;
  const refresh = () => {
    if (query.isFetching) return;
    void query.refetch();
    // This page also renders the project surface behind the drawer from its own
    // workspace read, so the operator's refresh refreshes both: refreshing only
    // the work item would leave the background read's failure on screen after
    // the connection the refresh was meant to recover has already recovered.
    void queryClient.refetchQueries({ queryKey: ["workspace"], type: "active" });
  };
  return <>
    {drawer && <>
      <Dialog.Title className="sr-only">{item?.title ?? "Work item"}</Dialog.Title>
      <Dialog.Description className="sr-only">Work detail. Escape closes it and returns to the project.</Dialog.Description>
    </>}
    <PageHeading title={item?.title ?? "Work item"} description={item === undefined ? "Loading this work item…" : `${item.key} · ${ownerLabel(item.assignee)}`}>
      <div className="heading-actions">
        {backToProjectId === null
          ? <Link className="button" to="/projects">Back to projects</Link>
          : <Link className="button" to="/projects/$projectId" params={{ projectId: backToProjectId }} search={backSearch}>Back to project</Link>}
        {item !== undefined && editable && <button className="button button-primary" type="button" disabled={opening || pendingWrites > 0} onClick={() => { void openEditor(); }}>{opening ? "Opening…" : "Edit work item"}</button>}
        <RefreshButton fetching={query.isFetching} refresh={refresh} />
      </div>
    </PageHeading>
    {notice !== null && (notice.tone === "status"
      ? <p className="notice" role="status">{notice.text}<button className="button" type="button" onClick={onDismissNotice}>Dismiss</button></p>
      : <p className="notice notice-error" role="alert">{notice.text}</p>)}
    {query.isPending && <LoadingState label="Loading work item…" />}
    {query.isError && <ErrorState error={query.error} retry={refresh} stale={detail !== undefined} />}
    {detail && item && <WorkDetailSections detail={detail} api={application.api} />}
  </>;
}

function WorkDetailSections({ detail, api }: { detail: WorkDetail; api: Api }) {
  const item = detail.work_item;
  const outcome = detail.outcome;
  const execution = item.execution;
  const reported = reportedResult(execution);
  const executionState = executionStateLabel(execution?.state);
  // The detail read serves no bound-resource snapshot, so this chip cannot
  // confirm the binding a project row confirms; the title states that limit
  // instead of implying the resource itself is unconfirmed or broken.
  const ci = ciLabel(item.delivery);
  const ciTitle = `${ci.title} · the bound resources are served by the project read, not this detail read`;
  const decision = decisionView(execution?.waiting, asText(outcome.blocker));
  const nextAction = nextActionLabel(execution?.result);
  const summary = reportedText(outcome.summary) ?? reported.text;
  const reportedFailure = failureText(execution?.failure);
  const findings = evidenceRows(outcome.findings);
  const artifacts = evidenceRows(outcome.changed_artifacts);
  const checks = evidenceRows(outcome.tests);
  const hasReported = summary !== null || reportedFailure !== null || findings.length > 0 || artifacts.length > 0 || checks.length > 0;
  const updated = timestamp(item.updated_at);
  const reviewStatus = asText(item.review_status);
  const external = item.external ?? null;
  const providerUrl = safeUrl(external?.url);
  const providerState = asText(external?.state);
  const recordedBlocker = asText(outcome.blocker);
  const scopeLabel = decisionScopeLabel(execution?.waiting);
  const generation = execution === null || execution === undefined ? null : typeof execution.generation === "number" ? execution.generation : null;
  const runId = execution?.run_id ?? null;
  const goalManaged = isGoalManaged(item);
  const providerOwned = item.external?.provider_owned_fields ?? [];
  return <WorkControlSurface detail={detail} cursor={detail.raw.next_before}>
    <p className="detail-chips">
      <StatusBadge status={item.status} />
      <OwnershipChip item={item} />
      {executionState !== null && <span className="chip">{executionState}</span>}
      <span className={`chip chip-${ci.tone}`} title={ciTitle}>{ci.text}</span>
      {reviewStatus !== null && <span className="chip">review {reviewStatus}</span>}
      <span className="chip chip-unknown" title={costHint}>{costLabel}</span>
    </p>
    {goalManaged && <p className="notice" role="status">This work item is managed by a Goal. Its run, retry and move paths go through Goal admission; this surface records its current state and offers no writable path for it.</p>}
    {!goalManaged && providerOwned.length > 0 && <p className="notice" role="status">The external work tracker owns {providerOwned.join(", ")} on this item; those fields are read-only here and its state column is not movable from this board.</p>}
    <WorkControls detail={detail} />
    <WorkWaitingInput detail={detail} />

    <section className="detail-section" aria-labelledby="outcome-section">
      <h2 id="outcome-section">Outcome</h2>
      <p className="outcome-goal">{asText(outcome.goal) ?? item.title}</p>
      <p className="muted">{item.description?.trim() ? item.description : "No description recorded."}</p>
      <dl className="detail-facts">
        <div><dt>Work state</dt><dd>{item.status}</dd></div>
        <div><dt>Execution</dt><dd>{executionState ?? "No execution recorded"}</dd></div>
        {priorityLabel(item.priority) !== null && <div><dt>Priority</dt><dd>{priorityLabel(item.priority)}</dd></div>}
        <div><dt>Owner</dt><dd>{ownerLabel(item.assignee)}</dd></div>
        <div><dt>Updated</dt><dd>{updated === null ? "Not recorded" : <time dateTime={updated.exact} title={updated.exact}>{updated.readable}</time>}</dd></div>
        <div><dt>Cost</dt><dd>{costLabel}</dd></div>
        {external !== null && <div><dt>Provider</dt><dd>
          {asText(external.provider) ?? "unrecorded"}{providerState === null ? "" : ` · ${providerState}`}{external.available === false ? " · unavailable" : ""}
          {providerUrl !== null && <> <a href={providerUrl} rel="noreferrer noopener" target="_blank">Open external record</a></>}
        </dd></div>}
      </dl>
    </section>

    <section className="detail-section" aria-labelledby="evidence-section">
      <h2 id="evidence-section">Latest accepted evidence</h2>
      <p>{noAcceptedEvidence}<span className="muted"> — this surface serves reported execution output; acceptance is not part of the read contract.</span></p>
      <h3>Reported execution output</h3>
      {!hasReported
        ? <p className="muted">{noReportedOutput}</p>
        : <>
          {summary !== null && <p>{summary}</p>}
          {reported.kind !== null && <p className="muted text-small">Reported result kind: {resultKindLabel(reported.kind)}</p>}
          {generation !== null && <p className="muted text-small">Source: execution generation {generation}{runId === null ? "" : ` · run ${runId}`}</p>}
          {reportedFailure !== null && <p className="reported-failure">{reportedFailure}</p>}
          <EvidenceList title="Findings" rows={findings} />
          <EvidenceList title="Changed artifacts" rows={artifacts} />
          <EvidenceList title="Checks" rows={checks} />
        </>}
      <h3>Delivery evidence</h3>
      <dl className="delivery-list">
        {deliveryRows(item.delivery).map((row) => <div key={row.label}>
          <dt>{row.label}</dt>
          <dd>
            <span className={row.label === "CI" ? `chip chip-${ci.tone}` : "chip"}>{row.value}</span>
            <span className="muted text-small">source: {row.source}{row.provider === null ? "" : ` · ${row.provider}`}{row.recordedAt === null ? "" : ` · recorded ${timestamp(row.recordedAt)?.readable ?? row.recordedAt}`}</span>
            {row.url !== null && <a href={row.url} rel="noreferrer noopener" target="_blank">Open</a>}
          </dd>
        </div>)}
      </dl>
      <p className="muted text-small">{acceptanceLabel}: no acceptance identity is served for reported output.</p>
    </section>

    <section className="detail-section" aria-labelledby="blocker-section">
      <h2 id="blocker-section">Blocker</h2>
      {item.blocked
        ? <p className="blocker">{item.blocker?.trim() || "Blocking reason unknown"}</p>
        : recordedBlocker !== null
          ? <p className="blocker">{recordedBlocker}</p>
          : <p className="muted">{noBlocker}</p>}
      {decision !== null && <div className="decision-card">
        <h3>Decision needed</h3>
        <p className="decision-question">{decision.question}</p>
        {decision.reason !== null && <p className="muted text-small">Reason: {decision.reason}</p>}
        {decision.context !== null && <p>{decision.context}</p>}
        {decision.options.length > 0 && <ul className="decision-options">
          {decision.options.map((option) => <li key={option.label}>
            <span className="decision-option-label">{option.label}</span>
            {option.recommended && <span className="chip decision-recommendation">Recommended · advisory only</span>}
            <span className="muted">{option.consequence}</span>
          </li>)}
        </ul>}
        <p className="muted text-small">The recommendation is advisory and separate from authority. This read-only surface records the decision; it does not act on it.</p>
        {scopeLabel !== null && <p className="muted text-small">{scopeLabel}</p>}
      </div>}
    </section>

    <section className="detail-section" aria-labelledby="next-action-section">
      <h2 id="next-action-section">Next action</h2>
      {nextAction === null ? <p className="muted">{noNextAction}</p> : <>
        <p>{nextAction}</p>
        <p className="muted text-small">Advisory text recorded with the reported result; no action control is part of this workspace.</p>
      </>}
    </section>

    <section className="detail-section" aria-labelledby="history-section">
      <h2 id="history-section">Execution history</h2>
      <WorkReceipts firstPage={detail.raw.timeline} />
      <WorkHistory workItemId={item.id} firstPage={detail.raw.timeline} cursor={detail.raw.next_before} />
    </section>
    {/* Last, so the read-only sections keep their order: the 0007b acceptance
        pins the first five detail sections, and this surface adds a section. */}
    <AssignmentSection api={api} item={item} />
  </WorkControlSurface>;
}

function EvidenceList({ title, rows }: { title: string; rows: EvidenceRow[] }) {
  if (rows.length === 0) return null;
  return <div className="evidence-group">
    <h4>{title}</h4>
    <ul>
      {rows.map((row, index) => <li key={`${row.text}-${index}`}>
        <span>{row.text}</span>
        {row.attribution !== null && <span className="muted text-small"> — {attributionLabel(row.attribution)}</span>}
      </li>)}
    </ul>
  </div>;
}

function attributionLabel(attribution: EvidenceAttribution): string {
  const recorded = attribution.recordedAt === null ? null : timestamp(attribution.recordedAt)?.readable ?? attribution.recordedAt;
  return [attribution.source, attribution.generation === null ? null : `generation ${attribution.generation}`, recorded]
    .filter((part): part is string => part !== null)
    .join(" · ");
}

/** Below the drawer breakpoint the detail is a full page; the URL stays the same. */
function useDesktopDrawer(): boolean {
  const [desktop, setDesktop] = useState(() => window.matchMedia("(min-width: 1024px)").matches);
  useEffect(() => {
    const media = window.matchMedia("(min-width: 1024px)");
    const update = () => setDesktop(media.matches);
    update();
    media.addEventListener("change", update);
    return () => media.removeEventListener("change", update);
  }, []);
  return desktop;
}
