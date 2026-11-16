import { useMutation, useQueries, useQuery, useQueryClient } from "@tanstack/react-query";
import { useParams, useRouteContext } from "@tanstack/react-router";
import { AlertCircle, CircleCheck, Inbox, ScrollText, ShieldAlert } from "lucide-react";
import { useState } from "react";
import { goalEventsOptions, goalOptions, sessionOptions, workspaceOptions } from "../../app/application";
import { EmptyState, ErrorState, LoadingState, PageHeading, RefreshButton, StatusBadge } from "../../components/workspace";
import { servedMember, servedText } from "./served-text";
import { errorMessage, postGoalCommand, type GoalDecision, type GoalEvent, type GoalGraph, type GoalProjection, type GoalWorkItem, type Workspace } from "../../services/api";
import { canonicalDigest, commandEnvelope, MutationRefused, newMutationId, type CommandKind } from "../../services/commands";

type ActionPayload = Record<string, unknown>;

const actionLabels: Record<string, string> = {
  request_plan: "Request a plan",
  accept_plan: "Approve the plan",
  activate: "Activate",
  pause: "Pause",
  resume: "Resume",
  cancel: "Cancel",
  amend: "Amend",
  resolve_decision: "Resolve",
  achieve: "Achieve",
};

function actionLabel(kind: string): string {
  return actionLabels[kind] ?? `Unknown action: ${kind}`;
}

function formatMicroUsd(value: string | null | undefined): string {
  if (value === null || value === undefined) return "none recorded";
  const micro = Number(value);
  if (!Number.isFinite(micro)) return `unknown (${String(value)})`;
  return `$${(micro / 1_000_000).toFixed(4)}`;
}

