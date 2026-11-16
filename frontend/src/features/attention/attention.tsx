import { useQuery } from "@tanstack/react-query";
import { Link, useRouteContext, useSearch } from "@tanstack/react-router";
import { AlertCircle } from "lucide-react";
import { workspaceOptions } from "../../app/application";
import { EmptyState, ErrorState, LoadingState, PageHeading, RefreshButton, StatusBadge } from "../../components/workspace";

export function AttentionPage() {
  const { application } = useRouteContext({ from: "__root__" });
  const search = useSearch({ from: "/attention" });
  const query = useQuery(workspaceOptions(application.api, search.projectId ?? null));
  const projectId = search.projectId ?? query.data?.selected_project_id;
  const projects = query.data?.projects.filter((project) => project.id === projectId);
  const blocked = projects?.flatMap((project) => project.work_items.filter((item) => item.blocked).map((item) => ({ project, item })));
  const refresh = () => { if (!query.isFetching) void query.refetch(); };
  return <>
    <PageHeading title="Attention" description="Reported blockers in the selected project.">
      <RefreshButton fetching={query.isFetching} refresh={refresh} />
    </PageHeading>
    {search.unavailable && <section className="notice" role="status"><div><h2>This destination is unavailable</h2><p>The previous portal destination is not included in this workspace. Use Attention or Projects to browse existing work.</p></div></section>}
    {projects?.[0] && <p className="filter-note">Showing {projects[0].name}. <Link to="/projects">Browse projects</Link></p>}
    {query.isPending && <LoadingState label="Loading blocked work…" />}
    {query.isError && <ErrorState error={query.error} retry={refresh} stale={query.data !== undefined} />}
    {query.data && (projects?.length === 0
      ? <EmptyState title={search.projectId ? "Project not found" : "No projects yet"}>{search.projectId ? "This project is unavailable or you no longer have access." : "Blocked work will appear here when projects are available."}</EmptyState>
      : blocked?.length === 0
        ? <EmptyState title="No blocked work">There are no reported blockers in this project.</EmptyState>
        : <section aria-labelledby="blocked-heading"><div className="section-heading"><h2 id="blocked-heading">Blocked work <span className="count">{blocked?.length}</span></h2><span className="muted text-small">Read-only</span></div>
          <ul className="attention-list">{blocked?.map(({ project, item }) => <li className="attention-card" key={`${project.id}/${item.id}`}>
            <AlertCircle className="attention-icon" size={20} aria-hidden="true" />
            <div className="attention-main"><div className="attention-context"><Link to="/projects/$projectId" params={{ projectId: project.id }}>{project.name}</Link><span className="item-key">{item.key}</span>{project.status === "archived" && <StatusBadge status="archived" />}</div>
              <h3>{item.title}</h3><p className="blocker">{item.blocker?.trim() || "Blocking reason unknown"}</p>
            </div><StatusBadge status={item.status} />
          </li>)}</ul>
        </section>)}
    <p className="sr-only" role="status">{query.isFetching && !query.isPending ? "Refreshing blocked work" : ""}</p>
  </>;
}
