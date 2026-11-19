/**
 * Per-scope conversation state.
 *
 * One scope key — `workspace`, `project:<id>` or `run:<id>` — owns its own
 * composer draft, selected intent, start-work selections, send status, frozen
 * request and the history pages the reader loaded. The page reads and writes
 * through this store, so switching scope restores that scope's values and a
 * background update for one scope can never write another scope's.
 */
import type { ChatConversationMessage, ChatMessageRequest, ChatScope } from "../../services/api";
import { useCallback, useRef, useState } from "react";

/** The intents the server's chat vocabulary accepts. */
export type ChatIntent = "discuss" | "status" | "start_work" | "guidance" | "decision" | "pause" | "resume" | "cancel";

/** The scope's send state; every rendered text is one of these exact observables. */
export type ChatSendStatus =
  | { readonly kind: "idle" }
  | { readonly kind: "sending" }
  | { readonly kind: "saved-question" }
  | { readonly kind: "saved-command" }
  | { readonly kind: "refused"; readonly message: string }
  | { readonly kind: "unconfirmed" }
  | { readonly kind: "notice"; readonly message: string };

/** One retained conversation page: the served messages and the cursor it came from. */
export interface ChatHistoryPage {
  /** The page's messages in the server's own order (oldest first). */
  readonly messages: readonly ChatConversationMessage[];
  /** The served cursor this page was fetched with; null for the page that arrived with a read. */
  readonly fetchBefore: string | null;
  /** The page's own served `next_before`; null when the server reported the end of history. */
  readonly nextBefore: string | null;
  /**
   * The id of the message the fetch cursor was relative to, recorded at fetch
   * time. A cursor is re-signed per response, so only the boundary message's id
   * proves the retained chain begins exactly where the current page ends.
   */
  readonly fetchBeforeId: string | null;
}

export interface ChatScopeState {
  readonly draft: string;
  readonly intent: ChatIntent;
  /** The start-work goal title, optional. */
  readonly workTitle: string;
  readonly targetProjectId: string;
  readonly repositoryId: string;
  readonly ciId: string;
  readonly status: ChatSendStatus;
  /** The exact request object of an unconfirmed send; a retry resends it byte for byte. */
  readonly frozen: ChatMessageRequest | null;
  /** The displayed pages, `[newest, ...older]`. */
  readonly pages: readonly ChatHistoryPage[];
  /** The served cursor `Load older` continues from; the only cursor this surface uses. */
  readonly nextCursor: string | null;
  /** The id of the message {@link nextCursor} is relative to. */
  readonly nextBoundaryId: string | null;
  /** The served page object the newest page was synced from, by identity. */
  readonly servedSource: object | null;
}

export function emptyChatScopeState(): ChatScopeState {
  return {
    draft: "",
    intent: "discuss",
    workTitle: "",
    targetProjectId: "",
    repositoryId: "",
    ciId: "",
    status: { kind: "idle" },
    frozen: null,
    pages: [],
    nextCursor: null,
    nextBoundaryId: null,
    servedSource: null,
  };
}

/** The key one scope identity owns: `workspace`, `project:<id>` or `run:<id>`. */
export function chatScopeKey(scope: ChatScope, projectId: string | null, runId: string | null): string {
  if (scope === "project") return `project:${projectId ?? ""}`;
  if (scope === "run") return `run:${runId ?? ""}`;
  return "workspace";
}

/**
 * Applies a served page (the initial read or a refresh). The current page is
 * replaced in place; the displayed older pages are kept and deduped at render
 * time. The older chain restarts from the refreshed boundary only when the
 * served page ends at a different boundary message: a served cursor is
 * re-signed per response, so the boundary message's id — not the token's
 * bytes — is what proves two reads paginate from the same position.
 */
export function retainServedPage(
  state: ChatScopeState,
  served: { readonly messages: readonly ChatConversationMessage[]; readonly nextBefore: string | null },
  source: object,
): ChatScopeState {
  if (state.servedSource === source) return state;
  const page: ChatHistoryPage = { messages: served.messages, fetchBefore: null, nextBefore: served.nextBefore, fetchBeforeId: null };
  // The retained chain hangs on the current page's boundary — the message its
  // own `next_before` was signed from — so a re-signed token for the same
  // boundary keeps the chain pointer where the reader's loads reached it. A
  // page that proves no boundary falls back to the token, as a fetched page's
  // own binding does.
  const servedBoundaryId = served.messages[0]?.id ?? null;
  const currentBoundaryId = state.pages[0]?.messages[0]?.id ?? null;
  const moved = servedBoundaryId !== null && currentBoundaryId !== null
    ? servedBoundaryId !== currentBoundaryId
    : served.nextBefore !== state.nextCursor;
  const nextCursor = moved ? served.nextBefore : state.nextCursor;
  const nextBoundaryId = moved ? served.messages[0]?.id ?? null : state.nextBoundaryId;
  return { ...state, pages: [page, ...state.pages.slice(1)], nextCursor, nextBoundaryId, servedSource: source };
}

