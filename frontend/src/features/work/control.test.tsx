import { QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory, RouterProvider, type RouterHistory } from "@tanstack/react-router";
import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createApplication, type Application } from "../../app/application";
import { createAppRouter } from "../../app/router";
import { readWorkDetail } from "../../services/api.fixtures";

/**
 * The execution-control surface on `/portal/work/:workItemId`: the four control
 * mutations consume the server's typed refusal instead of a status-derived
 * guess, carry the session's live CSRF token, keep the receipt list's older
 * pages across a refresh, render a queued run without claiming a runtime
 * acknowledgement, and never render a lost response as failed before the
 * server's own records say so.
 */

const projectId = "10000000-0000-4000-8000-000000000001";
const subjectId = "20000000-0000-4000-8000-000000000001";
const taskId = "55555555-5555-4555-8555-555555555555";
const cancelCommand = { command_id: "77777777-7777-4777-8777-777777777777", task_id: taskId, kind: "cancel", state: "pending", acknowledgement_outcome: null };

function workspaceBody() {
  return {
    selected_project_id: projectId,
    projects: [
      {
        id: projectId,
        key: "ONE",
        name: "Project One",
        description: null,
        status: "active",
        default_agent_profile: "codex-default",
        default_workspace: "primary",
        resources: [],
        work_items: [],
      },
    ],
    health: { status: "unknown" },
    runtimes: [],
    registered_runtimes: [],
  };
}

function detailBody(options: {
  execution?: Record<string, unknown>;
  item?: Record<string, unknown>;
  timeline?: unknown[];
  nextBefore?: string | null;
} = {}) {
  const body = readWorkDetail({
    id: subjectId,
    projectId,
    title: "Subject work item",
    ...(options.execution === undefined ? {} : { execution: options.execution }),
    ...(options.timeline === undefined ? {} : { timeline: options.timeline }),
    ...(options.nextBefore === undefined ? {} : { nextBefore: options.nextBefore }),
  });
  return { ...body, work_item: { ...(body["work_item"] as Record<string, unknown>), ...options.item } };
}

const runningDetail = () => detailBody({ execution: { task_id: taskId, run_id: null, generation: 1, state: "running", can_cancel: true } });
const idleDetail = () => detailBody({ item: { can_start: true } });
const queuedDetail = () => detailBody({ execution: { task_id: taskId, run_id: null, generation: 1, state: "queued", can_cancel: false } });

interface RecordedRequest {
  method: string;
  path: string;
  headers: Record<string, string>;
  body: unknown;
}

/** The status the harness answers each date-bearing call with. */
const accepted = (body: Record<string, unknown>) => new Response(JSON.stringify(body), { status: 202 });

interface HarnessOptions {
  /** The detail read, by 1-based read index. */
  detail?: (readIndex: number) => Response | Promise<Response>;
  /** A write POST: return the answer, a refusal, or reject with a transport failure. */
  command?: (path: string, body: unknown) => Response | Promise<Response>;
  /** The session read; `readIndex` starts at 1 so a rotation can be sequenced. */
  session?: (readIndex: number) => Response;
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
  let detailReads = 0;
  let sessionReads = 0;
  const requests: RecordedRequest[] = [];
  const fetcher = vi.fn<typeof fetch>().mockImplementation(async (input, init) => {
    const url = new URL(input instanceof Request ? input.url : String(input), "http://localhost");
    const method = init?.method ?? "GET";
    const headers = (init?.headers ?? {}) as Record<string, string>;
    const body: unknown = typeof init?.body === "string" ? JSON.parse(init.body) : undefined;
    requests.push({ method, path: url.pathname + url.search, headers, body });
    if (url.pathname === "/portal/api/session") {
      sessionReads += 1;
      return options.session === undefined
        ? new Response(JSON.stringify({ csrf_token: "session-token" }))
        : options.session(sessionReads);
    }
    if (url.pathname.startsWith("/portal/api/work-items/")) {
      if (method === "POST") {
        const respond = options.command ?? (() => new Response("{}", { status: 500 }));
        return respond(url.pathname, body);
      }
      detailReads += 1;
      return (options.detail ?? (() => new Response(JSON.stringify(runningDetail()))))(detailReads);
    }
    if (url.pathname === "/portal/api/workspace") {
      return new Response(JSON.stringify(workspaceBody()));
    }
    return new Response("{}", { status: 404 });
  });
  const application = createApplication({ fetcher, location: { href: `http://localhost${path}`, replace: vi.fn() } });
  applications.push(application);
  const history: RouterHistory = createMemoryHistory({ initialEntries: [path] });
  const router = createAppRouter(application, history);
  render(<QueryClientProvider client={application.queryClient}><RouterProvider router={router} /></QueryClientProvider>);
  const posts = () => requests.filter((request) => request.method === "POST");
  return { application, history, requests, posts };
}

