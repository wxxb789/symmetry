import { useQuery, useQueryClient } from "@tanstack/react-query";
import { getRouteApi, Link, useNavigate, useRouteContext, useRouter } from "@tanstack/react-router";
import { useEffect, useRef } from "react";
import { chatConversationOptions, workspaceOptions } from "../../app/application";
import { useSelectedProject } from "../../app/selected-project";
import { ErrorState, PageHeading, RefreshButton } from "../../components/workspace";
import type { ChatConversationMessage, ChatConversationRun, ChatScope, Project, WorkItem, Workspace } from "../../services/api";
import { ChatComposer, ChatDecisionSection, ChatEvidenceList, ChatScopeBar, ChatTranscript, servedRuns, type ServedRun } from "./chat-sections";
import {
  applicableChatIntents,
  chatDecisionPacket,
  chatEvidenceRows,
  chatFailureView,
  chatHistoryView,
  chatRunLinks,
  chatScopeContext,
  chatStateLabel,
  guidanceDeliveryLabel,
  pauseUnsupported,
  pauseUnsupportedLabel,
  type ChatRunSignals,
} from "./presentation";
import { retainServedPage } from "./scopes";
import { chatScopeParams, useChatScopeState } from "./use-chat-scope-state";
import { useChatScrollRestoration } from "./use-chat-scroll-restoration";
import { useOlderPageLoading } from "./use-older-page-loading";

const route = getRouteApi("/chat");

interface ServedConversation {
  readonly scope: ChatScope;
  readonly project_id: string | null;
  readonly run_id: string | null;
  readonly runs: readonly ChatConversationRun[];
}

function conversationRunOf(conversation: ServedConversation | undefined): ChatConversationRun | null {
  return conversation?.scope === "run" ? conversation.runs[0] ?? null : null;
}

function servedRunOf(runRows: readonly ServedRun[], conversationRun: ChatConversationRun | null, runId: string | null) {
  const directoryRun = runId === null ? null : runRows.find((row) => row.runId === runId) ?? null;
  return {
    item: conversationRun?.work_item ?? directoryRun?.item ?? null,
    execution: conversationRun?.work_item.execution ?? directoryRun?.execution ?? null,
  };
}

function runSignalsOf(directory: Workspace | undefined, scope: ChatScope, execution: ServedRun["execution"] | null) {
  const runtimeId = execution?.runtime_id ?? null;
  const runtime = runtimeId === null ? null : (directory?.runtimes ?? []).find((row) => row.runtime_id === runtimeId) ?? null;
  const adapter = runtime?.capabilities.adapter;
  const pauseIsUnsupported = pauseUnsupported(adapter);
  const runSignals: ChatRunSignals | null = scope === "run" && execution !== null
    ? {
      canGuide: execution.can_guide,
      canPause: execution.can_pause,
      canResume: execution.can_resume,
      pauseUnsupported: pauseIsUnsupported,
    }
    : null;
  return { adapter, pauseIsUnsupported, runSignals };
}

function chatRunFacts(directory: Workspace | undefined, conversation: ServedConversation | undefined, scope: ChatScope, runId: string | null) {
  const runRows = servedRuns(directory?.projects ?? []);
  const conversationRun = conversationRunOf(conversation);
  const { item, execution } = servedRunOf(runRows, conversationRun, runId);
  const waiting = execution?.waiting ?? null;
  const signals = runSignalsOf(directory, scope, execution);
  return {
    runRows,
    servedRunItem: item,
    servedRunExecution: execution,
    waitingTransitionId: waiting?.transition_id ?? null,
    decision: chatDecisionPacket(waiting),
    failure: chatFailureView(execution?.state, execution?.failure, conversationRun?.outcome.failure),
    evidence: chatEvidenceRows(conversationRun),
    ...signals,
    runState: execution === null ? null : chatStateLabel(execution.state),
    runSelectVisible: scope === "run" || runRows.length > 0,
  };
}