export function GoalDetailPage() {
  const { application } = useRouteContext({ from: "__root__" });
  const { goalId } = useParams({ from: "/goals/$goalId" });
  const query = useQuery(goalOptions(application.api, goalId));
  const workspace = useQuery(workspaceOptions(application.api, query.data?.project_id ?? null));
  const session = useQuery(sessionOptions(application.api));
  const queryClient = useQueryClient();
  const [refusal, setRefusal] = useState<MutationRefused | null>(null);
  const [notice, setNotice] = useState<string | null>(null);

  const command = useMutation({
    mutationFn: async ({ kind, payload }: { kind: CommandKind; payload: ActionPayload }) => {
      const goal = query.data;
      if (!goal) throw new Error("The goal is not loaded.");
      const csrf = session.data?.csrf_token;
      if (!csrf) throw new Error("The session token is unavailable; refresh the page.");
      const envelope = commandEnvelope(kind, payload, { version: goal.version, current_revision: goal.current_revision });
      return postGoalCommand(application.fetcher, csrf, goalId, envelope);
    },
    onSuccess: async (_body, variables) => {
      setRefusal(null);
      setNotice(`${actionLabel(variables.kind)} accepted by the server.`);
      await queryClient.invalidateQueries({ queryKey: ["goal", { goalId }] });
      await queryClient.invalidateQueries({ queryKey: ["goal-events", { goalId }] });
      await queryClient.invalidateQueries({ queryKey: ["attention"] });
    },
    onError: (error: unknown) => {
      setNotice(null);
      setRefusal(error instanceof MutationRefused ? error : null);
      if (!(error instanceof MutationRefused)) setNotice(errorMessage(error));
    },
  });

  /**
   * Plan approval is two accepted commands: resolving the served plan decision
   * with `accept`, then admitting the plan. The second command must carry the
   * versions the first one produced, so this reads the goal fresh between them
   * and never reuses a stale version.
   */
  const approvePlan = useMutation({
    mutationFn: async ({ decision, proposal, proposalHash }: { decision: GoalDecision; proposal: unknown; proposalHash: string }) => {
      const csrf = session.data?.csrf_token;
      if (!csrf) throw new Error("The session token is unavailable; refresh the page.");
      const goal = query.data;
      if (!goal) throw new Error("The goal is not loaded.");

      const resolved = await postGoalCommand(
        application.fetcher,
        csrf,
        goalId,
        commandEnvelope("resolve_decision", { decision_id: decision.id, expected_decision_version: decision.version ?? null, option_id: "accept", comment: "Accepted from the Goal surface." }, { version: goal.version, current_revision: goal.current_revision }),
      );
      if (!resolved) throw new Error("The server returned no receipt for the decision resolution.");

      // Read the goal back so the plan admission carries the served versions.
      const current = await application.api.goal(goalId, new AbortController().signal);
      return postGoalCommand(
        application.fetcher,
        csrf,
        goalId,
        commandEnvelope("accept_plan", { proposal, proposal_hash: proposalHash, decision_id: decision.id }, { version: current.version, current_revision: current.current_revision }),
      );
    },
    onSuccess: async () => {
      setRefusal(null);
      setNotice("The server admitted the plan.");
      await queryClient.invalidateQueries({ queryKey: ["goal", { goalId }] });
      await queryClient.invalidateQueries({ queryKey: ["goal-events", { goalId }] });
      await queryClient.invalidateQueries({ queryKey: ["attention"] });
    },
    onError: (error: unknown) => {
      setNotice(null);
      setRefusal(error instanceof MutationRefused ? error : null);
      if (!(error instanceof MutationRefused)) setNotice(errorMessage(error));
    },
  });

  const refresh = () => {
    setRefusal(null);
    setNotice(null);
    if (!query.isFetching) void query.refetch();
  };

  if (query.isPending) return <main className="workspace-content"><PageHeading title="Goal" description="Loading the durable Goal projection…"><RefreshButton fetching refresh={refresh} /></PageHeading><LoadingState label="Loading goal…" /></main>;
  if (query.isError && !query.data) return <main className="workspace-content"><PageHeading title="Goal" description="This Goal could not be read."><RefreshButton fetching={query.isFetching} refresh={refresh} /></PageHeading><ErrorState error={query.error} retry={refresh} /></main>;

  const goal = query.data;
  if (!goal) return null;
  const busy = command.isPending;

  return <main className="workspace-content">
    <PageHeading title={goal.title} description={`${goal.project_key ?? "Project"} · revision ${goal.current_revision} · version ${goal.version}`}>
      <RefreshButton fetching={query.isFetching} refresh={refresh} />
    </PageHeading>
    {query.isError && <ErrorState error={query.error} retry={refresh} stale />}
    {refusal && <RefusalNotice refusal={refusal} refresh={refresh} />}
    {notice && <section className="notice" role="status"><CircleCheck size={18} aria-hidden="true" /><div><p>{notice}</p></div></section>}
    <Objective goal={goal} />
    <AcceptedProgress goal={goal} />
    <Blockers goal={goal} />
    <Decisions goal={goal} busy={busy || approvePlan.isPending} onApprovePlan={approvePlan.mutate} />
    <NextAction goal={goal} busy={busy} onCommand={command.mutate} resources={workspace.data?.projects.find((project) => project.id === goal.project_id)?.resources ?? []} />
    <Budget goal={goal} />
    <WaitingExternal goal={goal} />
    <DependencyGraph graph={goal.graph} />
    <Receipts goalId={goalId} />
    <p className="sr-only" role="status">{busy ? "Sending the Goal command" : ""}</p>
  </main>;
}

function RefusalNotice({ refusal, refresh }: { refusal: MutationRefused; refresh: () => void }) {
  const stale = refusal.stale;
  return <section className={stale ? "notice notice-conflict" : "notice notice-error"} role="alert">
    <ShieldAlert size={20} aria-hidden="true" />
    <div>
      <h2>{stale ? "The goal changed since this page loaded" : "The server refused this action"}</h2>
      <p>{refusal.message}</p>
      {stale && <p className="text-small">Server state: version {servedText(refusal.details["current_version"], "unknown")}, revision {servedText(refusal.details["current_revision"], "unknown")}. Nothing was overwritten.</p>}
      <p className="text-small">Code: <code>{refusal.code}</code></p>
      <button className="button" type="button" onClick={refresh}>Reload the current state</button>
    </div>
  </section>;
}

