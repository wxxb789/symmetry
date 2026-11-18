import * as Dialog from "@radix-ui/react-dialog";
import { useQuery } from "@tanstack/react-query";
import { Link, Outlet, useMatch, useNavigate, useRouteContext, useRouterState } from "@tanstack/react-router";
import { Cable, Command, Folder, Inbox, LogOut, Menu, MessageSquare, MonitorCog, Search, X } from "lucide-react";
import { useEffect, useLayoutEffect, useRef, useState, useSyncExternalStore } from "react";
import { errorMessage } from "../services/api";
import type { RefreshSource, RealtimeStatus } from "../services/realtime";
import { ErrorState, LoadingState } from "../components/workspace";
import { sessionOptions, workDetailOptions, workspaceOptions, type Application } from "./application";
import { useRealtimeRefresh, useRealtimeStatus } from "./realtime";
import { SessionTokenProvider } from "./session";

function focusContent() {
  requestAnimationFrame(() => { document.getElementById("content-heading")?.focus({ preventScroll: true }); });
}

/** How the invalidation client's own status reads to an operator. */
const connectionLabels: Record<RealtimeStatus, string> = {
  connected: "Connected",
  connecting: "Connecting…",
  disconnected: "Offline, polling"
};

/** Which of the client's own events refreshed every read on screen. */
const refreshLabels: Record<RefreshSource, string> = {
  connection: "the connection opening",
  join: "the channel join",
  fallback: "the polling fallback",
  visibility: "the tab's return",
  invalidation: "a published change"
};

/**
 * The reads' connection and freshness, stated from the invalidation client's own
 * state: which connection serves the reads, which of the client's own events last
 * refreshed every read, and when the reads that pass issued finished computing.
 * A degraded connection is what an operator needs to see before blaming the data.
 *
 * Every member here is written where the work happens — the status by the client's
 * own socket hooks, the refresh record by the pass that issues the reads — so the
 * element cannot claim a refresh that no read pass performed, and it names the
 * pass rather than guessing at its outcome: a refused read is the surface's own
 * `Refresh failed — showing last loaded data`. It is deliberately not a live
 * region: it advances on every fallback tick, and a screen reader should not read
 * a timestamp every five seconds.
 */
function ReadsStatus() {
  const status = useRealtimeStatus();
  const refresh = useRealtimeRefresh();
  const freshness = refresh === null
    ? "no reads refreshed yet"
    : refresh.settledAt === null
      ? "refreshing reads"
      : `refreshed ${new Date(refresh.settledAt).toLocaleTimeString()}`;
  return <p className="reads-status" data-connection={status}
    data-refresh-source={refresh?.source ?? ""}
    data-refresh-issued-at={refresh === null ? "" : String(refresh.issuedAt)}
    data-refresh-settled-at={refresh === null || refresh.settledAt === null ? "" : String(refresh.settledAt)}
    title={refresh === null
      ? "The reads on screen have not been refreshed by the invalidation client yet"
      : `Every read was last refreshed by ${refreshLabels[refresh.source]}`}>
    {connectionLabels[status]} · <span className="reads-freshness">{freshness}</span>
    {refresh !== null && <span className="reads-source"> ({refreshLabels[refresh.source]})</span>}
  </p>;
}

function useProjectScope() {
  const { application } = useRouteContext({ from: "__root__" });
  const projectId = useMatch({ from: "/projects/$projectId", shouldThrow: false, select: (match) => match.params.projectId });
  const workScope = useMatch({ from: "/work/$workItemId", shouldThrow: false, select: (match) => ({ workItemId: match.params.workItemId, claimedProjectId: match.search.projectId }) });
  // The served projection owns a work item's project. This reads the same query key
  // the work detail page uses, so it adds no request and no second authority; the
  // URL's claim only covers the window before that projection arrives.
  const servedWork = useQuery({ ...workDetailOptions(application.api, workScope?.workItemId ?? ""), enabled: workScope !== undefined });
  const searchProjectId = useMatch({ from: "/attention", shouldThrow: false, select: (match) => match.search.projectId });
  const chatProjectId = useMatch({ from: "/chat", shouldThrow: false, select: (match) => match.search.projectId });
  const connectionsProjectId = useMatch({ from: "/connections", shouldThrow: false, select: (match) => match.search.projectId });
  const runtimesProjectId = useMatch({ from: "/runtimes", shouldThrow: false, select: (match) => match.search.projectId });
  return projectId ?? servedWork.data?.work_item.project_id ?? workScope?.claimedProjectId ?? searchProjectId ?? chatProjectId ?? connectionsProjectId ?? runtimesProjectId;
}

function useProjectDirectory() {
  const { application } = useRouteContext({ from: "__root__" });
  const projectId = useProjectScope();
  const query = useQuery(workspaceOptions(application.api, projectId ?? null));
  const recovery = useQuery({ ...workspaceOptions(application.api, null), enabled: !!projectId && query.isError });
  return projectId && query.isError ? recovery : query;
}

