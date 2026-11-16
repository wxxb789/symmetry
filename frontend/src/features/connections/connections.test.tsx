import { QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory, RouterProvider, type RouterHistory } from "@tanstack/react-router";
import { act, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { Connection } from "../../services/api";
import { createApplication, type Application } from "../../app/application";
import { createAppRouter } from "../../app/router";

const connectionId = "20000000-0000-4000-8000-000000000001";
const projectId = "10000000-0000-4000-8000-000000000001";
const resourceId = "30000000-0000-4000-8000-000000000001";

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

interface Harness {
  application: Application;
  requests: Array<{ method: string; path: string; body: unknown }>;
  history: RouterHistory;
  setConnections: (value: Connection[]) => void;
  failReads: (response: () => Response) => void;
  gateReads: (gate: Promise<void> | null) => void;
}

const applications: Application[] = [];
afterEach(() => {
  for (const application of applications) application.queryClient.clear();
  applications.length = 0;
});

function renderConnections(
  handler: (request: { method: string; path: string; body: unknown; url: URL }) => Response | Promise<Response>,
  path = "/portal/connections",
  options: { deferReads?: Promise<void> } = {},
) {
  let current: Connection[] = [connection()];
  let readFailure: (() => Response) | null = null;
  let readGate: Promise<void> | null = options.deferReads ?? null;
  const requests: Harness["requests"] = [];
  const fetcher = vi.fn<typeof fetch>().mockImplementation(async (input, init) => {
    const url = new URL(input instanceof Request ? input.url : String(input), "http://localhost");
    const method = init?.method ?? "GET";
    const body = typeof init?.body === "string" ? JSON.parse(init.body) as unknown : undefined;
    requests.push({ method, path: url.pathname + url.search, body });
    if (url.pathname === "/portal/api/session") {
      return new Response(JSON.stringify({ csrf_token: "session-token" }));
    }
    if (method === "GET" && url.pathname === "/portal/api/connections") {
      if (readGate) await readGate;
      if (readFailure) return readFailure();
      return new Response(JSON.stringify({ providers, connections: current }));
    }
    if (method === "GET" && url.pathname === "/portal/api/workspace") {
      return new Response(JSON.stringify({ selected_project_id: projectId, projects: [] }));
    }
    return handler({ method, path: url.pathname + url.search, body, url });
  });
  const application = createApplication({ fetcher, location: { href: `http://localhost${path}`, replace: vi.fn() } });
  applications.push(application);
  const history = createMemoryHistory({ initialEntries: [path] });
  const router = createAppRouter(application, history);
  render(<QueryClientProvider client={application.queryClient}><RouterProvider router={router} /></QueryClientProvider>);
  return {
    application,
    requests,
    history,
    setConnections: (value: Connection[]) => { current = value; },
    failReads: (response: () => Response) => { readFailure = response; },
    gateReads: (gate: Promise<void> | null) => { readGate = gate; },
  };
}

const refusal = (status: number, code: string, message: string, fields: Record<string, string[]> | null = null, extra: Record<string, unknown> = {}) =>
  new Response(JSON.stringify({ error: { code, message, ...(fields ? { fields } : {}) }, ...extra }), { status });

describe("connection management", () => {
  it("lists providers, capabilities and bound resource state without inventing values", async () => {
    renderConnections(() => new Response(null, { status: 404 }));
    expect(await screen.findByRole("heading", { level: 1, name: "Connections" })).toBeVisible();
    const row = await screen.findByRole("button", { name: /GitHub engineering/ });
    expect(row).toHaveTextContent("acme · GitHub CLI · HTTPS");
    expect(row).toHaveTextContent("Repositories");
    await userEvent.setup().click(row);
    expect(await screen.findByRole("complementary", { name: "Connection detail" })).toHaveTextContent("symmetry");
    expect(screen.getByText(/1 bound · 0 needing attention/)).toBeVisible();
  });

  it("renders an unknown status and capability instead of hiding them", async () => {
    const harness = renderConnections(() => new Response(null, { status: 404 }));
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
      : refusal(404, "not_found", "unexpected request"));
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
    await waitFor(() => expect(screen.queryByRole("button", { name: "Saving…" })).toBeNull());
    expect(harness.requests.filter((request) => request.method === "POST")).toHaveLength(1);
    expect(await screen.findByText("Connection created.")).toBeVisible();
    expect(harness.requests.filter((request) => request.method === "POST")[0]?.body).toEqual({
      provider: "github",
      name: "Azure delivery",
      account_ref: "acme",
      capabilities: ["repositories", "work_items", "changes", "ci"],
    });
  });

  it("renders the server's refusal code, message and field errors", async () => {
    renderConnections(({ method }) => method === "POST"
      ? refusal(422, "validation_failed", "request contains invalid fields", { name: ["has already been taken"] })
      : refusal(404, "not_found", "unexpected request"));
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
      : refusal(404, "not_found", "unexpected request"));
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: /GitHub engineering/ }));
    await user.click(await screen.findByRole("button", { name: "Edit capabilities" }));
    await user.click(screen.getByRole("checkbox", { name: "CI" }));
    harness.setConnections([connection({ version: 4, capabilities: ["repositories", "work_items", "changes", "ci"] })]);
    await user.click(screen.getByRole("button", { name: "Save" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("resource changed since it was loaded (stale)");
    expect(screen.getByText(/changed elsewhere; close and reopen/)).toBeVisible();
    expect(screen.getByRole("checkbox", { name: "CI" })).toBeDisabled();
    expect(harness.requests.filter((request) => request.method === "PATCH")[0]?.body).toEqual({
      version: 3,
      name: "GitHub engineering",
      capabilities: ["repositories", "work_items", "changes"],
    });
  });

  it("keeps a failed provider check actionable, redacted and truthful", async () => {
    const harness = renderConnections(({ method, path }) => {
      if (method !== "POST" || !path.endsWith("/check")) return refusal(404, "not_found", "unexpected request");
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
    expect(screen.queryByText(/gho_|Bearer |token/i)).toBeNull();
  });

  it("confirms a removal against the version the dialog opened with, even after a background refresh", async () => {
    const harness = renderConnections(({ method }) => method === "DELETE"
      ? refusal(409, "stale", "resource changed since it was loaded")
      : refusal(404, "not_found", "unexpected request"));
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
    expect(harness.requests.filter((request) => request.method === "DELETE")[0]?.path).toBe(`/portal/api/connections/${connectionId}?version=3`);
  });

  it("re-renders reset bound-resource health after a capability update", async () => {
    const harness = renderConnections(({ method }) => method === "PATCH"
      ? new Response(JSON.stringify({ disposition: "updated", connection: connection({ version: 4, capabilities: ["repositories", "work_items"] }) }))
      : refusal(404, "not_found", "unexpected request"));
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
      method === "POST" ? pending : refusal(404, "not_found", "unexpected request"));
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
      if (method !== "PATCH") return refusal(404, "not_found", "unexpected request");
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
    await waitFor(() => expect(harness.requests.some((request) => request.method === "PATCH")).toBe(true));
    expect(harness.requests.filter((request) => request.method === "PATCH")[0]?.body).toMatchObject({ version: 3 });
    expect(await screen.findByRole("alert")).toHaveTextContent("(stale)");
    expect(screen.getByText(/changed elsewhere; close and reopen/)).toBeVisible();
  });

  it("keeps provider check, update and removal mutually exclusive while one is pending", async () => {
    const { promise: pending, resolve: release } = Promise.withResolvers<Response>();
    const harness = renderConnections(({ method, path }) =>
      method === "POST" && path.endsWith("/check")
        ? pending
        : refusal(404, "not_found", "unexpected request"));
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
    expect(harness.requests.filter((request) => request.method === "POST")).toHaveLength(1);

    await act(async () => {
      release(new Response(JSON.stringify({ disposition: "checked", connection: connection({ version: 4 }) }), { status: 200 }));
      await pending;
    });
    expect(await screen.findByRole("button", { name: "Test connection" })).toBeEnabled();
    expect(harness.requests.filter((request) => request.method === "POST")).toHaveLength(1);
  });

  it("keeps check and removal blocked while an editor save is pending", async () => {
    const { promise: pending, resolve: release } = Promise.withResolvers<Response>();
    const harness = renderConnections(({ method }) =>
      method === "PATCH" ? pending : refusal(404, "not_found", "unexpected request"));
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
    expect(harness.requests.filter((request) => request.method === "POST")).toHaveLength(0);
    expect(harness.requests.filter((request) => request.method === "DELETE")).toHaveLength(0);

    await act(async () => {
      release(new Response(JSON.stringify({ disposition: "updated", connection: connection({ version: 4 }) }), { status: 200 }));
      await pending;
    });
    expect(await screen.findByRole("button", { name: "Test connection" })).toBeEnabled();
    expect(harness.requests.filter((request) => request.method === "PATCH")).toHaveLength(1);
  });

  it("keeps check and editing blocked while a removal is pending", async () => {
    const { promise: pending, resolve: release } = Promise.withResolvers<Response>();
    const harness = renderConnections(({ method }) =>
      method === "DELETE" ? pending : refusal(404, "not_found", "unexpected request"));
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
    expect(harness.requests.filter((request) => request.method === "POST")).toHaveLength(0);
    expect(harness.requests.filter((request) => request.method === "PATCH")).toHaveLength(0);

    await act(async () => {
      release(new Response(JSON.stringify({ disposition: "deleted", deleted_connection_id: connectionId }), { status: 200 }));
      await pending;
    });
    await waitFor(() => expect(harness.history.location.search).not.toContain(connectionId));
    expect(harness.requests.filter((request) => request.method === "DELETE")).toHaveLength(1);
  });

  it("drops a provider check receipt that arrives after the operator selected another connection", async () => {
    const { promise: pending, resolve: release } = Promise.withResolvers<Response>();
    const second = connection({ id: "20000000-0000-4000-8000-000000000002", name: "Second connection" });
    const harness = renderConnections(({ method, path }) =>
      method === "POST" && path.endsWith("/check") ? pending : refusal(404, "not_found", "unexpected request"));
    const user = userEvent.setup();
    harness.setConnections([connection(), second]);
    await user.click(await screen.findByRole("button", { name: "Refresh" }));
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
    const second = connection({ id: "20000000-0000-4000-8000-000000000002", name: "Second connection" });
    const harness = renderConnections(({ method, path }) =>
      method === "POST" && path.endsWith("/check") ? pending : refusal(404, "not_found", "unexpected request"));
    const user = userEvent.setup();
    harness.setConnections([connection(), second]);
    await user.click(await screen.findByRole("button", { name: "Refresh" }));
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
    expect(harness.requests.filter((request) => request.method === "POST")).toHaveLength(1);

    await act(async () => {
      release(new Response(JSON.stringify({ disposition: "checked", connection: connection({ version: 4 }) }), { status: 200 }));
      await pending;
    });
    expect(await screen.findByRole("button", { name: "Test connection" })).toBeEnabled();
    expect(harness.requests.filter((request) => request.method === "POST")).toHaveLength(1);
  });

  it("keeps a pending removal blocked after the detail remounts", async () => {
    const { promise: pending, resolve: release } = Promise.withResolvers<Response>();
    const second = connection({ id: "20000000-0000-4000-8000-000000000002", name: "Second connection" });
    const harness = renderConnections(({ method }) =>
      method === "DELETE" ? pending : refusal(404, "not_found", "unexpected request"));
    const user = userEvent.setup();
    harness.setConnections([connection(), second]);
    await user.click(await screen.findByRole("button", { name: "Refresh" }));
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
    expect(harness.requests.filter((request) => request.method === "DELETE")).toHaveLength(1);

    harness.setConnections([second]);
    await act(async () => {
      release(new Response(JSON.stringify({ disposition: "deleted", deleted_connection_id: connectionId }), { status: 200 }));
      await pending;
    });
    await waitFor(() => expect(screen.queryByRole("button", { name: /GitHub engineering/ })).toBeNull());
    expect(harness.history.location.search).not.toContain(connectionId);
  });

  it("surfaces a check refusal that settles after the operator returned to the same connection", async () => {
    const { promise: pending, resolve: release } = Promise.withResolvers<Response>();
    const second = connection({ id: "20000000-0000-4000-8000-000000000002", name: "Second connection" });
    const harness = renderConnections(({ method, path }) =>
      method === "POST" && path.endsWith("/check") ? pending : refusal(404, "not_found", "unexpected request"));
    const user = userEvent.setup();
    harness.setConnections([connection(), second]);
    await user.click(await screen.findByRole("button", { name: "Refresh" }));
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
    const second = connection({ id: "20000000-0000-4000-8000-000000000002", name: "Second connection" });
    const harness = renderConnections(({ method }) =>
      method === "DELETE" ? pending : refusal(404, "not_found", "unexpected request"));
    const user = userEvent.setup();
    harness.setConnections([connection(), second]);
    await user.click(await screen.findByRole("button", { name: "Refresh" }));
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
    await waitFor(() => expect(screen.queryByRole("button", { name: /GitHub engineering/ })).toBeNull());
    expect(harness.history.location.search).toContain(second.id);
  });

  it("does not navigate back when the operator selects another connection during the save's re-read", async () => {
    const second = connection({ id: "20000000-0000-4000-8000-000000000002", name: "Second connection" });
    const harness = renderConnections(({ method }) => method === "PATCH"
      ? new Response(JSON.stringify({ disposition: "updated", connection: connection({ version: 4 }) }), { status: 200 })
      : refusal(404, "not_found", "unexpected request"));
    const user = userEvent.setup();
    harness.setConnections([connection(), second]);
    await user.click(await screen.findByRole("button", { name: "Refresh" }));
    await user.click(await screen.findByRole("button", { name: /GitHub engineering/ }));
    await user.click(screen.getByRole("button", { name: "Edit capabilities" }));
    await user.click(screen.getByRole("checkbox", { name: "CI" }));

    // The authoritative re-read behind the save is still in flight when the
    // operator selects the other connection.
    const { promise: gate, resolve: release } = Promise.withResolvers<void>();
    harness.gateReads(gate);
    await user.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(screen.queryByRole("dialog", { name: "Connection settings" })).toBeNull());
    await waitFor(() => expect(harness.requests.filter((request) => request.method === "GET" && request.path === "/portal/api/connections")).toHaveLength(3));
    await user.click(screen.getByRole("button", { name: /Second connection/ }));
    expect(await screen.findByRole("complementary", { name: "Connection detail" })).toHaveTextContent("Second connection");

    release();
    harness.gateReads(null);
    await waitFor(() => expect(screen.queryByText("Refreshing connections")).toBeNull());
    expect(harness.history.location.search).toContain(second.id);
    expect(harness.history.location.search).not.toContain(connectionId);
  });

  it("does not select the saved connection when another editor opened during the save's re-read", async () => {
    const harness = renderConnections(({ method }) =>
      method === "POST"
        ? new Response(JSON.stringify({ disposition: "created", connection: { id: connectionId } }), { status: 201 })
        : refusal(404, "not_found", "unexpected request"));
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
    await waitFor(() => expect(screen.queryByRole("dialog", { name: "New connection" })).toBeNull());
    await waitFor(() => expect(screen.getByText("Refreshing connections")).toBeVisible());
    await user.click(screen.getByRole("button", { name: "New connection" }));
    await user.type(screen.getByLabelText("Name"), "Second connection");

    // The re-read completes while the second editor is open: the selection must
    // stay where the operator is, not move to the first save's connection.
    harness.setConnections([connection({ id: connectionId, name: "Azure delivery" })]);
    release();
    harness.gateReads(null);
    await waitFor(() => expect(screen.queryByText("Refreshing connections")).toBeNull());
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
      method === "PATCH" ? pending : refusal(404, "not_found", "unexpected request"));
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
    renderConnections(() => refusal(404, "not_found", "unexpected request"));
    const user = userEvent.setup();

    const create = await screen.findByRole("button", { name: "New connection" });
    await user.click(create);
    expect(await screen.findByRole("dialog", { name: "New connection" })).toBeVisible();
    await user.keyboard("{Escape}");
    await waitFor(() => expect(screen.queryByRole("dialog", { name: "New connection" })).toBeNull());
    expect(create).toHaveFocus();

    await user.click(await screen.findByRole("button", { name: /GitHub engineering/ }));
    const remove = await screen.findByRole("button", { name: "Remove" });
    await user.click(remove);
    expect(await screen.findByRole("dialog", { name: /Remove GitHub engineering/ })).toBeVisible();
    await user.keyboard("{Escape}");
    await waitFor(() => expect(screen.queryByRole("dialog", { name: /Remove GitHub engineering/ })).toBeNull());
    expect(remove).toHaveFocus();
  });

  it("keeps focus in the detail when the editor closes while its save is pending", async () => {
    const { promise: pending, resolve: release } = Promise.withResolvers<Response>();
    renderConnections(({ method }) => method === "PATCH" ? pending : refusal(404, "not_found", "unexpected request"));
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: /GitHub engineering/ }));
    await user.click(await screen.findByRole("button", { name: "Edit capabilities" }));
    await user.click(screen.getByRole("checkbox", { name: "CI" }));
    await user.click(screen.getByRole("button", { name: "Save" }));

    // The invoking control is disabled until the pending write settles; closing
    // must hand keyboard focus to a control the write cannot disable instead of
    // dropping it to the body.
    await user.click(screen.getByRole("button", { name: "Close connection dialog" }));
    await waitFor(() => expect(screen.queryByRole("dialog", { name: "Connection settings" })).toBeNull());
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
    renderConnections(({ method }) => method === "DELETE" ? pending : refusal(404, "not_found", "unexpected request"));
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: /GitHub engineering/ }));
    await user.click(screen.getByRole("button", { name: "Remove" }));
    await user.click(screen.getByRole("button", { name: "Remove connection" }));

    // The Remove button is disabled until the pending removal settles; closing
    // the confirmation must not drop keyboard focus to the body.
    await user.click(screen.getByRole("button", { name: "Close removal dialog" }));
    await waitFor(() => expect(screen.queryByRole("dialog", { name: /Remove GitHub engineering/ })).toBeNull());
    expect(document.activeElement).not.toBe(document.body);
    expect(screen.getByRole("button", { name: "Close connection detail" })).toHaveFocus();

    await act(async () => {
      release(new Response(JSON.stringify({ disposition: "deleted", deleted_connection_id: connectionId }), { status: 200 }));
      await pending;
    });
  });

  it("keeps keyboard focus on the connection row across a background refresh", async () => {
    const harness = renderConnections(() => refusal(404, "not_found", "unexpected request"));
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
        : refusal(404, "not_found", "unexpected request"));
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

  it("keeps the server's repositories-for-changes coupling observable in the editor", async () => {
    renderConnections(() => refusal(404, "not_found", "unexpected request"));
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
    const harness = renderConnections(() => refusal(404, "not_found", "unexpected request"), "/portal/connections", { deferReads: gate });
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
    await waitFor(() => expect(harness.requests.filter((request) => request.method === "POST")).toHaveLength(1));
    expect(harness.requests.find((request) => request.method === "POST")?.body).toEqual({
      provider: "github",
      name: "Catalog complete",
      account_ref: "acme",
      capabilities: providers[0]!.capabilities,
    });
  });

  it("closes the editor with a notice when the edited connection is removed elsewhere", async () => {
    const second = connection({ id: "20000000-0000-4000-8000-000000000002", name: "Second connection" });
    const harness = renderConnections(() => refusal(404, "not_found", "unexpected request"));
    const user = userEvent.setup();
    harness.setConnections([connection(), second]);
    await user.click(await screen.findByRole("button", { name: "Refresh" }));
    await user.click(await screen.findByRole("button", { name: /GitHub engineering/ }));
    await user.click(await screen.findByRole("button", { name: "Edit capabilities" }));
    await user.type(screen.getByLabelText("Name"), " (draft)");

    // Another operator removes the connection while the editor is open: the
    // draft has no subject any more, so the dialog closes and says so instead
    // of losing the draft silently and leaking the open editor into the next
    // selection.
    harness.setConnections([second]);
    await act(async () => { await harness.application.queryClient.refetchQueries({ queryKey: ["connections"] }); });
    await waitFor(() => expect(screen.queryByRole("dialog", { name: "Connection settings" })).toBeNull());
    expect(screen.getByText(/removed elsewhere/)).toBeVisible();

    await user.click(screen.getByRole("button", { name: /Second connection/ }));
    expect(await screen.findByRole("complementary", { name: "Connection detail" })).toHaveTextContent("Second connection");
    expect(screen.queryByRole("dialog", { name: "Connection settings" })).toBeNull();
  });

  it("does not claim a capability reset when the edit only changed the name", async () => {
    const harness = renderConnections(({ method }) => method === "PATCH"
      ? new Response(JSON.stringify({ disposition: "updated", connection: connection({ version: 4, name: "Renamed engineering" }) }), { status: 200 })
      : refusal(404, "not_found", "unexpected request"));
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: /GitHub engineering/ }));
    await user.click(screen.getByRole("button", { name: "Edit capabilities" }));
    await user.clear(screen.getByLabelText("Name"));
    await user.type(screen.getByLabelText("Name"), "Renamed engineering");
    harness.setConnections([connection({ version: 4, name: "Renamed engineering" })]);
    await user.click(screen.getByRole("button", { name: "Save" }));
    expect(await screen.findByText("Connection updated.")).toBeVisible();
    expect(screen.queryByText(/Bound resource health and sync state was reset/)).toBeNull();
    expect(harness.requests.filter((request) => request.method === "PATCH")[0]?.body).toEqual({
      version: 3,
      name: "Renamed engineering",
      capabilities: ["repositories", "work_items", "changes", "ci"],
    });
  });

  it("surfaces a replayed create receipt as already applied", async () => {
    const harness = renderConnections(({ method }) => method === "POST"
      ? new Response(JSON.stringify({ disposition: "replayed", connection: { id: connectionId } }), { status: 200 })
      : refusal(404, "not_found", "unexpected request"));
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: "New connection" }));
    await user.type(screen.getByLabelText("Name"), "GitHub engineering");
    await user.type(screen.getByLabelText("Account or organization"), "acme");
    harness.setConnections([connection({ name: "GitHub engineering" })]);
    await user.click(screen.getByRole("button", { name: "Connect" }));
    expect(await screen.findByText(/already applied; the server returned the existing connection/)).toBeVisible();
    expect(harness.requests.filter((request) => request.method === "POST")).toHaveLength(1);
  });
});