function Objective({ goal }: { goal: GoalProjection }) {
  const revision = goal.revision;
  if (!revision) return <section className="goal-section" aria-labelledby="objective-heading"><div className="section-heading"><h2 id="objective-heading">Objective</h2></div><p className="muted">No approved revision is recorded for this goal.</p></section>;
  return <section className="goal-section" aria-labelledby="objective-heading">
    <div className="section-heading"><h2 id="objective-heading">Objective</h2><span className="muted text-small">Revision {revision.revision} · {revision.inserted_at ?? "recorded time unknown"}</span></div>
    <p className="goal-objective">{revision.objective ?? "No objective recorded"}</p>
    {(revision.non_goals?.length ?? 0) > 0 && <div className="goal-facts"><span className="fact-label">Non-goals</span><ul>{revision.non_goals?.map((entry) => <li key={entry}>{entry}</li>)}</ul></div>}
    <div className="goal-facts">
      <span className="fact-label">Acceptance predicates</span>
      <span>{acceptancePredicates(revision.acceptance_contract)}</span>
    </div>
    <div className="goal-facts"><span className="fact-label">Final acceptance</span><span>{servedMember(revision.execution_policy, "final_acceptance", "not recorded")}</span></div>
    <div className="goal-facts goal-lifecycle"><span className="fact-label">Lifecycle</span><StatusBadge status={goal.state} /></div>

  </section>;
}

function acceptancePredicates(contract: unknown): string {
  if (!contract || typeof contract !== "object") return "not recorded";
  const predicates = (contract as Record<string, unknown>)["predicates"];
  if (!Array.isArray(predicates) || predicates.length === 0) return "none recorded";
  return predicates.map((predicate) => {
    if (!predicate || typeof predicate !== "object") return String(predicate);
    const kind = (predicate as Record<string, unknown>)["kind"];
    return typeof kind === "string" ? kind : "unrecognised predicate";
  }).join(", ");
}

function AcceptedProgress({ goal }: { goal: GoalProjection }) {
  const completion = goal.completion;
  return <section className="goal-section" aria-labelledby="progress-heading">
    <div className="section-heading"><h2 id="progress-heading">Accepted progress</h2><span className="count">{completion.accepted_required_items} of {completion.required_items} required items accepted</span></div>
    <ul className="work-list" aria-label="Goal work items">
      {goal.work_items.map((item) => <li className="work-row" key={item.id}><RowBody item={item} /></li>)}
    </ul>
    {(goal.accepted_outcomes?.length ?? 0) > 0 && <div className="goal-table-wrap">
      <h3>Accepted outcomes</h3>
      <table className="goal-table"><caption className="sr-only">Accepted Goal outcomes</caption>
        <thead><tr><th scope="col">Work item</th><th scope="col">Subject</th><th scope="col">Evidence</th><th scope="col">Accepted</th></tr></thead>
        <tbody>{(goal.accepted_outcomes ?? []).map((outcome) => <tr key={outcome.id}>
          <td>{itemTitle(goal.work_items, String(outcome.work_item_id ?? ""))}</td>
          <td className="mono">{outcome.subject_hash ?? "not recorded"}</td>
          <td>{outcome.evidence_ids?.length ? `${outcome.evidence_ids.length} record(s)` : "none recorded"}</td>
          <td>{outcome.inserted_at ?? "not recorded"}</td>
        </tr>)}</tbody>
      </table>
    </div>}
    {completion["ready?"] === true && <p className="goal-ready"><CircleCheck size={16} aria-hidden="true" />The server reports this goal ready to achieve; the achieve action below carries its expected versions.</p>}
  </section>;
}

function RowBody({ item }: { item: GoalWorkItem }) {
  return <>
    <div className="goal-row-head">
      <span className="item-key">{item.number === null || item.number === undefined ? "item" : `#${item.number}`}</span>
      <span className="goal-row-title">{item.title}</span>
      <StatusBadge status={item.status} />
      {item["required?"] === false && <span className="status-badge">optional</span>}
      {item["integration?"] === true && <span className="status-badge">integration</span>}
      <span className={item["accepted?"] ? "status-badge status-complete" : "status-badge"}>{item["accepted?"] ? "accepted" : "not accepted"}</span>
    </div>
    <p className="text-small muted">{item["accepted?"] ? `Accepted outcome ${item.accepted_outcome?.subject_hash ?? "recorded"}` : "No accepted outcome bound to this item"}{item.blockers && item.blockers.length > 0 ? ` · blocked: ${item.blockers.join(", ")}` : ""}</p>
    {item.execution && <p className="text-small muted">Execution: {servedMember(item.execution, "state", "unknown state")}</p>}
  </>;
}

function itemTitle(items: GoalWorkItem[], workItemId: string): string {
  return items.find((item) => item.id === workItemId)?.title ?? `Work item ${workItemId || "unknown"}`;
}