function chatHistoryNote(history: { readonly messages: readonly ChatConversationMessage[]; readonly loadCursor: string | null }, served: boolean, failed: boolean): string | null {
  if (history.messages.length > 0) return `${history.messages.length} messages loaded${history.loadCursor === null ? " · oldest history loaded" : " · older history available"}.`;
  if (served) return "No messages recorded in this context.";
  return failed ? "Messages could not be loaded." : null;
}

function scopeProjectOf(projects: readonly Project[], projectId: string | null): Project | null {
  return projectId === null ? null : projects.find((project) => project.id === projectId) ?? null;
}

function chatContextLabel(scope: ChatScope, projectId: string | null, runId: string | null, conversation: ServedConversation | undefined, pending: boolean, projects: readonly Project[], runItem: WorkItem | null, runExecution: ServedRun["execution"] | null): string {
  return chatScopeContext({
    urlScope: scope,
    urlProjectId: projectId,
    urlRunId: runId,
    served: conversation === undefined ? null : { scope: conversation.scope, projectId: conversation.project_id, runId: conversation.run_id },
    pending,
    project: conversation === undefined || conversation.project_id === null
      ? null
      : projects.find((project) => project.id === conversation.project_id) ?? null,
    run: { workItemKey: runItem?.key ?? null, generation: runExecution?.generation ?? null },
  });
}

