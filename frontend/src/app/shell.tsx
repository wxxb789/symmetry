import * as Dialog from "@radix-ui/react-dialog";
import { useQuery } from "@tanstack/react-query";
import { Link, Outlet, useMatch, useNavigate, useRouteContext, useRouterState } from "@tanstack/react-router";
import { Command, Folder, Inbox, LogOut, Menu, Search, X } from "lucide-react";
import { useEffect, useLayoutEffect, useRef, useState, useSyncExternalStore } from "react";
import { ErrorState, LoadingState } from "../components/workspace";
import { sessionOptions, workspaceOptions } from "./application";

function focusContent() {
  requestAnimationFrame(() => { document.getElementById("content-heading")?.focus({ preventScroll: true }); });
}

function useProjectScope() {
  const projectId = useMatch({ from: "/projects/$projectId", shouldThrow: false, select: (match) => match.params.projectId });
  const searchProjectId = useMatch({ from: "/attention", shouldThrow: false, select: (match) => match.search.projectId });
  return projectId ?? searchProjectId;
}

function NavigationLinks({ onNavigate }: { onNavigate?: () => void }) {
  const projectId = useProjectScope();
  return <nav aria-label="Workspace navigation" className="nav-links">
    <Link to="/attention" search={projectId ? { projectId } : {}} onClick={onNavigate} activeProps={{ className: "nav-link active", "aria-current": "page" }} inactiveProps={{ className: "nav-link" }}><Inbox size={18} aria-hidden="true" />Attention</Link>
    <Link to="/projects" onClick={onNavigate} activeProps={{ className: "nav-link active", "aria-current": "page" }} inactiveProps={{ className: "nav-link" }}><Folder size={18} aria-hidden="true" />Projects</Link>
  </nav>;
}

function ProjectPicker() {
  const { application } = useRouteContext({ from: "__root__" });
  const query = useQuery(workspaceOptions(application.api, null));
  const navigate = useNavigate();
  const pathname = useRouterState({ select: (state) => state.location.pathname });
  const projectId = useProjectScope();
  const isAttention = pathname.endsWith("/attention");
  const selected = projectId ?? (isAttention ? query.data?.selected_project_id ?? "" : "");
  return <div className="project-picker"><label htmlFor="project-selection">Project</label>
    <select id="project-selection" aria-label="Project" value={selected} disabled={!query.data} onChange={(event) => {
      const projectId = event.currentTarget.value;
      if (isAttention) void navigate({ to: "/attention", search: projectId ? { projectId } : {} });
      else if (projectId) void navigate({ to: "/projects/$projectId", params: { projectId } });
      else void navigate({ to: "/projects" });
    }}>
      <option value="">{query.isPending ? "Loading projects…" : query.isError && !query.data ? "Projects unavailable" : isAttention ? "Server-selected project" : "All projects"}</option>
      {selected && !query.data?.projects.some((project) => project.id === selected) && <option value={selected}>Unavailable project</option>}
      {query.data?.projects.map((project) => <option key={project.id} value={project.id}>{project.name}{project.status === "archived" ? " (archived)" : ""}</option>)}
    </select>
  </div>;
}

function CommandPalette() {
  const { application } = useRouteContext({ from: "__root__" });
  const query = useQuery(workspaceOptions(application.api, null));
  const projectId = useProjectScope();
  const [open, setOpen] = useState(false);
  const [search, setSearch] = useState("");
  const navigating = useRef(false);
  const previousFocus = useRef<HTMLElement | null>(null);
  const closeForNavigation = () => { navigating.current = true; setOpen(false); focusContent(); };
  useEffect(() => {
    const handleKey = (event: KeyboardEvent) => {
      if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "k" && !event.altKey) {
        event.preventDefault();
        if (!open) {
          previousFocus.current = document.activeElement instanceof HTMLElement ? document.activeElement : null;
          navigating.current = false;
          setSearch("");
        }
        setOpen(!open);
      }
    };
    document.addEventListener("keydown", handleKey);
    return () => document.removeEventListener("keydown", handleKey);
  }, [open]);
  const filter = search.trim().toLocaleLowerCase();
  const projects = query.data?.projects.filter((project) => `${project.name} ${project.key}`.toLocaleLowerCase().includes(filter)) ?? [];
  return <Dialog.Root open={open} onOpenChange={(value) => {
    if (value) { setSearch(""); navigating.current = false; }
    setOpen(value);
  }}>
    <Dialog.Trigger asChild><button className="command-trigger button" type="button" aria-label="Search or navigate" onClick={() => { previousFocus.current = null; }}><Search size={16} aria-hidden="true" /><span>Search or navigate</span><kbd><Command size={12} aria-hidden="true" />K</kbd></button></Dialog.Trigger>
    <Dialog.Portal><Dialog.Overlay className="dialog-overlay" />
      <Dialog.Content className="command-dialog" onCloseAutoFocus={(event) => {
        if (navigating.current) { event.preventDefault(); navigating.current = false; }
        else if (previousFocus.current) { event.preventDefault(); previousFocus.current.focus(); }
      }}>
        <div className="dialog-heading"><Dialog.Title>Command palette</Dialog.Title><Dialog.Close asChild><button className="icon-button" type="button" aria-label="Close command palette"><X size={20} aria-hidden="true" /></button></Dialog.Close></div>
        <Dialog.Description className="muted">Navigate this read-only workspace. Use Tab to select a result and Enter to open it.</Dialog.Description>
        <label className="sr-only" htmlFor="command-search">Search destinations</label>
        <input id="command-search" className="command-input" type="search" placeholder="Search projects and pages…" value={search} onChange={(event) => setSearch(event.currentTarget.value)} />
        <div className="command-results">
          {(!filter || "attention".includes(filter)) && <Link className="command-result" to="/attention" search={projectId ? { projectId } : {}} onClick={closeForNavigation}><Inbox size={18} aria-hidden="true" />Attention</Link>}
          {(!filter || "projects".includes(filter)) && <Link className="command-result" to="/projects" onClick={closeForNavigation}><Folder size={18} aria-hidden="true" />All projects</Link>}
          {projects.map((project) => <Link className="command-result" key={project.id} to="/projects/$projectId" params={{ projectId: project.id }} onClick={closeForNavigation}><Folder size={18} aria-hidden="true" /><span>{project.name}<span className="item-key"> {project.key}</span></span></Link>)}
          {filter && projects.length === 0 && !"attention".includes(filter) && !"projects".includes(filter) && <p className="muted" role="status">No matching destinations.</p>}
          {query.isError && <p className="muted">Project search is unavailable. You can still navigate to Attention or Projects.</p>}
        </div>
      </Dialog.Content>
    </Dialog.Portal>
  </Dialog.Root>;
}