function Blockers({ goal }: { goal: GoalProjection }) {
  return <section className="goal-section" aria-labelledby="blockers-heading">
    <div className="section-heading"><h2 id="blockers-heading">Blockers <span className="count">{goal.blockers.length}</span></h2><span className="muted text-small">Server-derived</span></div>
    {goal.blockers.length === 0
      ? <p className="muted">No blocker is recorded for this goal.</p>
      : <ul className="attention-list">{goal.blockers.map((blocker) => <li className="attention-card" key={blocker.reason}>
        <AlertCircle className="attention-icon" size={20} aria-hidden="true" />
        <div className="attention-main"><h3>{blocker.reason}</h3><p className="blocker">{blocker.count} item(s): {blocker.ids?.join(", ") || "no identifier recorded"}</p>
          {blocker.details?.map((detail, index) => <p className="text-small muted mono" key={index}>{JSON.stringify(detail) ?? ""}</p>)}
        </div>
      </li>)}</ul>}
    {(goal.settlement_attention?.length ?? 0) > 0 && <ul className="attention-list">{(goal.settlement_attention ?? []).map((entry) => <li className="attention-card" key={entry.event_id ?? entry.task_id ?? entry.settlement}>
      <ScrollText className="attention-icon" size={20} aria-hidden="true" />
      <div className="attention-main"><h3>Settlement: {entry.settlement}</h3>
        <p className="text-small muted">Task {entry.task_id ?? "not recorded"} · revision {entry.goal_revision ?? "not recorded"}{entry.reason ? ` · ${entry.reason}` : ""}</p>
        {entry.proposed_next_action && <p className="proposal">Recommendation (advisory): {entry.proposed_next_action}{entry.proposed_next_action_status ? ` — ${entry.proposed_next_action_status}` : ""}</p>}
      </div>
    </li>)}</ul>}
  </section>;
}

function Decisions({ goal, busy, onApprovePlan }: { goal: GoalProjection; busy: boolean; onApprovePlan: (request: { decision: GoalDecision; proposal: unknown; proposalHash: string }) => void }) {
  if (goal.decisions.length === 0) return <section className="goal-section" aria-labelledby="decisions-heading"><div className="section-heading"><h2 id="decisions-heading">Decisions</h2></div><p className="muted">No decision is recorded for this goal.</p></section>;
  return <section className="goal-section" aria-labelledby="decisions-heading">
    <div className="section-heading"><h2 id="decisions-heading">Decisions <span className="count">{goal.decisions.length}</span></h2></div>
    <ul className="attention-list">{goal.decisions.map((decision) => <li className="attention-card" key={decision.id}>
      <div className="attention-main">
        <div className="goal-row-head"><h3>{decision.kind} decision</h3><StatusBadge status={decision.state} /><span className="item-key">{decision.id}</span></div>
        <p>{decision.question ?? "No question recorded"}</p>
        <p className="text-small muted">Revision {decision.goal_revision ?? "not recorded"} · version {servedText(decision.version, "not served")}</p>
        <ul className="decision-options">{decision.options?.map((option, index) => {
          const row = option as Record<string, unknown>;
          const recommended = row["recommended"] === true;
          return <li key={servedText(row["id"], String(index))}>
            <span className="option-id">{servedText(row["id"], "option")}</span>
            <span>{servedText(row["label"], "")}</span>
            {recommended && <span className="recommendation">Recommendation (advisory): {servedText(row["consequence"] ?? row["consequences"], "")}</span>}
            {!recommended && row["consequence"] !== undefined && <span className="muted text-small"> {servedText(row["consequence"], "")}</span>}
          </li>;
        })}</ul>
        {/* An open plan decision is itself the surface's authority to approve:
            the server keeps `request_plan`/`accept_plan` out of `allowed_actions`
            while a plan decision is pending acceptance. */}
        {decision["open?"] === true && decision.kind === "plan" && <PlanApproval decision={decision} busy={busy} onApprove={onApprovePlan} />}
        {decision["open?"] === true && decision.kind !== "plan" && <p className="text-small muted">This decision type has no surface action; the server still lists the action it permits, if any.</p>}
        {decision["open?"] !== true && <p className="text-small muted">Resolved: {decision.resolution ? JSON.stringify(decision.resolution) : "no resolution recorded"}</p>}
      </div>
    </li>)}</ul>
  </section>;
}

