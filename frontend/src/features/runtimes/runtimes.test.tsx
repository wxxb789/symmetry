import { QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory, RouterProvider, type RouterHistory } from "@tanstack/react-router";
import { render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { createApplication, type Application } from "../../app/application";
import { createAppRouter } from "../../app/router";

/**
 * `/portal/runtimes`: the page renders the workspace read's compatibility
 * filter, and its empty state names that filter and cites the deployment's
 * registered count instead of claiming the registry itself is empty.
 */

const projectId = "10000000-0000-4000-8000-000000000001";

function runtimeRow(id: string, key: string) {
  return {
    machine_id: "30000000-0000-4000-8000-000000000010",
    machine_name: `machine-${key}`,
    runtime_id: id,
    runtime_key: key,
    runtime_name: `Runtime ${key}`,
    status: "offline",
    last_heartbeat_at: null,
    connection_epoch: 1,
    capacity: 1,
    reserved_capacity: 0,
    agent_profile: `profile-${key}`,
    workspace: `workspace-${key}`,
    repository_resource_id: null,
    capabilities: { structured_input: true },
    active_runs: [],
  };
}

const runtimeA = runtimeRow("30000000-0000-4000-8000-000000000001", "alpha");
const runtimeB = runtimeRow("30000000-0000-4000-8000-000000000002", "beta");

const applications: Application[] = [];
afterEach(() => {
  for (const application of applications) application.queryClient.clear();
  applications.length = 0;
});

function renderRuntimes(path: string, workspace: { runtimes: unknown[]; registered_runtimes: unknown[] }) {
  const body = {
    selected_project_id: projectId,
    projects: [{
      id: projectId,
      key: "ONE",
      name: "Project One",
      description: null,
      status: "active",
      default_agent_profile: "default",
      default_workspace: "primary",
      resources: [],
      work_items: [],
    }],
    health: { status: "unknown" },
    ...workspace,
  };
  const fetchResponse = (input: string | Request | URL): Response => {
    const url = new URL(input instanceof Request ? input.url : String(input), "http://localhost");
    if (url.pathname === "/portal/api/session") return new Response(JSON.stringify({ csrf_token: "session-token" }));
    if (url.pathname === "/portal/api/workspace") return new Response(JSON.stringify(body));
    return new Response("{}", { status: 404 });
  };
  const fetcher = vi.fn<typeof fetch>().mockImplementation((input) => Promise.resolve(fetchResponse(input)));
  const application = createApplication({ fetcher, location: { href: `http://localhost${path}`, replace: vi.fn() } });
  applications.push(application);
  const history: RouterHistory = createMemoryHistory({ initialEntries: [path] });
  const router = createAppRouter(application, history);
  render(<QueryClientProvider client={application.queryClient}><RouterProvider router={router} /></QueryClientProvider>);
}

describe("runtimes page", () => {
  it("renders the served compatibility rows for the unscoped read", async () => {
    renderRuntimes("/portal/runtimes", { runtimes: [runtimeA], registered_runtimes: [runtimeA, runtimeB] });
    await screen.findByRole("heading", { level: 1, name: "Runtimes" });
    const cards = await screen.findAllByRole("article");
    expect(cards).toHaveLength(1);
    expect(cards[0]).toHaveAttribute("data-runtime-key", "alpha");
  });

  it("never claims an empty registry when the unscoped read serves registered runtimes", async () => {
    renderRuntimes("/portal/runtimes", { runtimes: [], registered_runtimes: [runtimeA] });
    await screen.findByRole("heading", { level: 1, name: "Runtimes" });
    expect(await screen.findByText("No compatible runtimes")).toBeInTheDocument();
    expect(screen.getByText(/1 runtime is registered with this deployment/)).toBeInTheDocument();
    expect(screen.queryByText("No runtimes registered")).toBeNull();
  });

  it("names the project scope when the scoped read serves no compatible runtime", async () => {
    renderRuntimes(`/portal/runtimes?projectId=${projectId}`, { runtimes: [], registered_runtimes: [runtimeA, runtimeB] });
    await screen.findByRole("heading", { level: 1, name: "Runtimes" });
    expect(await screen.findByText("No compatible runtimes")).toBeInTheDocument();
    expect(screen.getByText(/2 runtimes are registered with this deployment/)).toBeInTheDocument();
    expect(screen.getByText(/none is bound to this project/)).toBeInTheDocument();
  });

  it("renders the scoped project's compatible rows", async () => {
    renderRuntimes(`/portal/runtimes?projectId=${projectId}`, { runtimes: [runtimeA], registered_runtimes: [runtimeA, runtimeB] });
    await screen.findByRole("heading", { level: 1, name: "Runtimes" });
    const cards = await screen.findAllByRole("article");
    expect(cards).toHaveLength(1);
    expect(cards[0]).toHaveAttribute("data-runtime-key", "alpha");
  });
});
