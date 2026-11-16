import { QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory, RouterProvider } from "@tanstack/react-router";
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
  setConnections: (value: Connection[]) => void;
}

const applications: Application[] = [];
afterEach(() => {
  for (const application of applications) application.queryClient.clear();
  applications.length = 0;
});

function renderConnections(handler: (request: { method: string; path: string; body: unknown; url: URL }) => Response | Promise<Response>, path = "/portal/connections") {
  let current: Connection[] = [connection()];
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
    setConnections: (value: Connection[]) => { current = value; },
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

  it("reports a refused removal and reconciles to the server's current state", async () => {
    const harness = renderConnections(({ method }) => method === "DELETE"
      ? refusal(409, "stale", "resource changed since it was loaded")
      : refusal(404, "not_found", "unexpected request"));
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: /GitHub engineering/ }));
    await user.click(await screen.findByRole("button", { name: "Remove" }));
    harness.setConnections([connection({ version: 4 })]);
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
});
