import { useMutation, useQueries, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useRouteContext, useSearch } from "@tanstack/react-router";
import { AlertCircle, CircleCheck, Plus, Target } from "lucide-react";
import { useState } from "react";
import { attentionOptions, sessionOptions, workspaceOptions } from "../../app/application";
import { attentionReadKey } from "../../app/read-cache";
import { StatusBadge } from "../../components/primitives";
import { EmptyState, ErrorState, LoadingState, PageHeading, RefreshButton } from "../../components/workspace";
import { errorMessage, postGoalCreation, type GoalAttentionEntry, type ProjectBlockedEntry } from "../../services/api";
import { MutationRefused, newMutationId } from "../../services/commands";

export function AttentionPage() {
  const { application } = useRouteContext({ from: "__root__" });
  const search = useSearch({ from: "/attention" });
  const projectId = search.projectId ?? null;
  // The chain the reader expanded: the served cursor the feed's first page ended
  // at when the first older page was loaded, and the cursors of the pages loaded
  // from it. The cursor IS the boundary (it encodes the last Goal's position and
  // is not re-minted per read), so the served value is the identity to compare.
  const [expanded, setExpanded] = useState<{ readonly start: string | null; readonly cursors: readonly string[] }>({ start: null, cursors: [] });
  const workspace = useQuery(workspaceOptions(application.api, projectId ?? null));
  // The first page is its own read; the feed's chain starts at the cursor it
  // serves, so only its own read can name that start.
  const first = useQuery(attentionOptions(application.api, projectId, null));
  const servedStart = first.data?.goal_attention.next_cursor ?? null;
  // The reader's pages belong to the chain they were fetched for: a re-read that
  // ends somewhere else does not serve them, because the entries between its
  // first page and them were never loaded and the list would present the jump as
  // contiguous. One hook for every loaded cursor page, since the page count grows
  // as the reader loads more and hooks must not be called in a loop over it.
  const cursors = expanded.start !== null && expanded.start === servedStart ? expanded.cursors : [];
  const older = useQueries({
    queries: cursors.map((cursor) => attentionOptions(application.api, projectId, cursor)),
  });
  const queries = [first, ...older];
  const goalEntries = queries.flatMap((query) => query.data?.goal_attention.entries ?? []);
  const projectBlocked = first?.data?.project_blocked.entries ?? [];
  const selectedProjectName = workspace.data?.projects.find((project) => project.id === (projectId ?? first?.data?.selected_project_id))?.name ?? null;
  const goalUnavailable = first?.data?.goal_attention.unavailable ?? null;
  const projectUnavailable = first?.data?.project_blocked.unavailable ?? null;
  const nextCursor = queries[queries.length - 1]?.data?.goal_attention.next_cursor ?? null;
  const fetching = queries.some((query) => query.isFetching);
  // The workspace read owns the project scope the picker and this page's
  // heading describe, so its failure is presented as this surface's failure.
  const surfaceError = first?.error ?? workspace.error ?? null;
  // The served feed names its own project; the picker alone does not state the scope.
  const scopedProject = projectBlocked[0]?.project_name ?? projectBlocked[0]?.project_key ?? selectedProjectName ?? null;
  const refresh = () => {
    setExpanded({ start: null, cursors: [] });
    if (workspace.isError) void workspace.refetch();
    for (const query of queries) void query.refetch();
  };

  return <main className="workspace-content">
    <PageHeading title="Attention" description="Goal decisions, settlement attention and derived blockers together with the project's blocked work.">
      <div className="heading-actions">
        <NewGoalButton projectId={projectId ?? first?.data?.selected_project_id ?? null} />
        <RefreshButton fetching={fetching} refresh={refresh} />
      </div>
    </PageHeading>
    {search.unavailable && <section className="notice" role="status"><div><h2>This destination is unavailable</h2><p>The previous portal destination is not included in this workspace. Use Attention or Projects to browse existing work.</p></div></section>}
    {scopedProject && <p className="filter-note">Showing {scopedProject}. <Link to="/projects" search={{}}>Browse projects</Link></p>}
    {(first?.isPending || workspace.isPending) && <LoadingState label="Loading blocked work…" />}
    {surfaceError !== null && <ErrorState error={surfaceError} retry={refresh} stale={first?.data !== undefined} />}
    {first?.data && <>
      <GoalAttention entries={goalEntries} unavailable={goalUnavailable} nextCursor={nextCursor} fetching={fetching} onLoadMore={() => { if (nextCursor !== null) setExpanded({ start: servedStart, cursors: [...cursors, nextCursor] }); }} />
      <ProjectBlocked entries={projectBlocked} unavailable={projectUnavailable} />
    </>}
    <p className="sr-only" role="status">{fetching && !first?.isPending ? "Refreshing attention" : ""}</p>
  </main>;
}

