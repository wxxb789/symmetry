import { useCallback, useEffect, useLayoutEffect, type RefObject, type UIEvent } from "react";
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

  const assertRecordedOffset = useCallback(() => {
    const node = scrollRef.current;
    if (node === null) return;
    const restored = scrollOffsetOf(key);
    node.scrollTop = restored;
    if (node.scrollTop !== restored) markScrollRestore(key, restored);
  }, [key, markScrollRestore, scrollOffsetOf, scrollRef]);

  useLayoutEffect(assertRecordedOffset, [assertRecordedOffset]);

  // The router repositions elements across navigations after this pane commits;
  // this surface's own per-scope offsets are the authority, re-asserted after it.
  useEffect(() => router.subscribe("onRendered", assertRecordedOffset), [assertRecordedOffset, router]);

  // A transcript replacement pins `scrollTop` at the pane's maximum and fires a
  // reader-like scroll event; pinned below the offset it stays owed, unrecorded.
  const wantedOffset = () => Math.max(scrollOffsetOf(key), owedScroll ?? 0);
  const recordReaderPosition = (offset: number) => {
    recordScrollOffset(key, offset);
    // A live position that is not the owed offset is the reader taking over.
    if (owedScroll !== null && offset !== owedScroll) settleScrollRestore(key);
  };

  const onScroll = (event: UIEvent<HTMLDivElement>) => {
    const node = event.currentTarget;
    const wanted = wantedOffset();
    if (panePinnedBelowOffset(node, wanted)) {
      markScrollRestore(key, wanted);
      return;
    }
    recordReaderPosition(node.scrollTop);
  };

  const rememberLeavingScope = () => {
    const node = scrollRef.current;
    if (node === null) return;
    if (panePinnedBelowOffset(node, wantedOffset())) return;
    recordReaderPosition(node.scrollTop);
  };

  return { onScroll, rememberLeavingScope };
}