function NavigationLinks({ onNavigate }: { onNavigate?: () => void }) {
  const projectId = useProjectScope();
  return <nav aria-label="Workspace navigation" className="nav-links">
    <Link to="/attention" search={projectId ? { projectId } : {}} onClick={onNavigate} activeProps={{ className: "nav-link active", "aria-current": "page" }} inactiveProps={{ className: "nav-link" }}><Inbox size={18} aria-hidden="true" />Attention</Link>
    <Link to="/chat" search={projectId ? { projectId } : {}} onClick={onNavigate} activeProps={{ className: "nav-link active", "aria-current": "page" }} inactiveProps={{ className: "nav-link" }}><MessageSquare size={18} aria-hidden="true" />Chat</Link>
    <Link to="/projects" onClick={onNavigate} activeProps={{ className: "nav-link active", "aria-current": "page" }} inactiveProps={{ className: "nav-link" }}><Folder size={18} aria-hidden="true" />Projects</Link>
    <Link to="/connections" search={projectId ? { projectId } : {}} onClick={onNavigate} activeProps={{ className: "nav-link active", "aria-current": "page" }} inactiveProps={{ className: "nav-link" }}><Cable size={18} aria-hidden="true" />Connections</Link>
    <Link to="/runtimes" search={projectId ? { projectId } : {}} onClick={onNavigate} activeProps={{ className: "nav-link active", "aria-current": "page" }} inactiveProps={{ className: "nav-link" }}><MonitorCog size={18} aria-hidden="true" />Runtimes</Link>
  </nav>;
}

function ProjectPicker() {
  const query = useProjectDirectory();
  const navigate = useNavigate();
  const pathname = useRouterState({ select: (state) => state.location.pathname });
  const projectId = useProjectScope();
  const isAttention = pathname.endsWith("/attention");
  const isConnections = pathname.endsWith("/connections");
  const selected = projectId ?? (isAttention || isConnections ? query.data?.selected_project_id ?? "" : "");
  return <div className="project-picker"><label htmlFor="project-selection">Project</label>
    <select id="project-selection" aria-label="Project" value={selected} disabled={!query.data} onChange={(event) => {
      const projectId = event.currentTarget.value;
      if (isConnections) void navigate({ to: "/connections", search: projectId ? { projectId } : {} });
      else if (isAttention) void navigate({ to: "/attention", search: projectId ? { projectId } : {} });
      else if (projectId) void navigate({ to: "/projects/$projectId", params: { projectId } });
      else void navigate({ to: "/projects" });
    }}>
      <option value="">{query.isPending ? "Loading projects…" : query.isError && !query.data ? "Projects unavailable" : isAttention || isConnections ? "Server-selected project" : "All projects"}</option>
      {selected && !query.data?.projects.some((project) => project.id === selected) && <option value={selected}>Unavailable project</option>}
      {query.data?.projects.map((project) => <option key={project.id} value={project.id}>{project.name}{project.status === "archived" ? " (archived)" : ""}</option>)}
    </select>
  </div>;
}