/**
 * Appends one loaded older page, under the boundary its own cursor was minted
 * from. The surface's chain continues from that page only while the boundary
 * is still the one its next read chains from: a page a re-serve overtook keeps
 * its own binding, and the served chain keeps its own continuation, so the
 * reader's next press loads the region the moved boundary skipped.
 */
export function retainOlderPage(
  state: ChatScopeState,
  fetchedBefore: string,
  fetchedBeforeId: string | null,
  page: { readonly messages: readonly ChatConversationMessage[]; readonly nextBefore: string | null },
): ChatScopeState {
  const continues = fetchedBeforeId !== null ? fetchedBeforeId === state.nextBoundaryId : state.nextCursor === fetchedBefore;
  return {
    ...state,
    pages: [...state.pages, { messages: page.messages, fetchBefore: fetchedBefore, nextBefore: page.nextBefore, fetchBeforeId: fetchedBeforeId }],
    nextCursor: continues ? page.nextBefore : state.nextCursor,
    nextBoundaryId: continues ? page.messages[0]?.id ?? null : state.nextBoundaryId,
  };
}

export interface ChatScopeStore {
  readonly stateOf: (key: string) => ChatScopeState;
  readonly update: (key: string, patch: Partial<ChatScopeState>) => void;
  readonly updateWith: (key: string, change: (state: ChatScopeState) => ChatScopeState) => void;
  /** The offset this scope's message list last showed; a scope that has never scrolled reads its top. */
  readonly scrollOffsetOf: (key: string) => number;
  /** Records the offset a scope's message list is showing: a reader's scroll, or a restore. */
  readonly recordScrollOffset: (key: string, offset: number) => void;
  /**
   * The offset this scope's pane still owes, or null. A pane whose transcript is
   * still arriving clamps `scrollTop`, so a restore is re-asserted on later
   * commits until the pane accepts it.
   */
  readonly scrollRestoreOf: (key: string) => number | null;
  readonly markScrollRestore: (key: string, offset: number) => void;
  readonly settleScrollRestore: (key: string) => void;
}

/** The page's per-scope state owner: one record keyed by scope, plus the scroll offsets. */
export function useChatScopeStore(): ChatScopeStore {
  const [states, setStates] = useState<Readonly<Record<string, ChatScopeState>>>({});
  const scrollOffsets = useRef<Record<string, number>>({});
  const stateOf = useCallback((key: string) => states[key] ?? emptyChatScopeState(), [states]);
  const updateWith = useCallback((key: string, change: (state: ChatScopeState) => ChatScopeState) => {
    setStates((previous) => {
      const current = previous[key] ?? emptyChatScopeState();
      const next = change(current);
      return next === current ? previous : { ...previous, [key]: next };
    });
  }, []);
  const update = useCallback((key: string, patch: Partial<ChatScopeState>) => {
    updateWith(key, (state) => ({ ...state, ...patch }));
  }, [updateWith]);
  // The per-scope message-list offsets live in the store, so a scope's own
  // offset is written and read through it rather than from outside.
  const scrollOffsetOf = useCallback((key: string) => scrollOffsets.current[key] ?? 0, []);
  const recordScrollOffset = useCallback((key: string, offset: number) => { scrollOffsets.current[key] = offset; }, []);
  const [restores, setRestores] = useState<Readonly<Record<string, number>>>({});
  const scrollRestoreOf = useCallback((key: string) => restores[key] ?? null, [restores]);
  const markScrollRestore = useCallback((key: string, offset: number) => {
    setRestores((previous) => (previous[key] === offset ? previous : { ...previous, [key]: offset }));
  }, []);
  const settleScrollRestore = useCallback((key: string) => {
    setRestores((previous) => {
      if (!(key in previous)) return previous;
      const next = { ...previous };
      delete next[key];
      return next;
    });
  }, []);
  return { stateOf, update, updateWith, scrollOffsetOf, recordScrollOffset, scrollRestoreOf, markScrollRestore, settleScrollRestore };
}
