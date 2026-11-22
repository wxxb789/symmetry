import { useEffect, useLayoutEffect, type RefObject, type UIEvent } from "react";
import type { ChatScopeStore } from "./scopes";

export type ChatScopeScroll = Pick<ChatScopeStore, "scrollOffsetOf" | "recordScrollOffset" | "scrollRestoreOf" | "markScrollRestore" | "settleScrollRestore">;

export interface RouterRenderedEvents {
  subscribe(event: "onRendered", listener: () => void): () => void;
}

function panePinnedBelowOffset(node: HTMLDivElement, wanted: number): boolean {
  const range = node.scrollHeight - node.clientHeight;
  return node.scrollTop < wanted && range < wanted && node.scrollTop === Math.max(0, range);
}

export function useChatScrollRestoration(key: string, scroll: ChatScopeScroll, router: RouterRenderedEvents, scrollRef: RefObject<HTMLDivElement | null>) {
  const { scrollOffsetOf, recordScrollOffset, scrollRestoreOf, markScrollRestore, settleScrollRestore } = scroll;
  const owedScroll = scrollRestoreOf(key);

  // A pane whose transcript is still arriving clamps `scrollTop`, so the owed
  // offset is re-asserted on every commit until the pane accepts it.
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
  }, [key, scrollRef, markScrollRestore, scrollOffsetOf]);

  // The router restores element scroll positions across navigations; this
  // surface's own per-scope offsets are the authority, so they are re-asserted
  // once the router's restore for the new location has run.
  useEffect(() => router.subscribe("onRendered", () => {
    const node = scrollRef.current;
    if (node === null) return;
    const restored = scrollOffsetOf(key);
    node.scrollTop = restored;
    if (node.scrollTop !== restored) markScrollRestore(key, restored);
  }), [key, scrollRef, markScrollRestore, router, scrollOffsetOf]);

  // A transcript that replaced this pane's own pins `scrollTop` at the pane's
  // maximum and raises a scroll event exactly like a reader's move: pinned
  // below this scope's offset it keeps the offset owed, below that it is the
  // reader's own position.
  const onScroll = (event: UIEvent<HTMLDivElement>) => {
    const node = event.currentTarget;
    const shown = node.scrollTop;
    const wanted = Math.max(scrollOffsetOf(key), owedScroll ?? 0);
    if (panePinnedBelowOffset(node, wanted)) {
      markScrollRestore(key, wanted);
      return;
    }
    recordScrollOffset(key, shown);
    // A live position that is not the owed offset is the reader taking over.
    if (owedScroll !== null && shown !== owedScroll) settleScrollRestore(key);
  };

  const rememberLeavingScope = () => {
    const node = scrollRef.current;
    if (node === null) return;
    if (panePinnedBelowOffset(node, Math.max(scrollOffsetOf(key), owedScroll ?? 0))) return;
    recordScrollOffset(key, node.scrollTop);
    // A live position that is not the owed offset is the reader taking over.
    if (owedScroll !== null && node.scrollTop !== owedScroll) settleScrollRestore(key);
  };

  return { onScroll, rememberLeavingScope };
}