function GoalAttention({ entries, unavailable, nextCursor, fetching, onLoadMore }: {
  entries: GoalAttentionEntry[];
  unavailable: { code: string; message: string } | null;
  nextCursor: string | null;
  fetching: boolean;
  onLoadMore: () => void;
}) {
  return <section aria-labelledby="goal-attention-heading">
    <div className="section-heading"><h2 id="goal-attention-heading">Goal attention <span className="count">{entries.length}</span></h2><span className="muted text-small">Server-derived</span></div>
    {unavailable && <section className="notice" role="status"><div>
      <h2>Goal attention is unavailable on this server</h2>
      <p>{unavailable.message} <span className="text-small">Code: <code>{unavailable.code}</code></span></p>
      <p className="text-small muted">The project's blocked work below is unaffected; nothing here is inferred client-side.</p>
    </div></section>}
    {!unavailable && entries.length === 0 && <EmptyState title="No Goal needs attention">No Goal currently carries a pending decision, settlement attention, active work or blocker.</EmptyState>}
    {entries.length > 0 && <ul className="attention-list">{entries.map((entry) => <li className="attention-card" key={entry.goal_id}>
      <Target className="attention-icon" size={20} aria-hidden="true" />
      <div className="attention-main">
        <div className="attention-context"><Link to="/goals/$goalId" params={{ goalId: entry.goal_id }}>{entry.title}</Link><StatusBadge status={entry.state} /><span className="item-key">{entry.goal_id}</span></div>
        <p className="text-small muted">Revision {entry.current_revision} · {entry.active_tasks ?? 0} active task(s) · {entry.nonterminal_tasks ?? 0} nonterminal</p>
        {(entry.blocker_reasons?.length ?? 0) > 0 && <p className="blocker">{entry.blocker_reasons?.join(", ")}</p>}
        {(entry.attention_reasons?.length ?? 0) > 0 && <p className="text-small muted">Settlement attention: {entry.attention_reasons?.join(", ")}</p>}
        {entry["ready_to_achieve?"] && <p className="goal-ready"><CircleCheck size={14} aria-hidden="true" />Ready to achieve on the server</p>}
      </div>
    </li>)}</ul>}
    {nextCursor && <button className="button" type="button" disabled={fetching} onClick={onLoadMore}>{fetching ? "Loading…" : "Load more Goal attention"}</button>}
  </section>;
}

function ProjectBlocked({ entries, unavailable }: {
  entries: ProjectBlockedEntry[];
  unavailable: { code: string; message: string } | null;
}) {
  return <section aria-labelledby="blocked-heading" className="goal-section">
    <div className="section-heading"><h2 id="blocked-heading">Blocked work <span className="count">{entries.length}</span></h2><span className="muted text-small">Project scope, complete</span></div>
    {unavailable && <section className="notice" role="status"><div><h2>The project read is unavailable</h2><p>{unavailable.message} <span className="text-small">Code: <code>{unavailable.code}</code></span></p></div></section>}
    {!unavailable && entries.length === 0 && <EmptyState title="No blocked work">There are no reported blockers in this project.</EmptyState>}
    {entries.length > 0 && <ul className="attention-list">{entries.map((entry) => <li className="attention-card" key={`${entry.project_id}/${entry.work_item_id}`}>
      <AlertCircle className="attention-icon" size={20} aria-hidden="true" />
      <div className="attention-main">
        <div className="attention-context"><Link to="/projects/$projectId" params={{ projectId: entry.project_id }}>{entry.project_name ?? entry.project_key ?? "Project"}</Link>{entry.work_item_key && <span className="item-key">{entry.work_item_key}</span>}{entry.project_status === "archived" && <StatusBadge status="archived" />}</div>
        <h3>{entry.title}</h3><p className="blocker">{entry.blocker?.trim() || "Blocking reason unknown"}</p>
      </div><StatusBadge status={entry.status} />
    </li>)}</ul>}
  </section>;
}

