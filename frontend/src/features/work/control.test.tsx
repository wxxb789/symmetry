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

describe("capability-bound controls", () => {
  it("renders cancel and retry only from the served signals, never from the live state", async () => {
    const user = userEvent.setup();
    renderWorkDetail({
      detail: (readIndex) => new Response(JSON.stringify(detailBody({
        // The same live state both reads; only the server's own signals change.
        execution: { task_id: taskId, run_id: null, generation: 1, state: "running", can_cancel: readIndex === 1, can_retry: readIndex === 1 },
      }))),
    });
    await openDetail();
    expect(await screen.findByRole("button", { name: "Cancel run" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Retry" })).toBeInTheDocument();
    expect(document.querySelector(".control-bar .chip")).toHaveTextContent("Running");

    // The refresh serves the same state with both operations withheld (the
    // runtime advertises neither). A state-derived surface would keep the
    // controls live; this one renders only the served signal.
    await user.click(screen.getByRole("button", { name: "Refresh" }));
    await waitFor(() => expect(screen.queryByRole("button", { name: "Cancel run" })).toBeNull());
    expect(screen.queryByRole("button", { name: "Retry" })).toBeNull();
    expect(document.querySelector(".control-bar .chip")).toHaveTextContent("Running");
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

  it("keeps the receipts of the older pages a refresh re-served at the same boundary", async () => {
    const user = userEvent.setup();
    const newest = commandEntry("aaaaaaa1-0000-4000-8000-000000000001", "2026-11-16T10:00:00Z", "cancel", "applied", null);
    const older = commandEntry("bbbbbbb2-0000-4000-8000-000000000002", "2026-11-16T09:00:00Z", "guidance", "acknowledged", "applied");
    const harness = renderTimelineHarness({
      // Every read mints its own token for the same boundary entry, exactly as
      // the server's signed cursor does; the boundary is what the pages hang on.
      detail: (readIndex) => detailBody({ timeline: [newest], nextBefore: `cursor-${readIndex}` }),
      timeline: () => ({ items: [older], next_before: null }),
    });
    await openDetail();
    expect(await screen.findByText("Queued for the runtime")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Load older history" }));
    expect(await screen.findByText("Applied")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Refresh" }));
    await waitFor(() => expect(harness.detailReads()).toBeGreaterThanOrEqual(2));
    // The refreshed read re-keys the query, so its own control replaces the
    // exhausted one — which is what proves this render is the refreshed one.
    await screen.findByRole("button", { name: "Load older history" });
    // The refreshed read ends at the entry the older page was fetched from, so
    // that page still belongs to the chain this detail serves.
    expect(receiptCards("Applied")).toHaveLength(1);
  });

  it("does not render the pages a replaced first-page boundary did not serve", async () => {
    const user = userEvent.setup();
    const servedBoundary = commandEntry("aaaaaaa1-0000-4000-8000-000000000001", "2026-11-16T10:00:00Z", "cancel", "applied", null);
    const replacedBoundary = commandEntry("ccccccc3-0000-4000-8000-000000000003", "2026-11-16T11:00:00Z", "start", "pending", null);
    const retained = commandEntry("bbbbbbb2-0000-4000-8000-000000000002", "2026-11-16T09:00:00Z", "guidance", "acknowledged", "applied");
    const harness = renderTimelineHarness({
      detail: (readIndex) => detailBody({ timeline: [readIndex === 1 ? servedBoundary : replacedBoundary], nextBefore: `cursor-${readIndex}` }),
      timeline: () => ({ items: [retained], next_before: null }),
    });
    await openDetail();
    expect(await screen.findByText("Queued for the runtime")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Load older history" }));
    expect(await screen.findByText("Applied")).toBeInTheDocument();
    expect(harness.cursors).toEqual(["cursor-1"]);
    await user.click(screen.getByRole("button", { name: "Refresh" }));
    await waitFor(() => expect(harness.detailReads()).toBeGreaterThanOrEqual(2));
    // The refreshed read's own first page renders before the page it did not
    // serve is judged: nothing else proves the render is the refreshed one.
    expect(await screen.findByText("Awaiting agent acknowledgement")).toBeInTheDocument();
    // The refreshed read paginates from its own boundary, so the page the reader
    // fetched under the replaced one is not served beside it.
    expect(screen.queryByText("Applied")).toBeNull();
    // The reader's next read continues from the served boundary, and the older
    // page renders under the cursor that actually serves it.
    await user.click(await screen.findByRole("button", { name: "Load older history" }));
    expect(await screen.findByText("Applied")).toBeInTheDocument();
    expect(harness.cursors).toEqual(["cursor-1", "cursor-2"]);
  });
});

/** One served command entry of a task's timeline, in the shape both reads carry. */
function commandEntry(commandId: string, recordedAt: string, kind: string, state: string, acknowledgement: string | null) {
  return {
    source: "command",
    run_id: null,
    generation: 1,
    recorded_at: recordedAt,
    data: {
      command_id: commandId,
      task_id: taskId,
      kind,
      payload: {},
      state,
      issued_at: recordedAt,
      applied_at: null,
      acknowledgement_id: null,
      acknowledgement_outcome: acknowledgement,
      acknowledged_at: null,
    },
  };
}

/**
 * The work detail with a scripted timeline read: one served first page per
 * detail read, and one older page per `before` cursor the surface asks for.
 */
function renderTimelineHarness(options: {
  detail: (readIndex: number) => Record<string, unknown>;
  timeline: (before: string) => { items: unknown[]; next_before: string | null };
}) {
  let detailReads = 0;
  const cursors: string[] = [];
  const fetcher = vi.fn<typeof fetch>().mockImplementation((input) => {
    const url = new URL(input instanceof Request ? input.url : String(input), "http://localhost");
    if (url.pathname === `/portal/api/work-items/${subjectId}/timeline`) {
      const before = url.searchParams.get("before");
      expect(before).toBeTruthy();
      cursors.push(before ?? "");
      return Promise.resolve(new Response(JSON.stringify(options.timeline(before ?? ""))));
    }
    if (url.pathname === `/portal/api/work-items/${subjectId}`) {
      detailReads += 1;
      return Promise.resolve(new Response(JSON.stringify(options.detail(detailReads))));
    }
    if (url.pathname === "/portal/api/session") return Promise.resolve(new Response(JSON.stringify({ csrf_token: "session-token" })));
    if (url.pathname === "/portal/api/workspace") return Promise.resolve(new Response(JSON.stringify(workspaceBody())));
    return Promise.resolve(new Response("{}", { status: 404 }));
  });
  const application = createApplication({ fetcher, location: { href: `http://localhost/portal/work/${subjectId}`, replace: vi.fn() } });
  applications.push(application);
  const history: RouterHistory = createMemoryHistory({ initialEntries: [`/portal/work/${subjectId}`] });
  const router = createAppRouter(application, history);
  render(<QueryClientProvider client={application.queryClient}><RouterProvider router={router} /></QueryClientProvider>);
  return { cursors, detailReads: () => detailReads };
}
