import type { RouterHistory } from "@tanstack/react-router";
import { act, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import type { Application } from "../../app/application";
import { portal } from "../../app/portal-double";
import type { PortalResolver, PortalWorkItem } from "../../app/portal-double";
import { renderRoute, trackApplications } from "../../app/test-harness";
import type { Connection } from "../../services/api";

const connectionId = "20000000-0000-4000-8000-000000000001";
const projectId = "10000000-0000-4000-8000-000000000001";
const resourceId = "30000000-0000-4000-8000-000000000001";
const secondProjectId = "10000000-0000-4000-8000-000000000002";
const secondConnectionId = "20000000-0000-4000-8000-000000000002";
const alphaId = "30000000-0000-4000-8000-00000000000a";
const betaId = "30000000-0000-4000-8000-00000000000b";

const providers = [
  { id: "github", auth_type: "gh_cli", capabilities: ["repositories", "work_items", "changes", "ci"] },
  { id: "azure_devops", auth_type: "entra_id", capabilities: ["repositories", "work_items", "changes", "ci"] },
];

function connection(overrides: Partial<Connection> = {}): Connection {
  return {
    id: connectionId,
    provider: "github",
    name: "GitHub engineering",
    account_ref: "acme",
    auth_type: "gh_cli",
    capabilities: ["repositories", "work_items", "changes", "ci"],
    status: "healthy",
    status_message: null,
    metadata: {},
    last_checked_at: "2026-11-16T08:30:00.000000Z",
    version: 3,
    resources: [
      {
        id: resourceId,
        project_id: projectId,
        project_key: "EXT",
        project_name: "Connected engineering",
        project_status: "active",
        kind: "repository",
        name: "symmetry",
        provider: "github",
        external_ref: "acme/symmetry",
        url: "https://github.com/acme/symmetry",
        status: "healthy",
        sync_status: "synced",
        status_message: null,
        metadata: {},
        last_checked_at: "2026-11-16T08:31:00.000000Z",
        last_synced_at: "2026-11-16T08:31:00.000000Z",
        version: 4,
      },
    ],
    ...overrides,
  };
}

// The second served connection: the row the operator switches to while a write
// for the first one is still in flight.
const secondConnection = () => connection({ id: secondConnectionId, name: "Second connection" });

interface ProjectFixture { id: string; key: string; name: string; description: string | null; status: string; work_items: PortalWorkItem[]; resources: Record<string, unknown>[] }
interface RuntimeFixture { runtime_id: string; runtime_name: string; agent_profile: string | null; workspace: string | null }

function projectFixture(overrides: Partial<ProjectFixture> = {}): ProjectFixture {
  return { id: projectId, key: "EXT", name: "Connected engineering", description: null, status: "active", work_items: [], resources: [], ...overrides };
}

// PortalJSON.resource/1 as served inside a project row.
function resourceFixture(overrides: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    id: alphaId,
    project_id: projectId,
    connection_id: connectionId,
    kind: "repository",
    name: "checkout service",
    provider: "github",
    external_ref: "acme/checkout",
    url: "https://github.com/acme/checkout",
    status: "healthy",
    sync_status: "synced",
    status_message: null,
    metadata: {},
    last_checked_at: "2026-11-16T08:30:00.000000Z",
    last_synced_at: "2026-11-16T08:31:00.000000Z",
    version: 4,
    ...overrides,
  };
}

const optionLabels = (select: HTMLElement) => within(select).getAllByRole("option").map((option) => option.textContent);

function resourceRow(name: string) {
  const row = screen.getByRole("heading", { level: 3, name }).closest("li");
  if (row === null) throw new Error(`no resource row for ${name}`);
  return within(row);
}

interface Harness {
  application: Application;
  history: RouterHistory;
  setConnections: (value: Connection[]) => void;
  setProjects: (value: ProjectFixture[]) => void;
  failReads: (response: () => Response) => void;
  failWorkspaceReads: (response: () => Response) => void;
  gateReads: (gate: Promise<void> | null) => void;
  gateWorkspaceReads: (gate: Promise<void> | null) => void;
}

const applications = trackApplications();

function renderConnections(
  serve: PortalResolver,
  path = "/portal/connections",
  options: { deferReads?: Promise<void>; connections?: Connection[]; projects?: ProjectFixture[]; registeredRuntimes?: RuntimeFixture[] } = {},
) {
  let readFailure: (() => Response) | null = null;
  let workspaceFailure: (() => Response) | null = null;
  let readGate: Promise<void> | null = options.deferReads ?? null;
  let workspaceGate: Promise<void> | null = null;
  portal.seed({
    workspace: {
      selectedProjectId: projectId,
      projects: options.projects ?? [],
      registeredRuntimes: options.registeredRuntimes ?? [],
    },
    connections: { providers, connections: options.connections ?? [connection()] },
  });
  portal.serve(async (request) => {
    if (request.method !== "GET") return undefined;
    if (request.url.pathname === "/portal/api/connections") {
      if (readGate !== null) await readGate;
      if (readFailure !== null) return readFailure();
    }
    if (request.url.pathname === "/portal/api/workspace") {
      if (workspaceGate !== null) await workspaceGate;
      if (workspaceFailure !== null) return workspaceFailure();
    }
    return undefined;
  });
  portal.serve(serve);
  const { application, history } = renderRoute(path);
  applications.push(application);
  return {
    application,
    history,
    setConnections: (value: Connection[]) => { portal.state.connections.connections = value; },
    setProjects: (value: ProjectFixture[]) => { portal.state.workspace.projects = value; },
    failReads: (response: () => Response) => { readFailure = response; },
    failWorkspaceReads: (response: () => Response) => { workspaceFailure = response; },
    gateReads: (gate: Promise<void> | null) => { readGate = gate; },
    gateWorkspaceReads: (gate: Promise<void> | null) => { workspaceGate = gate; },
  };
}

// The remount and late-response cases all serve the same two-connection list
// before their first case-specific click.
async function startTwoConnections(harness: Harness) {
  const second = secondConnection();
  const user = userEvent.setup();
  harness.setConnections([connection(), second]);
  await user.click(await screen.findByRole("button", { name: "Refresh" }));
  return { second, user };
}

const refusal = (status: number, code: string, message: string, fields: Record<string, string[]> | null = null, extra: Record<string, unknown> = {}) =>
  new Response(JSON.stringify({ error: { code, message, ...(fields ? { fields } : {}) }, ...extra }), { status });

