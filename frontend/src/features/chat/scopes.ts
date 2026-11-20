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
  /**
   * The last attempt's exact request object. A retry rebuilds the payload from
   * current state and reuses this attempt's identity exactly when the rebuilt
   * body is byte-equal ignoring the action id; a changed body mints a new id.
   */
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
 * time. Whether the served page ends at a different boundary message decides
 * if the chain pointer moves to it, compared by message id while both pages
 * name one and by token bytes while either does not. A served cursor is
 * re-signed per response, so the boundary message's id — not the token's
 * bytes — is what proves two reads paginate from the same position; at an
 * unchanged boundary the served token renews the reader's own cursor while
 * the reader's chain still designates that boundary.
 */
export function retainServedPage(
  state: ChatScopeState,
  served: { readonly messages: readonly ChatConversationMessage[]; readonly nextBefore: string | null },
  source: object,
): ChatScopeState {
  if (state.servedSource === source) return state;
  const page: ChatHistoryPage = { messages: served.messages, fetchBefore: null, nextBefore: served.nextBefore, fetchBeforeId: null };
  // The retained chain hangs on the served page's boundary — its oldest
  // message — so a re-signed token for the same boundary keeps the chain
  // pointer where the reader's loads reached it. While either side lacks a
  // boundary id (the first read has no previous page) the token bytes are
  // compared instead; the fetched page's binding in `retainOlderPage` is
  // narrower: it compares bytes only while its own captured boundary id is
  // absent.
  const servedBoundaryId = served.messages[0]?.id ?? null;
  const currentBoundaryId = state.pages[0]?.messages[0]?.id ?? null;
  const moved = servedBoundaryId !== null && currentBoundaryId !== null
    ? servedBoundaryId !== currentBoundaryId
    : served.nextBefore !== state.nextCursor;
  // A response at the same boundary offers its own token to the chain: it
  // replaces the aging token exactly while the reader's cursor still
  // designates that boundary. A cursor the reader's loads moved past names an
  // older message the served page cannot re-sign, so it stays where the loads
  // reached it, and a chain that already ended keeps its recorded boundary and
  // stays ended — a token the same boundary offers after a drain is dropped,
  // which `docs/TODO.md` records as open work.
  const renew = !moved && servedBoundaryId !== null && state.nextBoundaryId === servedBoundaryId;
  const nextCursor = moved || renew ? served.nextBefore : state.nextCursor;
  const nextBoundaryId = moved ? servedBoundaryId : state.nextBoundaryId;
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
  const continues = ownsChainPointer(state, fetchedBefore, fetchedBeforeId);
  return {
    ...state,
    pages: [...state.pages, { messages: page.messages, fetchBefore: fetchedBefore, nextBefore: page.nextBefore, fetchBeforeId: fetchedBeforeId }],
    nextCursor: continues ? page.nextBefore : state.nextCursor,
    nextBoundaryId: continues ? page.messages[0]?.id ?? null : state.nextBoundaryId,
  };
}

/**
 * The one chain-pointer ownership rule both callers rest on: true while the
 * boundary a load or press was minted at — compared by the boundary message's
 * id whenever one was recorded, by the cursor's bytes otherwise — is still the
 * boundary the surface's next load chains from. `retainOlderPage` binds an
 * older page to the chain under it. A refused press's refusal proves nothing
 * about the cursor the surface holds now; the predicate only authorizes
 * recovery of a chain boundary the press still owns. A boundary a later load
 * advanced past no longer owns the pointer, and a re-signed cursor at the same
 * recorded boundary keeps it, because the id comparison does not change on
 * renewal.
 */
export function ownsChainPointer(state: ChatScopeState, cursor: string, boundaryId: string | null): boolean {
  return boundaryId !== null ? state.nextBoundaryId === boundaryId : state.nextCursor === cursor;
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
