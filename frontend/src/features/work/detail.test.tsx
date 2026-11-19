import { QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory, RouterProvider, type RouterHistory } from "@tanstack/react-router";
import { render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { createApplication, type Application } from "../../app/application";
import { createAppRouter } from "../../app/router";
import { readWorkDetail } from "../../services/api.fixtures";

/**
 * The work detail's conversation entry point on `/portal/work/:workItemId`: the
 * execution's run links into the run scope `/portal/chat` already serves, the
 * item's project travels with the link, and the label names the decision only
 * while the served waiting context carries a decision packet — the member the
 * conversation's own decision intent is filtered by. Every case fails on the
 * pre-change detail, which renders no chat entry point at all.
 */

const projectId = "10000000-0000-4000-8000-000000000001";
const subjectId = "20000000-0000-4000-8000-000000000001";
const taskId = "55555555-5555-4555-8555-555555555555";
const runId = "50000000-0000-4000-8000-000000000001";
const transitionId = "60000000-0000-4000-8000-000000000001";

const conversationHref = `/portal/chat?scope=run&projectId=${projectId}&runId=${runId}`;

const execution = (overrides: Record<string, unknown>) => ({
  task_id: taskId,
  run_id: runId,
  generation: 2,
  state: "running",
  ...overrides,
});

const decisionPacket = {
  context: "The migration can be widened or kept narrow.",
  reason: "Both placements satisfy the acceptance.",
  options: [{ id: "widen", label: "Widen", consequence: "Both surfaces move." }],
  recommended_option_id: "widen",
};

const waiting = (decision: unknown) => ({
  run_id: runId,
  generation: 2,
  transition_id: transitionId,
  question: "Widen the migration or keep it narrow?",
  decision,
  payload: {},
  recorded_at: null,
});

const workspaceBody = () => ({
  selected_project_id: projectId,
  projects: [{ id: projectId, key: "ONE", name: "Project One", description: null, status: "active", resources: [], work_items: [] }],
  health: { status: "unknown" },
  runtimes: [],
  registered_runtimes: [],
});

const applications: Application[] = [];
afterEach(() => {
  for (const application of applications) application.queryClient.clear();
  applications.length = 0;
});

function renderWorkDetail(detail: Record<string, unknown>) {
  const path = `/portal/work/${subjectId}`;
  const fetcher = vi.fn<typeof fetch>().mockImplementation((input) => {
    const url = new URL(input instanceof Request ? input.url : String(input), "http://localhost");
    if (url.pathname === "/portal/api/session") return Promise.resolve(new Response(JSON.stringify({ csrf_token: "session-token" })));
    if (url.pathname === `/portal/api/work-items/${subjectId}`) return Promise.resolve(new Response(JSON.stringify(detail)));
    if (url.pathname === `/portal/api/workspace`) return Promise.resolve(new Response(JSON.stringify(workspaceBody())));
    return Promise.resolve(new Response("{}", { status: 404 }));
  });
  const application = createApplication({ fetcher, location: { href: `http://localhost${path}`, replace: vi.fn() } });
  applications.push(application);
  const history: RouterHistory = createMemoryHistory({ initialEntries: [path] });
  const router = createAppRouter(application, history);
  render(<QueryClientProvider client={application.queryClient}><RouterProvider router={router} /></QueryClientProvider>);
}

const detailWith = (executionOverride?: Record<string, unknown>) => readWorkDetail({
  id: subjectId,
  projectId,
  title: "Subject work item",
  ...(executionOverride === undefined ? {} : { execution: executionOverride }),
});

describe("work detail conversation entry point", () => {
  it("links the execution's run into the conversation scope the chat route serves", async () => {
    renderWorkDetail(detailWith(execution({ can_cancel: true })));
    const link = await screen.findByRole("link", { name: "Open in Chat" });
    expect(link).toHaveAttribute("href", conversationHref);
  });

  it("labels the link for review while the served waiting context carries a decision packet", async () => {
    renderWorkDetail(detailWith(execution({ state: "waiting_for_input", waiting: waiting(decisionPacket) })));
    const link = await screen.findByRole("link", { name: "Review decision in Chat" });
    expect(link).toHaveAttribute("href", conversationHref);
    expect(screen.queryByRole("link", { name: "Open in Chat" })).toBeNull();
  });

  it("keeps the generic label for a waiting request without a decision packet", async () => {
    renderWorkDetail(detailWith(execution({ state: "waiting_for_input", waiting: waiting(null) })));
    const link = await screen.findByRole("link", { name: "Open in Chat" });
    expect(link).toHaveAttribute("href", conversationHref);
    expect(screen.queryByRole("link", { name: "Review decision in Chat" })).toBeNull();
  });

  it("renders no conversation link without a run to link", async () => {
    renderWorkDetail(detailWith({ task_id: taskId, run_id: null, generation: 1, state: "queued", can_cancel: false }));
    expect(await screen.findByRole("heading", { level: 1, name: "Subject work item" })).toBeVisible();
    expect(screen.queryByRole("link", { name: "Open in Chat" })).toBeNull();
    expect(screen.queryByRole("link", { name: "Review decision in Chat" })).toBeNull();
  });
});