describe("connection management", () => {
  it("lists providers, capabilities and bound resource state without inventing values", async () => {
    renderConnections(() => undefined);
    expect(await screen.findByRole("heading", { level: 1, name: "Connections" })).toBeVisible();
    const row = await screen.findByRole("button", { name: /GitHub engineering/ });
    expect(row).toHaveTextContent("acme · GitHub CLI · HTTPS");
    expect(row).toHaveTextContent("Repositories");
    await userEvent.setup().click(row);
    expect(await screen.findByRole("complementary", { name: "Connection detail" })).toHaveTextContent("symmetry");
    expect(screen.getByText(/1 bound · 0 needing attention/)).toBeVisible();
  });

  it("renders an unknown status and capability instead of hiding them", async () => {
    const harness = renderConnections(() => undefined);
    harness.setConnections([connection({ status: "future_state", capabilities: ["future_capability"], status_message: "Provider authentication failed" })]);
    await userEvent.setup().click(await screen.findByRole("button", { name: "Refresh" }));
    expect(await screen.findByText("Unknown connection status: future_state")).toBeVisible();
    expect(screen.getByText("Unknown capability: future_capability")).toBeVisible();
    expect(screen.getByText("Provider authentication failed")).toBeVisible();
  });

  it("submits a new connection once while pending and reconciles to the server list", async () => {
    const { promise: pending, resolve: release } = Promise.withResolvers<Response>();
    const harness = renderConnections(({ method }) => method === "POST"
      ? pending
      : undefined);
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: "New connection" }));
    await user.type(screen.getByLabelText("Name"), "Azure delivery");
    await user.type(screen.getByLabelText("Account or organization"), "acme");
    const submit = screen.getByRole("button", { name: "Connect" });
    await user.click(submit);
    expect(screen.getByRole("button", { name: "Saving…" })).toBeDisabled();
    await user.click(screen.getByRole("button", { name: "Saving…" }));
    harness.setConnections([connection({ id: connectionId, name: "Azure delivery" })]);
    await act(async () => {
      release(new Response(JSON.stringify({ disposition: "created", connection: { id: connectionId } }), { status: 201 }));
      await pending;
    });
    await waitFor(() => { expect(screen.queryByRole("button", { name: "Saving…" })).toBeNull(); });
    expect(portal.requests.filter((request) => request.method === "POST")).toHaveLength(1);
    expect(await screen.findByText("Connection created.")).toBeVisible();
    expect(portal.requests.filter((request) => request.method === "POST")[0]?.body).toEqual({
      provider: "github",
      name: "Azure delivery",
      account_ref: "acme",
      capabilities: ["repositories", "work_items", "changes", "ci"],
    });
  });

  it("renders the server's refusal code, message and field errors", async () => {
    renderConnections(({ method }) => method === "POST"
      ? refusal(422, "validation_failed", "request contains invalid fields", { name: ["has already been taken"] })
      : undefined);
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: "New connection" }));
    await user.type(screen.getByLabelText("Name"), "GitHub engineering");
    await user.type(screen.getByLabelText("Account or organization"), "acme");
    await user.click(screen.getByRole("button", { name: "Connect" }));
    expect(await screen.findByText("has already been taken")).toBeVisible();
    expect(screen.getByText(/request contains invalid fields \(validation_failed\)/)).toBeVisible();
  });

  it("shows the server's refusal for a stale capability update and keeps the server state", async () => {
    const harness = renderConnections(({ method }) => method === "PATCH"
      ? refusal(409, "stale", "resource changed since it was loaded")
      : undefined);
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: /GitHub engineering/ }));
    await user.click(await screen.findByRole("button", { name: "Edit capabilities" }));
    await user.click(screen.getByRole("checkbox", { name: "CI" }));
    harness.setConnections([connection({ version: 4, capabilities: ["repositories", "work_items", "changes", "ci"] })]);
    await user.click(screen.getByRole("button", { name: "Save" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("resource changed since it was loaded (stale)");
    expect(screen.getByText(/changed elsewhere; close and reopen/)).toBeVisible();
    expect(screen.getByRole("checkbox", { name: "CI" })).toBeDisabled();
    expect(portal.requests.filter((request) => request.method === "PATCH")[0]?.body).toEqual({
      version: 3,
      name: "GitHub engineering",
      capabilities: ["repositories", "work_items", "changes"],
    });
  });

  it("keeps a failed provider check actionable and renders the server's own reason", async () => {
    const harness = renderConnections(({ method, path }) => {
      if (method !== "POST" || !path.endsWith("/check")) return undefined;
      harness.setConnections([connection({ version: 4, status: "degraded", status_message: "Provider authentication failed", last_checked_at: "2026-11-16T09:00:00.000000Z" })]);
      return refusal(502, "provider_unauthorized", "external provider authentication failed", null, {
        disposition: "checked",
        connection: connection({ version: 4, status: "degraded", status_message: "Provider authentication failed" }),
      });
    });
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: /GitHub engineering/ }));
    await user.click(await screen.findByRole("button", { name: "Test connection" }));
    expect(await screen.findByText(/external provider authentication failed \(provider_unauthorized\)/)).toBeVisible();
    const detail = screen.getByRole("complementary", { name: "Connection detail" });
    expect(within(detail).getByText("Provider authentication failed")).toBeVisible();
    expect(within(detail).getByText("Degraded")).toBeVisible();
    // The served strings are already redacted by the server; this case proves
    // the surface renders them without inventing a credential of its own.
    expect(screen.queryByText(/gho_|Bearer |token/i)).toBeNull();
  });

  it("renders prototype-name served values as unknown text, never as a known label or a function", async () => {
    const hostile = connection({
      provider: "constructor",
      auth_type: "toString",
      capabilities: ["constructor"],
      status: "constructor",
      resources: [{
        ...connection().resources[0]!,
        // `kind`, `status` and `sync_status` are open served strings too.
        kind: "constructor",
        status: "toString",
        sync_status: "constructor",
      }],
    });
    renderConnections(() => undefined, "/portal/connections", {
      connections: [hostile],
      projects: [projectFixture({ resources: [resourceFixture({ kind: "constructor", status: "toString", sync_status: "constructor" })] })],
    });
    const list = await screen.findByRole("list", { name: "Connections" });
    // Own-key lookups only: a plain index would return Object's own members, so
    // this would render "Constructor", a function or crash the row.
    expect(within(list).getByText("Unknown provider: constructor")).toBeVisible();
    expect(within(list).getByText(/Unknown authentication: toString/)).toBeVisible();
    expect(within(list).getByText("Unknown capability: constructor")).toBeVisible();
    expect(within(list).getByText("Unknown connection status: constructor")).toBeVisible();
    const resources = await screen.findByRole("list", { name: "Project resources" });
    expect(within(resources).getByText("Unknown kind: constructor")).toBeVisible();
    expect(within(resources).getByText("Unknown resource status: toString")).toBeVisible();
    expect(within(resources).getByText("Unknown sync status: constructor")).toBeVisible();
  });

  it("confirms a removal against the version the dialog opened with, even after a background refresh", async () => {
    const harness = renderConnections(({ method }) => method === "DELETE"
      ? refusal(409, "stale", "resource changed since it was loaded")
      : undefined);
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: /GitHub engineering/ }));
    await user.click(await screen.findByRole("button", { name: "Remove" }));
    // A background refresh loads the row another operator already changed while
    // the confirmation dialog is open; the removal stays fenced to the version
    // the operator confirmed instead of deleting the newer revision.
    harness.setConnections([connection({ version: 4, name: "Changed elsewhere" })]);
    await act(async () => { await harness.application.queryClient.refetchQueries({ queryKey: ["connections"] }); });
    await user.click(screen.getByRole("button", { name: "Remove connection" }));
    expect(await screen.findByText(/resource changed since it was loaded \(stale\)/)).toBeVisible();
    expect(screen.getByText(/The server's current state is shown instead\./)).toBeVisible();
    expect(portal.requests.filter((request) => request.method === "DELETE")[0]?.path).toBe(`/portal/api/connections/${connectionId}?version=3`);
  });

  it("re-renders reset bound-resource health after a capability update", async () => {
    const harness = renderConnections(({ method }) => method === "PATCH"
      ? new Response(JSON.stringify({ disposition: "updated", connection: connection({ version: 4, capabilities: ["repositories", "work_items"] }) }))
      : undefined);
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: /GitHub engineering/ }));
    expect(await screen.findByText("Synced")).toBeVisible();
    harness.setConnections([connection({
      version: 4,
      capabilities: ["repositories", "work_items"],
      status: "unknown",
      last_checked_at: null,
      resources: [{ ...connection().resources[0]!, status: "unknown", sync_status: "unknown", status_message: null, version: 5 }],
    })]);
    await user.click(screen.getByRole("button", { name: "Edit capabilities" }));
    await user.click(screen.getByRole("checkbox", { name: "CI" }));
    await user.click(screen.getByRole("button", { name: "Save" }));
    expect(await screen.findByText(/Bound resource health and sync state was reset/)).toBeVisible();
    await user.click(await screen.findByRole("button", { name: /GitHub engineering/ }));
    const resources = within(await screen.findByRole("list", { name: "Bound resources" }));
    expect(resources.getAllByText("Unknown")).toHaveLength(2);
    expect(resources.queryByText("Synced")).toBeNull();
  });

  it("a late create response never closes or hijacks a newer editor", async () => {
    const { promise: pending, resolve: release } = Promise.withResolvers<Response>();
    const harness = renderConnections(({ method }) =>
      method === "POST" ? pending : undefined);
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: "New connection" }));
    await user.type(screen.getByLabelText("Name"), "First connection");
    await user.type(screen.getByLabelText("Account or organization"), "first");
    await user.click(screen.getByRole("button", { name: "Connect" }));
    expect(screen.getByRole("button", { name: "Saving…" })).toBeDisabled();

    // The operator abandons the pending first editor and starts a second one.
    await user.click(screen.getByRole("button", { name: "Close connection dialog" }));
    expect(screen.queryByRole("dialog", { name: "New connection" })).toBeNull();
    await user.click(screen.getByRole("button", { name: "New connection" }));
    await user.type(screen.getByLabelText("Name"), "Second connection");
    await user.type(screen.getByLabelText("Account or organization"), "second");

    // The first submission lands late: it must not close the second editor or
    // navigate to the first connection.
    const firstId = "20000000-0000-4000-8000-000000000009";
    harness.setConnections([connection({ id: firstId, name: "First connection" })]);
    await act(async () => {
      release(new Response(JSON.stringify({ disposition: "created", connection: { id: firstId } }), { status: 201 }));
      await pending;
    });
    expect(screen.getByRole("dialog", { name: "New connection" })).toBeVisible();
    expect(screen.getByLabelText("Name")).toHaveValue("Second connection");
    expect(harness.history.location.search).not.toContain(firstId);
  });

  it("submits a draft against the version it was edited from after a background refresh", async () => {
    const harness = renderConnections(({ method, body }) => {
      if (method !== "PATCH") return undefined;
      const version = typeof body === "object" && body !== null && "version" in body ? body.version : undefined;
      return version === 3
        ? refusal(409, "stale", "resource changed since it was loaded")
        : new Response(JSON.stringify({ disposition: "updated", connection: connection({ version: 4 }) }), { status: 200 });
    });
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: /GitHub engineering/ }));
    await user.click(await screen.findByRole("button", { name: "Edit capabilities" }));
    await user.click(screen.getByRole("checkbox", { name: "CI" }));

    // A background refresh loads the row another operator already changed while
    // the editor is open; the draft and its edited-against version must stay put.
    harness.setConnections([connection({ version: 4, name: "Changed elsewhere" })]);
    await act(async () => { await harness.application.queryClient.refetchQueries({ queryKey: ["connections"] }); });
    expect(screen.getByLabelText("Name")).toHaveValue("GitHub engineering");
    expect(screen.getByRole("checkbox", { name: "CI" })).not.toBeChecked();

    await user.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => { expect(portal.requests.some((request) => request.method === "PATCH")).toBe(true); });
    expect(portal.requests.filter((request) => request.method === "PATCH")[0]?.body).toMatchObject({ version: 3 });
    expect(await screen.findByRole("alert")).toHaveTextContent("(stale)");
    expect(screen.getByText(/changed elsewhere; close and reopen/)).toBeVisible();
  });

  it("keeps provider check, update and removal mutually exclusive while one is pending", async () => {
    const { promise: pending, resolve: release } = Promise.withResolvers<Response>();
    renderConnections(({ method, path }) =>
      method === "POST" && path.endsWith("/check")
        ? pending
        : undefined);
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: /GitHub engineering/ }));
    await user.click(await screen.findByRole("button", { name: "Test connection" }));
    expect(screen.getByRole("button", { name: "Checking…" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Edit capabilities" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Remove" })).toBeDisabled();

    // Forced clicks must not start an editor or a removal while the check runs.
    screen.getByRole("button", { name: "Edit capabilities" }).dispatchEvent(new MouseEvent("click", { bubbles: true }));
    screen.getByRole("button", { name: "Remove" }).dispatchEvent(new MouseEvent("click", { bubbles: true }));
    expect(screen.queryByRole("dialog", { name: "Connection settings" })).toBeNull();
    expect(screen.queryByRole("dialog", { name: /Remove/ })).toBeNull();
    expect(portal.requests.filter((request) => request.method === "POST")).toHaveLength(1);

    await act(async () => {
      release(new Response(JSON.stringify({ disposition: "checked", connection: connection({ version: 4 }) }), { status: 200 }));
      await pending;
    });
    expect(await screen.findByRole("button", { name: "Test connection" })).toBeEnabled();
    expect(portal.requests.filter((request) => request.method === "POST")).toHaveLength(1);
  });

  it("keeps check and removal blocked while an editor save is pending", async () => {
    const { promise: pending, resolve: release } = Promise.withResolvers<Response>();
    renderConnections(({ method }) =>
      method === "PATCH" ? pending : undefined);
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: /GitHub engineering/ }));
    await user.click(screen.getByRole("button", { name: "Edit capabilities" }));
    await user.click(screen.getByRole("checkbox", { name: "CI" }));
    await user.click(screen.getByRole("button", { name: "Save" }));
    // The operator closes the editor while the update is pending; its write
    // still holds the surface until it settles.
    await user.click(screen.getByRole("button", { name: "Close connection dialog" }));
    expect(screen.queryByRole("dialog", { name: "Connection settings" })).toBeNull();

    expect(screen.getByRole("button", { name: "Test connection" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Remove" })).toBeDisabled();
    screen.getByRole("button", { name: "Test connection" }).dispatchEvent(new MouseEvent("click", { bubbles: true }));
    screen.getByRole("button", { name: "Remove" }).dispatchEvent(new MouseEvent("click", { bubbles: true }));
    expect(screen.queryByRole("dialog", { name: /Remove/ })).toBeNull();
    expect(portal.requests.filter((request) => request.method === "POST")).toHaveLength(0);
    expect(portal.requests.filter((request) => request.method === "DELETE")).toHaveLength(0);

    await act(async () => {
      release(new Response(JSON.stringify({ disposition: "updated", connection: connection({ version: 4 }) }), { status: 200 }));
      await pending;
    });
    expect(await screen.findByRole("button", { name: "Test connection" })).toBeEnabled();
    expect(portal.requests.filter((request) => request.method === "PATCH")).toHaveLength(1);
  });

  it("keeps check and editing blocked while a removal is pending", async () => {
    const { promise: pending, resolve: release } = Promise.withResolvers<Response>();
    const harness = renderConnections(({ method }) =>
      method === "DELETE" ? pending : undefined);
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: /GitHub engineering/ }));
    await user.click(await screen.findByRole("button", { name: "Remove" }));
    await user.click(screen.getByRole("button", { name: "Remove connection" }));
    expect(screen.getByRole("button", { name: "Removing…" })).toBeDisabled();
    // The operator closes the confirmation dialog while the removal is pending;
    // the write still holds the surface until it settles.
    await user.click(screen.getByRole("button", { name: "Close removal dialog" }));
    expect(screen.queryByRole("dialog", { name: /Remove GitHub engineering/ })).toBeNull();

    expect(screen.getByRole("button", { name: "Test connection" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Edit capabilities" })).toBeDisabled();
    screen.getByRole("button", { name: "Test connection" }).dispatchEvent(new MouseEvent("click", { bubbles: true }));
    screen.getByRole("button", { name: "Edit capabilities" }).dispatchEvent(new MouseEvent("click", { bubbles: true }));
    expect(screen.queryByRole("dialog", { name: "Connection settings" })).toBeNull();
    expect(portal.requests.filter((request) => request.method === "POST")).toHaveLength(0);
    expect(portal.requests.filter((request) => request.method === "PATCH")).toHaveLength(0);

    await act(async () => {
      release(new Response(JSON.stringify({ disposition: "deleted", deleted_connection_id: connectionId }), { status: 200 }));
      await pending;
    });
    await waitFor(() => { expect(harness.history.location.search).not.toContain(connectionId); });
    expect(portal.requests.filter((request) => request.method === "DELETE")).toHaveLength(1);
  });

  it("drops a provider check receipt that arrives after the operator selected another connection", async () => {
    const { promise: pending, resolve: release } = Promise.withResolvers<Response>();
    const harness = renderConnections(({ method, path }) =>
      method === "POST" && path.endsWith("/check") ? pending : undefined);
    const { second, user } = await startTwoConnections(harness);
    await user.click(await screen.findByRole("button", { name: /GitHub engineering/ }));
    await user.click(screen.getByRole("button", { name: "Test connection" }));
    expect(screen.getByRole("button", { name: "Checking…" })).toBeDisabled();

    // The operator moves to another connection while the check is in flight:
    // the pending check belongs to the first connection and must not surface
    // on the second one, not even when its refusal arrives late.
    await user.click(screen.getByRole("button", { name: /Second connection/ }));
    expect(await screen.findByRole("complementary", { name: "Connection detail" })).toHaveTextContent("Second connection");
    expect(screen.getByRole("button", { name: "Test connection" })).toBeEnabled();

    harness.setConnections([connection({ version: 4, status: "degraded", status_message: "Provider authentication failed" }), second]);
    await act(async () => {
      release(refusal(502, "provider_unauthorized", "external provider authentication failed"));
      await pending;
    });
    expect(screen.queryByText(/provider_unauthorized/)).toBeNull();
    expect(screen.getByRole("complementary", { name: "Connection detail" })).toHaveTextContent("Second connection");
    expect(screen.getByRole("button", { name: "Test connection" })).toBeEnabled();
  });

  it("keeps a pending provider check blocked after the detail remounts", async () => {
    const { promise: pending, resolve: release } = Promise.withResolvers<Response>();
    const harness = renderConnections(({ method, path }) =>
      method === "POST" && path.endsWith("/check") ? pending : undefined);
    const { user } = await startTwoConnections(harness);
    await user.click(await screen.findByRole("button", { name: /GitHub engineering/ }));
    await user.click(screen.getByRole("button", { name: "Test connection" }));
    expect(screen.getByRole("button", { name: "Checking…" })).toBeDisabled();

    // The operator inspects the other connection and comes back while the check
    // is still in flight: the request belongs to the connection, so the
    // remounted detail must not offer a second check.
    await user.click(screen.getByRole("button", { name: /Second connection/ }));
    expect(await screen.findByRole("complementary", { name: "Connection detail" })).toHaveTextContent("Second connection");
    expect(screen.getByRole("button", { name: "Test connection" })).toBeEnabled();
    await user.click(screen.getByRole("button", { name: /GitHub engineering/ }));
    const detail = await screen.findByRole("complementary", { name: "Connection detail" });
    expect(detail).toHaveTextContent("GitHub engineering");
    expect(within(detail).getByRole("button", { name: "Checking…" })).toBeDisabled();
    expect(within(detail).getByRole("button", { name: "Edit capabilities" })).toBeDisabled();
    expect(within(detail).getByRole("button", { name: "Remove" })).toBeDisabled();
    within(detail).getByRole("button", { name: "Checking…" }).dispatchEvent(new MouseEvent("click", { bubbles: true }));
    expect(portal.requests.filter((request) => request.method === "POST")).toHaveLength(1);

    await act(async () => {
      release(new Response(JSON.stringify({ disposition: "checked", connection: connection({ version: 4 }) }), { status: 200 }));
      await pending;
    });
    expect(await screen.findByRole("button", { name: "Test connection" })).toBeEnabled();
    expect(portal.requests.filter((request) => request.method === "POST")).toHaveLength(1);
  });

  it("keeps a pending removal blocked after the detail remounts", async () => {
    const { promise: pending, resolve: release } = Promise.withResolvers<Response>();
    const harness = renderConnections(({ method }) =>
      method === "DELETE" ? pending : undefined);
    const { second, user } = await startTwoConnections(harness);
    await user.click(await screen.findByRole("button", { name: /GitHub engineering/ }));
    await user.click(screen.getByRole("button", { name: "Remove" }));
    await user.click(screen.getByRole("button", { name: "Remove connection" }));
    expect(screen.getByRole("button", { name: "Removing…" })).toBeDisabled();

    // The operator closes the confirmation, inspects the other connection and
    // comes back while the removal is still in flight: the removal belongs to
    // the connection, so the remounted detail must not offer a second one.
    await user.click(screen.getByRole("button", { name: "Close removal dialog" }));
    await user.click(screen.getByRole("button", { name: /Second connection/ }));
    expect(await screen.findByRole("complementary", { name: "Connection detail" })).toHaveTextContent("Second connection");
    expect(screen.getByRole("button", { name: "Test connection" })).toBeEnabled();
    await user.click(screen.getByRole("button", { name: /GitHub engineering/ }));
    const detail = await screen.findByRole("complementary", { name: "Connection detail" });
    expect(detail).toHaveTextContent("GitHub engineering");
    expect(within(detail).getByRole("button", { name: "Test connection" })).toBeDisabled();
    expect(within(detail).getByRole("button", { name: "Edit capabilities" })).toBeDisabled();
    expect(within(detail).getByRole("button", { name: "Remove" })).toBeDisabled();
    within(detail).getByRole("button", { name: "Remove" }).dispatchEvent(new MouseEvent("click", { bubbles: true }));
    expect(screen.queryByRole("dialog", { name: /Remove GitHub engineering/ })).toBeNull();
    expect(portal.requests.filter((request) => request.method === "DELETE")).toHaveLength(1);

    harness.setConnections([second]);
    await act(async () => {
      release(new Response(JSON.stringify({ disposition: "deleted", deleted_connection_id: connectionId }), { status: 200 }));
      await pending;
    });
    await waitFor(() => { expect(screen.queryByRole("button", { name: /GitHub engineering/ })).toBeNull(); });
    expect(harness.history.location.search).not.toContain(connectionId);
  });

  it("surfaces a check refusal that settles after the operator returned to the same connection", async () => {
    const { promise: pending, resolve: release } = Promise.withResolvers<Response>();
    const harness = renderConnections(({ method, path }) =>
      method === "POST" && path.endsWith("/check") ? pending : undefined);
    const { second, user } = await startTwoConnections(harness);
    await user.click(await screen.findByRole("button", { name: /GitHub engineering/ }));
    await user.click(screen.getByRole("button", { name: "Test connection" }));

    // The operator leaves and returns while the check is in flight: the refusal
    // belongs to the connection the operator is looking at again, so its reason
    // and next step must be surfaced rather than dropped with the old detail.
    await user.click(screen.getByRole("button", { name: /Second connection/ }));
    expect(await screen.findByRole("complementary", { name: "Connection detail" })).toHaveTextContent("Second connection");
    await user.click(screen.getByRole("button", { name: /GitHub engineering/ }));
    expect(await screen.findByRole("complementary", { name: "Connection detail" })).toHaveTextContent("GitHub engineering");
    harness.setConnections([connection({ version: 4, status: "degraded", status_message: "Provider authentication failed" }), second]);
    await act(async () => {
      release(refusal(502, "provider_unauthorized", "external provider authentication failed"));
      await pending;
    });
    expect(await screen.findByText(/external provider authentication failed \(provider_unauthorized\)/)).toBeVisible();
    expect(screen.getByText(/Reauthenticate the provider session/)).toBeVisible();
  });

  it("does not clear the newly selected connection when a removal settles late", async () => {
    const { promise: pending, resolve: release } = Promise.withResolvers<Response>();
    const harness = renderConnections(({ method }) =>
      method === "DELETE" ? pending : undefined);
    const { second, user } = await startTwoConnections(harness);
    await user.click(await screen.findByRole("button", { name: /GitHub engineering/ }));
    await user.click(await screen.findByRole("button", { name: "Remove" }));
    await user.click(screen.getByRole("button", { name: "Remove connection" }));
    // The operator abandons the pending removal and inspects the other
    // connection; the late completion must not clear that selection.
    await user.click(screen.getByRole("button", { name: "Close removal dialog" }));
    await user.click(screen.getByRole("button", { name: /Second connection/ }));
    expect(await screen.findByRole("complementary", { name: "Connection detail" })).toHaveTextContent("Second connection");

    harness.setConnections([second]);
    await act(async () => {
      release(new Response(JSON.stringify({ disposition: "deleted", deleted_connection_id: connectionId }), { status: 200 }));
      await pending;
    });
    await waitFor(() => { expect(screen.queryByRole("button", { name: /GitHub engineering/ })).toBeNull(); });
    expect(harness.history.location.search).toContain(second.id);
  });

  it("states a removal refusal whose connection the change-discovery re-read removed first", async () => {
    const { promise: pending, resolve: release } = Promise.withResolvers<Response>();
    const harness = renderConnections(({ method }) =>
      method === "DELETE" ? pending : undefined);
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: /GitHub engineering/ }));
    await user.click(screen.getByRole("button", { name: "Remove" }));
    await user.click(screen.getByRole("button", { name: "Remove connection" }));
    expect(screen.getByRole("button", { name: "Removing…" })).toBeDisabled();

    // Another operator's removal reaches this page as an invalidation, and the
    // re-read settles before this removal's own refused answer: the served list
    // no longer holds the connection, so the reconcile has already cleared the
    // selection when the refusal arrives. The operator issued the removal, so
    // its reason and next step must still be stated instead of dropped with the
    // vanished detail.
    harness.setConnections([]);
    await act(async () => {
      await harness.application.queryClient.invalidateQueries({ queryKey: ["connections"] });
    });
    await waitFor(() => { expect(harness.history.location.search).not.toContain(connectionId); });
    await act(async () => {
      release(refusal(404, "not_found", "this connection no longer exists"));
      await pending;
    });
    expect(await screen.findByText(/this connection no longer exists \(not_found\)/)).toBeVisible();
    expect(screen.getByText(/The server's current state is shown instead\./)).toBeVisible();
  });

  it("does not navigate back when the operator selects another connection during the save's re-read", async () => {
    const harness = renderConnections(({ method }) => method === "PATCH"
      ? new Response(JSON.stringify({ disposition: "updated", connection: connection({ version: 4 }) }), { status: 200 })
      : undefined);
    const { second, user } = await startTwoConnections(harness);
    await user.click(await screen.findByRole("button", { name: /GitHub engineering/ }));
    await user.click(screen.getByRole("button", { name: "Edit capabilities" }));
    await user.click(screen.getByRole("checkbox", { name: "CI" }));

    // The authoritative re-read behind the save is still in flight when the
    // operator selects the other connection.
    const { promise: gate, resolve: release } = Promise.withResolvers<void>();
    harness.gateReads(gate);
    await user.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => { expect(screen.queryByRole("dialog", { name: "Connection settings" })).toBeNull(); });
    await waitFor(() => { expect(portal.requests.filter((request) => request.method === "GET" && request.path === "/portal/api/connections")).toHaveLength(3); });
    await user.click(screen.getByRole("button", { name: /Second connection/ }));
    expect(await screen.findByRole("complementary", { name: "Connection detail" })).toHaveTextContent("Second connection");

    release();
    harness.gateReads(null);
    await waitFor(() => { expect(screen.queryByText("Refreshing connections")).toBeNull(); });
    expect(harness.history.location.search).toContain(second.id);
    expect(harness.history.location.search).not.toContain(connectionId);
  });

  it("does not select the saved connection when another editor opened during the save's re-read", async () => {
    const harness = renderConnections(({ method }) =>
      method === "POST"
        ? new Response(JSON.stringify({ disposition: "created", connection: { id: connectionId } }), { status: 201 })
        : undefined);
    const user = userEvent.setup();
    await screen.findByRole("button", { name: /GitHub engineering/ });
    await user.click(screen.getByRole("button", { name: "New connection" }));
    await user.type(screen.getByLabelText("Name"), "Azure delivery");
    await user.type(screen.getByLabelText("Account or organization"), "acme");

    // The authoritative re-read behind the save is still in flight when the
    // operator starts another connection.
    const { promise: gate, resolve: release } = Promise.withResolvers<void>();
    harness.gateReads(gate);
    await user.click(screen.getByRole("button", { name: "Connect" }));
    await waitFor(() => { expect(screen.queryByRole("dialog", { name: "New connection" })).toBeNull(); });
    await waitFor(() => expect(screen.getByText("Refreshing connections")).toBeVisible());
    await user.click(screen.getByRole("button", { name: "New connection" }));
    await user.type(screen.getByLabelText("Name"), "Second connection");

    // The re-read completes while the second editor is open: the selection must
    // stay where the operator is, not move to the first save's connection.
    harness.setConnections([connection({ id: connectionId, name: "Azure delivery" })]);
    release();
    harness.gateReads(null);
    await waitFor(() => { expect(screen.queryByText("Refreshing connections")).toBeNull(); });
    // The selection move is a continuation of the same settle: flush one
    // macrotask so it has run before the URL is asserted.
    const flush = Promise.withResolvers<void>();
    setTimeout(flush.resolve, 0);
    await act(async () => { await flush.promise; });
    expect(harness.history.location.search).not.toContain(connectionId);
    expect(screen.getByRole("dialog", { name: "New connection" })).toBeVisible();
    expect(screen.getByLabelText("Name")).toHaveValue("Second connection");
  });

  it("does not pull the operator back after they left the page while a save was pending", async () => {
    const { promise: pending, resolve: release } = Promise.withResolvers<Response>();
    const harness = renderConnections(({ method }) =>
      method === "PATCH" ? pending : undefined);
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: /GitHub engineering/ }));
    await user.click(screen.getByRole("button", { name: "Edit capabilities" }));
    await user.click(screen.getByRole("checkbox", { name: "CI" }));
    await user.click(screen.getByRole("button", { name: "Save" }));

    // The operator leaves the connections page while the write is pending; the
    // late response must not navigate them back.
    act(() => { harness.history.push("/portal/projects"); });
    await act(async () => {
      release(new Response(JSON.stringify({ disposition: "updated", connection: connection({ version: 4 }) }), { status: 200 }));
      await pending;
    });
    expect(harness.history.location.pathname).toBe("/portal/projects");
  });

  it("returns keyboard focus to the invoking control when either dialog closes", async () => {
    renderConnections(() => undefined);
    const user = userEvent.setup();

    const create = await screen.findByRole("button", { name: "New connection" });
    await user.click(create);
    expect(await screen.findByRole("dialog", { name: "New connection" })).toBeVisible();
    await user.keyboard("{Escape}");
    await waitFor(() => { expect(screen.queryByRole("dialog", { name: "New connection" })).toBeNull(); });
    expect(create).toHaveFocus();

    await user.click(await screen.findByRole("button", { name: /GitHub engineering/ }));
    const remove = await screen.findByRole("button", { name: "Remove" });
    await user.click(remove);
    expect(await screen.findByRole("dialog", { name: /Remove GitHub engineering/ })).toBeVisible();
    await user.keyboard("{Escape}");
    await waitFor(() => { expect(screen.queryByRole("dialog", { name: /Remove GitHub engineering/ })).toBeNull(); });
    expect(remove).toHaveFocus();
  });

  it("keeps focus in the detail when the editor closes while its save is pending", async () => {
    const { promise: pending, resolve: release } = Promise.withResolvers<Response>();
    renderConnections(({ method }) => method === "PATCH" ? pending : undefined);
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: /GitHub engineering/ }));
    await user.click(await screen.findByRole("button", { name: "Edit capabilities" }));
    await user.click(screen.getByRole("checkbox", { name: "CI" }));
    await user.click(screen.getByRole("button", { name: "Save" }));

    // The invoking control is disabled until the pending write settles; closing
    // must hand keyboard focus to a control the write cannot disable instead of
    // dropping it to the body.
    await user.click(screen.getByRole("button", { name: "Close connection dialog" }));
    await waitFor(() => { expect(screen.queryByRole("dialog", { name: "Connection settings" })).toBeNull(); });
    expect(document.activeElement).not.toBe(document.body);
    expect(screen.getByRole("button", { name: "Close connection detail" })).toHaveFocus();

    await act(async () => {
      release(new Response(JSON.stringify({ disposition: "updated", connection: connection({ version: 4 }) }), { status: 200 }));
      await pending;
    });
    expect(await screen.findByRole("button", { name: "Test connection" })).toBeEnabled();
  });

  it("keeps focus in the detail when the removal dialog closes while its removal is pending", async () => {
    const { promise: pending, resolve: release } = Promise.withResolvers<Response>();
    renderConnections(({ method }) => method === "DELETE" ? pending : undefined);
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: /GitHub engineering/ }));
    await user.click(screen.getByRole("button", { name: "Remove" }));
    await user.click(screen.getByRole("button", { name: "Remove connection" }));

    // The Remove button is disabled until the pending removal settles; closing
    // the confirmation must not drop keyboard focus to the body.
    await user.click(screen.getByRole("button", { name: "Close removal dialog" }));
    await waitFor(() => { expect(screen.queryByRole("dialog", { name: /Remove GitHub engineering/ })).toBeNull(); });
    expect(document.activeElement).not.toBe(document.body);
    expect(screen.getByRole("button", { name: "Close connection detail" })).toHaveFocus();

    await act(async () => {
      release(new Response(JSON.stringify({ disposition: "deleted", deleted_connection_id: connectionId }), { status: 200 }));
      await pending;
    });
  });

  it("hands focus to the surviving page control when a settled removal unmounts the detail", async () => {
    const { promise: pending, resolve: release } = Promise.withResolvers<Response>();
    const harness = renderConnections(({ method }) => method === "DELETE" ? pending : undefined);
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: /GitHub engineering/ }));
    await user.click(screen.getByRole("button", { name: "Remove" }));
    await user.click(screen.getByRole("button", { name: "Remove connection" }));

    // The operator closes the confirmation while the removal is still pending:
    // focus hands off to the detail's own close control, which is the only
    // control the pending write cannot disable.
    await user.click(screen.getByRole("button", { name: "Close removal dialog" }));
    await waitFor(() => { expect(screen.queryByRole("dialog", { name: /Remove GitHub engineering/ })).toBeNull(); });
    expect(screen.getByRole("button", { name: "Close connection detail" })).toHaveFocus();

    // The removal settles and the refetched list drops the connection: the
    // detail unmounts with the focused control, so the page-level control that
    // survives the detail takes the focus instead of the document body.
    harness.setConnections([]);
    await act(async () => {
      release(new Response(JSON.stringify({ disposition: "deleted", deleted_connection_id: connectionId }), { status: 200 }));
      await pending;
    });
    // The detail's own close control is the focused element that dies with the
    // unmount; the empty-state aside that replaces it carries the same label.
    await waitFor(() => { expect(screen.queryByRole("button", { name: "Close connection detail" })).toBeNull(); });
    expect(document.activeElement).not.toBe(document.body);
    expect(screen.getByRole("button", { name: "New connection" })).toHaveFocus();
  });

  it("keeps keyboard focus on the connection row across a background refresh", async () => {
    const harness = renderConnections(() => undefined);
    const row = await screen.findByRole("button", { name: /GitHub engineering/ });
    row.focus();
    expect(row).toHaveFocus();
    harness.setConnections([connection()]);
    await act(async () => { await harness.application.queryClient.refetchQueries({ queryKey: ["connections"] }); });
    expect(row).toHaveFocus();
  });

  it("keeps the success notice and surfaces the failure when the refresh after a write fails", async () => {
    const { promise: pending, resolve: release } = Promise.withResolvers<Response>();
    const harness = renderConnections(({ method }) =>
      method === "PATCH"
        ? pending
        : undefined);
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: /GitHub engineering/ }));
    await user.click(await screen.findByRole("button", { name: "Edit capabilities" }));
    await user.click(screen.getByRole("checkbox", { name: "CI" }));
    await user.click(screen.getByRole("button", { name: "Save" }));
    expect(screen.getByRole("button", { name: "Saving…" })).toBeDisabled();

    // The write lands, but the authoritative re-read fails: the receipt stays
    // and the failed refresh is reported against the last loaded data.
    harness.setConnections([connection({ version: 4, capabilities: ["repositories", "work_items", "changes"] })]);
    harness.failReads(() => refusal(503, "provider_failure", "connections read failed"));
    await act(async () => {
      release(new Response(JSON.stringify({ disposition: "updated", connection: connection({ version: 4 }) }), { status: 200 }));
      await pending;
    });
    expect(await screen.findByText(/Connection capabilities updated/)).toBeVisible();
    expect(screen.queryByRole("dialog", { name: "Connection settings" })).toBeNull();
    // The 503 read is retried twice with backoff before the stale error shows.
    expect(await screen.findByText("Refresh failed — showing last loaded data", undefined, { timeout: 5_000 })).toBeVisible();
    expect(screen.getByText(/The server is temporarily unavailable/)).toBeVisible();
  });

  it("issues a new workspace read when a refresh is clicked while a re-read is in flight", async () => {
    const harness = renderConnections(() => undefined);
    const user = userEvent.setup();
    expect(await screen.findByText("Project not found")).toBeVisible();

    // A re-read is held in flight while the page re-renders (a settled
    // connections re-read), so the handler that would decide on
    // `if (!workspace.isFetching)` holds `true` from that render through the
    // click - the stale-snapshot race this case records. The operator's own
    // refresh must still issue its read: React Query's manual refetch cancels
    // the read it supersedes because the query already has data.
    const { promise: gate, resolve: release } = Promise.withResolvers<void>();
    harness.gateWorkspaceReads(gate);
    const reads = () => portal.requests.filter((request) => request.path.startsWith("/portal/api/workspace")).length;
    const settled = reads();
    act(() => { void harness.application.queryClient.refetchQueries({ queryKey: ["workspace"] }); });
    await waitFor(() => { expect(reads()).toBe(settled + 1); });
    harness.setConnections([connection({ name: "GitHub delivery" })]);
    await act(async () => { await harness.application.queryClient.refetchQueries({ queryKey: ["connections"] }); });

    await user.click(screen.getByRole("button", { name: "Refresh" }));
    await waitFor(() => { expect(reads()).toBe(settled + 2); });
    release();
    await waitFor(() => { expect(harness.application.queryClient.isFetching({ queryKey: ["workspace"] })).toBe(0); });
    expect(screen.getByText("Project not found")).toBeVisible();
  });

  it("re-reads the project when Try again is clicked while a re-read is in flight", async () => {
    const harness = renderConnections(() => undefined);
    const user = userEvent.setup();
    expect(await screen.findByText("Project not found")).toBeVisible();

    // A failed re-read leaves the stale error and its retry: that retry is the
    // second control that gated on the render-time `isFetching` snapshot.
    harness.failWorkspaceReads(() => refusal(503, "provider_failure", "workspace read failed"));
    act(() => { void harness.application.queryClient.refetchQueries({ queryKey: ["workspace"] }); });
    expect(await screen.findByText("Refresh failed — showing last loaded data", undefined, { timeout: 5_000 })).toBeVisible();

    // A further re-read is held in flight while the page re-renders (a settled
    // connections re-read); the stale error stays on screen and the click must
    // still issue the operator's own read.
    const { promise: gate, resolve: release } = Promise.withResolvers<void>();
    harness.gateWorkspaceReads(gate);
    const reads = () => portal.requests.filter((request) => request.path.startsWith("/portal/api/workspace")).length;
    const settled = reads();
    act(() => { void harness.application.queryClient.refetchQueries({ queryKey: ["workspace"] }); });
    await waitFor(() => { expect(reads()).toBe(settled + 1); });
    harness.setConnections([connection({ name: "GitHub delivery" })]);
    await act(async () => { await harness.application.queryClient.refetchQueries({ queryKey: ["connections"] }); });

    await user.click(screen.getByRole("button", { name: "Try again" }));
    await waitFor(() => { expect(reads()).toBe(settled + 2); });
    release();
    await waitFor(() => { expect(harness.application.queryClient.isFetching({ queryKey: ["workspace"] })).toBe(0); }, { timeout: 5_000 });
  });

  it("keeps the server's repositories-for-changes coupling observable in the editor", async () => {
    renderConnections(() => undefined);
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: "New connection" }));
    const repositories = screen.getByRole("checkbox", { name: "Repositories" });
    const changes = screen.getByRole("checkbox", { name: "Changes" });
    expect(repositories).toBeChecked();
    expect(changes).toBeChecked();

    await user.click(repositories);
    expect(repositories).not.toBeChecked();
    expect(changes).not.toBeChecked();
    await user.click(changes);
    expect(changes).toBeChecked();
    expect(repositories).toBeChecked();
  });

  it("waits for the provider catalog before opening the create editor", async () => {
    const { promise: gate, resolve: release } = Promise.withResolvers<void>();
    renderConnections(() => undefined, "/portal/connections", { deferReads: gate });
    const user = userEvent.setup();
    // The first read (which carries the provider catalog) is still in flight:
    // creation must wait instead of opening a form whose provider select has
    // no option and whose submit would send an empty provider.
    const create = await screen.findByRole("button", { name: "New connection" });
    expect(create).toBeDisabled();
    create.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    expect(screen.queryByRole("dialog", { name: "New connection" })).toBeNull();

    release();
    await waitFor(() => expect(screen.getByRole("button", { name: "New connection" })).toBeEnabled());
    await user.click(screen.getByRole("button", { name: "New connection" }));
    expect(await screen.findByRole("dialog", { name: "New connection" })).toBeVisible();
    expect(screen.getByLabelText("Provider")).toHaveValue("github");
    expect(screen.getByRole("checkbox", { name: "Repositories" })).toBeChecked();
    await user.type(screen.getByLabelText("Name"), "Catalog complete");
    await user.type(screen.getByLabelText("Account or organization"), "acme");
    await user.click(screen.getByRole("button", { name: "Connect" }));
    await waitFor(() => { expect(portal.requests.filter((request) => request.method === "POST")).toHaveLength(1); });
    expect(portal.requests.find((request) => request.method === "POST")?.body).toEqual({
      provider: "github",
      name: "Catalog complete",
      account_ref: "acme",
      capabilities: providers[0]!.capabilities,
    });
  });

  it("closes the editor with a notice when the edited connection is removed elsewhere", async () => {
    const harness = renderConnections(() => undefined);
    const { second, user } = await startTwoConnections(harness);
    await user.click(await screen.findByRole("button", { name: /GitHub engineering/ }));
    await user.click(await screen.findByRole("button", { name: "Edit capabilities" }));
    await user.type(screen.getByLabelText("Name"), " (draft)");

    // Another operator removes the connection while the editor is open: the
    // draft has no subject any more, so the dialog closes and says so instead
    // of losing the draft silently and leaking the open editor into the next
    // selection.
    harness.setConnections([second]);
    await act(async () => { await harness.application.queryClient.refetchQueries({ queryKey: ["connections"] }); });
    await waitFor(() => { expect(screen.queryByRole("dialog", { name: "Connection settings" })).toBeNull(); });
    expect(screen.getByText(/removed elsewhere/)).toBeVisible();

    await user.click(screen.getByRole("button", { name: /Second connection/ }));
    expect(await screen.findByRole("complementary", { name: "Connection detail" })).toHaveTextContent("Second connection");
    expect(screen.queryByRole("dialog", { name: "Connection settings" })).toBeNull();
  });

  it("does not claim a capability reset when the edit only changed the name", async () => {
    const harness = renderConnections(({ method }) => method === "PATCH"
      ? new Response(JSON.stringify({ disposition: "updated", connection: connection({ version: 4, name: "Renamed engineering" }) }), { status: 200 })
      : undefined);
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: /GitHub engineering/ }));
    await user.click(screen.getByRole("button", { name: "Edit capabilities" }));
    await user.clear(screen.getByLabelText("Name"));
    await user.type(screen.getByLabelText("Name"), "Renamed engineering");
    harness.setConnections([connection({ version: 4, name: "Renamed engineering" })]);
    await user.click(screen.getByRole("button", { name: "Save" }));
    expect(await screen.findByText("Connection updated.")).toBeVisible();
    expect(screen.queryByText(/Bound resource health and sync state was reset/)).toBeNull();
    expect(portal.requests.filter((request) => request.method === "PATCH")[0]?.body).toEqual({
      version: 3,
      name: "Renamed engineering",
      capabilities: ["repositories", "work_items", "changes", "ci"],
    });
  });

  it("surfaces a replayed create receipt as already applied", async () => {
    const harness = renderConnections(({ method }) => method === "POST"
      ? new Response(JSON.stringify({ disposition: "replayed", connection: { id: connectionId } }), { status: 200 })
      : undefined);
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: "New connection" }));
    await user.type(screen.getByLabelText("Name"), "GitHub engineering");
    await user.type(screen.getByLabelText("Account or organization"), "acme");
    harness.setConnections([connection({ name: "GitHub engineering" })]);
    await user.click(screen.getByRole("button", { name: "Connect" }));
    expect(await screen.findByText(/already applied; the server returned the existing connection/)).toBeVisible();
    expect(portal.requests.filter((request) => request.method === "POST")).toHaveLength(1);
  });
});