function MobileNavigation() {
  const [open, setOpen] = useState(false);
  const navigating = useRef(false);
  return <Dialog.Root open={open} onOpenChange={(value) => { navigating.current = false; setOpen(value); }}>
    <Dialog.Trigger asChild><button className="icon-button mobile-nav-trigger" type="button" aria-label="Open navigation"><Menu size={22} aria-hidden="true" /></button></Dialog.Trigger>
    <Dialog.Portal><Dialog.Overlay className="dialog-overlay" />
      <Dialog.Content className="navigation-dialog" onCloseAutoFocus={(event) => {
        if (navigating.current) { event.preventDefault(); navigating.current = false; }
      }}>
        <div className="dialog-heading"><Dialog.Title>Navigation</Dialog.Title><Dialog.Close asChild><button className="icon-button" type="button" aria-label="Close navigation"><X size={20} aria-hidden="true" /></button></Dialog.Close></div>
        <Dialog.Description className="sr-only">Browse your authenticated workspace.</Dialog.Description>
        <NavigationLinks onNavigate={() => { navigating.current = true; setOpen(false); focusContent(); }} />
      </Dialog.Content>
    </Dialog.Portal>
  </Dialog.Root>;
}

export function WorkspaceRoot() {
  const { application } = useRouteContext({ from: "__root__" });
  const signedOut = useSyncExternalStore(application.auth.subscribe, application.auth.getSnapshot);
  const session = useQuery({ ...sessionOptions(application.api), enabled: !signedOut });
  const href = useRouterState({ select: (state) => state.location.href });
  const navigationStatus = useRouterState({ select: (state) => state.status });
  const ready = session.isSuccess && !signedOut;
  useLayoutEffect(() => {
    if (!ready || navigationStatus !== "idle") return;
    document.getElementById("content-heading")?.focus({ preventScroll: true });
  }, [href, navigationStatus, ready]);
  if (signedOut) return <main className="session-state"><h1>Signed out</h1><p role="status">Your session has ended. Redirecting to sign in…</p><a className="button" href="/portal/login">Sign in</a></main>;
  if (session.isPending) return <main className="session-state"><h1>Symmetry workspace</h1><LoadingState label="Checking your session…" /></main>;
  if (session.isError && !session.data) return <main className="session-state"><h1>Workspace unavailable</h1><ErrorState error={session.error} retry={() => { void session.refetch(); }} /></main>;
  const csrf = session.data?.csrf_token;
  return <>
    <a className="skip-link" href="#content-heading">Skip to content</a>
    <div className="workspace-shell">
      <aside className="sidebar"><Link className="brand" to="/attention" search={{}}><span className="brand-mark" aria-hidden="true">S</span>Symmetry</Link><p className="sidebar-label">Workspace</p><NavigationLinks /><div className="sidebar-footer"><span className="read-only-dot" aria-hidden="true" />Read-only workspace</div></aside>
      <div className="workspace-body"><header className="topbar"><div className="topbar-start"><MobileNavigation /><span className="mobile-brand">Symmetry</span><ProjectPicker /></div><div className="topbar-actions"><CommandPalette /><form action="/portal/logout" method="post"><input type="hidden" name="_csrf_token" value={csrf ?? ""} /><button className="icon-button" type="submit" disabled={!csrf} aria-label="Sign out"><LogOut size={18} aria-hidden="true" /></button></form></div></header>
        <main id="main-content" className="workspace-content"><Outlet /></main>
        <footer className="content-footer">Workspace reads reflect server state. No changes can be made from this shell. <a href="/portal/classic">Classic workspace</a></footer>
      </div>
    </div>
  </>;
}