async function openDetail() {
  await screen.findByRole("heading", { level: 1, name: "Subject work item" });
}

/** The receipt cards whose rendered text matches; the list renders several outcome rows. */
function receiptCards(text: string) {
  return screen.queryAllByText(text).map((node) => node.closest(".receipt")).filter((node): node is HTMLElement => node !== null);
}

describe("control mutation boundary", () => {
  it("renders the server's typed refusal as a server refusal, never as a runtime rejection", async () => {
    const user = userEvent.setup();
    const refusal = { error: { code: "stale_generation", message: "The attempt has already moved on." } };
    renderWorkDetail({ command: () => new Response(JSON.stringify(refusal), { status: 409 }) });
    await openDetail();
    await user.click(screen.getByRole("button", { name: "Cancel run" }));
    const receipt = await screen.findByText("Refused by the server");
    const card = receipt.closest(".receipt");
    expect(card).not.toBeNull();
    expect(card).toHaveAttribute("data-outcome", "refused");
    expect(card).toHaveTextContent("The attempt has already moved on.");
    expect(screen.queryByText("Rejected by the runtime")).toBeNull();
  });

  it("sends each control mutation with the session's CSRF token", async () => {
    const user = userEvent.setup();
    const harness = renderWorkDetail({
      command: (path) => accepted(path.endsWith("/cancel")
        ? { disposition: "created", command: cancelCommand, execution: { task_id: taskId, run_id: null, generation: 1, state: "cancelling" } }
        : {
          disposition: "created",
          command: { command_id: "88888888-8888-4888-8888-888888888888", task_id: taskId, kind: "retry", state: "pending", acknowledgement_outcome: null },
          work_item: { id: subjectId },
          execution: { task_id: taskId, run_id: null, generation: 2, state: "queued" },
        }),
    });
    await openDetail();
    await user.click(screen.getByRole("button", { name: "Cancel run" }));
    await waitFor(() => expect(harness.posts()).toHaveLength(1));
    expect(harness.posts()[0]?.path).toBe(`/portal/api/work-items/${subjectId}/cancel`);
    expect(harness.posts()[0]?.headers["x-csrf-token"]).toBe("session-token");
    expect(harness.posts()[0]?.headers["x-csrfToken"]).toBeUndefined();
  });

  it("uses the rotated session token after another tab renews the shared cookie", async () => {
    const user = userEvent.setup();
    const harness = renderWorkDetail({
      session: (readIndex) => new Response(JSON.stringify({ csrf_token: readIndex === 1 ? "initial-session-token" : "rotated-session-token" })),
      command: () => accepted({
        disposition: "created",
        command: cancelCommand,
        execution: { task_id: taskId, run_id: null, generation: 1, state: "cancelling" },
      }),
    });
    await openDetail();
    await act(async () => {
      await harness.application.queryClient.invalidateQueries({ queryKey: ["session"] });
    });
    await waitFor(() => expect(harness.requests.filter((request) => request.path === "/portal/api/session")).toHaveLength(2));
    await user.click(screen.getByRole("button", { name: "Cancel run" }));
    await waitFor(() => expect(harness.posts()).toHaveLength(1));
    expect(harness.posts()[0]?.headers["x-csrf-token"]).toBe("rotated-session-token");
  });

  it("keeps an undescribed 403 a session-protection refusal instead of a workspace denial", async () => {
    const user = userEvent.setup();
    renderWorkDetail({ command: () => new Response("{}", { status: 403 }) });
    await openDetail();
    await user.click(screen.getByRole("button", { name: "Cancel run" }));
    const card = await waitFor(() => {
      const found = receiptCards("Refused by the server");
      expect(found).toHaveLength(1);
      return found[0];
    });
    expect(card).toHaveTextContent("If you signed in elsewhere, refresh the page and try again.");
  });
});

