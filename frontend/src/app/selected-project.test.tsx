import { QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory, RouterProvider } from "@tanstack/react-router";
import { act, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import type { Project } from "../services/api";
import { readWorkDetail } from "../services/api.fixtures";
import type { RealtimePush, RealtimeSocket } from "../services/realtime";
import { createApplication, type Application } from "./application";
import { portal } from "./portal-double";
import { createAppRouter } from "./router";

const firstProjectId = "11111111-1111-4111-8111-111111111111";
const secondProjectId = "22222222-2222-4222-8222-222222222222";
const subjectId = "77777777-7777-4777-8777-777777777777";
const subjectTitle = "Subject work item";

const projects: Project[] = [
  { id: firstProjectId, key: "ONE", name: "Project One", description: null, status: "active", work_items: [] },
  { id: secondProjectId, key: "TWO", name: "Project Two", description: null, status: "active", work_items: [] },
];

const workProjects: Project[] = [
  { id: firstProjectId, key: "ONE", name: "Project One", description: null, status: "active", work_items: [
    { id: subjectId, key: "ONE-9", title: subjectTitle, status: "ready", blocked: false, blocker: null },
  ] },
];

const applications: Application[] = [];
afterEach(() => {
  for (const application of applications) { application.realtime.close(); application.queryClient.clear(); }
  applications.length = 0;
});

function inertSocket(): RealtimeSocket {
  const push: RealtimePush = { receive: () => push };
  return { connect: () => undefined, disconnect: () => undefined, onOpen: () => undefined, onClose: () => undefined, onError: () => undefined, channel: () => ({ on: () => 1, join: () => push }) };
}

function renderScope(path: string) {
  const application = createApplication({
    location: { href: `http://localhost${path}`, replace: () => undefined },
    realtime: { socketFactory: () => inertSocket() },
  });
  applications.push(application);
  const history = createMemoryHistory({ initialEntries: [path] });
  const router = createAppRouter(application, history);
  const { unmount } = render(<QueryClientProvider client={application.queryClient}><RouterProvider router={router} /></QueryClientProvider>);
  return { application, history, router, unmount };
}

const navigation = () => within(screen.getByRole("navigation", { name: "Workspace navigation" }));
const picker = () => screen.getByLabelText("Project", { exact: true });

describe("selected project scope", () => {
  it("resolves the search member each page route carries (mutant: the chain drops the search hops)", async () => {
    for (const path of ["/portal/attention", "/portal/chat", "/portal/connections", "/portal/runtimes"]) {
      portal.seed({ session: { csrfToken: "session-token", socketToken: "test-socket-token" }, workspace: { selectedProjectId: firstProjectId, projects } });
      const { unmount } = renderScope(`${path}?projectId=${secondProjectId}`);
      await screen.findByRole("link", { name: "Runtimes" });
      for (const [route, label] of [["/portal/attention", "Attention"], ["/portal/chat", "Chat"], ["/portal/connections", "Connections"], ["/portal/runtimes", "Runtimes"]] as const) {
        expect(navigation().getByRole("link", { name: label })).toHaveAttribute("href", `${route}?projectId=${secondProjectId}`);
      }
      await waitFor(() => { expect(picker()).toHaveValue(secondProjectId); });
      unmount();
    }
  });

  it("resolves the project route's own parameter (mutant: the chain drops the parameter hop)", async () => {
    portal.seed({ session: { csrfToken: "session-token", socketToken: "test-socket-token" }, workspace: { selectedProjectId: firstProjectId, projects } });
    renderScope(`/portal/projects/${secondProjectId}`);
    await screen.findByRole("heading", { level: 1, name: "Project Two" });
    expect(navigation().getByRole("link", { name: "Attention" })).toHaveAttribute("href", `/portal/attention?projectId=${secondProjectId}`);
    expect(picker()).toHaveValue(secondProjectId);
  });

  it("reads the served work projection for a scope-less work URL (mutant: the chain drops the served projection hop)", async () => {
    portal.seed({
      session: { csrfToken: "session-token", socketToken: "test-socket-token" },
      workspace: { selectedProjectId: null, projects: workProjects },
      details: { [subjectId]: readWorkDetail({ id: subjectId, projectId: firstProjectId, title: subjectTitle }) },
    });
    renderScope(`/portal/work/${subjectId}?view=board`);
    const drawer = await screen.findByRole("dialog", { name: subjectTitle });
    expect(within(drawer).getByRole("heading", { level: 1, name: subjectTitle })).toBeVisible();
    await waitFor(() => { expect(picker()).toHaveValue(firstProjectId); });
  });

  it("keeps the URL claim while the served projection is pending (mutant: the chain drops the claimed-project hop)", async () => {
    const held = Promise.withResolvers<Response | undefined>();
    portal.seed({
      session: { csrfToken: "session-token", socketToken: "test-socket-token" },
      workspace: { selectedProjectId: secondProjectId, projects },
      details: { [subjectId]: readWorkDetail({ id: subjectId, projectId: firstProjectId, title: subjectTitle }) },
    });
    portal.serve(async (request) => {
      if (request.url.pathname !== `/portal/api/work-items/${subjectId}`) return undefined;
      await held.promise;
      return undefined;
    });
    renderScope(`/portal/work/${subjectId}?projectId=${secondProjectId}`);
    await waitFor(() => { expect(picker()).toHaveValue(secondProjectId); });
    await act(async () => { held.resolve(undefined); await held.promise; });
    await waitFor(() => { expect(picker()).toHaveValue(firstProjectId); });
  });

  it("leaves the scope undefined where the URL names none (mutant: the chain falls back to the served selection)", async () => {
    portal.seed({ session: { csrfToken: "session-token", socketToken: "test-socket-token" }, workspace: { selectedProjectId: firstProjectId, projects } });
    renderScope("/portal/runtimes");
    await screen.findByRole("option", { name: "All projects" });
    expect(picker()).toHaveValue("");
    expect(navigation().getByRole("link", { name: "Runtimes" })).toHaveAttribute("href", "/portal/runtimes");
    expect(portal.requests.some((request) => request.path === "/portal/api/workspace")).toBe(true);
    expect(portal.requests.some((request) => request.path.startsWith("/portal/api/workspace?"))).toBe(false);
  });

  it("issues exactly the base request set on a scoped route (mutant: the provider issues another read)", async () => {
    portal.seed({ session: { csrfToken: "session-token", socketToken: "test-socket-token" }, workspace: { selectedProjectId: firstProjectId, projects } });
    renderScope(`/portal/runtimes?projectId=${secondProjectId}`);
    await screen.findByRole("heading", { level: 1, name: "Runtimes" });
    await waitFor(() => { expect(picker()).toHaveValue(secondProjectId); });
    expect(portal.requests.filter((request) => request.path.startsWith("/portal/api/workspace")).map((request) => request.path)).toEqual([`/portal/api/workspace?project_id=${secondProjectId}`]);
    expect(portal.requests.filter((request) => request.path.startsWith("/portal/api/work-items"))).toHaveLength(0);
  });
});
