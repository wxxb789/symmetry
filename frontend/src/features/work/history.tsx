import { useInfiniteQuery } from "@tanstack/react-query";
import { useRouteContext } from "@tanstack/react-router";
import { UnexpectedResponse, type TimelineItem } from "../../services/api";
import { workTimelineOptions } from "../../app/application";
import { timelineItemKey, timelineItemView, timestamp } from "./presentation";

/**
 * Execution history for one work item. The newest page arrives with the detail
 * read; older pages load on demand through the served `before` cursor only.
 */
export function WorkHistory({ workItemId, firstPage, cursor }: { workItemId: string; firstPage: readonly TimelineItem[]; cursor: string | null }) {
  const { application } = useRouteContext({ from: "__root__" });
  const query = useInfiniteQuery(workTimelineOptions(application.api, workItemId, cursor));
  const items = [...firstPage, ...(query.data?.pages.flatMap((page) => page.items) ?? [])];
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
            void query.refetch();
          }}>Retry history</button>
        </div>
      </div>
      : exhausted
        ? <p className="muted">No older history recorded.</p>
        : <button className="button" type="button" disabled={query.isFetchingNextPage} onClick={() => { void query.fetchNextPage(); }}>
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