/**
 * Plan approval follows the accepted two-step contract: the decision is resolved
 * with `accept`, then `accept_plan` presents the same proposal with the canonical
 * digest this surface computes from the served proposal; the server compares it
 * with its own hash and refuses a mismatch.
 */
function PlanApproval({ decision, busy, onApprove }: { decision: GoalDecision; busy: boolean; onApprove: (request: { decision: GoalDecision; proposal: unknown; proposalHash: string }) => void }) {
  const [proposalText, setProposalText] = useState("");
  const [digest, setDigest] = useState<string | null>(null);
  const [parseError, setParseError] = useState<string | null>(null);

  const approve = async () => {
    setParseError(null);
    let proposal: unknown;
    try {
      proposal = JSON.parse(proposalText);
    } catch {
      setParseError("The proposal must be the JSON document the plan decision carries.");
      return;
    }
    const hash = await canonicalDigest(proposal);
    setDigest(hash);
    onApprove({ decision, proposal, proposalHash: hash });
  };

  return <div className="plan-approval">
    <label className="fact-label" htmlFor={`plan-${decision.id}`}>Plan proposal</label>
    <textarea id={`plan-${decision.id}`} className="plan-input" value={proposalText} rows={4} onChange={(event) => setProposalText(event.currentTarget.value)} placeholder="Paste the proposal JSON exactly as the plan decision carries it" />
    <p className="text-small muted">The proposal is hashed here exactly as the server does and sent back unchanged; the server validates the hash and refuses a mismatch. A proposal the server does not already hold cannot be approved from this page.</p>
    {parseError && <p className="form-error" role="alert">{parseError}</p>}
    {digest && <p className="text-small mono">canonical digest {digest}</p>}
    <button className="button" type="button" disabled={busy || proposalText.trim() === ""} onClick={() => { void approve(); }}>Resolve the decision with accept</button>
    <p className="text-small muted">Resolving is recorded first and the plan admission follows with the versions the server then serves; a refusal is shown as it arrives.</p>
  </div>;
}

