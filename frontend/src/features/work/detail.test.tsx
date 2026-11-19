import { screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { jsonResponse, renderRoute, requestPath } from "../../app/test-harness";
import { readWorkDetail } from "../../services/api.fixtures";

/**
 * The work detail's conversation entry point on `/portal/work/:workItemId`: the
 * execution's run links into the run scope `/portal/chat` already serves, the
 * item's project travels with the link (the served projection first, the URL's
 * scope hint when it serves none), and the label names the decision only while
 * the served waiting context carries a decision record — the same record the
 * conversation's own decision intent is filtered by. Every case but the
 * negative control fails on the pre-change detail, which renders no chat entry
 * point at all. The setup's matchMedia stub reports the desktop width, so the
 * detail renders as the drawer here, where control.test.tsx forces the narrow
 * page by stubbing the query the other way.
 */

const projectId = "10000000-0000-4000-8000-000000000001";
const urlProjectId = "10000000-0000-4000-8000-000000000002";
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

/** The waiting context with its decision carried by the member under test. */
const waiting = (decision: unknown, payload: unknown = {}) => ({
  run_id: runId,
  generation: 2,
  transition_id: transitionId,
  question: "Widen the migration or keep it narrow?",
  decision,
  payload,
  recorded_at: null,
});

const detailWith = (executionOverride?: Record<string, unknown>) => readWorkDetail({
  id: subjectId,
  projectId,
  title: "Subject work item",
  ...(executionOverride === undefined ? {} : { execution: executionOverride }),
});

/** The same detail with the served read naming no project; the contract keeps `project_id` optional. */
function detailWithoutProject(executionOverride?: Record<string, unknown>) {
  const detail = detailWith(executionOverride);
  delete (detail["work_item"] as Record<string, unknown>)["project_id"];
  return detail;
}

const workspaceBody = () => ({
  selected_project_id: projectId,
  projects: [{ id: projectId, key: "ONE", name: "Project One", description: null, status: "active", resources: [], work_items: [] }],
  health: { status: "unknown" },
  runtimes: [],
  registered_runtimes: [],
});

function renderWorkDetail(detail: Record<string, unknown>, search = "") {
  const fetcher = vi.fn<typeof fetch>().mockImplementation((input) => {
    const url = new URL(requestPath(input), "http://localhost");
    if (url.pathname === "/portal/api/session") return Promise.resolve(jsonResponse({ csrf_token: "session-token" }));
    if (url.pathname === `/portal/api/work-items/${subjectId}`) return Promise.resolve(jsonResponse(detail));
    if (url.pathname === "/portal/api/workspace") return Promise.resolve(jsonResponse(workspaceBody()));
    return Promise.resolve(jsonResponse({}, 404));
  });
  renderRoute(`/portal/work/${subjectId}${search}`, fetcher);
}

describe("work detail conversation entry point", () => {
  it("links the execution's run into the conversation scope the chat route serves", async () => {
    renderWorkDetail(detailWith(execution({ can_cancel: true })));
    const link = await screen.findByRole("link", { name: "Open in Chat" });
    expect(link).toHaveAttribute("href", conversationHref);
  });

  it("labels the link for review while the waiting context carries the decision record", async () => {
    renderWorkDetail(detailWith(execution({ state: "waiting_for_input", waiting: waiting(decisionPacket) })));
    const link = await screen.findByRole("link", { name: "Review decision in Chat" });
    expect(link).toHaveAttribute("href", conversationHref);
    expect(screen.queryByRole("link", { name: "Open in Chat" })).toBeNull();
  });

  it("reads the decision record from the payload when the extraction member is null", async () => {
    renderWorkDetail(detailWith(execution({ state: "waiting_for_input", waiting: waiting(null, { decision: decisionPacket }) })));
    const link = await screen.findByRole("link", { name: "Review decision in Chat" });
    expect(link).toHaveAttribute("href", conversationHref);
  });

  it("keeps the generic label for a waiting request without a decision record", async () => {
    renderWorkDetail(detailWith(execution({ state: "waiting_for_input", waiting: waiting(null) })));
    const link = await screen.findByRole("link", { name: "Open in Chat" });
    expect(link).toHaveAttribute("href", conversationHref);
    expect(screen.queryByRole("link", { name: "Review decision in Chat" })).toBeNull();
  });

  it("omits the project when neither the served read nor the URL names one", async () => {
    renderWorkDetail(detailWithoutProject(execution({ can_cancel: true })));
    const link = await screen.findByRole("link", { name: "Open in Chat" });
    expect(link).toHaveAttribute("href", `/portal/chat?scope=run&runId=${runId}`);
  });

  it("falls back to the URL's project scope when the served read names none", async () => {
    renderWorkDetail(detailWithoutProject(execution({ can_cancel: true })), `?projectId=${urlProjectId}`);
    const link = await screen.findByRole("link", { name: "Open in Chat" });
    expect(link).toHaveAttribute("href", `/portal/chat?scope=run&projectId=${urlProjectId}&runId=${runId}`);
  });

  it("renders no conversation link without a run to link", async () => {
    renderWorkDetail(detailWith({ task_id: taskId, run_id: null, generation: 1, state: "queued", can_cancel: false }));
    expect(await screen.findByRole("heading", { level: 1, name: "Subject work item" })).toBeVisible();
    expect(screen.queryByRole("link", { name: "Open in Chat" })).toBeNull();
    expect(screen.queryByRole("link", { name: "Review decision in Chat" })).toBeNull();
  });
});