function CommandPalette() {
  const query = useProjectDirectory();
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
        <Dialog.Description className="muted">Navigate this workspace. Use Tab to select a result and Enter to open it.</Dialog.Description>
        <label className="sr-only" htmlFor="command-search">Search destinations</label>
        <input id="command-search" className="command-input" type="search" placeholder="Search projects and pages…" value={search} onChange={(event) => setSearch(event.currentTarget.value)} />
        <div className="command-results">
          {(!filter || "attention".includes(filter)) && <Link className="command-result" to="/attention" search={projectId ? { projectId } : {}} onClick={closeForNavigation}><Inbox size={18} aria-hidden="true" />Attention</Link>}
          {(!filter || "chat".includes(filter)) && <Link className="command-result" to="/chat" search={projectId ? { projectId } : {}} onClick={closeForNavigation}><MessageSquare size={18} aria-hidden="true" />Chat</Link>}
          {(!filter || "projects".includes(filter)) && <Link className="command-result" to="/projects" onClick={closeForNavigation}><Folder size={18} aria-hidden="true" />All projects</Link>}
          {(!filter || "connections".includes(filter)) && <Link className="command-result" to="/connections" search={projectId ? { projectId } : {}} onClick={closeForNavigation}><Cable size={18} aria-hidden="true" />Connections</Link>}
          {(!filter || "runtimes".includes(filter)) && <Link className="command-result" to="/runtimes" search={projectId ? { projectId } : {}} onClick={closeForNavigation}><MonitorCog size={18} aria-hidden="true" />Runtimes</Link>}
          {projects.map((project) => <Link className="command-result" key={project.id} to="/projects/$projectId" params={{ projectId: project.id }} onClick={closeForNavigation}><Folder size={18} aria-hidden="true" /><span>{project.name}<span className="item-key"> {project.key}</span></span></Link>)}
          {filter && projects.length === 0 && !"attention".includes(filter) && !"chat".includes(filter) && !"projects".includes(filter) && !"connections".includes(filter) && !"runtimes".includes(filter) && <p className="muted" role="status">No matching destinations.</p>}
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
  // The bootstrap read is the only minter of the short-lived socket token, so the
  // invalidation client connects when that token arrives (and re-connects with a
  // fresh one after a logout/login rotation). Until it is connected the same
  // client polls, so no surface depends on the socket being up.
  const socketToken = session.data?.socket_token ?? "";
  useEffect(() => {
    if (socketToken === "" || signedOut) return;
    application.rememberSocketToken(socketToken);
    application.realtime.connect();
  }, [application, socketToken, signedOut]);
  // The invalidation client's lifetime is the signed-in shell's, not the bootstrap
  // token's: the effect above re-runs on a rotated token without reopening the
  // socket, so its release is keyed to the mount and to the session ending. A shell
  // that was torn down — or one whose session ended — must leave no socket
  // reconnecting (the client's own retry reuses the absolute endpoint it was built
  // with, so it needs no page to re-resolve one) and no polling fallback reading
  // with a token nobody re-mints.
  useEffect(() => () => { application.realtime.close(); }, [application, signedOut]);
  // Route changes own the heading reset. Search-parameter updates (the
  // URL-backed filters) are not route changes: keying the reset on the full
  // href moved focus to the heading on every keystroke of a filter input.
  const pathname = useRouterState({ select: (state) => state.location.pathname });
  const navigationStatus = useRouterState({ select: (state) => state.status });
  const ready = session.isSuccess && !signedOut;
  const focusedPathname = useRef<string | null>(null);
  useLayoutEffect(() => {
    if (!ready || navigationStatus !== "idle") return;
    if (focusedPathname.current === pathname) return;
    focusedPathname.current = pathname;
    document.getElementById("content-heading")?.focus({ preventScroll: true });
  }, [pathname, navigationStatus, ready]);
  if (signedOut) return <main className="session-state"><h1>Signed out</h1><p role="status">Your session has ended. Redirecting to sign in…</p><a className="button" href="/portal/login">Sign in</a></main>;
  if (session.isPending) return <main className="session-state"><h1>Symmetry workspace</h1><LoadingState label="Checking your session…" /></main>;
  if (session.isError && !session.data) return <main className="session-state"><h1>Workspace unavailable</h1><ErrorState error={session.error} retry={() => { void session.refetch(); }} /></main>;
  const csrf = session.data?.csrf_token;
  return <>
    <a className="skip-link" href="#content-heading">Skip to content</a>
    <div className="workspace-shell">
      <aside className="sidebar"><Link className="brand" to="/attention" search={{}}><span className="brand-mark" aria-hidden="true">S</span>Symmetry</Link><p className="sidebar-label">Workspace</p><NavigationLinks /><div className="sidebar-footer"><span className="state-dot" aria-hidden="true" />Reads and execution control; applied state comes from the server</div></aside>
      <div className="workspace-body"><header className="topbar"><div className="topbar-start"><MobileNavigation /><ProjectPicker /></div><ReadsStatus /><div className="topbar-actions"><CommandPalette /><LogoutForm application={application} cachedToken={csrf ?? ""} /></div></header>
        <main id="main-content" className="workspace-content"><SessionTokenProvider token={csrf ?? ""}><Outlet /></SessionTokenProvider></main>
        <footer className="content-footer">Reads and applied changes come from the server; a refused change shows the server's current state. Work detail carries the execution controls this shell dispatches; guidance and pause are dispatched from Chat.</footer>
      </div>
    </div>
  </>;
}

function LogoutForm({ application, cachedToken }: { application: Application; cachedToken: string }) {
  const [submitting, setSubmitting] = useState(false);
  const [logoutError, setLogoutError] = useState<string | null>(null);
  const formRef = useRef<HTMLFormElement>(null);
  // The cached bootstrap token can predate a logout/login in another window that
  // rotated the shared session cookie. Await the server's current token, then
  // issue exactly one native POST with it instead of submitting a stale token.
  const submit = async (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (submitting) return;
    setSubmitting(true);
    setLogoutError(null);
    try {
      await application.queryClient.invalidateQueries({ queryKey: ["session"] });
      const session = await application.queryClient.fetchQuery(sessionOptions(application.api));
      const form = formRef.current;
      const tokenInput = form?.querySelector<HTMLInputElement>('input[name="_csrf_token"]');
      if (!form || !tokenInput) return;
      tokenInput.value = session.csrf_token;
      setSubmitting(false);
      form.submit();
    } catch (cause) {
      setSubmitting(false);
      setLogoutError(errorMessage(cause));
    }
  };
  return <form className="topbar-form" ref={formRef} action="/portal/logout" method="post" onSubmit={(event) => { void submit(event); }}>
    <input type="hidden" name="_csrf_token" value={cachedToken} />
    <button className="icon-button" type="submit" aria-label="Sign out" disabled={submitting || !cachedToken}><LogOut size={18} aria-hidden="true" /></button>
    {logoutError && <p className="notice notice-error logout-error" role="alert">{logoutError} <button className="button" type="button" onClick={() => setLogoutError(null)}>Try again</button></p>}
  </form>;
}
