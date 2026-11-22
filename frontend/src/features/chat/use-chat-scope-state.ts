import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useLayoutEffect, useRef, type SyntheticEvent } from "react";
import { chatMessageMutation, type ChatScopeKey } from "../../app/application";
import { useSessionToken } from "../../app/session";
import { Conflict, Forbidden, ServerRefusal, Unsupported, Validation, errorMessage, type Api, type ChatMessageRequest, type ChatScope, type WorkItem } from "../../services/api";
import type { ChatDecisionOption } from "./presentation";
import { chatScopeKey, useChatScopeStore, type ChatIntent } from "./scopes";

const guidanceByteLimit = 32_768;

export interface ChatScopeSearch {
  readonly scope?: ChatScope;
  readonly projectId?: string | null;
  readonly runId?: string | null;
}

export interface ChatScopeWrites {
  readonly api: Api;
  readonly intents: readonly ChatIntent[];
  readonly runItem: WorkItem | null;
  readonly runGeneration: number | null;
  readonly waitingTransitionId: string | null;
}

interface ChatRequestInputs {
  readonly scope: ChatScope;
  readonly projectId: string | null;
  readonly runId: string | null;
  readonly intent: ChatIntent;
  readonly draft: string;
  readonly targetProjectId: string;
  readonly workTitle: string;
  readonly repositoryId: string;
  readonly ciId: string;
  readonly runItem: WorkItem | null;
  readonly runGeneration: number | null;
}

type ChatRequestOutcome =
  | { readonly kind: "request"; readonly request: ChatMessageRequest }
  | { readonly kind: "notice"; readonly message: string };

export function buildChatRequest(inputs: ChatRequestInputs): ChatRequestOutcome {
  const members = {
    scope: inputs.scope,
    ...(inputs.scope === "project" && inputs.projectId !== null ? { project_id: inputs.projectId } : {}),
    ...(inputs.scope === "run" && inputs.runId !== null ? { run_id: inputs.runId } : {}),
  };
  const content = inputs.draft;
  if (inputs.intent === "guidance" && new TextEncoder().encode(content).byteLength > guidanceByteLimit) {
    return { kind: "notice", message: "Guidance must be 32,768 UTF-8 bytes or fewer." };
  }
  if (inputs.intent === "start_work") {
    if (inputs.targetProjectId === "") {
      return { kind: "notice", message: "Select a target project before starting work." };
    }
    return {
      kind: "request",
      request: {
        ...members,
        intent: inputs.intent,
        content,
        action_id: crypto.randomUUID(),
        target_project_id: inputs.targetProjectId,
        work: {
          ...(inputs.workTitle.trim() === "" ? {} : { title: inputs.workTitle }),
          ...(inputs.repositoryId === "" ? {} : { repository_resource_id: inputs.repositoryId }),
          ...(inputs.ciId === "" ? {} : { ci_resource_id: inputs.ciId }),
        },
      },
    };
  }
  if (inputs.scope === "run" && inputs.intent !== "discuss" && inputs.intent !== "status") {
    if (inputs.runItem === null || inputs.runGeneration === null || inputs.runId === null) {
      return { kind: "notice", message: "No run is selected, so no control can be dispatched." };
    }
    return {
      kind: "request",
      request: {
        ...members,
        intent: inputs.intent,
        content,
        action_id: crypto.randomUUID(),
        work_item_id: inputs.runItem.id,
        generation: inputs.runGeneration,
      },
    };
  }
  return { kind: "request", request: { ...members, intent: inputs.intent, content, action_id: crypto.randomUUID() } };
}

function definitiveRefusal(error: unknown): boolean {
  return error instanceof Conflict || error instanceof Validation || error instanceof Forbidden || error instanceof Unsupported ||
    (error instanceof ServerRefusal && error.status >= 400 && error.status < 500);
}

function sameRequestBody(frozen: ChatMessageRequest, rebuilt: ChatMessageRequest): boolean {
  return JSON.stringify({ ...frozen, action_id: "" }) === JSON.stringify({ ...rebuilt, action_id: "" });
}

export interface ChatScopeParams {
  readonly scope: ChatScope;
  readonly projectId: string | null;
  readonly runId: string | null;
  readonly identity: ChatScopeKey;
  readonly key: string;
  readonly complete: boolean;
}

export function chatScopeParams(search: ChatScopeSearch): ChatScopeParams {
  const scope: ChatScope = search.scope ?? "workspace";
  const projectId = search.projectId ?? null;
  const runId = search.runId ?? null;
  return {
    scope,
    projectId,
    runId,
    identity: { scope, projectId, runId },
    key: chatScopeKey(scope, projectId, runId),
    complete: scope === "workspace" || (scope === "project" ? projectId !== null : runId !== null),
  };
}

