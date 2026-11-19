/**
 * `/portal/chat`: the scoped engineering conversation.
 *
 * The URL carries the scope the operator asked for; the conversation read's own
 * `scope`/`project_id`/`run_id` answer owns the interpretation and every
 * rendered run signal. Drafts, intents, start-work selections, send state and
 * the retained history pages live in one per-scope store, so switching scope
 * restores that scope's values and a background update for one scope never
 * writes another. Messages render as inert text; the only anchors are served
 * values whose scheme the one safe-URL helper admits, and raw execution data is
 * never a source.
 */
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { getRouteApi, Link, useNavigate, useRouteContext, useRouter } from "@tanstack/react-router";
import { useEffect, useLayoutEffect, useRef, type FormEvent } from "react";
import {
  chatConversationOptions,
  chatMessageMutation,
  chatOlderPageOptions,
  workspaceOptions,
  type ChatScopeKey,
} from "../../app/application";
import { useSessionToken } from "../../app/session";
import { EmptyState, ErrorState, LoadingState, PageHeading, RefreshButton } from "../../components/workspace";
import {
  Conflict,
  Forbidden,
  ServerRefusal,
  Unsupported,
  Validation,
  errorMessage,
  type ChatConversationMessage,
  type ChatConversationRun,
  type ChatMessageRequest,
  type ChatScope,
  type WorkItem,
} from "../../services/api";
import {
  applicableChatIntents,
  chatDecisionPacket,
  chatEvidenceRows,
  chatFailureView,
  chatHistoryView,
  chatIntentLabel,
  chatRunLinks,
  chatReceiptView,
  chatScopeContext,
  chatSendStatusText,
  guidanceDeliveryLabel,
  pauseUnsupported,
  pauseUnsupportedLabel,
  chatStateLabel,
  type ChatDecisionOption,
  type ChatReceiptView,
  type ChatRunSignals,
} from "./presentation";
import { chatScopeKey, retainOlderPage, retainServedPage, useChatScopeStore, type ChatIntent } from "./scopes";

const route = getRouteApi("/chat");

/** The run's execution as the directory's work projections carry it. */
type DirectoryExecution = NonNullable<WorkItem["execution"]>;

/** The server's own wording for the guidance byte bound; the same bound it enforces. */
const guidanceByteLimit = 32_768;

/** A run the served work projections expose for the picker. */
interface ServedRun {
  readonly runId: string;
  readonly item: WorkItem;
  readonly execution: DirectoryExecution;
}

