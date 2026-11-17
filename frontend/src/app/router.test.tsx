import { QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory, RouterProvider } from "@tanstack/react-router";
import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { savedViewStorageKey } from "../features/projects/saved-views";
import type { Project } from "../services/api";
import { readAttention, readWorkDetail, readWorkItem } from "../services/api.fixtures";
import { createApplication, safeReturnTo, type Application } from "./application";
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
afterEach(() => { for (const app of applications) app.queryClient.clear(); applications.length = 0; localStorage.clear(); });

function renderWorkspace(path: string, fetcher: typeof fetch = vi.fn<typeof fetch>().mockImplementation((input) => {
  const requestPath = input instanceof Request ? input.url : String(input);
  const url = new URL(requestPath, "http://localhost");
  const projectId = url.searchParams.get("project_id") ?? firstProjectId;
  if (url.pathname === "/portal/api/attention") {
    const scoped = url.searchParams.get("project_id") ?? firstProjectId;
    const project = projects.find((candidate) => candidate.id === scoped);
    return Promise.resolve(new Response(JSON.stringify(readAttention({
      selectedProjectId: scoped,
      blocked: (project?.work_items ?? []).filter((item) => item.blocked).map((item) => ({ projectId: scoped, projectName: project?.name ?? null, workItemId: item.id, key: item.key, title: item.title, status: item.status, blocker: item.blocker })),
    }))));
  }
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
    // The work-state filter also renders this label for an unknown served state,
    // so the honest-state assertion is scoped to the rendered work list.
    expect(within(screen.getByRole("list", { name: "Work items" })).getByText("Unknown status: future_state")).toBeVisible();
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

const subjectId = "77777777-7777-4777-8777-777777777777";
const subjectTitle = "Subject work item";
const siblingId = "88888888-8888-4888-8888-888888888888";
const siblingTitle = "Subject backlog item";
const foreignTitle = "Foreign work item";
const workViewProjects: Project[] = [
  { id: firstProjectId, key: "ONE", name: "Project One", description: null, status: "active", work_items: [
    { id: subjectId, key: "ONE-9", title: subjectTitle, status: "ready", blocked: false, blocker: null },
    { id: siblingId, key: "ONE-10", title: siblingTitle, status: "backlog", blocked: false, blocker: null },
  ] },
  { id: secondProjectId, key: "TWO", name: "Project Two", description: "Second project", status: "archived", work_items: [
    { id: "99999999-9999-4999-8999-999999999999", key: "TWO-1", title: foreignTitle, status: "future_state", blocked: true, blocker: null },
  ] },
];

/** Serves the project snapshot per requested scope plus one real work-detail envelope. */
function workReadFetcher({ detail, holdDetail }: { detail: Record<string, unknown>; holdDetail?: Promise<void> }) {
  return vi.fn<typeof fetch>().mockImplementation((input) => {
    const requestPath = input instanceof Request ? input.url : String(input);
    if (requestPath.endsWith("/session")) return Promise.resolve(new Response(JSON.stringify({ csrf_token: "session-token" })));
    if (requestPath.includes("/portal/api/work-items/")) {
      const respond = () => new Response(requestPath.endsWith("/timeline")
        ? JSON.stringify({ items: [], next_before: null })
        : JSON.stringify(detail));
      return holdDetail === undefined ? Promise.resolve(respond()) : holdDetail.then(respond);
    }
    const projectId = new URL(requestPath, "http://localhost").searchParams.get("project_id");
    return Promise.resolve(new Response(JSON.stringify({
      selected_project_id: null,
      projects: workViewProjects.map((project) => ({ ...project, work_items: project.id === projectId ? project.work_items : [] })),
      health: { status: "unknown" },
    })));
  });
}

const subjectDetail = () => readWorkDetail({ id: subjectId, projectId: firstProjectId, title: subjectTitle });

/**
 * Serves the two-item project snapshot plus the real work-detail envelope. The
 * subject's status is read through `subjectStatus`, so a refetch can regroup the
 * board (and reorder the list) the way a workspace refresh does.
 */
function movableSubjectFetcher(subjectStatus: () => string) {
  return vi.fn<typeof fetch>().mockImplementation((input) => {
    const requestPath = input instanceof Request ? input.url : String(input);
    if (requestPath.endsWith("/session")) return Promise.resolve(new Response(JSON.stringify({ csrf_token: "session-token" })));
    if (requestPath.includes("/portal/api/work-items/")) {
      return Promise.resolve(new Response(requestPath.endsWith("/timeline")
        ? JSON.stringify({ items: [], next_before: null })
        : JSON.stringify(subjectDetail())));
    }
    const projectId = new URL(requestPath, "http://localhost").searchParams.get("project_id");
    return Promise.resolve(new Response(JSON.stringify({
      selected_project_id: null,
      projects: [{ id: firstProjectId, key: "ONE", name: "Project One", description: null, status: "active",
        work_items: projectId === firstProjectId ? [
          { id: subjectId, key: "ONE-9", title: subjectTitle, status: subjectStatus(), blocked: false, blocker: null },
          { id: siblingId, key: "ONE-10", title: siblingTitle, status: "backlog", blocked: false, blocker: null },
        ] : [] }],
      health: { status: "unknown" },
    })));
  });
}

describe("readable work views", () => {
  it("mounts the desktop drawer while the detail is pending and renders no unverified project background", async () => {
    const release = Promise.withResolvers<void>();
    renderWorkspace(`/portal/work/${subjectId}`, workReadFetcher({ detail: subjectDetail(), holdDetail: release.promise }));
    const pending = await screen.findByRole("dialog", { name: "Work item" });
    expect(within(pending).getByText("Loading work item…")).toBeVisible();
    // Radix marks the page behind the modal inert (aria-hidden) while the drawer is
    // open, so the background is asserted as hidden DOM: it must not exist before
    // the served projection confirms which project the item belongs to.
    expect(screen.queryByRole("list", { name: "Work items", hidden: true })).not.toBeInTheDocument();
    release.resolve();
    const detail = await screen.findByRole("dialog", { name: subjectTitle });
    expect(within(detail).getByRole("heading", { level: 1, name: subjectTitle })).toBeVisible();
    expect(within(await screen.findByRole("list", { name: "Work items", hidden: true })).getByText(subjectTitle)).toBeVisible();
  });

  it("keeps the desktop drawer and the supplied view state for a work URL without project scope", async () => {
    const { history } = renderWorkspace(`/portal/work/${subjectId}?view=board`, workReadFetcher({ detail: subjectDetail() }));
    const detail = await screen.findByRole("dialog", { name: subjectTitle });
    expect(within(detail).getByRole("heading", { level: 1, name: subjectTitle })).toBeVisible();
    const board = await screen.findByRole("region", { name: "Work board", hidden: true });
    expect(within(board).getByText(subjectTitle)).toBeVisible();
    // The shell's picker follows the served identity for a scope-less work URL.
    await waitFor(() => expect(screen.getByLabelText("Project", { exact: true })).toHaveValue(firstProjectId));
    const url = new URL(history.location.href, "http://localhost");
    expect(url.searchParams.get("view")).toBe("board");
    expect(url.searchParams.has("projectId")).toBe(false);
  });

  it("canonicalizes a foreign project scope instead of showing the wrong project's work", async () => {
    const { history } = renderWorkspace(
      `/portal/work/${subjectId}?projectId=${secondProjectId}&q=Foreign&state=future_state`,
      workReadFetcher({ detail: subjectDetail() }),
    );
    const detail = await screen.findByRole("dialog", { name: subjectTitle });
    expect(within(detail).getByRole("heading", { level: 1, name: subjectTitle })).toBeVisible();
    await waitFor(() => {
      expect(new URL(history.location.href, "http://localhost").searchParams.get("projectId")).toBe(firstProjectId);
    });
    const url = new URL(history.location.href, "http://localhost");
    expect(url.searchParams.has("q")).toBe(false);
    expect(url.searchParams.has("state")).toBe(false);
    expect(screen.getByLabelText("Project", { exact: true })).toHaveValue(firstProjectId);
    expect(within(await screen.findByRole("list", { name: "Work items", hidden: true })).getByText(subjectTitle)).toBeVisible();
    expect(screen.queryByText(foreignTitle)).not.toBeInTheDocument();
  });

  it("keeps a URL filter visible when the current snapshot no longer contains that state or owner", async () => {
    renderWorkspace(`/portal/projects/${firstProjectId}?state=future_state&owner=human:Ada`, workReadFetcher({ detail: subjectDetail() }));
    await screen.findByRole("heading", { level: 1, name: "Project One" });
    expect(screen.getByLabelText("Work state")).toHaveValue("future_state");
    expect(screen.getByLabelText("Work owner")).toHaveValue("human:Ada");
    expect(screen.getByRole("option", { name: "Unknown status: future_state (not in this project)" })).toBeVisible();
    expect(screen.getByRole("option", { name: "Ada (not in this project)" })).toBeVisible();
    expect(screen.getByRole("heading", { name: "No matching work items" })).toBeVisible();
  });

  it("applies rapid compound filter changes on the latest search instead of a stale render", async () => {
    const { history } = renderWorkspace(`/portal/projects/${firstProjectId}`, workReadFetcher({ detail: subjectDetail() }));
    await screen.findByRole("heading", { level: 1, name: "Project One" });
    // Both changes land in the same tick, before either navigation commits.
    act(() => {
      fireEvent.change(screen.getByLabelText("Filter work items"), { target: { value: "Subject" } });
      fireEvent.change(screen.getByLabelText("Work state"), { target: { value: "ready" } });
    });
    await waitFor(() => {
      const url = new URL(history.location.href, "http://localhost");
      expect(url.searchParams.get("q")).toBe("Subject");
      expect(url.searchParams.get("state")).toBe("ready");
    });
    const list = screen.getByRole("list", { name: "Work items" });
    expect(within(list).getByText(subjectTitle)).toBeVisible();
    expect(within(list).queryByText(siblingTitle)).not.toBeInTheDocument();
  });

  it("keeps focus and every keystroke in the URL-backed work filter while the search changes", async () => {
    const user = userEvent.setup();
    const { history } = renderWorkspace(`/portal/projects/${firstProjectId}`, workReadFetcher({ detail: subjectDetail() }));
    await screen.findByRole("heading", { level: 1, name: "Project One" });
    const filter = screen.getByLabelText("Filter work items");
    // Every keystroke updates the URL search. A search-only update is not a route
    // change, so the input must keep focus and every character must land.
    await user.click(filter);
    await user.keyboard("Subject");
    expect(filter).toHaveFocus();
    expect(filter).toHaveValue("Subject");
    await waitFor(() => expect(new URL(history.location.href, "http://localhost").searchParams.get("q")).toBe("Subject"));
    // The same class of URL-backed control: a select keeps focus on its change.
    const state = screen.getByLabelText("Work state");
    await user.selectOptions(state, "ready");
    expect(state).toHaveFocus();
    await waitFor(() => expect(new URL(history.location.href, "http://localhost").searchParams.get("state")).toBe("ready"));
    const list = screen.getByRole("list", { name: "Work items" });
    expect(within(list).getByText(subjectTitle)).toBeVisible();
    expect(within(list).queryByText(siblingTitle)).not.toBeInTheDocument();
  });

  it("focuses the content heading on a genuine route change", async () => {
    const user = userEvent.setup();
    const { history } = renderWorkspace(`/portal/projects/${firstProjectId}`, workReadFetcher({ detail: subjectDetail() }));
    const list = await screen.findByRole("list", { name: "Work items" });
    // The route change into the work detail still moves focus to its heading.
    await user.click(within(list).getByRole("link", { name: subjectTitle }));
    const heading = await screen.findByRole("heading", { level: 1, name: subjectTitle });
    await waitFor(() => expect(heading).toHaveFocus());
    expect(new URL(history.location.href, "http://localhost").pathname).toBe(`/portal/work/${subjectId}`);
  });

  it("returns focus to the filter input after Clear filters removes its own button", async () => {
    const user = userEvent.setup();
    const { history } = renderWorkspace(`/portal/projects/${firstProjectId}?q=nothing-matches`, workReadFetcher({ detail: subjectDetail() }));
    await screen.findByRole("heading", { level: 1, name: "Project One" });
    expect(screen.getByRole("heading", { name: "No matching work items" })).toBeVisible();
    const filter = screen.getByLabelText("Filter work items");
    await user.click(screen.getByRole("button", { name: "Clear filters" }));
    // The click's own navigation keeps the pathname, so the shell does not move
    // focus and the button is unmounted with the empty state: the still-present
    // filter input must be focused explicitly instead of focus falling to body.
    await waitFor(() => expect(screen.queryByRole("button", { name: "Clear filters" })).not.toBeInTheDocument());
    expect(filter).toHaveFocus();
    expect(filter).toHaveValue("");
    expect(new URL(history.location.href, "http://localhost").searchParams.has("q")).toBe(false);
  });

  it("clears filters without dropping the selected view", async () => {
    const user = userEvent.setup();
    const { history } = renderWorkspace(`/portal/projects/${firstProjectId}?view=board&q=nothing-matches`, workReadFetcher({ detail: subjectDetail() }));
    await screen.findByRole("heading", { level: 1, name: "Project One" });
    expect(screen.getByRole("heading", { name: "No matching work items" })).toBeVisible();
    await user.click(screen.getByRole("button", { name: "Clear filters" }));
    // Clearing filters is not a view change: the board the operator selected
    // stays selected, and only the filter parameters leave the URL.
    expect(await screen.findByRole("region", { name: "Work board" })).toBeVisible();
    const url = new URL(history.location.href, "http://localhost");
    expect(url.searchParams.get("view")).toBe("board");
    expect(url.searchParams.has("q")).toBe(false);
    expect(screen.getByLabelText("Filter work items")).toHaveValue("");
  });

  it("keeps focus on the saved-views control when Delete saved view removes its own button", async () => {
    const user = userEvent.setup();
    localStorage.setItem(savedViewStorageKey, JSON.stringify({
      version: 1,
      scope: window.location.origin,
      projects: { [firstProjectId]: [{ name: "Blocked only", search: { blocked: "1" } }] },
    }));
    renderWorkspace(`/portal/projects/${firstProjectId}?blocked=1`, workReadFetcher({ detail: subjectDetail() }));
    await screen.findByRole("heading", { level: 1, name: "Project One" });
    const savedViews = screen.getByLabelText("Saved views");
    expect(savedViews).toHaveValue("Blocked only");
    // Same class as Clear filters: the delete control is removed by its own
    // update, so focus must land on the control that stays mounted.
    await user.click(screen.getByRole("button", { name: "Delete saved view" }));
    await waitFor(() => expect(screen.queryByRole("button", { name: "Delete saved view" })).not.toBeInTheDocument());
    expect(savedViews).toHaveFocus();
    const stored = JSON.parse(localStorage.getItem(savedViewStorageKey) ?? "{}") as { projects: Record<string, unknown[]> };
    expect(stored.projects[firstProjectId]).toEqual([]);
  });

  it("keeps keyboard focus on the board card when a refresh moves it to another column", async () => {
    const user = userEvent.setup();
    let subjectStatus = "ready";
    const { application } = renderWorkspace(`/portal/projects/${firstProjectId}?view=board`, movableSubjectFetcher(() => subjectStatus));
    const board = await screen.findByRole("region", { name: "Work board" });
    const card = within(board).getByRole("link", { name: subjectTitle });
    const siblingCard = within(board).getByRole("link", { name: siblingTitle });
    card.focus();
    expect(card).toHaveFocus();
    // The refresh regroups by state: the element that held focus is unmounted and
    // a new card is mounted for the same work item in the destination column.
    subjectStatus = "done";
    await act(async () => { await application.queryClient.refetchQueries({ queryKey: ["workspace", { projectId: firstProjectId }], exact: true }); });
    // The regroup lands on the tick after the refetch resolves, not inside it.
    const moved = await waitFor(() => {
      const link = within(board).getByRole("link", { name: subjectTitle });
      expect(link).not.toBe(card);
      return link;
    });
    expect(moved.closest("[data-board-column]")).toHaveAttribute("data-board-column", "done");
    expect(moved).toHaveFocus();
    // Arrow keys still act on the moved work item, and Enter still opens it.
    await user.keyboard("{ArrowLeft}");
    expect(siblingCard).toHaveFocus();
    await user.keyboard("{ArrowRight}");
    expect(moved).toHaveFocus();
    await user.keyboard("{Enter}");
    expect(await screen.findByRole("dialog", { name: subjectTitle })).toBeVisible();
  });

  it("does not move focus to a moved card while the operator is in the work filter", async () => {
    const user = userEvent.setup();
    let subjectStatus = "ready";
    const { application } = renderWorkspace(`/portal/projects/${firstProjectId}?view=board`, movableSubjectFetcher(() => subjectStatus));
    const board = await screen.findByRole("region", { name: "Work board" });
    within(board).getByRole("link", { name: subjectTitle }).focus();
    // The operator's focus left the card for the filter before the refresh landed:
    // the regroup must not pull focus back out of the field being used.
    const filter = screen.getByLabelText("Filter work items");
    await user.click(filter);
    expect(filter).toHaveFocus();
    subjectStatus = "done";
    await act(async () => { await application.queryClient.refetchQueries({ queryKey: ["workspace", { projectId: firstProjectId }], exact: true }); });
    await waitFor(() => expect(within(board).getByRole("link", { name: subjectTitle }).closest("[data-board-column]")).toHaveAttribute("data-board-column", "done"));
    expect(filter).toHaveFocus();
  });

  it("renders a row's object result summary as its JSON text and marks a task-less row unreported", async () => {
    const user = userEvent.setup();
    const fetcher = vi.fn<typeof fetch>().mockImplementation((input) => {
      const requestPath = input instanceof Request ? input.url : String(input);
      if (requestPath.endsWith("/session")) return Promise.resolve(new Response(JSON.stringify({ csrf_token: "session-token" })));
      if (requestPath.includes("/portal/api/work-items/")) {
        return Promise.resolve(new Response(JSON.stringify(readWorkDetail({
          id: subjectId,
          projectId: firstProjectId,
          title: subjectTitle,
          outcomeSummary: { message: "legacy structured summary" },
        }))));
      }
      const projectId = new URL(requestPath, "http://localhost").searchParams.get("project_id");
      return Promise.resolve(new Response(JSON.stringify({
        selected_project_id: null,
        projects: [{
          id: firstProjectId,
          key: "ONE",
          name: "Project One",
          description: null,
          status: "active",
          work_items: projectId === firstProjectId ? [
            readWorkItem({ id: subjectId, projectId: firstProjectId, title: subjectTitle, key: "ONE-9", execution: { generation: 2, result: { summary: { message: "12 passed" } } } }),
            readWorkItem({ id: siblingId, projectId: firstProjectId, title: siblingTitle, key: "ONE-10", status: "backlog" }),
          ] : [],
        }],
        health: { status: "unknown" },
      })));
    });
    renderWorkspace(`/portal/projects/${firstProjectId}`, fetcher);
    const list = await screen.findByRole("list", { name: "Work items" });
    const reportedRow = within(list).getByRole("link", { name: subjectTitle });
    expect(within(reportedRow).getByText("Acceptance not recorded")).toBeVisible();
    expect(within(reportedRow).getByText('Reported: {"message":"12 passed"}')).toBeVisible();
    const tasklessRow = within(list).getByRole("link", { name: siblingTitle });
    expect(within(tasklessRow).getByText("No reported result")).toBeVisible();

    // The detail's outcome summary is the same open JSON: a structured value is
    // rendered as its JSON text, never as the "no reported output" absence.
    await user.click(reportedRow);
    const detail = await screen.findByRole("dialog", { name: subjectTitle });
    expect(within(detail).getByText('{"message":"legacy structured summary"}')).toBeVisible();
    expect(within(detail).queryByText("No reported execution output")).not.toBeInTheDocument();
  });

  it("counts an unrecognised structured outcome value as reported output, not as an absence", async () => {
    renderWorkspace(`/portal/work/${subjectId}`, workReadFetcher({
      detail: readWorkDetail({ id: subjectId, projectId: firstProjectId, title: subjectTitle, outcomeTests: { passed: 12, failed: 0 } }),
    }));
    const detail = await screen.findByRole("dialog", { name: subjectTitle });
    expect(within(detail).getByText('{"passed":12,"failed":0}')).toBeVisible();
    expect(within(detail).queryByText("No reported execution output")).not.toBeInTheDocument();
  });

  it("ties each row's CI tone to the rendered chip class", async () => {
    const boundResourceId = "dddddddd-dddd-4ddd-8ddd-dddddddddddd";
    const offlineResourceId = "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee";
    const emptyDelivery = { value: null, source: "none" as const, generation: null, url: null, status: null };
    const passedDelivery = { value: "pass", source: "provider" as const, generation: 1, url: null, status: "passed", recorded_at: new Date().toISOString() };
    const project: Project = {
      id: firstProjectId,
      key: "ONE",
      name: "Project One",
      description: null,
      status: "active",
      resources: [
        { id: boundResourceId, status: "healthy", sync_status: "synced" },
        { id: offlineResourceId, status: "offline", sync_status: "synced" },
      ],
      work_items: [
        { id: subjectId, key: "ONE-9", title: subjectTitle, status: "ready", blocked: false, blocker: null, ci_resource_id: boundResourceId,
          delivery: { pull_request: emptyDelivery, ci: passedDelivery, review: emptyDelivery } },
        { id: siblingId, key: "ONE-10", title: siblingTitle, status: "ready", blocked: false, blocker: null, ci_resource_id: offlineResourceId,
          delivery: { pull_request: emptyDelivery, ci: passedDelivery, review: emptyDelivery } },
      ],
    };
    const fetcher = vi.fn<typeof fetch>().mockImplementation((input) => {
      const requestPath = input instanceof Request ? input.url : String(input);
      if (requestPath.endsWith("/session")) return Promise.resolve(new Response(JSON.stringify({ csrf_token: "session-token" })));
      if (requestPath.includes("/portal/api/work-items/")) {
        return Promise.resolve(new Response(requestPath.endsWith("/timeline")
          ? JSON.stringify({ items: [], next_before: null })
          : JSON.stringify(readWorkDetail({ id: subjectId, projectId: firstProjectId, title: subjectTitle }))));
      }
      return Promise.resolve(new Response(JSON.stringify({ selected_project_id: project.id, projects: [project], health: { status: "unknown" } })));
    });
    renderWorkspace(`/portal/projects/${firstProjectId}`, fetcher);
    const list = await screen.findByRole("list", { name: "Work items" });
    // The tone, not a fixed class or a swapped stylesheet pair, decides the chip:
    // a confirmed fresh provider CI is green and a bound offline resource is not.
    expect(within(within(list).getByRole("link", { name: subjectTitle })).getByText("CI passed")).toHaveClass("chip-passed");
    const staleChip = within(within(list).getByRole("link", { name: siblingTitle })).getByText("CI passed · bound resource offline");
    expect(staleChip).toHaveClass("chip-stale");
    expect(staleChip).not.toHaveClass("chip-passed");
  });
});

describe("execution history boundaries", () => {
  const timelineItemId = "66666666-6666-4666-8666-666666666666";
  const olderEntry = {
    source: "transition",
    run_id: timelineItemId,
    generation: 1,
    recorded_at: "2026-03-01T09:00:00Z",
    data: { transition_id: timelineItemId, state: "failed", payload: { reason: "older entry" } },
  };

  /**
   * Counts detail reads and answers the timeline boundary the current cursor
   * names: the first served boundary is refused as aged out, and the refreshed
   * detail serves the next one.
   */
  function historyFetcher(reads: { details: number; timelineBefore: string[] }) {
    return vi.fn<typeof fetch>().mockImplementation((input) => {
      const requestPath = input instanceof Request ? input.url : String(input);
      if (requestPath.endsWith("/session")) return Promise.resolve(new Response(JSON.stringify({ csrf_token: "session-token" })));
      if (requestPath.includes("/portal/api/work-items/")) {
        const url = new URL(requestPath, "http://localhost");
        if (url.pathname.endsWith("/timeline")) {
          const before = url.searchParams.get("before");
          reads.timelineBefore.push(before ?? "");
          return Promise.resolve(before === "cursor-1"
            ? new Response("{}", { status: 400 })
            : new Response(JSON.stringify({ items: [olderEntry], next_before: null })));
        }
        reads.details += 1;
        return Promise.resolve(new Response(JSON.stringify(readWorkDetail({
          id: subjectId,
          projectId: firstProjectId,
          title: subjectTitle,
          nextBefore: reads.details === 1 ? "cursor-1" : "cursor-2",
        }))));
      }
      const projectId = new URL(requestPath, "http://localhost").searchParams.get("project_id");
      return Promise.resolve(new Response(JSON.stringify({
        selected_project_id: null,
        projects: workViewProjects.map((project) => ({ ...project, work_items: project.id === projectId ? project.work_items : [] })),
        health: { status: "unknown" },
      })));
    });
  }

  it("re-reads the served boundary when the timeline cursor has aged out", async () => {
    const user = userEvent.setup();
    const reads = { details: 0, timelineBefore: [] as string[] };
    renderWorkspace(`/portal/work/${subjectId}`, historyFetcher(reads));
    await screen.findByRole("heading", { level: 1, name: subjectTitle });

    await user.click(screen.getByRole("button", { name: "Load older history" }));
    expect(await screen.findByText("Earlier history could not be loaded.")).toBeVisible();
    expect(reads.timelineBefore).toEqual(["cursor-1"]);

    await user.click(screen.getByRole("button", { name: "Retry history" }));
    await waitFor(() => expect(reads.details).toBeGreaterThan(1));
    await user.click(await screen.findByRole("button", { name: "Load older history" }));
    expect(await screen.findByText("older entry")).toBeVisible();
    expect(reads.timelineBefore).toEqual(["cursor-1", "cursor-2"]);
  });

  it("loads older history behind the refreshed detail boundary instead of the stale one", async () => {
    const user = userEvent.setup();
    const reads = { details: 0, timelineBefore: [] as string[] };
    const { application } = renderWorkspace(`/portal/work/${subjectId}`, historyFetcher(reads));
    await screen.findByRole("heading", { level: 1, name: subjectTitle });

    await act(async () => { await application.queryClient.refetchQueries({ queryKey: ["work-item", { workItemId: subjectId }], exact: true }); });
    expect(reads.details).toBe(2);

    await user.click(screen.getByRole("button", { name: "Load older history" }));
    expect(await screen.findByText("older entry")).toBeVisible();
    expect(reads.timelineBefore).toEqual(["cursor-2"]);
  });

  it("keeps already expanded older history when the detail refresh re-issues the boundary", async () => {
    const user = userEvent.setup();
    const reads = { details: 0, timelineBefore: [] as string[] };
    const fetcher = vi.fn<typeof fetch>().mockImplementation((input) => {
      const requestPath = input instanceof Request ? input.url : String(input);
      if (requestPath.endsWith("/session")) return Promise.resolve(new Response(JSON.stringify({ csrf_token: "session-token" })));
      if (requestPath.includes("/portal/api/work-items/")) {
        const url = new URL(requestPath, "http://localhost");
        if (url.pathname.endsWith("/timeline")) {
          reads.timelineBefore.push(url.searchParams.get("before") ?? "");
          return Promise.resolve(new Response(JSON.stringify({ items: [olderEntry], next_before: null })));
        }
        reads.details += 1;
        return Promise.resolve(new Response(JSON.stringify(readWorkDetail({
          id: subjectId,
          projectId: firstProjectId,
          title: subjectTitle,
          nextBefore: reads.details === 1 ? "cursor-1" : "cursor-2",
        }))));
      }
      const projectId = new URL(requestPath, "http://localhost").searchParams.get("project_id");
      return Promise.resolve(new Response(JSON.stringify({
        selected_project_id: null,
        projects: workViewProjects.map((project) => ({ ...project, work_items: project.id === projectId ? project.work_items : [] })),
        health: { status: "unknown" },
      })));
    });
    const { application } = renderWorkspace(`/portal/work/${subjectId}`, fetcher);
    await screen.findByRole("heading", { level: 1, name: subjectTitle });

    await user.click(screen.getByRole("button", { name: "Load older history" }));
    expect(await screen.findByText("older entry")).toBeVisible();
    expect(await screen.findByText("No older history recorded.")).toBeVisible();

    await act(async () => { await application.queryClient.refetchQueries({ queryKey: ["work-item", { workItemId: subjectId }], exact: true }); });
    expect(reads.details).toBe(2);
    // The refreshed boundary re-keys the timeline query, so the exhausted state
    // belongs to the superseded boundary and the control returns; the page loaded
    // from that boundary still belongs to the reader and stays rendered.
    await screen.findByRole("button", { name: "Load older history" });
    expect(screen.getByText("older entry")).toBeVisible();
    expect(reads.timelineBefore).toEqual(["cursor-1"]);
  });

  it("does not paginate a loaded older page against a superseded detail boundary", async () => {
    const user = userEvent.setup();
    const reads = { details: 0, timelineBefore: [] as string[] };
    const fetcher = vi.fn<typeof fetch>().mockImplementation((input) => {
      const requestPath = input instanceof Request ? input.url : String(input);
      if (requestPath.endsWith("/session")) return Promise.resolve(new Response(JSON.stringify({ csrf_token: "session-token" })));
      if (requestPath.includes("/portal/api/work-items/")) {
        const url = new URL(requestPath, "http://localhost");
        if (url.pathname.endsWith("/timeline")) {
          const before = url.searchParams.get("before");
          reads.timelineBefore.push(before ?? "");
          // The first boundary has a second page; the refreshed boundary is the
          // only valid continuation once the detail re-renders.
          if (before === "cursor-1") return Promise.resolve(new Response(JSON.stringify({ items: [olderEntry], next_before: "cursor-1b" })));
          return Promise.resolve(new Response(JSON.stringify({ items: [{ ...olderEntry, data: { ...olderEntry.data, payload: { reason: "refreshed entry" } } }], next_before: null })));
        }
        reads.details += 1;
        return Promise.resolve(new Response(JSON.stringify(readWorkDetail({
          id: subjectId,
          projectId: firstProjectId,
          title: subjectTitle,
          nextBefore: reads.details === 1 ? "cursor-1" : "cursor-2",
        }))));
      }
      const projectId = new URL(requestPath, "http://localhost").searchParams.get("project_id");
      return Promise.resolve(new Response(JSON.stringify({
        selected_project_id: null,
        projects: workViewProjects.map((project) => ({ ...project, work_items: project.id === projectId ? project.work_items : [] })),
        health: { status: "unknown" },
      })));
    });
    const { application } = renderWorkspace(`/portal/work/${subjectId}`, fetcher);
    await screen.findByRole("heading", { level: 1, name: subjectTitle });

    await user.click(screen.getByRole("button", { name: "Load older history" }));
    expect(await screen.findByText("older entry")).toBeVisible();

    await act(async () => { await application.queryClient.refetchQueries({ queryKey: ["work-item", { workItemId: subjectId }], exact: true }); });
    // The refreshed boundary re-identifies the history query, so the page that
    // was loaded against the superseded cursor is not paginated further.
    await user.click(await screen.findByRole("button", { name: "Load older history" }));
    expect(await screen.findByText("refreshed entry")).toBeVisible();
    expect(reads.timelineBefore).toEqual(["cursor-1", "cursor-2"]);
  });

  it("re-renders the cached older pages when the detail is reopened inside the freshness window", async () => {
    const user = userEvent.setup();
    const reads = { details: 0, timelineBefore: [] as string[] };
    const fetcher = vi.fn<typeof fetch>().mockImplementation((input) => {
      const requestPath = input instanceof Request ? input.url : String(input);
      if (requestPath.endsWith("/session")) return Promise.resolve(new Response(JSON.stringify({ csrf_token: "session-token" })));
      if (requestPath.includes("/portal/api/work-items/")) {
        const url = new URL(requestPath, "http://localhost");
        if (url.pathname.endsWith("/timeline")) {
          reads.timelineBefore.push(url.searchParams.get("before") ?? "");
          return Promise.resolve(new Response(JSON.stringify({ items: [olderEntry], next_before: null })));
        }
        reads.details += 1;
        return Promise.resolve(new Response(JSON.stringify(readWorkDetail({
          id: subjectId,
          projectId: firstProjectId,
          title: subjectTitle,
          nextBefore: "cursor-1",
        }))));
      }
      const projectId = new URL(requestPath, "http://localhost").searchParams.get("project_id");
      return Promise.resolve(new Response(JSON.stringify({
        selected_project_id: null,
        projects: workViewProjects.map((project) => ({ ...project, work_items: project.id === projectId ? project.work_items : [] })),
        health: { status: "unknown" },
      })));
    });
    renderWorkspace(`/portal/work/${subjectId}`, fetcher);
    await screen.findByRole("heading", { level: 1, name: subjectTitle });

    await user.click(screen.getByRole("button", { name: "Load older history" }));
    expect(await screen.findByText("older entry")).toBeVisible();
    expect(screen.getByText("No older history recorded.")).toBeVisible();
    expect(reads.details).toBe(1);

    // Return to the project and reopen the same item inside the freshness window:
    // the detail is served from cache (no new read), and the cached timeline
    // pages must render again instead of starting from an empty local copy.
    await user.click(screen.getByRole("button", { name: "Close work detail" }));
    const list = await screen.findByRole("list", { name: "Work items" });
    await user.click(within(list).getByRole("link", { name: subjectTitle }));
    await screen.findByRole("heading", { level: 1, name: subjectTitle });
    expect(reads.details).toBe(1);
    expect(await screen.findByText("older entry")).toBeVisible();
    expect(screen.getByText("No older history recorded.")).toBeVisible();
  });
});

describe("chat route", () => {
  const chatFetcher = () => vi.fn<typeof fetch>().mockImplementation((input) => {
    const requestPath = input instanceof Request ? input.url : String(input);
    const url = new URL(requestPath, "http://localhost");
    if (url.pathname === "/portal/api/session") return Promise.resolve(new Response('{"csrf_token":"session-token"}'));
    if (url.pathname === "/portal/api/chat") {
      const scope = url.searchParams.get("scope");
      return Promise.resolve(new Response(JSON.stringify({
        scope,
        project_id: scope === "project" ? url.searchParams.get("project_id") : null,
        run_id: scope === "run" ? url.searchParams.get("run_id") : null,
        messages: [],
        runs: [],
        next_before: null,
      })));
    }
    return Promise.resolve(new Response(JSON.stringify({ selected_project_id: firstProjectId, projects })));
  });

  it("subscribes /chat and carries the shell's project scope in its navigation", async () => {
    const user = userEvent.setup();
    const { history } = renderWorkspace(`/portal/chat?scope=workspace&projectId=${firstProjectId}`, chatFetcher());
    expect(await screen.findByRole("heading", { level: 1, name: "Chat" })).toBeVisible();
    await screen.findByText("Workspace conversation");
    // The scope member and the project travel with the route, so the shell's
    // navigation links and a session-expiry return target keep the conversation.
    expect(screen.getByRole("link", { name: "Chat" })).toHaveAttribute("href", `/portal/chat?projectId=${firstProjectId}`);

    await user.click(screen.getByRole("button", { name: /Search or navigate/ }));
    const palette = await screen.findByRole("dialog", { name: "Command palette" });
    const paletteChat = within(palette).getByRole("link", { name: "Chat" });
    expect(paletteChat).toHaveAttribute("href", `/portal/chat?projectId=${firstProjectId}`);
    await user.click(paletteChat);
    await waitFor(() => expect(new URL(history.location.href, "http://localhost").searchParams.get("projectId")).toBe(firstProjectId));
    expect(new URL(history.location.href, "http://localhost").searchParams.has("scope")).toBe(false);
    await screen.findByText("Workspace conversation");
  });

  it("returns a session expiry to the chat scope it was holding", () => {
    const runScope = "33333333-3333-4333-8333-333333333333";
    expect(safeReturnTo(`http://localhost/portal/chat?scope=run&runId=${runScope}`)).toBe(`/portal/chat?scope=run&runId=${runScope}`);
    expect(safeReturnTo("http://localhost/portal/chat/elsewhere")).toBe("/portal/attention");
    expect(safeReturnTo("http://localhost/portal/logout")).toBe("/portal/attention");
  });
});