/**
 * Goal creation is an operator-authored contract posted to the accepted creation
 * command; the server validates every predicate and returns its own refusal. No
 * client rule decides whether a Goal may be created, and no capability is
 * fabricated when the project does not bind a repository resource.
 */
function NewGoalButton({ projectId }: { projectId: string | null }) {
  const { application } = useRouteContext({ from: "__root__" });
  const queryClient = useQueryClient();
  const workspace = useQuery({ ...workspaceOptions(application.api, projectId), enabled: projectId !== null });
  const session = useQuery(sessionOptions(application.api));
  const [open, setOpen] = useState(false);
  const [title, setTitle] = useState("");
  const [objective, setObjective] = useState("");
  const [notice, setNotice] = useState<string | null>(null);
  const project = workspace.data?.projects.find((candidate) => candidate.id === projectId);

  const create = useMutation({
    mutationFn: async () => {
      if (!project) throw new Error("Select a project first.");
      const csrf = session.data?.csrf_token;
      if (!csrf) throw new Error("The session token is unavailable; refresh the page.");
      const body = {
        schema_version: "symmetry.goal_create.v1",
        mutation_id: newMutationId(),
        title,
        initial_revision: {
          objective,
          non_goals: [],
          acceptance_contract: {
            schema_version: "symmetry.acceptance.v1",
            description: `Operator verifies ${title}`,
            predicates: [{ id: "operator", kind: "operator_acceptance" }],
          },
          authority_policy: {
            operator_required_for_scope_change: true,
            operator_required_for_completion: true,
            publication_allowed: false,
            allowed_actions: [],
          },
          execution_policy: {
            automatic_execution: false,
            max_parallel_tasks: 1,
            max_task_admissions: 2,
            max_run_attempts_per_task: 2,
            budget_limit_microusd: null,
            per_run_cost_limit_microusd: null,
            budget_mode: "soft",
            hard_cost_limit_required: false,
            allowed_runtime_ids: [],
            allowed_model_profiles: [],
            final_acceptance: "operator",
            allowed_actions: [],
            allowed_resource_ids: [],
          },
          context_manifest: { byte_budget: 32768, required_source_kinds: ["repository"], include_advisory_recall: false },
          reason: "Initial approved Goal contract from the attention entry point",
        },
      };
      return postGoalCreation(application.fetcher, csrf, project.id, body);
    },
    onSuccess: async () => {
      setNotice("The server created the Goal.");
      await queryClient.invalidateQueries({ queryKey: attentionReadKey() });
      setTitle("");
      setObjective("");
    },
  });
  const failure = create.error;

  return <div className="goal-create">
    <button className="button" type="button" disabled={!project} onClick={() => { create.reset(); setOpen(!open); setNotice(null); }} aria-expanded={open}><Plus size={16} aria-hidden="true" />New goal</button>
    {open && <form className="dialog-panel" onSubmit={(event) => { event.preventDefault(); create.mutate(); }}>
      <h2>Create a Goal</h2>
      <label className="field">Title<input value={title} onChange={(event) => { setTitle(event.currentTarget.value); }} required /></label>
      <label className="field">Objective<input value={objective} onChange={(event) => { setObjective(event.currentTarget.value); }} required /></label>
      <p className="text-small muted">The initial revision carries operator final acceptance, the repository context requirement and the soft budget mode. The server validates the contract and returns its own refusal.</p>
      {failure && <p className="form-error" role="alert">{failure instanceof MutationRefused ? `${failure.message} (${failure.code})` : errorMessage(failure)}</p>}
      {notice && !failure && <p role="status">{notice}</p>}
      <button className="button primary" type="submit" disabled={create.isPending || !project}>{create.isPending ? "Creating…" : "Create goal"}</button>
    </form>}
  </div>;
}
