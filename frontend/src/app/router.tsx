import { createRootRouteWithContext, createRoute, createRouter, Link, redirect } from "@tanstack/react-router";
import type { Router, RouterHistory } from "@tanstack/react-router";
import { AttentionPage } from "../features/attention/attention";
import { ChatPage } from "../features/chat/chat";
import { ConnectionsPage } from "../features/connections/connections";
import { GoalDetailPage } from "../features/goals/detail";
import { workItemSearch, workSearch } from "../features/projects/filters";
import { ProjectPage, ProjectsPage } from "../features/projects/projects";
import { RuntimesPage } from "../features/runtimes/runtimes";
import { WorkDetailPage } from "../features/work/detail";
import type { Application } from "./application";
import { WorkspaceRoot } from "./shell";

const rootRoute = createRootRouteWithContext<{ application: Application }>()({
  component: WorkspaceRoot,
  notFoundComponent: () => <><h1 id="content-heading" tabIndex={-1}>Page not found</h1><p>This destination is not available in this workspace.</p><Link to="/attention" search={{}}>Go to Attention</Link></>,
});

const indexRoute = createRoute({ getParentRoute: () => rootRoute, path: "/", beforeLoad: (): void => { redirect({ to: "/attention", search: {}, throw: true }); } });
const attentionRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/attention",
  validateSearch: (search: Record<string, unknown>): { projectId?: string; unavailable?: string } => ({
    ...(typeof search["projectId"] === "string" && search["projectId"] ? { projectId: search["projectId"] } : {}),
    ...(typeof search["unavailable"] === "string" && search["unavailable"] ? { unavailable: search["unavailable"] } : {}),
  }),
  component: AttentionPage,
});
/** The scope members the conversation URL carries; a value the server's scope vocabulary does not name is dropped. */
type ChatScopeSearch = "workspace" | "project" | "run";
const chatScopeSearchValues: readonly string[] = ["workspace", "project", "run"];
const isChatScopeSearch = (value: unknown): value is ChatScopeSearch => typeof value === "string" && chatScopeSearchValues.includes(value);
const chatRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/chat",
  // The URL keeps the asked-for scope so a session expiry returns to the same
  // conversation; the read's own answer still owns the interpretation, and an
  // empty or unknown member is dropped rather than sent.
  validateSearch: (search: Record<string, unknown>): { scope?: ChatScopeSearch; projectId?: string; runId?: string } => ({
    ...(isChatScopeSearch(search["scope"]) ? { scope: search["scope"] } : {}),
    ...(typeof search["projectId"] === "string" && search["projectId"] ? { projectId: search["projectId"] } : {}),
    ...(typeof search["runId"] === "string" && search["runId"] ? { runId: search["runId"] } : {}),
  }),
  component: ChatPage,
});
const projectsRoute = createRoute({ getParentRoute: () => rootRoute, path: "/projects", component: ProjectsPage });
const projectRoute = createRoute({ getParentRoute: () => rootRoute, path: "/projects/$projectId", validateSearch: workSearch, component: ProjectPage });
const workItemRoute = createRoute({ getParentRoute: () => rootRoute, path: "/work/$workItemId", validateSearch: workItemSearch, component: WorkDetailPage });
const connectionsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/connections",
  // `projectId` scopes the project-resource surface on this route; the server
  // selected project is used when the URL carries none.
  validateSearch: (search: Record<string, unknown>): { connectionId?: string; projectId?: string } => ({
    ...(typeof search["connectionId"] === "string" && search["connectionId"] ? { connectionId: search["connectionId"] } : {}),
    ...(typeof search["projectId"] === "string" && search["projectId"] ? { projectId: search["projectId"] } : {}),
  }),
  component: ConnectionsPage,
});
const runtimesRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/runtimes",
  // The selected project is view state, like the Attention scope: it selects
  // which runtimes are compatible with the work this deployment holds.
  validateSearch: (search: Record<string, unknown>): { projectId?: string } =>
    typeof search["projectId"] === "string" && search["projectId"] ? { projectId: search["projectId"] } : {},
  component: RuntimesPage,
});
const goalRoute = createRoute({ getParentRoute: () => rootRoute, path: "/goals/$goalId", component: GoalDetailPage });
const routeTree = rootRoute.addChildren([indexRoute, attentionRoute, chatRoute, projectsRoute, projectRoute, workItemRoute, connectionsRoute, runtimesRoute, goalRoute]);

export type AppRouter = Router<typeof routeTree, "never", false, RouterHistory>;

export function createAppRouter(application: Application, history?: RouterHistory): AppRouter {
  return createRouter({ routeTree, context: { application }, basepath: "/portal", ...(history ? { history } : {}), defaultPreload: false, scrollRestoration: true });
}

declare module "@tanstack/react-router" {
  interface Register { router: AppRouter }
}