export function ChatPage() {
  const { application } = useRouteContext({ from: "__root__" });
  const queryClient = useQueryClient();
  const csrf = useSessionToken();
  const navigate = useNavigate();
  const router = useRouter();
  const search = route.useSearch();
  const urlScope: ChatScope = search.scope ?? "workspace";
  const urlProjectId = search.projectId ?? null;
  const urlRunId = search.runId ?? null;
  const identity: ChatScopeKey = { scope: urlScope, projectId: urlProjectId, runId: urlRunId };
  const key = chatScopeKey(urlScope, urlProjectId, urlRunId);
  // A scoped read needs its own identity: a deep link that asks for the project
  // or run scope without one keeps the picker empty instead of guessing.
  const scopeComplete = urlScope === "workspace" || (urlScope === "project" ? urlProjectId !== null : urlRunId !== null);

  const {
    stateOf, update, updateWith,
    scrollOffsetOf, recordScrollOffset, scrollRestoreOf, markScrollRestore, settleScrollRestore,
  } = useChatScopeStore();
  const state = stateOf(key);

  const conversation = useQuery({ ...chatConversationOptions(application.api, identity), enabled: scopeComplete });
  // The directory the shell already loads: the projects, their work items' run
  // bindings and the runtime rows this surface reads run signals from.
  const directory = useQuery(workspaceOptions(application.api, urlProjectId));
  const directoryProjects = directory.data?.projects ?? [];
  const runRows: readonly ServedRun[] = directoryProjects.flatMap((project) =>
    project.work_items.flatMap((item): ServedRun[] => {
      const execution = item.execution ?? null;
      const runId = execution?.run_id ?? null;
      return execution === null || runId === null ? [] : [{ runId, item, execution }];
    }),
  );

  // The read's own run projection is the authority for a run conversation; the
  // directory row covers the window before it answers, and a run the served
  // list does not carry is named by its id rather than by another run.
  const conversationRun: ChatConversationRun | null = conversation.data !== undefined && conversation.data.scope === "run" ? conversation.data.runs[0] ?? null : null;
  const directoryRun = urlRunId === null ? null : runRows.find((row) => row.runId === urlRunId) ?? null;
  const servedRunItem = conversationRun?.work_item ?? directoryRun?.item ?? null;
  const servedRunExecution: DirectoryExecution | null = conversationRun?.work_item.execution ?? directoryRun?.execution ?? null;

  const waiting = servedRunExecution?.waiting ?? null;
  const decision = chatDecisionPacket(waiting);
  const failure = chatFailureView(servedRunExecution?.state, servedRunExecution?.failure, conversationRun?.outcome.failure);
  const runtimeId = servedRunExecution?.runtime_id ?? null;
  const runtime = runtimeId === null ? null : (directory.data?.runtimes ?? []).find((row) => row.runtime_id === runtimeId) ?? null;
  const adapter = runtime?.capabilities.adapter;
  const pauseIsUnsupported = pauseUnsupported(adapter);
  const runSignals: ChatRunSignals | null = urlScope === "run" && servedRunExecution !== null
    ? {
      canGuide: servedRunExecution.can_guide === true,
      canPause: servedRunExecution.can_pause === true,
      canResume: servedRunExecution.can_resume === true,
      hasDecision: decision !== null,
      pauseUnsupported: pauseIsUnsupported,
    }
    : null;
  const intents = applicableChatIntents({ scope: urlScope, run: runSignals, hasProjects: directoryProjects.length > 0 });
  // The intent select only ever offers applicable intents; a stored choice the
  // served state no longer allows falls back instead of being sent as-is.
  const intent: ChatIntent = intents.includes(state.intent) ? state.intent : intents[0] ?? "discuss";
  const startFields = (urlScope === "workspace" || urlScope === "project") && intent === "start_work";

  // The start-work resources belong to the selected target project, so a target
  // other than the read's project is read on its own key; an identical key the
  // shell already loaded adds no request.
  const resourcesProjectId = urlScope === "workspace" && state.targetProjectId !== "" ? state.targetProjectId : urlProjectId;
  const resources = useQuery({ ...workspaceOptions(application.api, resourcesProjectId), enabled: urlScope !== "run" });
  const resourceRows = resources.data?.projects.find((project) => project.id === resourcesProjectId)?.resources ?? [];
  const repositories = resourceRows.filter((resource) => resource.kind === "repository");
  const ciResources = resourceRows.filter((resource) => resource.kind === "ci");
  const scopeProject = urlProjectId === null ? null : directoryProjects.find((project) => project.id === urlProjectId) ?? null;
  const targetProjectId = urlScope === "project" ? urlProjectId ?? "" : state.targetProjectId;

  const history = chatHistoryView(state.pages, state.nextCursor);

  useEffect(() => {
    const served = conversation.data;
    if (served === undefined) return;
    updateWith(key, (previous) => retainServedPage(previous, { messages: served.messages, nextBefore: served.next_before }, served));
  }, [conversation.data, key, updateWith]);

  // `Load older` reads one page through the served cursor and retains it; the
  // disabled query is keyed by that cursor, so a press can never duplicate one.
  const olderQuery = useQuery({ ...chatOlderPageOptions(application.api, identity, history.loadCursor ?? ""), enabled: false });
  const loadOlder = () => {
    const cursor = history.loadCursor;
    if (cursor === null || olderQuery.isFetching) return;
    void olderQuery.refetch().then((result) => {
      const fetched = result.data;
      if (fetched === undefined) return;
      // The cursor was relative to the boundary `${state.nextBoundaryId}` names;
      // the id is what later reads compare, because a cursor is re-signed.
      updateWith(key, (previous) => retainOlderPage(previous, cursor, previous.nextBoundaryId, { messages: fetched.messages, nextBefore: fetched.next_before }));
    });
  };

  const scrollRef = useRef<HTMLDivElement | null>(null);
  const owedScroll = scrollRestoreOf(key);

  // Applies a scope's own offset. The pane clamps `scrollTop` while its
  // transcript is still arriving, and the router's own scroll restoration can
  // move it afterwards, so a restore that did not stick is owed and re-asserted
  // on later commits. The offset a scope leaves with is recorded by its own
  // scroll events — never by reading the pane after its content was swapped.
  useLayoutEffect(() => {
    const node = scrollRef.current;
    if (node === null || owedScroll === null) return;
    node.scrollTop = owedScroll;
    if (node.scrollTop === owedScroll) settleScrollRestore(key);
  });

  useLayoutEffect(() => {
    const node = scrollRef.current;
    if (node === null) return;
    const restored = scrollOffsetOf(key);
    node.scrollTop = restored;
    if (node.scrollTop !== restored) markScrollRestore(key, restored);
  }, [key, markScrollRestore, scrollOffsetOf]);

  // The router restores elements' scroll positions across navigations; this
  // surface's own per-scope offsets are the authority, so they are re-asserted
  // once the router's restore for the new location has run, and owed while the
  // pane cannot hold them yet.
  useEffect(() => router.subscribe("onRendered", () => {
    const node = scrollRef.current;
    if (node === null) return;
    const restored = scrollOffsetOf(key);
    node.scrollTop = restored;
    if (node.scrollTop !== restored) markScrollRestore(key, restored);
  }), [key, markScrollRestore, router, scrollOffsetOf]);

  const refresh = () => {
    if (!conversation.isFetching) void conversation.refetch();
  };

  /** A composer change abandons an unconfirmed send: what is visible is then the new intent. */
  const edit = (patch: Parameters<typeof update>[1]) => {
    updateWith(key, (current) => ({
      ...current,
      ...patch,
      frozen: null,
      status: current.status.kind === "unconfirmed" ? { kind: "idle" } : current.status,
    }));
  };

  const mutation = useMutation(chatMessageMutation(application.api));
  const send = (request: ChatMessageRequest) => {
    const sendKey = key;
    // The scope this attempt belongs to, by the same identity its read is keyed
    // by: a run-scope send carries no project member, so the request's own
    // members would not name the query that must re-read.
    const sendIdentity = identity;
    updateWith(sendKey, (current) => ({ ...current, status: { kind: "sending" }, frozen: request }));
    mutation.mutate({ payload: request, csrf }, {
      onSuccess: (outcome) => {
        updateWith(sendKey, (current) => ({
          ...current,
          // A question's answer is the server's saved message; only a command
          // awaits an acknowledgement, and even that is never rendered as applied.
          status: outcome.command === null ? { kind: "saved-question" } : { kind: "saved-command" },
          frozen: null,
          draft: current.draft === request.content ? "" : current.draft,
        }));
        // The durable message is the server's own record: a scoped re-read
        // renders it and leaves every other scope's cache untouched.
        void queryClient.invalidateQueries({ queryKey: ["chat", sendIdentity] });
      },
      onError: (error) => {
        const refused = definitiveRefusal(error);
        updateWith(sendKey, (current) => ({
          ...current,
          status: refused ? { kind: "refused", message: errorMessage(error) } : { kind: "unconfirmed" },
          frozen: refused ? null : current.frozen,
        }));
      },
    });
  };

  const buildRequest = (): ChatMessageRequest | null => {
    const members = {
      scope: urlScope,
      ...(urlScope === "project" && urlProjectId !== null ? { project_id: urlProjectId } : {}),
      ...(urlScope === "run" && urlRunId !== null ? { run_id: urlRunId } : {}),
    };
    const content = state.draft;
    if (intent === "guidance" && new TextEncoder().encode(content).byteLength > guidanceByteLimit) {
      // The server's own bound and its own wording; the draft and selection stay.
      update(key, { status: { kind: "notice", message: "Guidance must be 32,768 UTF-8 bytes or fewer." } });
      return null;
    }
    if (intent === "start_work") {
      if (targetProjectId === "") {
        update(key, { status: { kind: "notice", message: "Select a target project before starting work." } });
        return null;
      }
      return {
        ...members,
        intent,
        content,
        action_id: crypto.randomUUID(),
        target_project_id: targetProjectId,
        work: {
          ...(state.workTitle.trim() === "" ? {} : { title: state.workTitle }),
          ...(state.repositoryId === "" ? {} : { repository_resource_id: state.repositoryId }),
          ...(state.ciId === "" ? {} : { ci_resource_id: state.ciId }),
        },
      };
    }
    if (urlScope === "run" && intent !== "discuss" && intent !== "status") {
      if (servedRunItem === null || servedRunExecution === null || urlRunId === null) {
        update(key, { status: { kind: "notice", message: "No run is selected, so no control can be dispatched." } });
        return null;
      }
      return {
        ...members,
        intent,
        content,
        action_id: crypto.randomUUID(),
        work_item_id: servedRunItem.id,
        generation: servedRunExecution.generation,
      };
    }
    return { ...members, intent, content, action_id: crypto.randomUUID() };
  };

  const submit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (state.status.kind === "sending") return;
    // A retry resends the frozen object itself: the same action id and a
    // byte-equivalent body, so the server replays its recorded message.
    if (state.frozen !== null) {
      send(state.frozen);
      return;
    }
    const request = buildRequest();
    if (request !== null) send(request);
  };

  const sendDecision = (option: ChatDecisionOption) => {
    if (state.status.kind === "sending") return;
    if (servedRunItem === null || servedRunExecution === null || waiting === null || urlRunId === null) return;
    send({
      scope: urlScope,
      run_id: urlRunId,
      intent: "decision",
      content: option.label ?? option.id,
      action_id: crypto.randomUUID(),
      work_item_id: servedRunItem.id,
      generation: servedRunExecution.generation,
      // Both identities come from the same served snapshot that rendered the
      // buttons, so a stale packet is the server's refusal to give.
      waiting_transition_id: waiting.transition_id,
      option_id: option.id,
    });
  };

  // The offset this scope leaves with is the pane's own position at the moment
  // the switch is requested: the browser coalesces one scroll event per frame,
  // so a pane positioned and left in the same frame — by the surface's own
  // controls, or by a reader mid-gesture — is never reported as a scroll, and
  // the commit that replaces this scope's transcript takes the position with it.
  // A pane pinned below this scope's own offset is that replacement's clamp, not
  // a position the reader chose: it stays owed and is not recorded here.
  const rememberLeavingScope = () => {
    const node = scrollRef.current;
    if (node === null) return;
    if (panePinnedBelowOffset(node, Math.max(scrollOffsetOf(key), owedScroll ?? 0))) return;
    recordScrollOffset(key, node.scrollTop);
    // A live position that is not the owed offset is the reader taking over.
    if (owedScroll !== null && node.scrollTop !== owedScroll) settleScrollRestore(key);
  };

  const changeScope = (next: ChatScope) => {
    if (next === urlScope) return;
    if (next === "project" && urlProjectId === null) {
      update(key, { status: { kind: "notice", message: "No project is selected, so the scope was unchanged. Open Chat from a project to hold a project conversation." } });
      return;
    }
    rememberLeavingScope();
    // The current project/run members are carried so a round trip through
    // another scope returns to the same conversation.
    void navigate({ to: "/chat", search: { scope: next, ...(urlProjectId === null ? {} : { projectId: urlProjectId }), ...(urlRunId === null ? {} : { runId: urlRunId }) } });
  };

  const changeRun = (runId: string) => {
    rememberLeavingScope();
    void navigate({ to: "/chat", search: { scope: "run", ...(urlProjectId === null ? {} : { projectId: urlProjectId }), ...(runId === "" ? {} : { runId }) } });
  };

  const pending = scopeComplete && conversation.isPending;
  const contextLabel = chatScopeContext({
    urlScope,
    urlProjectId,
    urlRunId,
    served: conversation.data === undefined ? null : { scope: conversation.data.scope, projectId: conversation.data.project_id, runId: conversation.data.run_id },
    pending,
    project: conversation.data === undefined || conversation.data.project_id === null
      ? null
      : directoryProjects.find((project) => project.id === conversation.data?.project_id) ?? null,
    run: { workItemKey: servedRunItem?.key ?? null, generation: servedRunExecution?.generation ?? null },
  });
  const runLinks = chatRunLinks(servedRunItem);
  const evidence = chatEvidenceRows(conversationRun);
  const runSelectVisible = urlScope === "run" || runRows.length > 0;
  const runState = servedRunExecution === null ? null : chatStateLabel(servedRunExecution.state);
  const emptyScope = urlScope === "project"
    ? { title: "No project selected", body: "Open Chat from a project to hold a project conversation." }
    : { title: "No run selected", body: "Select a run to hold a run-scoped conversation." };

  return <>
    <PageHeading title="Chat" description="One conversation per scope. Questions read the recorded work; guidance, pause, resume and decisions are dispatched here and answered by the runtime's own acknowledgement.">
      <RefreshButton fetching={conversation.isFetching} refresh={refresh} />
    </PageHeading>

    <div className="chat-scope-bar">
      <div className="chat-field">
        <label htmlFor="chat-scope">Scope</label>
        <select id="chat-scope" value={urlScope} onChange={(event) => { changeScope(event.currentTarget.value as ChatScope); }}>
          <option value="workspace">Workspace</option>
          <option value="project">Project</option>
          <option value="run">Run</option>
        </select>
      </div>
      <div className="chat-field" hidden={!runSelectVisible}>
        <label htmlFor="chat-run-select">Run</label>
        <select id="chat-run-select" value={urlRunId ?? ""} onChange={(event) => { changeRun(event.currentTarget.value); }}>
          <option value="">{runRows.length === 0 ? "No runs recorded" : "Select a run…"}</option>
          {runRows.map((row) => <option key={row.runId} value={row.runId}>{runOptionLabel(row)}</option>)}
          {urlRunId !== null && !runRows.some((row) => row.runId === urlRunId) && <option value={urlRunId}>Run {urlRunId}</option>}
        </select>
      </div>
    </div>

    <section id="chat-scope-context" className="chat-scope-context" aria-label="Conversation scope">
      <p className="chat-context-heading">{contextLabel}</p>
      {servedRunItem !== null && urlScope === "run" && <div className="chat-run-context">
        <p className="muted text-small">{servedRunItem.key} · {servedRunItem.title}{servedRunExecution === null ? "" : ` · Attempt ${servedRunExecution.generation} · ${runState}`}</p>
        {runLinks.length > 0 && <p className="chat-run-links">{runLinks.map(({ label, link }) => <span key={label}>
          <span className="muted text-small">{label}:</span>{" "}
          {link.href === null ? <span className="chat-link-text">{link.text}</span> : <a href={link.href} rel="noreferrer noopener" target="_blank">{link.text}</a>}
        </span>)}</p>}
        {/* Cancel and retry stay on the work detail, which owns those controls and their receipts. */}
        <p className="chat-work-link"><Link to="/work/$workItemId" params={{ workItemId: servedRunItem.id }}>Open work detail</Link></p>
        {failure !== null && <p id="chat-failure" className="chat-failure" data-failure-state={failure.state}>{failure.text}</p>}
      </div>}
    </section>

    {decision !== null && <section id="chat-decision" className="chat-decision" aria-labelledby="chat-decision-question">
      <h2 id="chat-decision-question">{decision.question ?? "The run is waiting for a decision."}</h2>
      {decision.context !== null && <p>{decision.context}</p>}
      <ul className="decision-options">
        {decision.options.map((option) => <li key={option.id}>
          <button className="button chat-decision-option" type="button" data-chat-decision={option.id} disabled={state.status.kind === "sending"} onClick={() => { sendDecision(option); }}>
            <span className="decision-option-label">{option.label ?? option.id}</span>
            {option.consequence !== null && <span className="muted text-small">{option.consequence}</span>}
            {option.recommended && <span className="chip decision-recommendation">Recommended</span>}
          </button>
        </li>)}
      </ul>
    </section>}

    {evidence.length > 0 && <section id="chat-execution-evidence" className="chat-execution-evidence" aria-label="Recorded execution evidence">
      <h2>Recorded execution evidence</h2>
      <p className="muted text-small">The run's own semantic record, oldest first; raw tool output stays on the work detail.</p>
      <ol className="chat-evidence-list">
        {evidence.map((row) => <li key={row.key} data-evidence-source={row.source}>
          <span className="chip">{row.kind}</span>
          {row.summary !== null && <span className="text-small">{row.summary}</span>}
        </li>)}
      </ol>
    </section>}

    {urlScope === "run" && <p id="chat-guidance-delivery" className="chat-delivery muted text-small">{guidanceDeliveryLabel(adapter)}</p>}
    {urlScope === "run" && pauseIsUnsupported && <p id="chat-pause-support" className="chat-pause-support muted text-small">{pauseUnsupportedLabel}</p>}

    {conversation.isError && <ErrorState error={conversation.error} retry={refresh} stale={conversation.data !== undefined} />}

    <div
      id="chat-scroll"
      className="chat-scroll"
      ref={scrollRef}
      onScroll={(event) => {
        const node = event.currentTarget;
        const shown = node.scrollTop;
        // A transcript that replaced this pane's own pins `scrollTop` at the
        // pane's maximum, and that pinning fires a scroll event exactly like a
        // reader's move. A pane pinned at its own maximum, below an offset it
        // cannot hold, was clamped rather than scrolled: the offset stays this
        // scope's and the restore stays owed, so the next commit re-asserts it
        // once the layout can hold it. A reader's own move inside such a pane
        // sits below that maximum and is still theirs.
        const wanted = Math.max(scrollOffsetOf(key), owedScroll ?? 0);
        if (panePinnedBelowOffset(node, wanted)) {
          markScrollRestore(key, wanted);
          return;
        }
        recordScrollOffset(key, shown);
        // A live position that is not the owed offset is the reader taking over.
        if (owedScroll !== null && shown !== owedScroll) settleScrollRestore(key);
      }}
    >
      {!scopeComplete
        ? <EmptyState title={emptyScope.title}>{emptyScope.body}</EmptyState>
        : pending && history.messages.length === 0
          ? <LoadingState label="Loading this conversation…" />
          : <ol id="chat-messages" className="chat-messages">
            {history.messages.map((message) => <ChatMessageRow
              key={message.id}
              message={message}
              receipt={chatReceiptView(message.command, history.currentIds.has(message.id))}
            />)}
          </ol>}
    </div>

    <div className="chat-history">
      {history.gap !== null && <p
        id="chat-history-gap"
        className="notice notice-history-gap"
        role="status"
        data-boundary-current={history.gap.current}
        data-boundary-retained={history.gap.retained}
      >
        Entries between the loaded history and the retained older pages were not loaded; Load older history continues from the boundary the loaded history ends at.
      </p>}
      <p id="chat-history-note" className="muted text-small">
        {history.messages.length === 0
          ? "No messages recorded in this context."
          : `${history.messages.length} messages loaded${history.loadCursor === null ? " · oldest history loaded" : " · older history available"}.`}
      </p>
      {history.loadCursor !== null && <button
        id="chat-load-older"
        className="button"
        type="button"
        aria-disabled={olderQuery.isFetching}
        onClick={loadOlder}
      >Load older</button>}
    </div>

    <form id="chat-form" className="chat-form" aria-busy={state.status.kind === "sending"} onSubmit={submit}>
      <div className="chat-field">
        <label htmlFor="chat-intent">Intent</label>
        <select id="chat-intent" value={intent} onChange={(event) => { edit({ intent: event.currentTarget.value as ChatIntent }); }}>
          {intents.map((candidate) => <option key={candidate} value={candidate}>{chatIntentLabel(candidate)}</option>)}
        </select>
      </div>

      {startFields && <details id="chat-start-fields" className="chat-start-fields">
        <summary>Start work details</summary>
        <div className="chat-start-grid">
          <div className="chat-field">
            <label htmlFor="chat-work-title">Work item title (optional)</label>
            <input id="chat-work-title" type="text" value={state.workTitle} onChange={(event) => { edit({ workTitle: event.currentTarget.value }); }} />
          </div>
          <div className="chat-field">
            <label htmlFor="chat-target-project">Target project</label>
            <select
              id="chat-target-project"
              required={urlScope === "workspace"}
              value={targetProjectId}
              disabled={urlScope === "project"}
              onChange={(event) => { edit({ targetProjectId: event.currentTarget.value }); }}
            >
              {urlScope === "workspace" && <option value="">Select a project…</option>}
              {urlScope === "project"
                ? <option value={urlProjectId ?? ""}>{scopeProject === null ? urlProjectId ?? "" : `${scopeProject.name} (${scopeProject.key})`}</option>
                : directoryProjects.map((project) => <option key={project.id} value={project.id}>{project.name} ({project.key})</option>)}
            </select>
          </div>
          <div className="chat-field">
            <label htmlFor="chat-repository">Repository resource</label>
            <select id="chat-repository" value={state.repositoryId} onChange={(event) => { edit({ repositoryId: event.currentTarget.value }); }}>
              <option value="">None</option>
              {repositories.map((resource) => <option key={resource.id} value={resource.id}>{resource.name}</option>)}
            </select>
          </div>
          <div className="chat-field">
            <label htmlFor="chat-ci">CI resource</label>
            <select id="chat-ci" value={state.ciId} onChange={(event) => { edit({ ciId: event.currentTarget.value }); }}>
              <option value="">None</option>
              {ciResources.map((resource) => <option key={resource.id} value={resource.id}>{resource.name}</option>)}
            </select>
          </div>
        </div>
      </details>}

      <label htmlFor="chat-content">Message</label>
      <textarea
        id="chat-content"
        className="chat-content"
        required
        rows={3}
        value={state.draft}
        placeholder={intent === "start_work" ? "State the goal this work should accomplish." : "Write the message this scope should keep."}
        onChange={(event) => { edit({ draft: event.currentTarget.value }); }}
      />
      <div className="chat-composer-actions">
        <button id="chat-send" className="button control-button-primary" type="submit">Send</button>
        <p id="chat-send-status" className="chat-send-status" role="status" aria-live="polite">{chatSendStatusText(state.status)}</p>
      </div>
    </form>
  </>;
}

