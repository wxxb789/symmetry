import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useParams, useRouteContext, useSearch } from "@tanstack/react-router";
import { Folder, ArrowUpRight, Plus } from "lucide-react";
import { useState } from "react";
import { workspaceOptions } from "../../app/application";
import { workspaceReadKey } from "../../app/read-cache";
import { useSelectedProject } from "../../app/selected-project";
import { useSessionToken } from "../../app/session";
import { StatusBadge } from "../../components/primitives";
import { EmptyState, ErrorState, LoadingState, PageHeading, RefreshButton } from "../../components/workspace";
import { hasServedRevision } from "../planning/write-policy";
import { ProjectEditor, WorkItemEditor } from "../planning/editors";
import { refusalNotice } from "../../services/refusals";
import { ProjectWorkSurface } from "./work-surface";

interface SurfaceNotice {
  tone: "status" | "alert";
  text: string;
}

function NoticeLine({ notice, onDismiss }: { notice: SurfaceNotice | null; onDismiss: () => void }) {
  if (notice === null) return null;
  return notice.tone === "status"
    ? <p className="notice" role="status">{notice.text}<button className="button" type="button" onClick={onDismiss}>Dismiss</button></p>
    : <p className="notice notice-error" role="alert">{notice.text}</p>;
}

export function ProjectsPage() {
  const { application } = useRouteContext({ from: "__root__" });
  const csrf = useSessionToken();
  const queryClient = useQueryClient();
  const query = useQuery(workspaceOptions(application.api, null));
  const [creating, setCreating] = useState(false);
  const [notice, setNotice] = useState<SurfaceNotice | null>(null);
  const refresh = () => { if (!query.isFetching) void query.refetch(); };
  // The re-read is what makes the notice truthful: the created project appears
  // in this list from the server's answer, and the operator opens it from here
  // instead of being navigated away before the notice can be read.
  const reRead = () => queryClient.invalidateQueries({ queryKey: workspaceReadKey() });
  return <>
    <PageHeading title="Projects" description="Your workspace, organized by project.">
      <button className="button button-primary" type="button" disabled={query.data === undefined}
        onClick={() => { setNotice(null); setCreating(true); }}><Plus size={16} aria-hidden="true" />New project</button>
      <RefreshButton fetching={query.isFetching} refresh={refresh} />
    </PageHeading>
    <NoticeLine notice={notice} onDismiss={() => { setNotice(null); }} />
    {query.isPending && <LoadingState label="Loading projects…" />}
    {query.isError && <ErrorState error={query.error} retry={refresh} stale={query.data !== undefined} />}
    {query.data && (query.data.projects.length === 0
      ? <EmptyState title="No projects yet">Create the first project to plan work in it.</EmptyState>
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
    {creating && <ProjectEditor project={null} api={application.api} csrf={csrf}
      onWriteStart={() => undefined} onWriteSettled={() => undefined}
      onSaved={(_receipt, text) => {
        setCreating(false);
        setNotice({ tone: "status", text });
        void reRead();
      }}
      onRefused={(message) => { setCreating(false); setNotice({ tone: "alert", text: message }); void reRead(); }}
      onClose={() => { setCreating(false); }} />}
  </>;
}

export function ProjectPage() {
  const { projectId } = useParams({ from: "/projects/$projectId" });
  const search = useSearch({ from: "/projects/$projectId" });
  const selectedProjectId = useSelectedProject();
  const navigate = useNavigate();
  const { application } = useRouteContext({ from: "__root__" });
  const csrf = useSessionToken();
  const queryClient = useQueryClient();
  const query = useQuery(workspaceOptions(application.api, selectedProjectId ?? null));
  const project = query.data?.projects.find((item) => item.id === projectId);
  const [settings, setSettings] = useState(false);
  const [creatingWork, setCreatingWork] = useState(false);
  const [pendingWrites, setPendingWrites] = useState(0);
  const [notice, setNotice] = useState<SurfaceNotice | null>(null);
  // A partial projection without the served revision keeps its read path and is
  // never offered a write the server would refuse as malformed.
  const versioned = project !== undefined && hasServedRevision(project) ? project : null;
  const refresh = () => { if (!query.isFetching) void query.refetch(); };
  const reRead = () => queryClient.invalidateQueries({ queryKey: workspaceReadKey() });
  // Archived projects accept exactly one write: the restore. It is versioned
  // like every other update, and the surface re-reads the server's answer.
  const restore = useMutation({
    mutationFn: (target: { id: string; version: number }) =>
      application.api.updateProject(target.id, { version: target.version, status: "active" }, csrf),
    onSuccess: () => { setNotice({ tone: "status", text: "Project restored." }); },
    onError: (error) => { setNotice({ tone: "alert", text: refusalNotice(error) }); },
    onSettled: async () => { await reRead(); },
  });
  return <>
    <div className="breadcrumbs"><Link to="/projects">Projects</Link><span aria-hidden="true">/</span><span>{project?.key ?? "Project"}</span></div>
    <PageHeading title={project?.name ?? "Project"} description={project?.description?.trim() || "Work and current blockers in this project."}>
      <div className="heading-actions">
        {versioned !== null && project?.status === "active" && <button className="button" type="button" disabled={pendingWrites > 0}
          onClick={() => { setNotice(null); setSettings(true); }}>Project settings</button>}
        {versioned !== null && project?.status === "archived" && <button className="button button-primary" type="button" disabled={pendingWrites > 0 || restore.isPending}
          onClick={() => { setNotice(null); restore.mutate({ id: versioned.id, version: versioned.version }); }}>{restore.isPending ? "Restoring…" : "Restore project"}</button>}
        <RefreshButton fetching={query.isFetching} refresh={refresh} />
      </div>
    </PageHeading>
    <NoticeLine notice={notice} onDismiss={() => { setNotice(null); }} />
    {query.isPending && <LoadingState label="Loading project…" />}
    {query.isError && <ErrorState error={query.error} retry={refresh} stale={query.data !== undefined} />}
    {query.data && !project && <EmptyState title="Project not found">This project is unavailable or you no longer have access. <Link to="/projects">View all projects</Link>.</EmptyState>}
    {project && <>
      {project.status === "archived" && <p className="notice" role="status">This project is archived. Its work stays visible here and is read-only until it is restored.</p>}
      <ProjectWorkSurface project={project} navigation={{
        search,
        // Functional form: the router supplies the latest search, so rapid compound
        // filter updates cannot drop an earlier one.
        setSearch: (next, options) => { void navigate({ from: "/projects/$projectId", to: "/projects/$projectId", params: { projectId }, search: (previous) => next(previous), replace: options?.replace ?? true }); },
        renderViewLink: (options) => <Link className={options.className} aria-current={options["aria-current"]} to="/projects/$projectId" params={{ projectId }} search={options.search}>{options.label}</Link>,
        createWorkItem: () => { setNotice(null); setCreatingWork(true); },
        createDisabled: pendingWrites > 0,
      }} />
    </>}
    <p className="sr-only" role="status">{query.isFetching && !query.isPending ? "Refreshing project" : ""}</p>
    {versioned !== null && settings && <ProjectEditor project={versioned} api={application.api} csrf={csrf}
      onWriteStart={() => { setPendingWrites((count) => count + 1); }}
      onWriteSettled={() => { setPendingWrites((count) => Math.max(0, count - 1)); }}
      onSaved={(_receipt, text) => { setSettings(false); setNotice({ tone: "status", text }); void reRead(); }}
      onRefused={(message) => { setSettings(false); setNotice({ tone: "alert", text: message }); void reRead(); }}
      onClose={() => { setSettings(false); }} />}
    {project !== undefined && creatingWork && <WorkItemEditor presentation="page" project={project} item={null} api={application.api} csrf={csrf}
      onWriteStart={() => { setPendingWrites((count) => count + 1); }}
      onWriteSettled={() => { setPendingWrites((count) => Math.max(0, count - 1)); }}
      onSaved={(_receipt, text) => { setCreatingWork(false); setNotice({ tone: "status", text }); void reRead(); }}
      onRefused={(message) => { setCreatingWork(false); setNotice({ tone: "alert", text: message }); void reRead(); }}
      onClose={() => { setCreatingWork(false); }} />}
  </>;
}
