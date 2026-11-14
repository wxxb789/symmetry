import { QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory, RouterProvider } from "@tanstack/react-router";
import { act, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { Project } from "../services/api";
import { createApplication, type Application } from "./application";
import { createAppRouter } from "./router";

const firstProjectId = "11111111-1111-4111-8111-111111111111";
const secondProjectId = "22222222-2222-4222-8222-222222222222";
const projects: Project[] = [
  { id: firstProjectId, key: "ONE", name: "Project One", description: null, status: "active", work_items: [] },
  { id: secondProjectId, key: "TWO", name: "Project Two", description: "Second project", status: "archived", work_items: [
    { id: "33333333-3333-4333-8333-333333333333", key: "TWO-1", title: "Resolve release blocker", status: "future_state", blocked: true, blocker: null },
  ] },
];
const applications: Application[] = [];
afterEach(() => { for (const app of applications) app.queryClient.clear(); applications.length = 0; });

function renderWorkspace(path: string, fetcher: typeof fetch = vi.fn<typeof fetch>().mockImplementation((input) => {
  const requestPath = input instanceof Request ? input.url : String(input);
  const projectId = new URL(requestPath, "http://localhost").searchParams.get("project_id") ?? firstProjectId;
  return Promise.resolve(new Response(JSON.stringify(requestPath.endsWith("/session")
    ? { csrf_token: "session-token" }
    : { selected_project_id: projectId, projects: projects.map((project) => ({ ...project, work_items: project.id === projectId ? project.work_items : [] })) })));
})) {
  const application = createApplication({ fetcher, location: { href: `http://localhost${path}`, replace: vi.fn() } });
  applications.push(application);
  const history = createMemoryHistory({ initialEntries: [path] });
  const router = createAppRouter(application, history);
  const { unmount } = render(<QueryClientProvider client={application.queryClient}><RouterProvider router={router} /></QueryClientProvider>);
  return { application, history, router, unmount };
}

describe("workspace navigation", () => {
  it("does not report unloaded project work as zero and opens project selection from the list", async () => {
    const user = userEvent.setup();
    const { history } = renderWorkspace("/portal/projects");
    await screen.findByRole("heading", { level: 2, name: "Project Two" });
    expect(screen.getByText("Open project to load work")).toBeVisible();
    expect(screen.getByRole("option", { name: "All projects" })).toHaveValue("");
    await user.selectOptions(screen.getByLabelText("Project"), secondProjectId);
    expect(await screen.findByRole("heading", { level: 1, name: "Project Two" })).toBeVisible();
    expect(history.location.pathname).toBe(`/portal/projects/${secondProjectId}`);
  });

  it("renders deep-linked archived work with honest unknown state and blocker labels", async () => {
    renderWorkspace(`/portal/projects/${secondProjectId}`);
    expect(await screen.findByRole("heading", { level: 1, name: "Project Two" })).toBeVisible();
    expect(screen.getByText("Resolve release blocker")).toBeVisible();
    expect(screen.getByText("Unknown status: future_state")).toBeVisible();
    expect(screen.getByText(/Blocking reason unknown/)).toBeVisible();
    expect(screen.getByText(/This project is archived/)).toBeVisible();
  });

  it("keeps Attention project scope in search, history and reload, and clears to the server default", async () => {
    const user = userEvent.setup();
    const { history, unmount } = renderWorkspace("/portal/attention");
    expect(await screen.findByRole("heading", { name: "No blocked work" })).toBeVisible();
    expect(screen.getByLabelText("Project")).toHaveValue(firstProjectId);
    expect(screen.getByText("Showing Project One.")).toBeVisible();

    await user.selectOptions(screen.getByLabelText("Project"), secondProjectId);
    expect(await screen.findByText("Resolve release blocker")).toBeVisible();
    expect(screen.getByRole("heading", { level: 1, name: "Attention" })).toBeVisible();
    expect(history.location.pathname).toBe("/portal/attention");
    expect(new URL(history.location.href, "http://localhost").searchParams.get("projectId")).toBe(secondProjectId);
    expect(screen.getByRole("link", { name: "Attention" })).toHaveAttribute("href", `/portal/attention?projectId=${secondProjectId}`);

    act(() => history.back());
    expect(await screen.findByRole("heading", { name: "No blocked work" })).toBeVisible();
    expect(screen.getByLabelText("Project")).toHaveValue(firstProjectId);
    act(() => history.forward());
    expect(await screen.findByText("Resolve release blocker")).toBeVisible();
    expect(screen.getByLabelText("Project")).toHaveValue(secondProjectId);

    const scopedPath = history.location.href;
    unmount();
    const reloaded = renderWorkspace(scopedPath);
    expect(await screen.findByText("Resolve release blocker")).toBeVisible();
    expect(screen.getByLabelText("Project")).toHaveValue(secondProjectId);
    expect(screen.getByRole("option", { name: "Server-selected project" })).toHaveValue("");
    await user.selectOptions(screen.getByLabelText("Project"), "");
    expect(await screen.findByRole("heading", { name: "No blocked work" })).toBeVisible();
    expect(reloaded.history.location.pathname).toBe("/portal/attention");
    expect(new URL(reloaded.history.location.href, "http://localhost").searchParams.has("projectId")).toBe(false);
    expect(screen.getByLabelText("Project")).toHaveValue(firstProjectId);
    expect(screen.getByText("Showing Project One.")).toBeVisible();
  });

  it("carries project detail scope into Attention navigation and command palette links", async () => {
    const user = userEvent.setup();
    const { history } = renderWorkspace(`/portal/projects/${secondProjectId}`);
    await screen.findByRole("heading", { level: 1, name: "Project Two" });
    const attentionLink = screen.getByRole("link", { name: "Attention" });
    expect(attentionLink).toHaveAttribute("href", `/portal/attention?projectId=${secondProjectId}`);
    await user.click(attentionLink);
    expect(await screen.findByRole("heading", { level: 1, name: "Attention" })).toBeVisible();
    expect(await screen.findByText("Resolve release blocker")).toBeVisible();

    act(() => history.back());
    await screen.findByRole("heading", { level: 1, name: "Project Two" });
    await user.click(screen.getByRole("button", { name: /Search or navigate/ }));
    const palette = await screen.findByRole("dialog", { name: "Command palette" });
    const paletteAttention = within(palette).getByRole("link", { name: "Attention" });
    expect(paletteAttention).toHaveAttribute("href", `/portal/attention?projectId=${secondProjectId}`);
    await user.click(paletteAttention);
    const heading = await screen.findByRole("heading", { level: 1, name: "Attention" });
    expect(await screen.findByText("Resolve release blocker")).toBeVisible();
    await waitFor(() => expect(heading).toHaveFocus());
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });

  it("keeps selection in history and focuses content after navigation, not refresh", async () => {
    const user = userEvent.setup();
    const { history } = renderWorkspace(`/portal/projects/${firstProjectId}`);
    await screen.findByRole("heading", { level: 1, name: "Project One" });
    await user.selectOptions(screen.getByLabelText("Project"), secondProjectId);
    const heading = await screen.findByRole("heading", { level: 1, name: "Project Two" });
    await waitFor(() => expect(heading).toHaveFocus());
    expect(history.location.pathname).toBe(`/portal/projects/${secondProjectId}`);
    const refresh = screen.getByRole("button", { name: "Refresh" });
    await user.click(refresh);
    await waitFor(() => expect(screen.getByRole("button", { name: "Refresh" })).toHaveFocus());
    act(() => history.back());
    await screen.findByRole("heading", { level: 1, name: "Project One" });
    expect(screen.getByLabelText("Project")).toHaveValue(firstProjectId);
  });

  it("ignores a late response from a project left during loading", async () => {
    const old = Promise.withResolvers<Response>();
    const fetcher = vi.fn<typeof fetch>().mockImplementation((input) => {
      const path = input instanceof Request ? input.url : String(input);
      if (path.endsWith("/session")) return Promise.resolve(new Response('{"csrf_token":"token"}'));
      if (path.includes(`project_id=${firstProjectId}`)) return old.promise;
      return Promise.resolve(new Response(JSON.stringify({ selected_project_id: secondProjectId, projects: [projects[1]] })));
    });
    const { router } = renderWorkspace(`/portal/projects/${firstProjectId}`, fetcher);
    await screen.findByText("Loading project…");
    await act(async () => { await router.navigate({ to: "/projects/$projectId", params: { projectId: secondProjectId } }); });
    await screen.findByRole("heading", { level: 1, name: "Project Two" });
    old.resolve(new Response(JSON.stringify({ selected_project_id: firstProjectId, projects: [projects[0]] })));
    await waitFor(() => expect(screen.queryByRole("heading", { level: 1, name: "Project One" })).not.toBeInTheDocument());
    expect(screen.getByRole("heading", { level: 1, name: "Project Two" })).toBeVisible();
  });

  it.each(["/portal/projects/missing", "/portal/attention?projectId=missing"])("keeps project navigation usable after a scoped read fails at %s", async (path) => {
    const user = userEvent.setup();
    const fetcher = vi.fn<typeof fetch>().mockImplementation((input) => {
      const requestPath = input instanceof Request ? input.url : String(input);
      if (requestPath.endsWith("/session")) return Promise.resolve(new Response('{"csrf_token":"token"}'));
      const projectId = new URL(requestPath, "http://localhost").searchParams.get("project_id") ?? firstProjectId;
      return Promise.resolve(projectId === "missing" ? new Response("{}", { status: 404 }) : new Response(JSON.stringify({
        selected_project_id: projectId,
        projects: projects.map((project) => ({ ...project, work_items: project.id === projectId ? project.work_items : [] })),
      })));
    });
    const { history } = renderWorkspace(path, fetcher);
    expect(await screen.findByRole("alert")).toHaveTextContent("The requested resource was not found");
    const selection = screen.getByLabelText("Project");
    await waitFor(() => expect(selection).toBeEnabled());
    expect(screen.getByRole("option", { name: "Project One" })).toBeVisible();
    await user.click(screen.getByRole("button", { name: /Search or navigate/ }));
    const palette = await screen.findByRole("dialog", { name: "Command palette" });
    await user.click(within(palette).getByRole("link", { name: /Project One/ }));
    expect(await screen.findByRole("heading", { level: 1, name: "Project One" })).toBeVisible();
    await user.selectOptions(selection, secondProjectId);
    expect(await screen.findByRole("heading", { level: 1, name: "Project Two" })).toBeVisible();
    expect(history.location.pathname).toBe(`/portal/projects/${secondProjectId}`);
  });

  it("returns keyboard focus when the command palette closes with Escape", async () => {
    const user = userEvent.setup();
    renderWorkspace("/portal/attention");
    await screen.findByRole("heading", { level: 1, name: "Attention" });
    const projectSelection = screen.getByLabelText("Project");
    await waitFor(() => expect(projectSelection).toBeEnabled());
    projectSelection.focus();
    expect(projectSelection).toHaveFocus();
    await user.keyboard("{Control>}k{/Control}");
    expect(await screen.findByRole("dialog", { name: "Command palette" })).toBeVisible();
    await user.keyboard("{Escape}");
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    expect(projectSelection).toHaveFocus();
  });

  it("shows a recoverable error and renders empty data only after successful retry", async () => {
    let workspaceRead = 0;
    const fetcher = vi.fn<typeof fetch>().mockImplementation((input) => {
      if ((input instanceof Request ? input.url : String(input)).endsWith("/session")) return Promise.resolve(new Response('{"csrf_token":"token"}'));
      workspaceRead += 1;
      return Promise.resolve(workspaceRead === 1 ? new Response("{}", { status: 403 }) : new Response('{"selected_project_id":null,"projects":[]}'));
    });
    renderWorkspace("/portal/projects", fetcher);
    expect(await screen.findByRole("alert")).toHaveTextContent("You do not have access");
    expect(screen.queryByText("No projects yet")).not.toBeInTheDocument();
    await userEvent.setup().click(screen.getByRole("button", { name: "Try again" }));
    expect(await screen.findByRole("heading", { name: "No projects yet" })).toBeVisible();
  });

  it("removes private shell content when bootstrap authentication expires", async () => {
    renderWorkspace("/portal/attention", vi.fn<typeof fetch>().mockResolvedValue(new Response("{}", { status: 401 })));
    expect(await screen.findByRole("heading", { name: "Signed out" })).toBeVisible();
    expect(screen.queryByRole("navigation")).not.toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Sign in" })).toHaveAttribute("href", "/portal/login");
  });

  it("keeps the operator's focus when a background session read fails and then recovers", async () => {
    const sessionReads: Array<(response: Response) => void> = [];
    const fetcher = vi.fn<typeof fetch>().mockImplementation((input) => {
      const path = input instanceof Request ? input.url : String(input);
      if (path.endsWith("/session")) {
        const { promise, resolve } = Promise.withResolvers<Response>();
        sessionReads.push(resolve);
        return promise;
      }
      return Promise.resolve(new Response(JSON.stringify({ selected_project_id: firstProjectId, projects: [projects[0]] })));
    });
    const { application } = renderWorkspace("/portal/attention", fetcher);
    // The 503 must settle into an error state before recovery; transient retries
    // would keep the fetch in flight and mask the focus transition under test.
    application.queryClient.setQueryDefaults(["session"], { retry: false });
    await vi.waitFor(() => expect(sessionReads.length).toBeGreaterThan(0));
    sessionReads[0]?.(new Response('{"csrf_token":"cached-token"}'));
    await screen.findByRole("heading", { level: 1, name: "Attention" });
    const selection = screen.getByLabelText("Project");
    await waitFor(() => expect(selection).toBeEnabled());
    selection.focus();
    expect(selection).toHaveFocus();
    // A background session refetch fails while the cached session still renders the shell.
    act(() => { window.dispatchEvent(new Event("visibilitychange")); });
    await vi.waitFor(() => expect(sessionReads.length).toBeGreaterThan(1));
    sessionReads[1]?.(new Response("{}", { status: 503 }));
    await act(async () => { const { promise, resolve } = Promise.withResolvers<void>(); setTimeout(resolve, 20); await promise; });
    expect(screen.getByRole("heading", { level: 1, name: "Attention" })).toBeVisible();
    expect(selection).toHaveFocus();
    // The read then recovers; the URL is unchanged and the router stays idle.
    act(() => { window.dispatchEvent(new Event("visibilitychange")); });
    await vi.waitFor(() => expect(sessionReads.length).toBeGreaterThan(2));
    sessionReads[2]?.(new Response('{"csrf_token":"recovered-token"}'));
    await act(async () => { const { promise, resolve } = Promise.withResolvers<void>(); setTimeout(resolve, 20); await promise; });
    expect(selection).toHaveFocus();
    expect(screen.getByRole("button", { name: "Sign out" })).toBeEnabled();
  });

  it("uses the current bootstrap CSRF token for logout after session refetch, not the HTML token", async () => {
    const meta = document.createElement("meta");
    meta.name = "csrf-token";
    meta.content = "old-html-token";
    document.head.append(meta);
    let csrfToken = "initial-session-token";
    const fetcher = vi.fn<typeof fetch>().mockImplementation((input) => Promise.resolve(new Response(JSON.stringify(
      (input instanceof Request ? input.url : String(input)).endsWith("/session")
        ? { csrf_token: csrfToken }
        : { selected_project_id: firstProjectId, projects: [projects[0]] },
    ))));
    try {
      const { application } = renderWorkspace("/portal/attention", fetcher);
      await screen.findByRole("heading", { level: 1, name: "Attention" });
      const form = screen.getByRole("button", { name: "Sign out" }).closest("form");
      expect(form).toHaveAttribute("action", "/portal/logout");
      expect(form).toHaveAttribute("method", "post");
      expect(form?.querySelector('input[name="_csrf_token"]')).toHaveValue("initial-session-token");
      csrfToken = "rotated-session-token";
      await act(async () => { await application.queryClient.refetchQueries({ queryKey: ["session"], exact: true }); });
      await waitFor(() => expect(form?.querySelector('input[name="_csrf_token"]')).toHaveValue("rotated-session-token"));
      expect(screen.getByRole("button", { name: "Sign out" })).toBeEnabled();
    } finally {
      meta.remove();
    }
  });

  it("awaits the current session before the native logout POST instead of submitting a stale token", async () => {
    const user = userEvent.setup();
    let csrfToken = "initial-session-token";
    let sessionRead = 0;
    const { promise: bootstrap, resolve: releaseBootstrap } = Promise.withResolvers<Response>();
    const fetcher = vi.fn<typeof fetch>().mockImplementation((input) => {
      const path = input instanceof Request ? input.url : String(input);
      if (!path.endsWith("/session")) return Promise.resolve(new Response(JSON.stringify({ selected_project_id: firstProjectId, projects: [projects[0]] })));
      sessionRead += 1;
      return sessionRead === 1
        ? Promise.resolve(new Response(JSON.stringify({ csrf_token: csrfToken })))
        : bootstrap.then(() => new Response(JSON.stringify({ csrf_token: csrfToken })));
    });
    renderWorkspace("/portal/attention", fetcher);
    await screen.findByRole("heading", { level: 1, name: "Attention" });
    expect(screen.getByRole("button", { name: "Sign out" })).toBeEnabled();
    // Another window logs out and back in, rotating the shared cookie's token;
    // both windows stay visible, so no visibility event refreshes this one.
    csrfToken = "rotated-session-token";
    const form = screen.getByRole("button", { name: "Sign out" }).closest("form");
    expect(form).not.toBeNull();
    // form.submit() is the one native POST; it bypasses submit events, so spy on it
    // and capture the hidden token at the moment of the call.
    let postedToken: string | null = null;
    const nativeSubmit = vi.fn(() => {
      postedToken = form!.querySelector<HTMLInputElement>('input[name="_csrf_token"]')?.value ?? null;
    });
    form!.submit = nativeSubmit;
    // While the fresh bootstrap read is still pending, a click must not POST yet.
    await user.click(screen.getByRole("button", { name: "Sign out" }));
    expect(nativeSubmit).not.toHaveBeenCalled();
    releaseBootstrap(new Response(JSON.stringify({ csrf_token: csrfToken })));
    await vi.waitFor(() => {
      if (postedToken === null) throw new Error("native logout POST not reached");
    });
    expect(nativeSubmit).toHaveBeenCalledTimes(1);
    expect(postedToken).toBe("rotated-session-token");
  });
});