function NextAction({ goal, busy, onCommand, resources }: { goal: GoalProjection; busy: boolean; onCommand: (request: { kind: CommandKind; payload: ActionPayload }) => void; resources: NonNullable<Workspace["projects"][number]["resources"]> }) {
  const [reason, setReason] = useState("");
  const [commit, setCommit] = useState("");
  const [treeDigest, setTreeDigest] = useState("");
  const [resourceId, setResourceId] = useState("");
  const [modelProfile, setModelProfile] = useState("");
  const [objective, setObjective] = useState(goal.revision?.objective ?? "");
  const [openAction, setOpenAction] = useState<string | null>(null);
  const [mutationIds, setMutationIds] = useState<Record<string, string>>({});

  const allowed = goal.allowed_actions;
  const repositories = resources.filter((resource) => resource.kind === "repository");
  const integrationItem = goal.work_items.find((item) => item["integration?"] === true);

  const identity = (kind: string): string => {
    const existing = mutationIds[kind];
    if (existing) return existing;
    const fresh = newMutationId();
    setMutationIds({ ...mutationIds, [kind]: fresh });
    return fresh;
  };

  const send = (kind: CommandKind, payload: ActionPayload) => {
    onCommand({ kind, payload });
    setMutationIds((current) => { const next = { ...current }; delete next[kind]; return next; });
    setOpenAction(null);
  };

  if (allowed.length === 0) return <section className="goal-section" aria-labelledby="next-action-heading"><div className="section-heading"><h2 id="next-action-heading">Next authorized action</h2></div><p className="muted">The server currently authorizes no action on this goal. An open plan decision is approved from its own card above.</p></section>;

  return <section className="goal-section" aria-labelledby="next-action-heading">
    <div className="section-heading"><h2 id="next-action-heading">Next authorized action</h2><span className="muted text-small">Version {goal.version} · revision {goal.current_revision} · mutation identity per action</span></div>
    <ul className="goal-actions">{allowed.map((kind) => <li key={kind}>
      {/* The action list is exactly what the server allows; no client rule adds to it. */}
      <button className="button action-button" type="button" disabled={busy} aria-expanded={openAction === kind} onClick={() => setOpenAction(openAction === kind ? null : kind)}>{actionLabel(kind)}</button>
      {openAction === kind && <form className="action-form" onSubmit={(event) => event.preventDefault()}>
        <p className="text-small muted">Carries expected_version {goal.version}, expected_revision {goal.current_revision} and mutation identity {identity(kind)}.</p>
        {(kind === "pause" || kind === "resume" || kind === "cancel") && <label className="field">Reason<input value={reason} onChange={(event) => setReason(event.currentTarget.value)} required /></label>}
        {kind === "activate" && <p>The current revision {goal.current_revision} is the approved revision this command presents.</p>}
        {kind === "request_plan" && <>
          <label className="field">Repository resource<select value={resourceId} onChange={(event) => setResourceId(event.currentTarget.value)} required>
            <option value="">Select a repository</option>
            {repositories.map((resource, index) => <option key={resource.id ?? index} value={resource.id ?? ""}>{servedText(resource.name ?? resource.id, "repository")}</option>)}
          </select></label>
          <label className="field">Subject commit<input value={commit} onChange={(event) => setCommit(event.currentTarget.value)} placeholder="40-character commit" required /></label>
          <label className="field">Tree digest<input value={treeDigest} onChange={(event) => setTreeDigest(event.currentTarget.value)} placeholder="sha256:…" required /></label>
          <label className="field">Model profile<input value={modelProfile} onChange={(event) => setModelProfile(event.currentTarget.value)} required /></label>
        </>}
        {kind === "amend" && <>
          <label className="field">Objective<input value={objective} onChange={(event) => setObjective(event.currentTarget.value)} required /></label>
          <label className="field">Reason<input value={reason} onChange={(event) => setReason(event.currentTarget.value)} required /></label>
          <p className="text-small muted">An amendment opens a new revision and pauses the goal; the accepted authority and execution policy of the current revision are carried unchanged into the new revision.</p>
        </>}
        {kind === "achieve" && <AchieveForm goal={goal} integrationItem={integrationItem} />}
        {kind === "accept_plan" && <p>Approve the open plan decision above first; the server admits the plan only from a resolved plan decision.</p>}
        {kind === "resolve_decision" && <p>Decisions are resolved from their own card above; the server validates the decision version you present.</p>}
        {!actionInputs(kind) && kind !== "activate" && kind !== "achieve" && <p className="text-small muted">This surface has no command form for this action; the server still lists it as allowed.</p>}
        <button className="button primary" type="submit" disabled={busy} onClick={() => {
          if (!isCommandKind(kind)) return;
          const payload = actionPayload(kind, goal, { reason, commit, treeDigest, resourceId, modelProfile, objective, evidenceSubject: currentSubject(goal, integrationItem) });
          if (payload) send(kind, payload);
        }}>Send {actionLabel(kind)}</button>
      </form>}
    </li>)}</ul>
  </section>;
}

const commandKinds: readonly CommandKind[] = ["request_plan", "accept_plan", "activate", "pause", "resume", "cancel", "amend", "resolve_decision", "achieve"];

// The served action list is free-form strings; only a recognised command kind is sent.
function isCommandKind(kind: string): kind is CommandKind {
  return (commandKinds as readonly string[]).includes(kind);
}

function actionInputs(kind: string): boolean {
  return kind === "pause" || kind === "resume" || kind === "cancel" || kind === "request_plan" || kind === "amend";
}

function currentSubject(goal: GoalProjection, integrationItem: GoalWorkItem | undefined) {
  const outcome = integrationItem?.accepted_outcome ?? (goal.accepted_outcomes ?? [])[0];
  return outcome?.candidate_subject ?? null;
}

function actionPayload(
  kind: string,
  goal: GoalProjection,
  input: { reason: string; commit: string; treeDigest: string; resourceId: string; modelProfile: string; objective: string; evidenceSubject: unknown },
): ActionPayload | null {
  switch (kind) {
    case "activate":
      return { approved_revision: goal.current_revision };
    case "pause":
    case "resume":
    case "cancel":
      return { reason: input.reason };
    case "amend": {
      const revision = goal.revision;
      if (!revision) return null;
      return {
        reason: input.reason,
        revision_contract: {
          objective: input.objective,
          non_goals: revision.non_goals ?? [],
          acceptance_contract: revision.acceptance_contract ?? {},
          authority_policy: revision.authority_policy ?? {},
          execution_policy: revision.execution_policy ?? {},
          context_manifest: revision.context_manifest ?? {},
        },
      };
    }
    case "request_plan":
      return {
        model_profile: input.modelProfile,
        repository_resource_id: input.resourceId,
        subject: { resource_id: input.resourceId, commit: input.commit, tree_digest: input.treeDigest },
        session_mode: "fresh",
        requested_session_id: null,
      };
    case "achieve": {
      if (!input.evidenceSubject) return null;
      const evidenceIds = [...new Set((goal.accepted_outcomes ?? []).flatMap((outcome) => outcome.evidence_ids ?? []))];
      const integrationWorkItemId = goal.work_items.find((item) => item["integration?"] === true)?.id;
      return {
        subject: input.evidenceSubject,
        // Exactly-one-required-item contracts bind their integration item
        // server-side; the member is sent only when a designation is served.
        ...(integrationWorkItemId === undefined ? {} : { integration_work_item_id: integrationWorkItemId }),
        evidence_ids: evidenceIds,
        decision_id: null,
      };
    }
    default:
      return null;
  }
}

