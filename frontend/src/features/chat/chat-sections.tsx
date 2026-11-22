import type { RefObject, SyntheticEvent, UIEvent } from "react";
import { EmptyState, LoadingState } from "../../components/workspace";
import type { ChatConversationMessage, ChatScope, Project, WorkItem } from "../../services/api";
import { chatIntentLabel, chatReceiptView, chatSendStatusText, chatStateLabel, type ChatDecisionOption, type ChatReceiptView } from "./presentation";
import type { ChatIntent, ChatSendStatus } from "./scopes";

export interface ServedRun {
  readonly runId: string;
  readonly item: WorkItem;
  readonly execution: NonNullable<WorkItem["execution"]>;
}

export function servedRuns(projects: readonly Project[]): readonly ServedRun[] {
  return projects.flatMap((project) =>
    project.work_items.flatMap((item): ServedRun[] => {
      const execution = item.execution ?? null;
      const runId = execution?.run_id ?? null;
      return execution === null || runId === null ? [] : [{ runId, item, execution }];
    }),
  );
}

function runOptionLabel(row: ServedRun): string {
  return `${row.item.key} · ${row.item.title} · Attempt ${row.execution.generation} · ${chatStateLabel(row.execution.state)}`;
}

export function ChatScopeBar({ scope, runId, runRows, runSelectVisible, onScope, onRun }: {
  readonly scope: ChatScope;
  readonly runId: string | null;
  readonly runRows: readonly ServedRun[];
  readonly runSelectVisible: boolean;
  readonly onScope: (next: ChatScope) => void;
  readonly onRun: (runId: string) => void;
}) {
  return <div className="chat-scope-bar">
    <div className="chat-field">
      <label htmlFor="chat-scope">Scope</label>
      <select id="chat-scope" value={scope} onChange={(event) => { onScope(event.currentTarget.value as ChatScope); }}>
        <option value="workspace">Workspace</option>
        <option value="project">Project</option>
        <option value="run">Run</option>
      </select>
    </div>
    <div className="chat-field" hidden={!runSelectVisible}>
      <label htmlFor="chat-run-select">Run</label>
      <select id="chat-run-select" value={runId ?? ""} onChange={(event) => { onRun(event.currentTarget.value); }}>
        <option value="">{runRows.length === 0 ? "No runs recorded" : "Select a run…"}</option>
        {runRows.map((row) => <option key={row.runId} value={row.runId}>{runOptionLabel(row)}</option>)}
        {runId !== null && !runRows.some((row) => row.runId === runId) && <option value={runId}>Run {runId}</option>}
      </select>
    </div>
  </div>;
}

export interface ChatDecisionView {
  readonly question: string | null;
  readonly context: string | null;
  readonly options: readonly ChatDecisionOption[];
}

export function ChatDecisionSection({ decision, sending, onChoose }: {
  readonly decision: ChatDecisionView | null;
  readonly sending: boolean;
  readonly onChoose: (option: ChatDecisionOption) => void;
}) {
  if (decision === null) return null;
  return <section id="chat-decision" className="chat-decision" aria-labelledby="chat-decision-question">
    <h2 id="chat-decision-question">{decision.question ?? "The run is waiting for a decision."}</h2>
    {decision.context !== null && <p>{decision.context}</p>}
    <ul className="decision-options">
      {decision.options.map((option) => <li key={option.id}>
        <button className="button chat-decision-option" type="button" data-chat-decision={option.id} disabled={sending} onClick={() => { onChoose(option); }}>
          <span className="decision-option-label">{option.label ?? option.id}</span>
          {option.consequence !== null && <span className="muted text-small">{option.consequence}</span>}
          {option.recommended && <span className="chip decision-recommendation">Recommended</span>}
        </button>
      </li>)}
    </ul>
  </section>;
}

export interface ChatEvidenceView {
  readonly key: string;
  readonly source: "event" | "transition" | "command";
  readonly kind: string;
  readonly summary: string | null;
}

export function ChatEvidenceList({ rows }: { readonly rows: readonly ChatEvidenceView[] }) {
  if (rows.length === 0) return null;
  return <section id="chat-execution-evidence" className="chat-execution-evidence" aria-label="Recorded execution evidence">
    <h2>Recorded execution evidence</h2>
    <p className="muted text-small">The run's own semantic record, oldest first; raw tool output stays on the work detail.</p>
    <ol className="chat-evidence-list">
      {rows.map((row) => <li key={row.key} data-evidence-source={row.source}>
        <span className="chip">{row.kind}</span>
        {row.summary !== null && <span className="text-small">{row.summary}</span>}
      </li>)}
    </ol>
  </section>;
}

export interface EmptyScopeView {
  readonly title: string;
  readonly body: string;
}