describe("control receipts", () => {
  it("renders an accepted start on a queued run with its own headline, never an acknowledgement", async () => {
    const user = userEvent.setup();
    renderWorkDetail({
      detail: (readIndex) => new Response(JSON.stringify(readIndex === 1 ? idleDetail() : queuedDetail())),
      command: () => accepted({
        disposition: "created",
        execution: { task_id: taskId, run_id: null, generation: 1, state: "queued" },
      }),
    });
    await openDetail();
    await user.click(screen.getByRole("button", { name: "Start run" }));
    const headline = await screen.findByText("Awaiting a runtime");
    expect(headline.closest(".receipt")).toHaveAttribute("data-outcome", "queued");
    expect(screen.queryByText("Awaiting agent acknowledgement")).toBeNull();
  });

  it("never renders a lost response as failed before the server's own records say so", async () => {
    const user = userEvent.setup();
    const held = Promise.withResolvers<Response>();
    renderWorkDetail({
      detail: (readIndex) => (readIndex === 1 ? new Response(JSON.stringify(runningDetail())) : held.promise),
      command: () => Promise.reject(new TypeError("offline")),
    });
    await openDetail();
    await user.click(screen.getByRole("button", { name: "Cancel run" }));
    const card = await waitFor(() => {
      const found = receiptCards("Outcome unknown");
      expect(found).toHaveLength(1);
      return found[0];
    });
    expect(card).toHaveTextContent("Cannot reach the server");
    expect(screen.queryByText("Failed")).toBeNull();
  });

  it("keeps the older pages' command receipts after a detail refresh re-keys the timeline", async () => {
    const user = userEvent.setup();
    let detailReads = 0;
    const firstPageCommand = {
      source: "command",
      run_id: null,
      generation: 1,
      recorded_at: "2026-11-16T10:00:00Z",
      data: {
        command_id: "aaaaaaa1-0000-4000-8000-000000000001",
        task_id: taskId,
        kind: "cancel",
        payload: {},
        state: "applied",
        issued_at: "2026-11-16T10:00:00Z",
        applied_at: null,
        acknowledgement_id: null,
        acknowledgement_outcome: null,
        acknowledged_at: null,
      },
    };
    const olderPageCommand = {
      source: "command",
      run_id: null,
      generation: 1,
      recorded_at: "2026-11-16T09:00:00Z",
      data: {
        command_id: "bbbbbbb2-0000-4000-8000-000000000002",
        task_id: taskId,
        kind: "guidance",
        payload: {},
        state: "acknowledged",
        issued_at: "2026-11-16T09:00:00Z",
        applied_at: null,
        acknowledgement_id: null,
        acknowledgement_outcome: "applied",
        acknowledged_at: null,
      },
    };
    const respond = (input: string | Request | URL): Response => {
      const url = new URL(input instanceof Request ? input.url : String(input), "http://localhost");
      if (url.pathname === "/portal/api/session") return new Response(JSON.stringify({ csrf_token: "session-token" }));
      if (url.pathname === `/portal/api/work-items/${subjectId}/timeline`) {
        expect(url.searchParams.get("before")).toBeTruthy();
        return new Response(JSON.stringify({ items: [olderPageCommand], next_before: null }));
      }
      if (url.pathname === `/portal/api/work-items/${subjectId}`) {
        detailReads += 1;
        return new Response(JSON.stringify(detailBody({
          execution: { task_id: taskId, run_id: null, generation: 1, state: "running", can_cancel: true },
          timeline: [firstPageCommand],
          nextBefore: `cursor-${detailReads}`,
        })));
      }
      if (url.pathname === "/portal/api/workspace") return new Response(JSON.stringify(workspaceBody()));
      return new Response("{}", { status: 404 });
    };
    const fetcher = vi.fn<typeof fetch>().mockImplementation((input) => Promise.resolve(respond(input)));
    const application = createApplication({ fetcher, location: { href: `http://localhost/portal/work/${subjectId}`, replace: vi.fn() } });
    applications.push(application);
    const history: RouterHistory = createMemoryHistory({ initialEntries: [`/portal/work/${subjectId}`] });
    const router = createAppRouter(application, history);
    render(<QueryClientProvider client={application.queryClient}><RouterProvider router={router} /></QueryClientProvider>);
    await openDetail();
    // The first page's own command renders in the receipt list.
    expect(await screen.findByText("Queued for the runtime")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Load older history" }));
    expect(await screen.findByText("Applied")).toBeInTheDocument();
    // The refresh serves a new boundary, re-keying the disabled timeline query.
    await user.click(screen.getByRole("button", { name: "Refresh" }));
    await waitFor(() => expect(detailReads).toBeGreaterThanOrEqual(2));
    expect(receiptCards("Applied")).toHaveLength(1);
  });
});