export function useChatScopeState(search: ChatScopeSearch, writes: ChatScopeWrites) {
  const { scope, projectId, runId, identity, key, complete } = chatScopeParams(search);

  const store = useChatScopeStore();
  const {
    stateOf,
    update: updateStored,
    updateWith: updateStoredWith,
    scrollOffsetOf,
    recordScrollOffset,
    scrollRestoreOf,
    markScrollRestore,
    settleScrollRestore,
  } = store;
  const state = stateOf(key);
  // Settle-time callbacks judge the chain the reader holds now, not the render
  // their own closure was made in.
  const latestStateOf = useRef(stateOf);
  useLayoutEffect(() => { latestStateOf.current = stateOf; });
  const edit = (patch: Parameters<typeof updateStored>[1]) => {
    updateStoredWith(key, (current) => ({ ...current, ...patch, status: current.status.kind === "unconfirmed" ? { kind: "idle" } : current.status }));
  };
  const intent: ChatIntent = writes.intents.includes(state.intent) ? state.intent : writes.intents[0] ?? "discuss";
  const startFields = (scope === "workspace" || scope === "project") && intent === "start_work";
  const targetProjectId = scope === "project" ? projectId ?? "" : state.targetProjectId;
  const resourcesProjectId = scope === "workspace" && state.targetProjectId !== "" ? state.targetProjectId : projectId;
  const sending = state.status.kind === "sending";

  const queryClient = useQueryClient();
  const csrf = useSessionToken();
  const mutation = useMutation(chatMessageMutation(writes.api));

  const send = (request: ChatMessageRequest) => {
    const sendIdentity = identity;
    updateStoredWith(key, (current) => ({ ...current, status: { kind: "sending" }, frozen: request }));
    mutation.mutate({ payload: request, csrf }, {
      onSuccess: (outcome) => {
        updateStoredWith(key, (current) => ({
          ...current,
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
        updateStoredWith(key, (current) => ({
          ...current,
          status: refused
            ? { kind: "refused", message: errorMessage(error) }
            : { kind: "unconfirmed", source: request.intent === "decision" ? "decision" : "composer" },
          frozen: refused ? null : current.frozen,
        }));
      },
    });
  };

  const submit = (event: SyntheticEvent<HTMLFormElement>) => {
    event.preventDefault();
    // The composer is disabled for an incomplete scope; the guard also keeps a
    // programmatic submit from asking the server for a scope it cannot resolve.
    if (!complete) return;
    if (state.status.kind === "sending") return;
    const outcome = buildChatRequest({
      scope,
      projectId,
      runId,
      intent,
      draft: state.draft,
      targetProjectId,
      workTitle: state.workTitle,
      repositoryId: state.repositoryId,
      ciId: state.ciId,
      runItem: writes.runItem,
      runGeneration: writes.runGeneration,
    });
    if (outcome.kind === "notice") {
      updateStored(key, { status: { kind: "notice", message: outcome.message } });
      return;
    }
    const frozen = state.frozen;
    send(frozen !== null && sameRequestBody(frozen, outcome.request) ? { ...outcome.request, action_id: frozen.action_id } : outcome.request);
  };

  const sendDecision = (option: ChatDecisionOption) => {
    if (state.status.kind === "sending") return;
    if (writes.runItem === null || writes.runGeneration === null || writes.waitingTransitionId === null || runId === null) return;
    const request: ChatMessageRequest = {
      scope,
      run_id: runId,
      intent: "decision",
      content: option.label ?? option.id,
      action_id: crypto.randomUUID(),
      work_item_id: writes.runItem.id,
      generation: writes.runGeneration,
      // Both identities come from the same served snapshot that rendered the
      // buttons, so a stale packet is the server's refusal to give.
      waiting_transition_id: writes.waitingTransitionId,
      option_id: option.id,
    };
    const frozen = state.frozen;
    send(frozen !== null && sameRequestBody(frozen, request) ? { ...request, action_id: frozen.action_id } : request);
  };

  return {
    scope,
    projectId,
    runId,
    identity,
    key,
    complete,
    state,
    latestStateOf,
    targetProjectId,
    resourcesProjectId,
    intent,
    startFields,
    sending,
    update: updateStored,
    updateWith: updateStoredWith,
    edit,
    submit,
    sendDecision,
    scroll: {
      scrollOffsetOf,
      recordScrollOffset,
      scrollRestoreOf,
      markScrollRestore,
      settleScrollRestore,
    },
  };
}