function AchieveForm({ goal, integrationItem }: { goal: GoalProjection; integrationItem: GoalWorkItem | undefined }) {
  const subject = currentSubject(goal, integrationItem);
  const evidenceIds = [...new Set((goal.accepted_outcomes ?? []).flatMap((outcome) => outcome.evidence_ids ?? []))];
  return <div>
    <p>The achieve command presents the accepted integration subject and every accepted evidence record: <span className="mono">{subject ? JSON.stringify(subject) : "no accepted subject is served"}</span></p>
    <p className="text-small muted">Evidence records: {evidenceIds.length === 0 ? "none" : evidenceIds.join(", ")}</p>
    <p className="text-small muted">The server validates the subject, the required outcomes and the exactly-one-required-item binding; a refusal is shown as it arrives.</p>
    {integrationItem && <p className="text-small muted">Integration item: {integrationItem.title}</p>}
    {!subject && <p className="form-error" role="alert">No accepted subject is served yet; the server will refuse an achieve built from nothing.</p>}
  </div>;
}

function Budget({ goal }: { goal: GoalProjection }) {
  const accounting = goal.accounting;
  return <section className="goal-section" aria-labelledby="budget-heading">
    <div className="section-heading"><h2 id="budget-heading">Budget</h2><span className="muted text-small">Reservation and consumption as served</span></div>
    <dl className="fact-grid">
      <div><dt>Held reservation</dt><dd>{formatMicroUsd(accounting.held_microusd)}</dd></div>
      <div><dt>Known consumption</dt><dd>{formatMicroUsd(accounting.known_usage_cost_microusd)}</dd></div>
      <div><dt>Settled consumption</dt><dd>{accounting.usage_cost_unknown ? "unknown (contains unknown usage)" : formatMicroUsd(accounting.usage_cost_microusd)}</dd></div>
      <div><dt>Unknown usage records</dt><dd>{accounting.unknown_usage_count}</dd></div>
      <div><dt>Unknown reservations</dt><dd>{accounting.unknown_reservation_count}</dd></div>
      <div><dt>Reservations held / settled / released</dt><dd>{accounting.settled_reservation_count} settled · {accounting.released_reservation_count} released</dd></div>
    </dl>
    {accounting.usage_cost_unknown && <p className="text-small muted">Unknown consumption is never shown as zero.</p>}
  </section>;
}

function WaitingExternal({ goal }: { goal: GoalProjection }) {
  return <section className="goal-section" aria-labelledby="waiting-heading">
    <div className="section-heading"><h2 id="waiting-heading">Waiting on external boundaries <span className="count">{(goal.external_waits?.length ?? 0)}</span></h2></div>
    {(goal.external_waits?.length ?? 0) === 0
      ? <p className="muted">The current revision has no external wait.</p>
      : <ul className="work-list">{(goal.external_waits ?? []).map((wait) => <li className="work-row" key={wait.id}>
        <div className="goal-row-head"><StatusBadge status={wait.state} /><span className="goal-row-title">{wait.external_ref ?? wait.resource_id ?? wait.id}</span></div>
        <p className="text-small muted">{wait.reason ? `${wait.reason} · ` : ""}next check {wait.next_check_at ?? "not scheduled"} · check {wait.check_seq ?? "unknown"}</p>
      </li>)}</ul>}
  </section>;
}

/**
 * The dependency projection is read-only: it renders the served nodes and edges
 * and offers no editing affordance. A dependency change remains a decision-gated
 * server operation.
 */
