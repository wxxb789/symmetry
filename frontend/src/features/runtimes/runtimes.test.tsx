import { screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { jsonResponse, portal } from "../../app/portal-double";
import { renderRoute, trackApplications } from "../../app/test-harness";

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

const applications = trackApplications();

interface ServedRuntimeSet {
  runtimes: unknown[];
  registered_runtimes: unknown[];
}

function renderRuntimes(path: string, served: { scoped: ServedRuntimeSet; unscoped: ServedRuntimeSet }) {
  const body = (workspace: ServedRuntimeSet) => ({
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
  });
  portal.serve((request) => {
    if (request.method !== "GET" || request.url.pathname !== "/portal/api/workspace") return undefined;
    // The scoped and unscoped reads serve different rows, so a page that
    // ignored the URL's project scope cannot pass the scoped assertions.
    const scoped = request.url.searchParams.get("project_id") === projectId;
    return jsonResponse(body(scoped ? served.scoped : served.unscoped));
  });
  applications.push(renderRoute(path).application);
}

describe("runtimes page", () => {
  it("renders the served compatibility rows for the unscoped read", async () => {
    renderRuntimes("/portal/runtimes", { unscoped: { runtimes: [runtimeA], registered_runtimes: [runtimeA, runtimeB] }, scoped: { runtimes: [runtimeB], registered_runtimes: [runtimeA, runtimeB] } });
    await screen.findByRole("heading", { level: 1, name: "Runtimes" });
    const cards = await screen.findAllByRole("article");
    expect(cards).toHaveLength(1);
    expect(cards[0]).toHaveAttribute("data-runtime-key", "alpha");
  });

  it("never claims an empty registry when the unscoped read serves registered runtimes", async () => {
    renderRuntimes("/portal/runtimes", { unscoped: { runtimes: [], registered_runtimes: [runtimeA] }, scoped: { runtimes: [runtimeB], registered_runtimes: [runtimeA] } });
    await screen.findByRole("heading", { level: 1, name: "Runtimes" });
    expect(await screen.findByText("No compatible runtimes")).toBeInTheDocument();
    expect(screen.getByText(/1 runtime is registered with this deployment/)).toBeInTheDocument();
    expect(screen.queryByText("No runtimes registered")).toBeNull();
  });

  it("names the project scope when the scoped read serves no compatible runtime", async () => {
    renderRuntimes(`/portal/runtimes?projectId=${projectId}`, { scoped: { runtimes: [], registered_runtimes: [runtimeA, runtimeB] }, unscoped: { runtimes: [runtimeA], registered_runtimes: [runtimeA, runtimeB] } });
    await screen.findByRole("heading", { level: 1, name: "Runtimes" });
    expect(await screen.findByText("No compatible runtimes")).toBeInTheDocument();
    expect(screen.getByText(/2 runtimes are registered with this deployment/)).toBeInTheDocument();
    expect(screen.getByText(/none is bound to this project/)).toBeInTheDocument();
  });

  it("renders only the scoped project's compatible rows", async () => {
    renderRuntimes(`/portal/runtimes?projectId=${projectId}`, { scoped: { runtimes: [runtimeB], registered_runtimes: [runtimeA, runtimeB] }, unscoped: { runtimes: [runtimeA], registered_runtimes: [runtimeA, runtimeB] } });
    await screen.findByRole("heading", { level: 1, name: "Runtimes" });
    const cards = await screen.findAllByRole("article");
    expect(cards).toHaveLength(1);
    expect(cards[0]).toHaveAttribute("data-runtime-key", "beta");
    expect(screen.queryByText("Runtime alpha")).toBeNull();
    // The rows prove which response was rendered; the request proves the page
    // asked for the project's own scope instead of the unscoped read.
    const workspaceRequests = portal.requests
      .filter((request) => request.path.startsWith("/portal/api/workspace"))
      .map((request) => request.path);
    expect(workspaceRequests).toContain(`/portal/api/workspace?project_id=${projectId}`);
    expect(workspaceRequests).not.toContain("/portal/api/workspace");
  });
});
