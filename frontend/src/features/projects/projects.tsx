import { useQuery } from "@tanstack/react-query";
import { Link, useNavigate, useParams, useRouteContext, useSearch } from "@tanstack/react-router";
import { Folder, ArrowUpRight } from "lucide-react";
import { workspaceOptions } from "../../app/application";
import { EmptyState, ErrorState, LoadingState, PageHeading, RefreshButton, StatusBadge } from "../../components/workspace";
import { ProjectWorkSurface } from "./work-surface";

export function ProjectsPage() {
  const { application } = useRouteContext({ from: "__root__" });
  const query = useQuery(workspaceOptions(application.api, null));
  const refresh = () => { if (!query.isFetching) void query.refetch(); };
  return <>
    <PageHeading title="Projects" description="Your workspace, organized by project.">
      <RefreshButton fetching={query.isFetching} refresh={refresh} />
    </PageHeading>
    {query.isPending && <LoadingState label="Loading projects…" />}
    {query.isError && <ErrorState error={query.error} retry={refresh} stale={query.data !== undefined} />}
    {query.data && (query.data.projects.length === 0
      ? <EmptyState title="No projects yet">Projects will appear here when they are available in your workspace.</EmptyState>
      : <ul className="project-list" aria-label="Projects">
        {query.data.projects.map((project) => <li key={project.id}>
          <Link className="project-card" to="/projects/$projectId" params={{ projectId: project.id }}>
            <div className="project-icon"><Folder size={20} aria-hidden="true" /></div>
            <div className="project-main"><div className="project-title"><span className="item-key">{project.key}</span><h2>{project.name}</h2></div>
              <p className="muted">{project.description?.trim() || "No description provided."}</p>
              <p className="project-count">{project.id === query.data?.selected_project_id
                ? `${project.work_items.length} work ${project.work_items.length === 1 ? "item" : "items"} · ${project.work_items.filter((item) => item.blocked).length} blocked`
                : "Open project to load work"}</p>
            </div>
            <div className="project-meta"><StatusBadge status={project.status} /><ArrowUpRight size={16} aria-hidden="true" /></div>
          </Link>
        </li>)}
      </ul>)}
    <p className="sr-only" role="status">{query.isFetching && !query.isPending ? "Refreshing projects" : ""}</p>
  </>;
}

export function ProjectPage() {
  const { projectId } = useParams({ from: "/projects/$projectId" });
  const search = useSearch({ from: "/projects/$projectId" });
  const navigate = useNavigate();
  const { application } = useRouteContext({ from: "__root__" });
  const query = useQuery(workspaceOptions(application.api, projectId));
  const project = query.data?.projects.find((item) => item.id === projectId);
  const refresh = () => { if (!query.isFetching) void query.refetch(); };
  return <>
    <div className="breadcrumbs"><Link to="/projects">Projects</Link><span aria-hidden="true">/</span><span>{project?.key ?? "Project"}</span></div>
    <PageHeading title={project?.name ?? "Project"} description={project?.description?.trim() || "Work and current blockers in this project."}>
      <RefreshButton fetching={query.isFetching} refresh={refresh} />
    </PageHeading>
    {query.isPending && <LoadingState label="Loading project…" />}
    {query.isError && <ErrorState error={query.error} retry={refresh} stale={query.data !== undefined} />}
    {query.data && !project && <EmptyState title="Project not found">This project is unavailable or you no longer have access. <Link to="/projects">View all projects</Link>.</EmptyState>}
    {project && <>
      {project.status === "archived" && <p className="notice" role="status">This project is archived. Its existing work remains visible here.</p>}
      <ProjectWorkSurface project={project} navigation={{
        search,
        // Functional form: the router supplies the latest search, so rapid compound
        // filter updates cannot drop an earlier one.
        setSearch: (next, options) => { void navigate({ from: "/projects/$projectId", to: "/projects/$projectId", params: { projectId }, search: (previous) => next(previous), replace: options?.replace ?? true }); },
        renderViewLink: (options) => <Link className={options.className} aria-current={options["aria-current"]} to="/projects/$projectId" params={{ projectId }} search={options.search}>{options.label}</Link>,
      }} />
    </>}
    <p className="sr-only" role="status">{query.isFetching && !query.isPending ? "Refreshing project" : ""}</p>
  </>;
}