function DependencyGraph({ graph }: { graph: GoalGraph }) {
  return <section className="goal-section" aria-labelledby="graph-heading">
    <div className="section-heading"><h2 id="graph-heading">Dependencies</h2><span className="muted text-small">Read-only projection</span></div>
    {graph.nodes.length === 0
      ? <p className="muted">No admitted work item carries a dependency in the current revision.</p>
      : <>
        {graph.cycle_detected === true && <p className="form-error" role="alert">The server reports a dependency cycle in the current revision.</p>}
        <ul className="graph-nodes">{graph.nodes.map((node) => <li key={node.id}>
          <span className="item-key">{node.number === null || node.number === undefined ? "item" : `#${node.number}`}</span>
          <span className="goal-row-title">{node.title}</span>
          <span className={node["accepted?"] ? "status-badge status-complete" : "status-badge"}>{node["accepted?"] ? "accepted" : "not accepted"}</span>
          {node.blockers?.map((blocker) => <span className="status-badge warning" key={blocker}>{blocker}</span>)}
        </li>)}
        </ul>
        <ul className="graph-edges" aria-label="Dependency edges">{graph.edges.map((edge) => <li key={edge.id}>
          <span>{nodeLabel(graph, edge.from)}</span>
          <span className="edge-arrow" aria-hidden="true">→</span>
          <span>{nodeLabel(graph, edge.to)}</span>
          <span className={edge.satisfied ? "status-badge status-complete" : "status-badge warning"}>{edge.satisfied ? "satisfied" : edge.blocker ?? "waiting"}</span>
        </li>)}</ul>
      </>}
  </section>;
}

function nodeLabel(graph: GoalGraph, nodeId: string): string {
  const node = graph.nodes.find((candidate) => candidate.id === nodeId);
  return node ? `${node.number === null || node.number === undefined ? "" : `#${node.number} `}${node.title}` : nodeId;
}

function Receipts({ goalId }: { goalId: string }) {
  const { application } = useRouteContext({ from: "__root__" });
  // Every page is its own key over the served `after` boundary; the reader's
  // loaded boundaries are the rendered identity, so a refresh re-reads the same
  // pages instead of silently dropping the receipts already shown.
  const [boundaries, setBoundaries] = useState<number[]>([0]);
  const pages = useQueries({
    queries: boundaries.map((after) => goalEventsOptions(application.api, goalId, after)),
  });
  const entries: GoalEvent[] = pages.flatMap((page) => page.data?.entries ?? []);
  const latest = pages[pages.length - 1];
  const nextAfter = latest?.data?.next_after ?? null;
  const firstPageError = pages[0]?.error;
  const firstPagePending = pages[0]?.isPending ?? false;
  const fetching = pages.some((page) => page.isFetching);

  return <section className="goal-section" aria-labelledby="receipts-heading">
    <div className="section-heading"><h2 id="receipts-heading">Receipts</h2><span className="muted text-small">{entries.length} loaded</span></div>
    {firstPagePending && entries.length === 0 && <LoadingState label="Loading receipts…" />}
    {firstPageError !== undefined && <ErrorState error={firstPageError} retry={() => { void pages[0]?.refetch(); }} stale={entries.length > 0} />}
    {entries.length === 0 && !firstPagePending && firstPageError === undefined && <EmptyState title="No receipts">This goal has no recorded audit event.</EmptyState>}
    {entries.length > 0 && <ul className="receipt-list">{entries.map((entry: GoalEvent) => <li key={entry.sequence}>
      <span className="item-key">#{entry.sequence}</span>
      <span className="goal-row-title">{entry.kind}</span>
      <span className="muted text-small">{entry.actor_ref ?? "actor not recorded"} · revision {entry.revision ?? "not recorded"} · {entry.inserted_at ?? "time not recorded"}</span>
    </li>)}</ul>}
    {nextAfter !== null && nextAfter > 0 && <button className="button" type="button" disabled={fetching} onClick={() => setBoundaries([...boundaries, nextAfter])}>{fetching ? "Loading…" : "Load older receipts"}</button>}
    {nextAfter === null && entries.length > 0 && <p className="text-small muted"><Inbox size={14} aria-hidden="true" /> All recorded receipts are loaded.</p>}
    {fetching && entries.length > 0 && <p className="sr-only" role="status">Refreshing receipts</p>}
  </section>;
}
