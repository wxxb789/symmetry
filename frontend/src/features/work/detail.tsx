import * as Dialog from "@radix-ui/react-dialog";
import { useQuery } from "@tanstack/react-query";
import { Link, useNavigate, useParams, useRouteContext, useSearch } from "@tanstack/react-router";
import { X } from "lucide-react";
import { useEffect, useState } from "react";
import { workDetailOptions, workspaceOptions } from "../../app/application";
import { EmptyState, ErrorState, LoadingState, PageHeading, RefreshButton, StatusBadge } from "../../components/workspace";
import type { WorkDetail } from "../../services/api";
import { workSearch, type WorkSearch } from "../projects/filters";
import { ProjectWorkSurface, type WorkSurfaceNavigation } from "../projects/work-surface";
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
  // Shares the detail cache with the body below: no second request, and the
  // query's project scope stays a hint until the served projection confirms it.
  const detail = useQuery(workDetailOptions(application.api, workItemId)).data;
  const listState = workSearch({ ...search });
  const queryProjectId = search.projectId;
  const itemProjectId = detail?.work_item.project_id;
  const foreignScope = queryProjectId !== undefined && itemProjectId !== undefined && queryProjectId !== itemProjectId;
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
  // Filters of another project never describe this subject, so both detail branches
  // drop them once the served scope is known to be foreign.
  if (!desktop) return <WorkDetailBody workItemId={workItemId} scopedProjectId={queryProjectId ?? null} backSearch={foreignScope ? {} : listState} />;
  const close = () => {
    const projectId = itemProjectId ?? queryProjectId;
    if (projectId === undefined) {
      void navigate({ to: "/projects", replace: true });
      return;
    }
    void navigate({ to: "/projects/$projectId", params: { projectId }, search: foreignScope ? {} : listState, replace: true });
  };
  const backgroundProjectId = foreignScope ? undefined : itemProjectId;
  return <>
    {backgroundProjectId !== undefined && <ProjectWorkBackground projectId={backgroundProjectId} navigation={{
      search: listState,
      setSearch,
      renderViewLink: (options) => <Link className={options.className} aria-current={options["aria-current"]} to="/work/$workItemId" params={{ workItemId }} search={{ ...options.search, projectId: backgroundProjectId }}>{options.label}</Link>,
    }} />}
    <Dialog.Root open onOpenChange={(open) => { if (!open) close(); }}>
      <Dialog.Portal>
        <Dialog.Overlay className="dialog-overlay" />
        <Dialog.Content className="work-drawer" onCloseAutoFocus={(event) => event.preventDefault()}>
          <Dialog.Close asChild><button className="icon-button work-drawer-close" type="button" aria-label="Close work detail"><X size={20} aria-hidden="true" /></button></Dialog.Close>
          <WorkDetailBody workItemId={workItemId} scopedProjectId={queryProjectId ?? itemProjectId ?? null} backSearch={foreignScope ? {} : listState} drawer />
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  </>;
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

function WorkDetailBody({ workItemId, scopedProjectId, backSearch, drawer = false }: {
  workItemId: string;
  scopedProjectId: string | null;
  backSearch: WorkSearch;
  drawer?: boolean;
}) {
  const { application } = useRouteContext({ from: "__root__" });
  const query = useQuery(workDetailOptions(application.api, workItemId));
  const detail = query.data;
  const item = detail?.work_item;
  const backToProjectId = item?.project_id ?? scopedProjectId;
  const refresh = () => { if (!query.isFetching) void query.refetch(); };
  return <>
    {drawer && <>
      <Dialog.Title className="sr-only">{item?.title ?? "Work item"}</Dialog.Title>
      <Dialog.Description className="sr-only">Read-only work detail. Escape closes it and returns to the project.</Dialog.Description>
    </>}
    <PageHeading title={item?.title ?? "Work item"} description={item === undefined ? "Loading this work item…" : `${item.key} · ${ownerLabel(item.assignee)}`}>
      <div className="heading-actions">
        {backToProjectId === null
          ? <Link className="button" to="/projects">Back to projects</Link>
          : <Link className="button" to="/projects/$projectId" params={{ projectId: backToProjectId }} search={backSearch}>Back to project</Link>}
        <RefreshButton fetching={query.isFetching} refresh={refresh} />
      </div>
    </PageHeading>
    {query.isPending && <LoadingState label="Loading work item…" />}
    {query.isError && <ErrorState error={query.error} retry={refresh} stale={detail !== undefined} />}
    {detail && item && <WorkDetailSections detail={detail} />}
  </>;
}

function WorkDetailSections({ detail }: { detail: WorkDetail }) {
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
  return <>
    <p className="detail-chips">
      <StatusBadge status={item.status} />
      {executionState !== null && <span className="chip">{executionState}</span>}
      <span className={`chip chip-${ci.tone}`} title={ciTitle}>{ci.text}</span>
      {reviewStatus !== null && <span className="chip">review {reviewStatus}</span>}
      <span className="chip chip-unknown" title={costHint}>{costLabel}</span>
    </p>

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
            <span className="muted text-small">source: {row.source}{row.recordedAt === null ? "" : ` · recorded ${timestamp(row.recordedAt)?.readable ?? row.recordedAt}`}</span>
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
      <WorkHistory workItemId={item.id} firstPage={detail.raw.timeline} cursor={detail.raw.next_before} />
    </section>
  </>;
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
