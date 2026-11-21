import { useInfiniteQuery } from "@tanstack/react-query";
import { useRouteContext } from "@tanstack/react-router";
import { UnexpectedResponse, type TimelineItem } from "../../services/api";
import { workTimelineOptions } from "../../app/application";
import { useControlSurface } from "./control";
import { timelineItemKey, timelineItemView, timestamp } from "./presentation";

/**
 * Execution history for one work item. The newest page arrives with the detail
 * read; older pages load on demand through the served `before` cursor only, and
 * the pages the reader loaded belong to the boundary of the first page they were
 * fetched from. The control surface owns that retained state, because the receipt
 * list beside this section renders the same pages, and it hands them over only
 * while the detail read still ends at that boundary: a served read that ends
 * somewhere else does not chain where those pages begin, so it renders its own
 * first page alone and the reader loads older history from its cursor.
 */
export function WorkHistory({ workItemId, firstPage, cursor }: { workItemId: string; firstPage: readonly TimelineItem[]; cursor: string | null }) {
  const { application } = useRouteContext({ from: "__root__" });
  const surface = useControlSurface();
  const query = useInfiniteQuery(workTimelineOptions(application.api, workItemId, cursor));
  const retained = surface.olderEntries;
  // A first page the same boundary serves again can carry an entry the retained
  // pages already hold; the newest page owns it then, so no entry renders twice.
  const firstPageKeys = new Set(firstPage.map(timelineItemKey));
  const items = [...firstPage, ...retained.filter((item) => !firstPageKeys.has(timelineItemKey(item)))];
  const exhausted = query.isSuccess && !query.hasNextPage;
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
            void query.refetch().then((result) => { surface.retainTimelinePages(result.data); });
          }}>Retry history</button>
        </div>
      </div>
      : exhausted
        ? <p className="muted">No older history recorded.</p>
        : <button className="button" type="button" disabled={query.isFetchingNextPage} onClick={() => { void query.fetchNextPage().then((result) => { surface.retainTimelinePages(result.data); }); }}>
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