export function ChatPage() {
  const { application } = useRouteContext({ from: "__root__" });
  const navigate = useNavigate();
  const router = useRouter();
  const queryClient = useQueryClient();
  const search = route.useSearch();
  const params = chatScopeParams(search);
  const selectedProjectId = useSelectedProject();

  const conversation = useQuery({ ...chatConversationOptions(application.api, params.identity), enabled: params.complete });
  const directory = useQuery(workspaceOptions(application.api, selectedProjectId ?? null));
  const directoryProjects = directory.data?.projects ?? [];
  const served = chatRunFacts(directory.data, conversation.data, params.scope, params.runId);
  const intents = applicableChatIntents({ scope: params.scope, run: served.runSignals, hasProjects: directoryProjects.length > 0 });

  const {
    state, latestStateOf,
    targetProjectId, resourcesProjectId, intent, startFields, sending,
    update, updateWith, edit, submit, sendDecision, scroll,
  } = useChatScopeState(params, {
    api: application.api,
    intents,
    runItem: served.servedRunItem,
    runGeneration: served.servedRunExecution?.generation ?? null,
    waitingTransitionId: served.waitingTransitionId,
  });

  const resources = useQuery({ ...workspaceOptions(application.api, resourcesProjectId), enabled: params.scope !== "run" });
  const resourceRows = resources.data?.projects.find((project) => project.id === resourcesProjectId)?.resources ?? [];
  const repositories = resourceRows.filter((resource) => resource.kind === "repository");
  const ciResources = resourceRows.filter((resource) => resource.kind === "ci");
  const scopeProject = scopeProjectOf(directoryProjects, params.projectId);

  const history = chatHistoryView(state.pages, state.nextCursor);
  const historyNote = chatHistoryNote(history, conversation.data !== undefined, conversation.isError);

  useEffect(() => {
    const servedPage = conversation.data;
    if (servedPage === undefined) return;
    updateWith(params.key, (previous) => retainServedPage(previous, { messages: servedPage.messages, nextBefore: servedPage.next_before }, servedPage));
  }, [conversation.data, params.key, updateWith]);

  const older = useOlderPageLoading({
    key: params.key,
    identity: params.identity,
    cursor: history.loadCursor,
    boundaryId: state.nextBoundaryId,
    latestStateOf,
    application,
    queryClient,
    updateWith,
  });
  const paneRef = useRef<HTMLDivElement | null>(null);
  const scrollRestoration = useChatScrollRestoration(params.key, scroll, router, paneRef);

  const refresh = () => {
    if (!conversation.isFetching) void conversation.refetch();
  };

  const changeScope = (next: ChatScope) => {
    if (next === params.scope) return;
    if (next === "project" && params.projectId === null) {
      update(params.key, { status: { kind: "notice", message: "No project is selected, so the scope was unchanged. Open Chat from a project to hold a project conversation." } });
      return;
    }
    scrollRestoration.rememberLeavingScope();
    // The current project/run members are carried so a round trip through
    // another scope returns to the same conversation.
    void navigate({ to: "/chat", search: { scope: next, ...(params.projectId === null ? {} : { projectId: params.projectId }), ...(params.runId === null ? {} : { runId: params.runId }) } });
  };

  const changeRun = (next: string) => {
    scrollRestoration.rememberLeavingScope();
    void navigate({ to: "/chat", search: { scope: "run", ...(params.projectId === null ? {} : { projectId: params.projectId }), ...(next === "" ? {} : { runId: next }) } });
  };

  const pending = params.complete && conversation.isPending;
  const contextLabel = chatContextLabel(params.scope, params.projectId, params.runId, conversation.data, pending, directoryProjects, served.servedRunItem, served.servedRunExecution);
  const runLinks = chatRunLinks(served.servedRunItem);
  const emptyScope = params.scope === "project"
    ? { title: "No project selected", body: "Open Chat from a project to hold a project conversation." }
    : { title: "No run selected", body: "Select a run to hold a run-scoped conversation." };
  const transcript = {
    complete: params.complete,
    pending,
    empty: emptyScope,
    history,
    historyNote,
    olderPending: older.pending,
    onOlderLoad: older.load,
    scrollRef: paneRef,
    onScroll: scrollRestoration.onScroll,
  };
  const composer = {
    scope: params.scope,
    complete: params.complete,
    intent,
    intents,
    startFields,
    projectId: params.projectId,
    scopeProject,
    projects: directoryProjects,
    targetProjectId,
    draft: state.draft,
    workTitle: state.workTitle,
    repositoryId: state.repositoryId,
    ciId: state.ciId,
    repositories,
    ciResources,
    sending,
    status: state.status,
    onEdit: edit,
    onSubmit: submit,
  };

  return <>
    <PageHeading title="Chat" description="One conversation per scope. Questions read the recorded work; guidance, pause, resume and decisions are dispatched here and answered by the runtime's own acknowledgement.">
      <RefreshButton fetching={conversation.isFetching} refresh={refresh} />
    </PageHeading>

    <ChatScopeBar scope={params.scope} runId={params.runId} runRows={served.runRows} runSelectVisible={served.runSelectVisible} onScope={changeScope} onRun={changeRun} />

    <section id="chat-scope-context" className="chat-scope-context" aria-label="Conversation scope">
      <p className="chat-context-heading">{contextLabel}</p>
      {served.servedRunItem !== null && params.scope === "run" && <div className="chat-run-context">
        <p className="muted text-small">{served.servedRunItem.key} · {served.servedRunItem.title}{served.servedRunExecution === null ? "" : ` · Attempt ${served.servedRunExecution.generation} · ${served.runState}`}</p>
        {runLinks.length > 0 && <p className="chat-run-links">{runLinks.map(({ label, link }) => <span key={label}>
          <span className="muted text-small">{label}:</span>{" "}
          {link.href === null ? <span className="chat-link-text">{link.text}</span> : <a href={link.href} rel="noreferrer noopener" target="_blank">{link.text}</a>}
        </span>)}</p>}
        <p className="chat-work-link"><Link to="/work/$workItemId" params={{ workItemId: served.servedRunItem.id }}>Open work detail</Link></p>
        {served.failure !== null && <p id="chat-failure" className="chat-failure" data-failure-state={served.failure.state}>{served.failure.text}</p>}
      </div>}
    </section>

    {params.scope === "run" && <ChatDecisionSection decision={served.decision} sending={sending} onChoose={sendDecision} />}

    <ChatEvidenceList rows={served.evidence} />

    {params.scope === "run" && <p id="chat-guidance-delivery" className="chat-delivery muted text-small">{guidanceDeliveryLabel(served.adapter)}</p>}
    {params.scope === "run" && served.pauseIsUnsupported && <p id="chat-pause-support" className="chat-pause-support muted text-small">{pauseUnsupportedLabel}</p>}

    {conversation.isError && <ErrorState error={conversation.error} retry={refresh} stale={conversation.data !== undefined} />}

    <ChatTranscript {...transcript} />

    <ChatComposer {...composer} />
  </>;
}
