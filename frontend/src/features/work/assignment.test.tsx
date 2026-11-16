import { QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory, RouterProvider, type RouterHistory } from "@tanstack/react-router";
import { act, render, screen, waitFor, within } from "@testing-library/react";
import userEvent, { type UserEvent } from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createApplication, type Application } from "../../app/application";
import { createAppRouter } from "../../app/router";
import { readWorkDetail } from "../../services/api.fixtures";

/**
 * The work assignment editor on `/portal/work/:workItemId`: option sets scoped
 * to the item's own project, the exact submitted payload, the absence of
 * provider-owned controls, intent locking, refusals, and the continuation that
 * settles after the operator selected another item. Every test fails on the
 * pre-change detail, which has no assignment surface at all.
 */

const projectId = "10000000-0000-4000-8000-000000000001";
const otherProjectId = "10000000-0000-4000-8000-000000000002";
const subjectId = "20000000-0000-4000-8000-000000000001";
const siblingItemId = "20000000-0000-4000-8000-000000000002";
const repositoryResourceId = "30000000-0000-4000-8000-000000000001";
const ciResourceId = "30000000-0000-4000-8000-000000000002";
const unrelatedKindResourceId = "30000000-0000-4000-8000-000000000003";
const siblingResourceId = "30000000-0000-4000-8000-000000000004";
const foreignRowResourceId = "30000000-0000-4000-8000-000000000005";
const subjectVersion = 4;

/**
 * The item's project's served rows: one repository, one CI resource, a resource
 * of another kind, and a row that names another project. The sibling project
 * carries a repository resource of its own, so a leak from it would show.
 */
const itemProjectResources = [
  { id: repositoryResourceId, project_id: projectId, kind: "repository", name: "symmetry", external_ref: "acme/symmetry", status: "healthy", sync_status: "synced" },
  { id: ciResourceId, project_id: projectId, kind: "ci", name: "Release pipeline", external_ref: "acme/symmetry/pipeline/7", status: "healthy", sync_status: "synced" },
  { id: unrelatedKindResourceId, project_id: projectId, kind: "work_tracking", name: "Issue tracker", external_ref: "acme/issues", status: "healthy", sync_status: "synced" },
  { id: foreignRowResourceId, project_id: otherProjectId, kind: "repository", name: "mislabeled", external_ref: "acme/mislabeled", status: "healthy", sync_status: "synced" },
];

const siblingProjectResources = [
  { id: siblingResourceId, project_id: otherProjectId, kind: "repository", name: "sibling-repo", external_ref: "acme/sibling", status: "healthy", sync_status: "synced" },
];

function workspaceBody(requested: string | null) {
  return {
    selected_project_id: requested,
    projects: [
      { id: projectId, key: "ONE", name: "Project One", description: null, status: "active", default_agent_profile: "codex-default", default_workspace: "primary", resources: itemProjectResources, work_items: [] },
      { id: otherProjectId, key: "TWO", name: "Project Two", description: null, status: "active", resources: siblingProjectResources, work_items: [] },
    ],
    health: { status: "unknown" },
  };
}

/** A real detail envelope whose work item carries the served assignment fields. */
function detailBody(workItemId: string, overrides: Record<string, unknown> = {}, execution?: Record<string, unknown>) {
  const envelope = readWorkDetail({
    id: workItemId,
    projectId,
    title: workItemId === subjectId ? "Subject work item" : "Sibling work item",
    ...(execution === undefined ? {} : { execution }),
  });
  return {
    ...envelope,
    work_item: {
      ...(envelope["work_item"] as Record<string, unknown>),
      version: subjectVersion,
      repository: null,
      ci_resource: null,
      repository_resource_id: null,
      ci_resource_id: null,
      workspace: null,
      ...overrides,
    },
  };
}

interface RequestRecord {
  method: string;
  path: string;
  body: unknown;
}

interface HarnessOptions {
  desktop?: boolean;
  detail?: (workItemId: string) => Record<string, unknown>;
  workspace?: (requested: string | null) => Response | Promise<Response>;
  patch?: (body: unknown) => Response | Promise<Response>;
}