function runOptionLabel(row: ServedRun): string {
  return `${row.item.key} · ${row.item.title} · Attempt ${row.execution.generation} · ${chatStateLabel(row.execution.state)}`;
}

/**
 * True when the pane is pinned at a maximum its own transcript cannot grow past
 * while sitting below an offset this scope holds: the pinning a transcript that
 * replaced this pane's own raises, never a position the reader chose.
 */
function panePinnedBelowOffset(node: HTMLDivElement, wanted: number): boolean {
  const range = node.scrollHeight - node.clientHeight;
  return node.scrollTop < wanted && range < wanted && node.scrollTop === Math.max(0, range);
}

function ChatMessageRow({ message, receipt }: {
  readonly message: ChatConversationMessage;
  readonly receipt: ChatReceiptView | null;
}) {
  return <li className={`chat-message chat-message-${message.role}`} data-message-id={message.id} data-role={message.role}>
    <p className="chat-message-meta">
      <span className="chat-message-role">{message.role}</span>
      <span className="chat-message-intent">{message.intent}</span>
    </p>
    <p className="chat-message-content">{message.content}</p>
    {receipt !== null && <div className="receipt chat-receipt" data-receipt-for={message.id} data-outcome={receipt.outcome}>
      <p className="receipt-outcome">{receipt.headline}</p>
      {message.command !== null && <p className="muted text-small">{message.command.kind}</p>}
    </div>}
  </li>;
}

/**
 * A definitive server refusal: the attempt's identity is dead and the surface
 * renders the server's own message. A lost or unproven response is not a
 * refusal, so it stays unconfirmed and its frozen request can be retried.
 */
function definitiveRefusal(error: unknown): boolean {
  return error instanceof Conflict || error instanceof Validation || error instanceof Forbidden || error instanceof Unsupported ||
    (error instanceof ServerRefusal && error.status >= 400 && error.status < 500);
}
