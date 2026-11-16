/**
 * `/portal/runtimes`: machine status and the runtime's own advertised
 * capability record. Every row renders a served member or an explicit absence
 * label; the page adds no operation, capability or verification of its own.
 */

import { useQuery } from "@tanstack/react-query";
import { getRouteApi, useRouteContext } from "@tanstack/react-router";
import { runtimesOptions } from "../../app/application";
import { EmptyState, ErrorState, LoadingState, PageHeading, RefreshButton, StatusBadge } from "../../components/workspace";
import type { Runtime } from "../../services/api";
import { timestamp } from "../work/presentation";
import { capacityLabel, operationRows } from "./presentation";

function RuntimeCard({ runtime }: { runtime: Runtime }) {
  const adapter = runtime.capabilities.adapter;
  const heartbeat = timestamp(runtime.last_heartbeat_at);
  const capabilitiesHeadingId = `runtime-capabilities-${runtime.runtime_id}`;
  return <article
    className="runtime-card"
    data-runtime-id={runtime.runtime_id}
    data-runtime-key={runtime.runtime_key}
    data-adapter={adapter === undefined ? "none" : JSON.stringify(adapter)}
    data-capabilities={JSON.stringify(runtime.capabilities)}
    data-repository-resource-id={runtime.repository_resource_id ?? ""}
    data-heartbeat={runtime.last_heartbeat_at ?? ""}
  >
    <div className="runtime-card-heading">
      <div className="runtime-identity">
        <h2>{runtime.runtime_name}</h2>
        <span className="item-key">{runtime.runtime_key}</span>
      </div>
      <StatusBadge status={runtime.status} />
    </div>
    <dl className="runtime-facts">
      <div><dt>Machine</dt><dd>{runtime.machine_name}</dd></div>
      <div><dt>Connection epoch</dt><dd>{runtime.connection_epoch ?? "Not recorded"}</dd></div>
      <div><dt>Capacity</dt><dd>{capacityLabel(runtime.reserved_capacity, runtime.capacity)}</dd></div>
      <div><dt>Agent profile</dt><dd>{runtime.agent_profile ?? "Not recorded"}</dd></div>
      <div><dt>Workspace</dt><dd>{runtime.workspace ?? "Not recorded"}</dd></div>
      <div><dt>Repository resource affinity</dt><dd>{runtime.repository_resource_id ?? "Not bound"}</dd></div>
      <div><dt>Last heartbeat</dt><dd>{heartbeat === null ? "Not recorded" : <time dateTime={heartbeat.exact} title={heartbeat.exact}>{heartbeat.readable}</time>}</dd></div>
    </dl>
    <section className="runtime-capabilities" aria-labelledby={capabilitiesHeadingId}>
      <h3 id={capabilitiesHeadingId}>Advertised capabilities</h3>
      {adapter === undefined
        ? <p className="muted">Adapter record not advertised. This runtime reports no verified capability model, so no operation is claimed.</p>
        : <>
          <dl className="runtime-facts">
            <div><dt>Harness</dt><dd>{adapter.kind}</dd></div>
            <div><dt>Native version</dt><dd>{adapter.native_version}</dd></div>
            <div><dt>Implementation version</dt><dd>{adapter.implementation_version}</dd></div>
            <div><dt>Protocol version</dt><dd>{adapter.protocol_version}</dd></div>
          </dl>
          <ul className="capability-list">
            {operationRows(adapter.operations).map((row) => <li key={row.key}>
              <div className="capability-row" data-operation={row.key}>
                <span className="capability-name">{row.name}</span>
                <span className="capability-value"><span className={`chip chip-${row.tone}`}>{row.label}</span></span>
              </div>
            </li>)}
          </ul>
        </>}
    </section>
  </article>;
}

const route = getRouteApi("/runtimes");

export function RuntimesPage() {
  const { application } = useRouteContext({ from: "__root__" });
  // The project scope selects which runtimes are compatible with this
  // deployment's work; the shell carries it in the URL the same way Attention
  // does, and the read falls back to the server-selected project without it.
  // A registered runtime that no project binds is therefore not listed here;
  // that registry set stays available through the classic Resources view.
  const { projectId } = route.useSearch();
  const query = useQuery({ ...runtimesOptions(application.api, projectId ?? null) });
  const refresh = () => { if (!query.isFetching) void query.refetch(); };
  const runtimes = query.data?.runtimes ?? [];
  return <>
    <PageHeading title="Runtimes" description="Machine status and each runtime's own advertised capability record, shown as advertised rather than verified by this surface.">
      <RefreshButton fetching={query.isFetching} refresh={refresh} />
    </PageHeading>
    {query.isPending && <LoadingState label="Loading runtimes…" />}
    {query.isError && <ErrorState error={query.error} retry={refresh} stale={query.data !== undefined} />}
    {query.data && (runtimes.length === 0
      ? <EmptyState title="No runtimes registered">Queued work waits for a compatible runtime to register; capability rows appear only for verified runtimes.</EmptyState>
      : <div className="runtime-list">{runtimes.map((runtime) => <RuntimeCard key={runtime.runtime_id} runtime={runtime} />)}</div>)}
    <p className="sr-only" role="status">{query.isFetching && !query.isPending ? "Refreshing runtimes" : ""}</p>
  </>;
}