const applications: Application[] = [];
afterEach(() => {
  for (const application of applications) application.queryClient.clear();
  applications.length = 0;
});

/** jsdom has no matchMedia; the detail is a drawer above 1024px and a page below it. */
function stubMatchMedia(desktop: boolean) {
  window.matchMedia = (query: string): MediaQueryList => ({
    matches: desktop && query.includes("min-width"),
    media: query,
    onchange: null,
    addEventListener: () => undefined,
    removeEventListener: () => undefined,
    addListener: () => undefined,
    removeListener: () => undefined,
    dispatchEvent: () => false,
  });
}

beforeEach(() => stubMatchMedia(false));

function renderWorkDetail(options: HarnessOptions = {}) {
  const path = `/portal/work/${subjectId}`;
  stubMatchMedia(options.desktop ?? false);
  const requests: RequestRecord[] = [];
  const fetcher = vi.fn<typeof fetch>().mockImplementation(async (input, init) => {
    const url = new URL(input instanceof Request ? input.url : String(input), "http://localhost");
    const method = init?.method ?? "GET";
    const body: unknown = typeof init?.body === "string" ? JSON.parse(init.body) : undefined;
    requests.push({ method, path: url.pathname + url.search, body });
    if (url.pathname === "/portal/api/session") return new Response(JSON.stringify({ csrf_token: "session-token" }));
    if (url.pathname.startsWith("/portal/api/work-items/")) {
      if (url.pathname.endsWith("/timeline")) return new Response(JSON.stringify({ items: [], next_before: null }));
      const workItemId = decodeURIComponent(url.pathname.slice("/portal/api/work-items/".length));
      if (method === "PATCH") {
        const respond = options.patch ?? (() => new Response(JSON.stringify({ work_item: { id: workItemId, version: subjectVersion + 1 } })));
        return respond(body);
      }
      return new Response(JSON.stringify((options.detail ?? detailBody)(workItemId)));
    }
    if (url.pathname === "/portal/api/workspace") {
      const requested = url.searchParams.get("project_id");
      const respond = options.workspace ?? ((scope: string | null) => new Response(JSON.stringify(workspaceBody(scope))));
      return respond(requested);
    }
    return new Response("{}", { status: 404 });
  });
  const application = createApplication({ fetcher, location: { href: `http://localhost${path}`, replace: vi.fn() } });
  applications.push(application);
  const history: RouterHistory = createMemoryHistory({ initialEntries: [path] });
  const router = createAppRouter(application, history);
  render(<QueryClientProvider client={application.queryClient}><RouterProvider router={router} /></QueryClientProvider>);
  const countReads = (method: string, matches: (request: RequestRecord) => boolean) =>
    requests.filter((request) => request.method === method && matches(request)).length;
  return {
    application,
    history,
    requests,
    detailReads: (workItemId: string) => countReads("GET", (request) => request.path === `/portal/api/work-items/${workItemId}`),
    projectReads: (requested: string | null) =>
      countReads("GET", (request) => request.path.startsWith("/portal/api/workspace") && new URL(request.path, "http://localhost").searchParams.get("project_id") === requested),
    patches: () => requests.filter((request) => request.method === "PATCH"),
  };
}

async function openAssignmentEditor(user: UserEvent) {
  await screen.findByRole("heading", { level: 1, name: "Subject work item" });
  await user.click(screen.getByRole("button", { name: "Edit assignment" }));
  return await screen.findByRole("dialog", { name: "Assignment" });
}

