import { createRootRouteWithContext, createRoute, createRouter, Link, redirect } from "@tanstack/react-router";
import type { Router, RouterHistory } from "@tanstack/react-router";
import { AttentionPage } from "../features/attention/attention";
import { workItemSearch, workSearch } from "../features/projects/filters";
import { ProjectPage, ProjectsPage } from "../features/projects/projects";
import { WorkDetailPage } from "../features/work/detail";
import type { Application } from "./application";
import { WorkspaceRoot } from "./shell";

const rootRoute = createRootRouteWithContext<{ application: Application }>()({
  component: WorkspaceRoot,
  notFoundComponent: () => <><h1 id="content-heading" tabIndex={-1}>Page not found</h1><p>This destination is not available in the read-only workspace.</p><Link to="/attention" search={{}}>Go to Attention</Link></>,
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
const projectsRoute = createRoute({ getParentRoute: () => rootRoute, path: "/projects", component: ProjectsPage });
const projectRoute = createRoute({ getParentRoute: () => rootRoute, path: "/projects/$projectId", validateSearch: workSearch, component: ProjectPage });
const workItemRoute = createRoute({ getParentRoute: () => rootRoute, path: "/work/$workItemId", validateSearch: workItemSearch, component: WorkDetailPage });
const routeTree = rootRoute.addChildren([indexRoute, attentionRoute, projectsRoute, projectRoute, workItemRoute]);

export type AppRouter = Router<typeof routeTree, "never", false, RouterHistory>;

export function createAppRouter(application: Application, history?: RouterHistory): AppRouter {
  return createRouter({ routeTree, context: { application }, basepath: "/portal", ...(history ? { history } : {}), defaultPreload: false, scrollRestoration: true });
}

declare module "@tanstack/react-router" {
  interface Register { router: AppRouter }
}