describe("project resources", () => {
  it("offers only the connections that grant a kind's capability and submits the served bind payload", async () => {
    const boards = connection({ id: secondConnectionId, provider: "azure_devops", name: "Azure Boards", account_ref: "acme", capabilities: ["work_items"] });
    renderConnections(({ method }) => {
      if (method !== "POST") return undefined;
      return new Response(JSON.stringify({ resource: resourceFixture({ name: "checkout service", version: 1 }) }), { status: 201 });
    }, "/portal/connections", { connections: [connection(), boards], projects: [projectFixture()] });
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: "Bind resource" }));
    const dialog = within(await screen.findByRole("dialog", { name: "Bind resource" }));
    const connectionSelect = dialog.getByLabelText("Provider connection");
    expect(dialog.getByLabelText("Type")).toHaveValue("repository");
    expect(optionLabels(connectionSelect)).toEqual(["GitHub engineering"]);
    await user.selectOptions(dialog.getByLabelText("Type"), "work_tracking");
    expect(optionLabels(connectionSelect)).toEqual(["GitHub engineering", "Azure Boards"]);
    await user.selectOptions(dialog.getByLabelText("Type"), "ci");
    expect(optionLabels(connectionSelect)).toEqual(["GitHub engineering"]);
    await user.selectOptions(dialog.getByLabelText("Type"), "repository");
    await user.type(dialog.getByLabelText("Name"), "checkout service");
    await user.type(dialog.getByLabelText("Reference"), "acme/checkout");
    await user.click(dialog.getByRole("button", { name: "Bind" }));
    await waitFor(() => { expect(portal.requests.filter((request) => request.method === "POST")).toHaveLength(1); });
    expect(portal.requests.find((request) => request.method === "POST")?.path).toBe(`/portal/api/projects/${projectId}/resources`);
    expect(portal.requests.find((request) => request.method === "POST")?.body).toEqual({
      kind: "repository", connection_id: connectionId, name: "checkout service", external_ref: "acme/checkout",
    });
    expect(await screen.findByText("Resource attached — checkout service.")).toBeVisible();
    await waitFor(() => { expect(screen.queryByRole("dialog", { name: "Bind resource" })).toBeNull(); });
    // The bind's pending counts released with its settle: the surface accepts
    // the next write instead of staying locked.
    expect(screen.getByRole("button", { name: "Bind resource" })).toBeEnabled();
    expect(screen.getByRole("button", { name: "Synchronize project" })).toBeEnabled();
  });

  it("renders the resource form's own field error from the server's refusal", async () => {
    renderConnections(({ method }) => method === "POST"
      ? refusal(422, "validation_failed", "request contains invalid fields", { external_ref: ["must reference a registered runtime"] })
      : undefined, "/portal/connections", {
      connections: [connection()],
      projects: [projectFixture()],
    });
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: "Bind resource" }));
    const dialog = within(await screen.findByRole("dialog", { name: "Bind resource" }));
    await user.type(dialog.getByLabelText("Name"), "checkout service");
    await user.type(dialog.getByLabelText("Reference"), "acme/checkout");
    await user.click(dialog.getByRole("button", { name: "Bind" }));
    // The refused bind keeps the dialog open and shows the server's per-field
    // reason on the resource form itself, not only on the connection form: the
    // field error and the refusal notice are two separate alerts.
    const fieldError = await dialog.findByText("must reference a registered runtime");
    expect(fieldError).toBeVisible();
    expect(fieldError).toHaveClass("field-error");
    expect(fieldError).toHaveAttribute("role", "alert");
    expect(dialog.getByText(/request contains invalid fields \(validation_failed\)/)).toBeVisible();
    expect(portal.requests.find((request) => request.method === "POST")?.path).toBe(`/portal/api/projects/${projectId}/resources`);
  });

  it("leaves a provider_owned refusal to the server's own message, without assignment-surface guidance", async () => {
    // No connections-page command can answer `provider_owned`: its only producer
    // is the work-item update behind the assignment surface, which carries its
    // own guidance. The page's fold must render the server's message alone
    // instead of guidance for a code no call site here can receive.
    renderConnections(({ method }) => method === "POST"
      ? refusal(409, "provider_owned", "field is authoritative in the external provider")
      : undefined, "/portal/connections", {
      connections: [connection()],
      projects: [projectFixture()],
    });
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: "Bind resource" }));
    const dialog = within(await screen.findByRole("dialog", { name: "Bind resource" }));
    await user.type(dialog.getByLabelText("Name"), "checkout service");
    await user.type(dialog.getByLabelText("Reference"), "acme/checkout");
    await user.click(dialog.getByRole("button", { name: "Bind" }));
    expect(await dialog.findByText("field is authoritative in the external provider (provider_owned)")).toBeVisible();
    expect(dialog.queryByText(/This field is authored by the provider/)).toBeNull();
    expect(portal.requests.find((request) => request.method === "POST")?.path).toBe(`/portal/api/projects/${projectId}/resources`);
  });

  it("takes a local resource's reference from the workspace's registered runtimes", async () => {
    renderConnections(({ method }) => method === "POST"
      ? new Response(JSON.stringify({ resource: resourceFixture({ kind: "runtime", name: "review runner" }) }), { status: 201 })
      : undefined, "/portal/connections", {
      projects: [projectFixture()],
      registeredRuntimes: [
        { runtime_id: "77777777-7777-4777-8777-777777777771", runtime_name: "builder-a", agent_profile: "oss-default", workspace: "/work/alpha" },
        { runtime_id: "77777777-7777-4777-8777-777777777772", runtime_name: "builder-b", agent_profile: "review", workspace: "/work/beta" },
      ],
    });
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: "Bind resource" }));
    const dialog = within(await screen.findByRole("dialog", { name: "Bind resource" }));
    await user.selectOptions(dialog.getByLabelText("Type"), "agent");
    // A local kind needs no connection, and its reference is a registered profile.
    expect(dialog.getByLabelText("Provider connection")).toBeDisabled();
    expect(optionLabels(dialog.getByLabelText("Reference"))).toEqual([
      "builder-a · oss-default · /work/alpha",
      "builder-b · review · /work/beta",
    ]);
    expect(dialog.getByLabelText("Reference")).toHaveValue("oss-default");
    await user.selectOptions(dialog.getByLabelText("Type"), "runtime");
    // The runtime kind's reference is the registered runtime id.
    expect(optionLabels(dialog.getByLabelText("Reference"))).toEqual([
      "builder-a · oss-default · /work/alpha",
      "builder-b · review · /work/beta",
    ]);
    expect(dialog.getByLabelText("Reference")).toHaveValue("77777777-7777-4777-8777-777777777771");
    await user.selectOptions(dialog.getByLabelText("Reference"), "77777777-7777-4777-8777-777777777772");
    expect(dialog.getByLabelText("Reference")).toHaveValue("77777777-7777-4777-8777-777777777772");
    await user.type(dialog.getByLabelText("Name"), "review runner");
    await user.click(dialog.getByRole("button", { name: "Bind" }));
    await waitFor(() => { expect(portal.requests.filter((request) => request.method === "POST")).toHaveLength(1); });
    expect(portal.requests.find((request) => request.method === "POST")?.body).toEqual({
      kind: "runtime", connection_id: null, name: "review runner", external_ref: "77777777-7777-4777-8777-777777777772",
    });
  });

  it("offers no connection for a kind whose capability no connection grants", async () => {
    const boards = connection({ id: secondConnectionId, provider: "azure_devops", name: "Azure Boards", account_ref: "acme", capabilities: ["work_items"] });
    renderConnections(() => undefined, "/portal/connections", { connections: [boards], projects: [projectFixture()] });
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: "Bind resource" }));
    const dialog = within(await screen.findByRole("dialog", { name: "Bind resource" }));
    expect(optionLabels(dialog.getByLabelText("Type"))).toEqual(["Repository", "Work tracking", "CI", "Agent", "Runtime"]);
    expect(optionLabels(dialog.getByLabelText("Provider connection"))).toEqual(["No connection grants the required capability"]);
    expect(dialog.getByRole("button", { name: "Bind" })).toBeDisabled();
    await user.selectOptions(dialog.getByLabelText("Type"), "work_tracking");
    expect(optionLabels(dialog.getByLabelText("Provider connection"))).toEqual(["Azure Boards"]);
    expect(dialog.getByRole("button", { name: "Bind" })).toBeEnabled();
  });

  it("renders the served resource state, including a failed sync and an unknown kind", async () => {
    renderConnections(() => undefined, "/portal/connections", {
      projects: [projectFixture({
        resources: [
          resourceFixture(),
          resourceFixture({
            id: betaId, kind: "work_tracking", name: "engineering issues", external_ref: "acme", url: null,
            status: "degraded", sync_status: "failed", status_message: "Provider returned HTTP 500",
            last_synced_at: "2026-11-15T07:00:00.000000Z", version: 5,
          }),
          resourceFixture({
            id: "30000000-0000-4000-8000-00000000000c", kind: "future_kind", name: "future thing",
            connection_id: null, provider: null, external_ref: null, url: null,
            status: "unknown", sync_status: "unknown", status_message: null, last_synced_at: null, version: 1,
          }),
        ],
      })],
    });
    expect(await screen.findByRole("heading", { name: "Project resources" })).toBeVisible();
    const section = within(screen.getByRole("region", { name: "Project resources" }));
    // The heading renders while the project read is still in flight: wait for
    // the served project before reading its count and rows.
    await section.findByText(/Showing Connected engineering/);
    expect(section.getByText("3")).toBeVisible();

    const synced = resourceRow("checkout service");
    expect(synced.getByText("Repository")).toBeVisible();
    expect(synced.getByText("GitHub engineering · acme/checkout")).toBeVisible();
    expect(synced.getByRole("link", { name: "https://github.com/acme/checkout" })).toBeVisible();
    expect(synced.getByText("Healthy")).toBeVisible();
    expect(synced.getByText("Synced")).toBeVisible();
    expect(synced.getByText("Last synced 2026-11-16T08:31:00.000000Z")).toBeVisible();

    const failed = resourceRow("engineering issues");
    expect(failed.getByText("Work tracking")).toBeVisible();
    expect(failed.getByText("Degraded")).toBeVisible();
    expect(failed.getByText("Failed")).toBeVisible();
    expect(failed.getByText("Provider returned HTTP 500")).toBeVisible();
    expect(failed.getByText("Last synced 2026-11-15T07:00:00.000000Z")).toBeVisible();
    expect(failed.queryByText("Synced")).toBeNull();

    const unknown = resourceRow("future thing");
    expect(unknown.getByText("Unknown kind: future_kind")).toBeVisible();
    expect(unknown.getByText("Local resource · No external reference")).toBeVisible();
    expect(unknown.getByText("Never synced")).toBeVisible();
    expect(unknown.getAllByText("Unknown")).toHaveLength(2);
  });

  it("renders a whitespace-only served name, status and sync status verbatim", async () => {
    renderConnections(() => undefined, "/portal/connections", {
      projects: [projectFixture({ resources: [resourceFixture({ name: " ", status: " ", sync_status: " " })] })],
    });
    expect(await screen.findByRole("heading", { name: "Project resources" })).toBeVisible();
    const section = within(screen.getByRole("region", { name: "Project resources" }));
    await section.findByText(/Showing Connected engineering/);
    expect(screen.getAllByRole("heading", { level: 3 }).some((heading) => heading.textContent === " ")).toBe(true);
    expect(screen.queryByRole("heading", { level: 3, name: alphaId })).toBeNull();
    expect(screen.getAllByText(/^Unknown (resource|sync) status:/)).toHaveLength(2);
  });

  it("renders a refused sync with the server's code and next step and never as synchronized", async () => {
    const target = resourceFixture({
      id: betaId, kind: "work_tracking", name: "engineering issues", connection_id: null, external_ref: "acme",
      url: null, status: "unknown", sync_status: "unknown", last_synced_at: null, version: 5,
    });
    const harness = renderConnections(({ method, path }) => {
      if (method !== "POST" || !path.endsWith("/sync")) return undefined;
      harness.setProjects([projectFixture({ resources: [{ ...target, status: "degraded", sync_status: "failed", status_message: "Resource has no external connection" }] })]);
      return refusal(409, "not_connected", "resource has no external connection");
    }, "/portal/connections", { projects: [projectFixture({ resources: [target] })] });
    const user = userEvent.setup();
    const reads = portal.requests.filter((request) => request.path === "/portal/api/workspace").length;
    await user.click(await screen.findByRole("button", { name: "Sync engineering issues" }));
    expect(await screen.findByText(/resource has no external connection \(not_connected\)/)).toBeVisible();
    expect(screen.getByText(/Bind the resource to a connection first\./)).toBeVisible();
    const row = resourceRow("engineering issues");
    expect(await row.findByText("Failed")).toBeVisible();
    expect(row.getByText("Resource has no external connection")).toBeVisible();
    expect(row.getByText("Never synced")).toBeVisible();
    expect(row.queryByText("Synced")).toBeNull();
    await waitFor(() => { expect(portal.requests.filter((request) => request.path === "/portal/api/workspace").length).toBeGreaterThan(reads); });
  });

  it("renders the served capability refusal with the capability-specific next step", async () => {
    const target = resourceFixture();
    const harness = renderConnections(({ method, path }) => {
      if (method !== "POST" || !path.endsWith("/sync")) return undefined;
      harness.setProjects([projectFixture({ resources: [{ ...target, status: "degraded", sync_status: "failed", status_message: "Connection capability is not granted" }] })]);
      return refusal(403, "capability_not_granted", "connection capability is not granted");
    }, "/portal/connections", { projects: [projectFixture({ resources: [target] })] });
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: "Sync checkout service" }));
    expect(await screen.findByText(/connection capability is not granted \(capability_not_granted\)/)).toBeVisible();
    expect(screen.getByText(/Grant the connection the capability this resource kind needs, then try again\./)).toBeVisible();
    const row = resourceRow("checkout service");
    expect(await row.findByText("Failed")).toBeVisible();
    expect(row.getByText("Connection capability is not granted")).toBeVisible();
  });

  it("keeps a failed provider read explicit and never renders it as synchronized", async () => {
    const failed = resourceFixture({ status: "degraded", sync_status: "failed", status_message: "Provider returned HTTP 500", version: 5 });
    const harness = renderConnections(({ method, path }) => {
      if (method !== "POST" || !path.endsWith("/sync")) return undefined;
      harness.setProjects([projectFixture({ resources: [failed] })]);
      return refusal(502, "provider_failure", "external provider request failed", null, { resource: failed });
    }, "/portal/connections", { projects: [projectFixture({ resources: [resourceFixture()] })] });
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: "Sync checkout service" }));
    expect(await screen.findByText(/external provider request failed \(provider_failure\)/)).toBeVisible();
    expect(screen.getByText(/Check provider availability/)).toBeVisible();
    const row = resourceRow("checkout service");
    expect(await row.findByText("Degraded")).toBeVisible();
    expect(row.getByText("Failed")).toBeVisible();
    expect(row.getByText("Provider returned HTTP 500")).toBeVisible();
    // The served freshness stays whatever the server holds: the failure must
    // not carry the previously synchronized timestamp into a synchronized look.
    expect(row.getByText("Last synced 2026-11-16T08:31:00.000000Z")).toBeVisible();
    expect(row.queryByText("Synced")).toBeNull();
  });

  it("reports the project sync count from the served receipt", async () => {
    renderConnections(({ method, path }) => method === "POST" && path.endsWith("/sync")
      ? new Response(JSON.stringify({ synced_resource_ids: [alphaId, betaId] }))
      : undefined, "/portal/connections", { projects: [projectFixture({ resources: [resourceFixture()] })] });
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: "Synchronize project" }));
    expect(await screen.findByText("Project resources synchronized: 2 resources updated.")).toBeVisible();
    expect(portal.requests.find((request) => request.method === "POST")?.path).toBe(`/portal/api/projects/${projectId}/sync`);
  });

  it("renders an aggregate project sync failure with the server's code and re-reads the project", async () => {
    renderConnections(({ method, path }) => {
      if (method !== "POST" || !path.endsWith("/sync")) return undefined;
      return refusal(502, "provider_failure", "external provider request failed", null, {
        causes: [{ code: "provider_failure", message: "external provider request failed", count: 1 }],
        results: [{ resource_id: alphaId, status: "failed", error: { code: "provider_failure", message: "external provider request failed" } }],
      });
    }, "/portal/connections", { projects: [projectFixture({ resources: [resourceFixture()] })] });
    const user = userEvent.setup();
    const reads = portal.requests.filter((request) => request.path === "/portal/api/workspace").length;
    await user.click(await screen.findByRole("button", { name: "Synchronize project" }));
    expect(await screen.findByText(/external provider request failed \(provider_failure\)/)).toBeVisible();
    await waitFor(() => { expect(portal.requests.filter((request) => request.path === "/portal/api/workspace").length).toBeGreaterThan(reads); });
    expect(screen.queryByText(/Project resources synchronized/)).toBeNull();
  });

  it("submits the version the update dialog opened with and renders the stale refusal", async () => {
    const harness = renderConnections(({ method }) => method === "PATCH"
      ? refusal(409, "stale", "resource changed since it was loaded")
      : undefined, "/portal/connections", { projects: [projectFixture({ resources: [resourceFixture()] })] });
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: "Update checkout service" }));
    const dialog = within(await screen.findByRole("dialog", { name: "Update resource" }));
    expect(dialog.getByText("Repository")).toBeVisible();
    expect(dialog.getByText("Healthy")).toBeVisible();
    await user.clear(dialog.getByLabelText("Name"));
    await user.type(dialog.getByLabelText("Name"), "checkout service (renamed)");
    // A background refresh loads the row another operator already changed while
    // the dialog is open; the draft and its edited-against version stay put.
    harness.setProjects([projectFixture({ resources: [resourceFixture({ version: 7, name: "Changed elsewhere" })] })]);
    await act(async () => { await harness.application.queryClient.refetchQueries({ queryKey: ["workspace"] }); });
    expect(dialog.getByLabelText("Name")).toHaveValue("checkout service (renamed)");
    await user.click(dialog.getByRole("button", { name: "Save" }));
    await waitFor(() => { expect(portal.requests.filter((request) => request.method === "PATCH")).toHaveLength(1); });
    expect(portal.requests.find((request) => request.method === "PATCH")?.body).toEqual({
      version: 4, name: "checkout service (renamed)", connection_id: connectionId, external_ref: "acme/checkout",
    });
    expect(await dialog.findByRole("alert")).toHaveTextContent("resource changed since it was loaded (stale)");
    expect(dialog.getByText(/This resource changed elsewhere; close and reopen/)).toBeVisible();
    expect(dialog.getByRole("button", { name: "Save" })).toBeDisabled();
  });

  it("renders a not-found update refusal with the server's reason instead of assuming success", async () => {
    renderConnections(({ method }) => method === "PATCH"
      ? refusal(404, "not_found", "resource was not found")
      : undefined, "/portal/connections", { projects: [projectFixture({ resources: [resourceFixture()] })] });
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: "Update checkout service" }));
    const dialog = within(await screen.findByRole("dialog", { name: "Update resource" }));
    await user.click(dialog.getByRole("button", { name: "Save" }));
    expect(await dialog.findByRole("alert")).toHaveTextContent("resource was not found (not_found)");
    expect(dialog.getByText(/Reload the page to see the server's current state\./)).toBeVisible();
    expect(screen.queryByText(/Resource updated/)).toBeNull();
  });

  it("submits the version the unbind dialog opened with and renders the state conflict", async () => {
    const harness = renderConnections(({ method }) => method === "DELETE"
      ? refusal(409, "state_conflict", "state has already advanced")
      : undefined, "/portal/connections", { projects: [projectFixture({ resources: [resourceFixture()] })] });
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: "Unbind checkout service" }));
    const dialog = within(await screen.findByRole("dialog", { name: "Unbind checkout service?" }));
    harness.setProjects([projectFixture({ resources: [resourceFixture({ version: 9 })] })]);
    await act(async () => { await harness.application.queryClient.refetchQueries({ queryKey: ["workspace"] }); });
    await user.click(dialog.getByRole("button", { name: "Unbind resource" }));
    await waitFor(() => { expect(portal.requests.filter((request) => request.method === "DELETE")).toHaveLength(1); });
    expect(portal.requests.find((request) => request.method === "DELETE")?.path).toBe(`/portal/api/resources/${alphaId}?version=4`);
    expect(await dialog.findByRole("alert")).toHaveTextContent("state has already advanced (state_conflict)");
    expect(dialog.getByText(/Reload and resolve the changed state\./)).toBeVisible();
    expect(dialog.getByRole("button", { name: "Unbind resource" })).toBeDisabled();
  });

  it("locks every resource control while one write is pending", async () => {
    const { promise: pending, resolve: release } = Promise.withResolvers<Response>();
    renderConnections(({ method, path }) => method === "POST" && path.endsWith("/sync")
      ? pending
      : undefined, "/portal/connections", { projects: [projectFixture({ resources: [resourceFixture()] })] });
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: "Sync checkout service" }));
    expect(screen.getByRole("button", { name: "Bind resource" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Synchronize project" })).toBeDisabled();
    const row = resourceRow("checkout service");
    expect(row.getByRole("button", { name: "Sync checkout service" })).toBeDisabled();
    expect(row.getByRole("button", { name: "Update checkout service" })).toBeDisabled();
    expect(row.getByRole("button", { name: "Unbind checkout service" })).toBeDisabled();
    // Forced clicks must not start an editor or a second request while the
    // single-flight window is open.
    screen.getByRole("button", { name: "Bind resource" }).dispatchEvent(new MouseEvent("click", { bubbles: true }));
    row.getByRole("button", { name: "Unbind checkout service" }).dispatchEvent(new MouseEvent("click", { bubbles: true }));
    expect(screen.queryByRole("dialog", { name: "Bind resource" })).toBeNull();
    expect(screen.queryByRole("dialog", { name: /Unbind/ })).toBeNull();
    expect(portal.requests.filter((request) => request.method === "POST")).toHaveLength(1);

    await act(async () => {
      release(new Response(JSON.stringify({ resource: resourceFixture({ version: 5 }) }), { status: 200 }));
      await pending;
    });
    await waitFor(() => expect(screen.getByRole("button", { name: "Bind resource" })).toBeEnabled());
    expect(resourceRow("checkout service").getByRole("button", { name: "Sync checkout service" })).toBeEnabled();
    expect(portal.requests.filter((request) => request.method === "POST")).toHaveLength(1);
  });

  it("never lets a late response for the previous project touch the newly selected project's rows", async () => {
    const beta = resourceFixture({
      id: betaId, project_id: secondProjectId, kind: "work_tracking", name: "beta issues", external_ref: "acme",
      url: null, status: "unknown", sync_status: "unknown", last_synced_at: null, version: 2,
    });
    const { promise: pending, resolve: release } = Promise.withResolvers<Response>();
    renderConnections(({ method, path }) => method === "POST" && path.endsWith("/sync")
      ? pending
      : undefined, "/portal/connections", {
      projects: [
        projectFixture({ resources: [resourceFixture()] }),
        projectFixture({ id: secondProjectId, key: "SEC", name: "Second engineering", resources: [beta] }),
      ],
    });
    const user = userEvent.setup();
    expect(await screen.findByRole("heading", { level: 3, name: "checkout service" })).toBeVisible();
    await user.click(screen.getByRole("button", { name: "Synchronize project" }));
    expect(screen.getByRole("button", { name: "Bind resource" })).toBeDisabled();
    expect(resourceRow("checkout service").getByRole("button", { name: "Sync checkout service" })).toBeDisabled();

    // The operator switches projects while the project sync is in flight.
    await user.selectOptions(screen.getByLabelText("Project"), secondProjectId);
    expect(await screen.findByRole("heading", { level: 3, name: "beta issues" })).toBeVisible();
    expect(screen.queryByRole("heading", { level: 3, name: "checkout service" })).toBeNull();
    // The write still holds the page: the new project's rows stay locked.
    expect(await screen.findByRole("button", { name: "Bind resource" })).toBeDisabled();

    await act(async () => {
      release(new Response(JSON.stringify({ synced_resource_ids: [alphaId] }), { status: 200 }));
      await pending;
    });
    // The late response belongs to the previous project: no notice, no row
    // mutation, and the switched project keeps its served state.
    expect(screen.queryByText(/Project resources synchronized/)).toBeNull();
    expect(screen.queryByText(/Resource synchronized/)).toBeNull();
    const row = resourceRow("beta issues");
    expect(row.getByText("Never synced")).toBeVisible();
    expect(row.queryByText("Synced")).toBeNull();
    // The exclusion survived the section's remount: the pending write released.
    await waitFor(() => expect(screen.getByRole("button", { name: "Bind resource" })).toBeEnabled());
    expect(portal.requests.filter((request) => request.path === `/portal/api/projects/${projectId}/sync`)).toHaveLength(1);
  });

  it("does not reopen an editor whose response arrives after the dialog closed", async () => {
    const { promise: pending, resolve: release } = Promise.withResolvers<Response>();
    renderConnections(({ method, path }) => method === "POST" && path.endsWith("/resources")
      ? pending
      : undefined, "/portal/connections", { projects: [projectFixture()] });
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: "Bind resource" }));
    const dialog = within(await screen.findByRole("dialog", { name: "Bind resource" }));
    await user.type(dialog.getByLabelText("Name"), "checkout service");
    await user.type(dialog.getByLabelText("Reference"), "acme/checkout");
    await user.click(dialog.getByRole("button", { name: "Bind" }));
    // The operator closes the dialog while its write is still in flight.
    await user.click(dialog.getByRole("button", { name: "Close bind dialog" }));
    expect(screen.queryByRole("dialog", { name: "Bind resource" })).toBeNull();
    const reads = portal.requests.filter((request) => request.path === "/portal/api/workspace").length;

    await act(async () => {
      release(new Response(JSON.stringify({ resource: resourceFixture({ version: 1 }) }), { status: 201 }));
      await pending;
    });
    expect(screen.queryByRole("dialog", { name: "Bind resource" })).toBeNull();
    expect(screen.queryByText(/Resource attached/)).toBeNull();
    // The write is real, so the project read is reconciled even for a closed editor.
    await waitFor(() => { expect(portal.requests.filter((request) => request.path === "/portal/api/workspace").length).toBeGreaterThan(reads); });
  });

  it("disables every mutation control for an archived project with a visible reason", async () => {
    renderConnections(() => undefined, "/portal/connections", {
      projects: [projectFixture({ status: "archived", resources: [resourceFixture()] })],
    });
    expect(await screen.findByText(/This project is archived, so binding, updating, unbinding and synchronizing its resources are disabled\./)).toBeVisible();
    expect(screen.getByRole("button", { name: "Bind resource" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Synchronize project" })).toBeDisabled();
    const row = resourceRow("checkout service");
    expect(row.getByRole("button", { name: "Sync checkout service" })).toBeDisabled();
    expect(row.getByRole("button", { name: "Update checkout service" })).toBeDisabled();
    expect(row.getByRole("button", { name: "Unbind checkout service" })).toBeDisabled();
    screen.getByRole("button", { name: "Bind resource" }).dispatchEvent(new MouseEvent("click", { bubbles: true }));
    expect(screen.queryByRole("dialog", { name: "Bind resource" })).toBeNull();
  });

  it("returns keyboard focus to the invoking control when a resource dialog closes", async () => {
    renderConnections(() => undefined, "/portal/connections", { projects: [projectFixture({ resources: [resourceFixture()] })] });
    const user = userEvent.setup();
    const bind = await screen.findByRole("button", { name: "Bind resource" });
    await user.click(bind);
    expect(await screen.findByRole("dialog", { name: "Bind resource" })).toBeVisible();
    await user.keyboard("{Escape}");
    await waitFor(() => { expect(screen.queryByRole("dialog", { name: "Bind resource" })).toBeNull(); });
    expect(bind).toHaveFocus();

    const unbind = screen.getByRole("button", { name: "Unbind checkout service" });
    await user.click(unbind);
    expect(await screen.findByRole("dialog", { name: "Unbind checkout service?" })).toBeVisible();
    await user.keyboard("{Escape}");
    await waitFor(() => { expect(screen.queryByRole("dialog", { name: "Unbind checkout service?" })).toBeNull(); });
    expect(unbind).toHaveFocus();
  });

  it("renders the project-not-found state for an unknown scope instead of another project's rows", async () => {
    const missingId = "10000000-0000-4000-8000-0000000000ff";
    renderConnections(() => undefined, `/portal/connections?projectId=${missingId}`, {
      projects: [projectFixture({ resources: [resourceFixture()] })],
    });
    expect(await screen.findByRole("heading", { name: "Project not found" })).toBeVisible();
    expect(screen.queryByRole("heading", { level: 3, name: "checkout service" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Bind resource" })).toBeNull();
  });
});