describe("work assignment", () => {
  it("offers only the item's project's resources of the matching kind", async () => {
    const user = userEvent.setup();
    renderWorkDetail();
    const dialog = await openAssignmentEditor(user);
    const repository = within(dialog).getByLabelText("Repository");
    // The sibling project's repository, the work-tracking resource and the row
    // whose `project_id` names another project are all absent.
    expect(within(repository).getAllByRole("option").map((option) => option.textContent)).toEqual([
      "Not linked",
      "symmetry · acme/symmetry",
    ]);
    expect(within(repository).getByRole("option", { name: "Not linked" })).toHaveAttribute("value", "");
    expect(within(repository).getByRole("option", { name: "symmetry · acme/symmetry" })).toHaveAttribute("value", repositoryResourceId);
    const ci = within(dialog).getByLabelText("CI resource");
    expect(within(ci).getAllByRole("option").map((option) => option.textContent)).toEqual([
      "Use repository provider",
      "Release pipeline · acme/symmetry/pipeline/7",
    ]);
    expect(within(ci).getByRole("option", { name: "Release pipeline · acme/symmetry/pipeline/7" })).toHaveAttribute("value", ciResourceId);
  });

  it("submits the served version with the selected resource ids and re-reads both reads", async () => {
    const user = userEvent.setup();
    const harness = renderWorkDetail();
    const dialog = await openAssignmentEditor(user);
    const ownerType = within(dialog).getByLabelText("Owner type");
    const profile = within(dialog).getByLabelText("Agent profile");
    // The draft starts from the served assignment, not from a guess.
    expect(ownerType).toHaveValue("agent");
    expect(profile).toHaveValue("default");
    expect(profile).toHaveAttribute("placeholder", "codex-default");
    expect(within(dialog).getByLabelText("Workspace")).toHaveAttribute("placeholder", "primary");
    await user.type(within(dialog).getByLabelText("Owner"), "Codex");
    // An emptied text field is submitted as null so the server applies the
    // project's default; it is never sent as an empty string.
    await user.clear(profile);
    await user.type(within(dialog).getByLabelText("Workspace"), "primary");
    await user.selectOptions(within(dialog).getByLabelText("Repository"), repositoryResourceId);
    await user.selectOptions(within(dialog).getByLabelText("CI resource"), ciResourceId);
    const detailsBefore = harness.detailReads(subjectId);
    const projectsBefore = harness.projectReads(projectId);
    await user.click(within(dialog).getByRole("button", { name: "Save assignment" }));
    await waitFor(() => expect(harness.patches()).toHaveLength(1));
    expect(harness.patches()[0]?.path).toBe(`/portal/api/work-items/${subjectId}`);
    expect(harness.patches()[0]?.body).toEqual({
      version: subjectVersion,
      assignee_type: "agent",
      assignee_name: "Codex",
      agent_profile: null,
      workspace: "primary",
      repository_resource_id: repositoryResourceId,
      ci_resource_id: ciResourceId,
    });
    // A successful save re-reads the work detail and the project read.
    await waitFor(() => expect(harness.detailReads(subjectId)).toBeGreaterThan(detailsBefore));
    await waitFor(() => expect(harness.projectReads(projectId)).toBeGreaterThan(projectsBefore));
    expect(await screen.findByText(/Assignment saved at version 5/)).toBeVisible();
    expect(screen.queryByRole("dialog", { name: "Assignment" })).not.toBeInTheDocument();
  });

  it("keeps provider-owned fields off the assignment editor", async () => {
    const user = userEvent.setup();
    renderWorkDetail();
    const dialog = await openAssignmentEditor(user);
    // The dialog's whole control set is the assignment fields: no title,
    // description, priority or work-state control can be reached from here.
    expect([...dialog.querySelectorAll("input, select")].map((control) => control.getAttribute("name"))).toEqual([
      "assignee_type",
      "assignee_name",
      "agent_profile",
      "workspace",
      "repository_resource_id",
      "ci_resource_id",
    ]);
    for (const label of ["Title", "Description", "Priority", "Status", "Work state", "Branch", "Pull request URL"]) {
      expect(within(dialog).queryByLabelText(label)).not.toBeInTheDocument();
    }
  });

  it("disables the editor while the served execution holds the item's intent", async () => {
    const user = userEvent.setup();
    renderWorkDetail({ detail: (workItemId) => detailBody(workItemId, {}, { intent_locked: true }) });
    await screen.findByRole("heading", { level: 1, name: "Subject work item" });
    expect(screen.getByText(/holds this item's intent/)).toBeVisible();
    const edit = screen.getByRole("button", { name: "Edit assignment" });
    expect(edit).toBeDisabled();
    await user.click(edit);
    expect(screen.queryByRole("dialog", { name: "Assignment" })).not.toBeInTheDocument();
  });

  it("renders the server's refusal code and re-reads the detail", async () => {
    const user = userEvent.setup();
    const harness = renderWorkDetail({
      patch: () => new Response(JSON.stringify({ error: { code: "provider_owned", message: "title is authoritative in the external provider" } }), { status: 409 }),
    });
    const dialog = await openAssignmentEditor(user);
    const detailsBefore = harness.detailReads(subjectId);
    await user.click(within(dialog).getByRole("button", { name: "Save assignment" }));
    const alert = await within(dialog).findByRole("alert");
    expect(alert).toHaveTextContent("title is authoritative in the external provider (provider_owned)");
    await waitFor(() => expect(harness.detailReads(subjectId)).toBeGreaterThan(detailsBefore));
    // A provider_owned refusal is not a version conflict: the editor stays usable.
    expect(within(dialog).getByRole("button", { name: "Save assignment" })).toBeEnabled();
  });

  it("does not let a late save mark the item the operator switched to", async () => {
    const user = userEvent.setup();
    const deferred = Promise.withResolvers<Response>();
    const harness = renderWorkDetail({
      detail: (workItemId) => workItemId === siblingItemId
        ? detailBody(siblingItemId, { assignee: { type: "human", name: "Grace", agent_profile: null } })
        : detailBody(subjectId),
      patch: () => deferred.promise,
    });
    const subjectDialog = await openAssignmentEditor(user);
    await user.type(within(subjectDialog).getByLabelText("Owner"), "Codex");
    await user.click(within(subjectDialog).getByRole("button", { name: "Save assignment" }));
    await waitFor(() => expect(harness.patches()).toHaveLength(1));
    const projectsBefore = harness.projectReads(projectId);
    // The operator selects the sibling item and opens its editor while the
    // subject's save is still in flight.
    act(() => harness.history.push(`/portal/work/${siblingItemId}`));
    await screen.findByRole("heading", { level: 1, name: "Sibling work item" });
    await user.click(screen.getByRole("button", { name: "Edit assignment" }));
    const siblingDialog = await screen.findByRole("dialog", { name: "Assignment" });
    expect(within(siblingDialog).getByLabelText("Owner")).toHaveValue("Grace");
    expect(within(siblingDialog).getByLabelText("Repository")).toHaveValue("");
    deferred.resolve(new Response(JSON.stringify({ work_item: { id: subjectId, version: subjectVersion + 1 } })));
    // The continuation still re-reads the project the option sets came from…
    await waitFor(() => expect(harness.projectReads(projectId)).toBeGreaterThan(projectsBefore));
    // …but it cannot close, mark or rewrite the sibling's editor.
    expect(within(siblingDialog).getByLabelText("Owner")).toHaveValue("Grace");
    expect(within(siblingDialog).getByLabelText("Repository")).toHaveValue("");
    expect(within(siblingDialog).getByRole("button", { name: "Save assignment" })).toBeEnabled();
    expect(screen.queryByText(/Assignment saved/)).not.toBeInTheDocument();
  });

  it("offers only the empty option and blocks the save when the project read is unavailable", async () => {
    const user = userEvent.setup();
    const consoleError = vi.spyOn(console, "error").mockImplementation(() => undefined);
    const harness = renderWorkDetail({
      workspace: () => new Response(JSON.stringify({ error: { code: "forbidden", message: "connection is not authorized" } }), { status: 403 }),
    });
    // The served facts stay rendered from the detail read even though the project
    // read failed: the failure is only about the option sets.
    await screen.findByRole("heading", { level: 1, name: "Subject work item" });
    const section = screen.getByRole("region", { name: "Assignment" });
    expect(within(section).getByText("Not linked")).toBeVisible();
    expect(within(section).getByText("Repository provider")).toBeVisible();
    const dialog = await openAssignmentEditor(user);
    // Both resource selects render their empty option alone.
    const repository = within(dialog).getByLabelText("Repository");
    const ci = within(dialog).getByLabelText("CI resource");
    await waitFor(() => expect(within(repository).getAllByRole("option")).toHaveLength(1));
    expect(within(ci).getAllByRole("option")).toHaveLength(1);
    expect(within(dialog).getByLabelText("Repository")).toHaveValue("");
    expect(within(dialog).getByLabelText("CI resource")).toHaveValue("");
    expect(within(dialog).getByText(/project resources could not be read/)).toBeVisible();
    expect(within(dialog).getByRole("button", { name: "Save assignment" })).toBeDisabled();
    // A disabled submit cannot write: no request leaves the surface.
    await user.click(within(dialog).getByRole("button", { name: "Save assignment" }));
    expect(harness.patches()).toHaveLength(0);
    expect(consoleError).not.toHaveBeenCalled();
  });

  it("keeps the served facts and blocks the save while the project read is still loading", async () => {
    const user = userEvent.setup();
    const gate = Promise.withResolvers<void>();
    renderWorkDetail({
      workspace: async (scope) => {
        await gate.promise;
        return new Response(JSON.stringify(workspaceBody(scope)));
      },
    });
    await screen.findByRole("heading", { level: 1, name: "Subject work item" });
    const section = screen.getByRole("region", { name: "Assignment" });
    expect(within(section).getByText("Not linked")).toBeVisible();
    const dialog = await openAssignmentEditor(user);
    expect(within(dialog).getByText("Loading this work item's project resources…")).toBeVisible();
    expect(within(within(dialog).getByLabelText("Repository")).getAllByRole("option")).toHaveLength(1);
    expect(within(within(dialog).getByLabelText("CI resource")).getAllByRole("option")).toHaveLength(1);
    expect(within(dialog).getByRole("button", { name: "Save assignment" })).toBeDisabled();
    gate.resolve();
    // The served read is the only source of the options: they appear when it does.
    await waitFor(() => expect(within(dialog).getByRole("option", { name: "symmetry · acme/symmetry" })).toBeVisible());
    expect(within(dialog).getByRole("button", { name: "Save assignment" })).toBeEnabled();
  });

  it("renders the served assignment facts in the desktop drawer", async () => {
    const user = userEvent.setup();
    renderWorkDetail({ desktop: true, detail: (workItemId) => detailBody(workItemId, { repository: "acme/symmetry" }) });
    const drawer = await screen.findByRole("dialog", { name: "Subject work item" });
    expect(within(drawer).getByRole("heading", { name: "Assignment" })).toBeVisible();
    // The served facts name this item's own binding; the Outcome section above
    // also shows the owner, so the assignment facts are scoped to their section.
    const facts = within(within(drawer).getByRole("region", { name: "Assignment" }));
    expect(facts.getByText("Agent · default")).toBeVisible();
    expect(facts.getByText("Project default")).toBeVisible();
    expect(facts.getByText("acme/symmetry")).toBeVisible();
    expect(facts.getByText("Repository provider")).toBeVisible();
    // The editor nests inside the drawer dialog and closing it gives the drawer
    // content back instead of leaving it hidden behind the closed modal.
    await user.click(within(drawer).getByRole("button", { name: "Edit assignment" }));
    const editor = await screen.findByRole("dialog", { name: "Assignment" });
    expect(within(editor).getByLabelText("Owner type")).toHaveValue("agent");
    await user.click(within(editor).getByRole("button", { name: "Cancel" }));
    expect(screen.queryByRole("dialog", { name: "Assignment" })).not.toBeInTheDocument();
    expect(within(drawer).getByRole("button", { name: "Edit assignment" })).toBeEnabled();
  });
});
