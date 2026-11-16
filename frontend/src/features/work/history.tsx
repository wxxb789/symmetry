import { useInfiniteQuery } from "@tanstack/react-query";
import { useRouteContext } from "@tanstack/react-router";
import { useState } from "react";
import { UnexpectedResponse, type TimelineItem, type WorkTimeline } from "../../services/api";
import { workTimelineOptions } from "../../app/application";
import { timelineItemKey, timelineItemView, timestamp } from "./presentation";

/**
 * Merges newly served older pages into the entries already expanded. A detail
 * refresh re-issues the served cursor, so the query restarts from the refreshed
 * boundary: the fetched pages replace their own entries (same identity, newest
 * served text) while everything the reader already expanded stays rendered
 * instead of silently collapsing to the newest first page.
 */
function mergeOlderEntries(fetched: readonly TimelineItem[], retained: readonly TimelineItem[]): readonly TimelineItem[] {
  const fetchedKeys = new Set(fetched.map(timelineItemKey));
  const merged = [...fetched, ...retained.filter((item) => !fetchedKeys.has(timelineItemKey(item)))];
  return merged.length === retained.length && merged.every((item, index) => item === retained[index]) ? retained : merged;
}

/**
 * Execution history for one work item. The newest page arrives with the detail
 * read; older pages load on demand through the served `before` cursor only, and
 * the loaded pages belong to the reader rather than to one cursor generation.
 */
export function WorkHistory({ workItemId, firstPage, cursor }: { workItemId: string; firstPage: readonly TimelineItem[]; cursor: string | null }) {
  const { application } = useRouteContext({ from: "__root__" });
  const query = useInfiniteQuery(workTimelineOptions(application.api, workItemId, cursor));
  // Reopening the detail inside the freshness window remounts this component
  // while the disabled query still holds the pages the reader expanded. The
  // retained list starts from those cached pages, so the reader's history
  // survives the remount and a later re-keyed refresh alike.
  const [older, setOlder] = useState<{ workItemId: string; entries: readonly TimelineItem[] }>(
    () => ({ workItemId, entries: query.data?.pages.flatMap((page) => page.items) ?? [] }),
  );
  // Every older page is served by a reader action, so the merge runs when that
  // fetch resolves; a refresh that re-keys the query keeps the expanded entries.
  const retainPages = (data: { readonly pages: readonly WorkTimeline[] } | undefined) => {
    const fetched = data?.pages.flatMap((page) => page.items) ?? [];
    if (fetched.length === 0) return;
    setOlder((previous) => {
      const retained = previous.workItemId === workItemId ? previous.entries : [];
      const entries = mergeOlderEntries(fetched, retained);
      return entries === retained && previous.workItemId === workItemId ? previous : { workItemId, entries };
    });
  };
  const retained = older.workItemId === workItemId ? older.entries : [];
  // A refreshed first page can re-serve an entry the retained pages already
  // hold; the newest page owns it then, so no entry renders twice.
  const firstPageKeys = new Set(firstPage.map(timelineItemKey));
  const items = [...firstPage, ...retained.filter((item) => !firstPageKeys.has(timelineItemKey(item)))];
  const exhausted = query.isSuccess && query.hasNextPage === false;
  const staleCursor = query.error instanceof UnexpectedResponse && query.error.status === 400;
  if (items.length === 0 && cursor === null) return <p className="muted">No execution history recorded</p>;
  return <>
    {items.length > 0 && <ol className="timeline">
      {items.map((item) => <TimelineEntry key={timelineItemKey(item)} item={item} />)}
    </ol>}
    {cursor === null ? null : query.isError
      ? <div className="notice notice-error" role="alert">
        <div><p>Earlier history could not be loaded.</p>
          <button className="button" type="button" onClick={() => {
            if (staleCursor) {
              // The served boundary aged out (cursor max age): a fresh detail read
              // re-issues it, and the disabled timeline query restarts from the
              // newly served cursor through its query key.
              void application.queryClient.resetQueries({ queryKey: ["work-item-timeline", { workItemId }] });
              void application.queryClient.invalidateQueries({ queryKey: ["work-item", { workItemId }] });
              return;
            }
            void query.refetch().then((result) => { retainPages(result.data); });
          }}>Retry history</button>
        </div>
      </div>
      : exhausted
        ? <p className="muted">No older history recorded.</p>
        : <button className="button" type="button" disabled={query.isFetchingNextPage} onClick={() => { void query.fetchNextPage().then((result) => { retainPages(result.data); }); }}>
          {query.isFetchingNextPage ? "Loading history…" : "Load older history"}
        </button>}
  </>;
}

function TimelineEntry({ item }: { item: TimelineItem }) {
  const view = timelineItemView(item);
  const recorded = timestamp(item.recorded_at);
  const runId = item.run_id ?? null;
  const generation = typeof item.generation === "number" ? item.generation : null;
  return <li className="timeline-entry">
    <div className="timeline-meta">
      <span className="chip">{view.label}</span>
      <span className="muted text-small" title={runId ?? undefined}>
        generation {generation ?? "not recorded"}{runId === null ? "" : ` · run ${runId.slice(0, 8)}…`}
      </span>
      {recorded !== null && <time className="muted text-small" dateTime={recorded.exact} title={recorded.exact}>{recorded.readable}</time>}
    </div>
    {view.headline !== null && <p>{view.headline}</p>}
    {view.link !== null && <a href={view.link} rel="noreferrer noopener" target="_blank">{view.link}</a>}
  </li>;
}