export interface ChatTranscriptHistory {
  readonly messages: readonly ChatConversationMessage[];
  readonly currentIds: ReadonlySet<string>;
  readonly gap: { readonly current: string; readonly retained: string } | null;
  readonly loadCursor: string | null;
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

export function ChatTranscript({ complete, pending, empty, history, historyNote, olderPending, onOlderLoad, scrollRef, onScroll }: {
  readonly complete: boolean;
  readonly pending: boolean;
  readonly empty: EmptyScopeView;
  readonly history: ChatTranscriptHistory;
  readonly historyNote: string | null;
  readonly olderPending: boolean;
  readonly onOlderLoad: () => void;
  readonly scrollRef: RefObject<HTMLDivElement | null>;
  readonly onScroll: (event: UIEvent<HTMLDivElement>) => void;
}) {
  return <>
    <div id="chat-scroll" className="chat-scroll" ref={scrollRef} onScroll={onScroll}>
      {!complete
        ? <EmptyState title={empty.title}>{empty.body}</EmptyState>
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
        Entries between the loaded history and the retained older pages were not loaded; Load older history loads one page per press, so filling the gap may take more than one press.
      </p>}
      {historyNote !== null && <p id="chat-history-note" className="muted text-small">{historyNote}</p>}
      {history.loadCursor !== null && <button
        id="chat-load-older"
        className="button"
        type="button"
        aria-disabled={olderPending}
        onClick={onOlderLoad}
      >Load older</button>}
    </div>
  </>;
}

export interface ChatComposerEdit {
  readonly intent?: ChatIntent;
  readonly draft?: string;
  readonly workTitle?: string;
  readonly repositoryId?: string;
  readonly ciId?: string;
  readonly targetProjectId?: string;
}

interface ResourceOption {
  readonly id?: string;
  readonly name?: string;
}

interface ProjectOption {
  readonly id: string;
  readonly name: string;
  readonly key: string;
}

export function ChatComposer({ scope, complete, intent, intents, startFields, projectId, scopeProject, projects, targetProjectId, draft, workTitle, repositoryId, ciId, repositories, ciResources, sending, status, onEdit, onSubmit }: {
  readonly scope: ChatScope;
  readonly complete: boolean;
  readonly intent: ChatIntent;
  readonly intents: readonly ChatIntent[];
  readonly startFields: boolean;
  readonly projectId: string | null;
  readonly scopeProject: ProjectOption | null;
  readonly projects: readonly ProjectOption[];
  readonly targetProjectId: string;
  readonly draft: string;
  readonly workTitle: string;
  readonly repositoryId: string;
  readonly ciId: string;
  readonly repositories: readonly ResourceOption[];
  readonly ciResources: readonly ResourceOption[];
  readonly sending: boolean;
  readonly status: ChatSendStatus;
  readonly onEdit: (patch: ChatComposerEdit) => void;
  readonly onSubmit: (event: SyntheticEvent<HTMLFormElement>) => void;
}) {
  return <form id="chat-form" className="chat-form" aria-busy={sending} onSubmit={onSubmit}>
    <div className="chat-field">
      <label htmlFor="chat-intent">Intent</label>
      <select id="chat-intent" value={intent} disabled={!complete} onChange={(event) => { onEdit({ intent: event.currentTarget.value as ChatIntent }); }}>
        {intents.map((candidate) => <option key={candidate} value={candidate}>{chatIntentLabel(candidate)}</option>)}
      </select>
    </div>

    {startFields && <details id="chat-start-fields" className="chat-start-fields">
      <summary>Start work details</summary>
      <div className="chat-start-grid">
        <div className="chat-field">
          <label htmlFor="chat-work-title">Work item title (optional)</label>
          <input id="chat-work-title" type="text" value={workTitle} disabled={!complete} onChange={(event) => { onEdit({ workTitle: event.currentTarget.value }); }} />
        </div>
        <div className="chat-field">
          <label htmlFor="chat-target-project">Target project</label>
          <select
            id="chat-target-project"
            required={scope === "workspace"}
            value={targetProjectId}
            disabled={!complete || scope === "project"}
            onChange={(event) => {
              onEdit({ targetProjectId: event.currentTarget.value, repositoryId: "", ciId: "" });
            }}
          >
            {scope === "workspace" && <option value="">Select a project…</option>}
            {scope === "project"
              ? <option value={projectId ?? ""}>{scopeProject === null ? projectId ?? "" : `${scopeProject.name} (${scopeProject.key})`}</option>
              : projects.map((project) => <option key={project.id} value={project.id}>{project.name} ({project.key})</option>)}
          </select>
        </div>
        <div className="chat-field">
          <label htmlFor="chat-repository">Repository resource</label>
          <select id="chat-repository" value={repositoryId} disabled={!complete} onChange={(event) => { onEdit({ repositoryId: event.currentTarget.value }); }}>
            <option value="">None</option>
            {repositories.map((resource) => <option key={resource.id} value={resource.id}>{resource.name}</option>)}
          </select>
        </div>
        <div className="chat-field">
          <label htmlFor="chat-ci">CI resource</label>
          <select id="chat-ci" value={ciId} disabled={!complete} onChange={(event) => { onEdit({ ciId: event.currentTarget.value }); }}>
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
      disabled={!complete}
      value={draft}
      placeholder={intent === "start_work" ? "State the goal this work should accomplish." : "Write the message this scope should keep."}
      onChange={(event) => { onEdit({ draft: event.currentTarget.value }); }}
    />
    <div className="chat-composer-actions">
      <button id="chat-send" className="button control-button-primary" type="submit" disabled={!complete}>Send</button>
      <p id="chat-send-status" className="chat-send-status" role="status" aria-live="polite">{chatSendStatusText(status)}</p>
    </div>
  </form>;
}
