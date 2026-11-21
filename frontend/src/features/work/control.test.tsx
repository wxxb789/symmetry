import { act, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse } from "msw";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { jsonResponse, portal } from "../../app/portal-double";
import { renderRoute, trackApplications } from "../../app/test-harness";
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
  return { ...body, work_item: { ...(body.work_item as Record<string, unknown>), ...options.item } };
}

const runningDetail = () => detailBody({ execution: { task_id: taskId, run_id: null, generation: 1, state: "running", can_cancel: true } });
const idleDetail = () => detailBody({ item: { can_start: true } });
const queuedDetail = () => detailBody({ execution: { task_id: taskId, run_id: null, generation: 1, state: "queued", can_cancel: false } });

const accepted = (body: Record<string, unknown>) => jsonResponse(body, 202);

interface HarnessOptions {
  detail?: Record<string, unknown>;
  /** The detail read, by 1-based read index, when the served state changes per read. */
  detailReads?: (readIndex: number) => Response | Promise<Response>;
  /** A write POST: the answer, a refusal, or a transport failure. */
  command?: (path: string, body: Record<string, unknown> | null) => Response | Promise<Response>;
  /** The session read; `readIndex` starts at 1 so a rotation can be sequenced. */
  session?: (readIndex: number) => Record<string, unknown>;
}

const applications = trackApplications();

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

beforeEach(() => { stubMatchMedia(false); });

function seedPortal(detail: Record<string, unknown> = runningDetail()) {
  portal.seed({
    workspace: {
      selectedProjectId: projectId,
      projects: [{
        id: projectId,
        key: "ONE",
        name: "Project One",
        description: null,
        status: "active",
        default_agent_profile: "codex-default",
        default_workspace: "primary",
        resources: [],
        work_items: [],
      }],
      runtimes: [],
      registeredRuntimes: [],
      health: { status: "unknown" },
    },
    details: { [subjectId]: detail },
  });
}

function renderWorkDetail(options: HarnessOptions = {}) {
  let detailReads = 0;
  let sessionReads = 0;
  seedPortal(options.detail);
  portal.serve((request) => {
    if (options.session !== undefined && request.method === "GET" && request.url.pathname === "/portal/api/session") {
      sessionReads += 1;
      return jsonResponse(options.session(sessionReads));
    }
    if (options.detailReads !== undefined && request.method === "GET" && request.url.pathname === `/portal/api/work-items/${subjectId}`) {
      detailReads += 1;
      return options.detailReads(detailReads);
    }
    if (request.method === "POST" && request.url.pathname.startsWith("/portal/api/work-items/")) {
      const respond = options.command ?? (() => jsonResponse({}, 500));
      return respond(request.url.pathname, request.body);
    }
    return undefined;
  });
  const { application } = renderRoute(`/portal/work/${subjectId}`);
  applications.push(application);
  return {
    application,
    posts: () => portal.requests.filter((request) => request.method === "POST"),
  };
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
    renderWorkDetail({ command: () => jsonResponse(refusal, 409) });
    await openDetail();
    await user.click(screen.getByRole("button", { name: "Cancel run" }));
    const receipt = await screen.findByText("Refused by the server");
    const card = receipt.closest(".receipt");
    expect(card).not.toBeNull();
    expect(card).toHaveAttribute("data-outcome", "refused");
    expect(card).toHaveTextContent("The attempt has already moved on.");
    expect(screen.queryByText("Rejected by the runtime")).toBeNull();
  });

  const mutations: readonly {
    control: string;
    path: string;
    served: () => Record<string, unknown>;
    answer: () => Record<string, unknown>;
  }[] = [
    {
      control: "Start run",
      path: "run",
      served: idleDetail,
      answer: () => ({ disposition: "created", execution: { task_id: taskId, run_id: null, generation: 1, state: "queued" } }),
    },
    {
      control: "Cancel run",
      path: "cancel",
      served: runningDetail,
      answer: () => ({ disposition: "created", command: cancelCommand, execution: { task_id: taskId, run_id: null, generation: 1, state: "cancelling" } }),
    },
    {
      control: "Retry",
      path: "retry",
      served: () => detailBody({ execution: { task_id: taskId, run_id: null, generation: 1, state: "running", can_retry: true } }),
      answer: () => ({
        disposition: "created",
        command: { command_id: "88888888-8888-4888-8888-888888888888", task_id: taskId, kind: "retry", state: "pending", acknowledgement_outcome: null },
        work_item: { id: subjectId },
        execution: { task_id: taskId, run_id: null, generation: 2, state: "queued" },
      }),
    },
  ];

  it.each(mutations)("sends the $control control with the session's CSRF token", async ({ control, path, served, answer }) => {
    const user = userEvent.setup();
    const harness = renderWorkDetail({
      detail: served(),
      command: () => accepted(answer()),
    });
    await openDetail();
    await user.click(screen.getByRole("button", { name: control }));
    await waitFor(() => { expect(harness.posts()).toHaveLength(1); });
    expect(harness.posts()[0]?.path).toBe(`/portal/api/work-items/${subjectId}/${path}`);
    expect(harness.posts()[0]?.headers["x-csrf-token"]).toBe("session-token");
    expect(harness.posts()[0]?.headers["x-csrftoken"]).toBeUndefined();
  });

  it("uses the rotated session token after another tab renews the shared cookie", async () => {
    const user = userEvent.setup();
    const harness = renderWorkDetail({
      session: (readIndex) => ({ csrf_token: readIndex === 1 ? "initial-session-token" : "rotated-session-token" }),
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
    await waitFor(() => { expect(portal.requests.filter((request) => request.path === "/portal/api/session")).toHaveLength(2); });
    await user.click(screen.getByRole("button", { name: "Cancel run" }));
    await waitFor(() => { expect(harness.posts()).toHaveLength(1); });
    expect(harness.posts()[0]?.headers["x-csrf-token"]).toBe("rotated-session-token");
  });

  it("keeps an undescribed 403 a session-protection refusal instead of a workspace denial", async () => {
    const user = userEvent.setup();
    renderWorkDetail({ command: () => jsonResponse({}, 403) });
    await openDetail();
    await user.click(screen.getByRole("button", { name: "Cancel run" }));
    const card = await waitFor(() => {
      const found = receiptCards("Refused by the server");
      expect(found).toHaveLength(1);
      return found[0];
    });
    expect(card).toHaveTextContent("If you signed in elsewhere, refresh the page and try again.");
  });

  it("refuses a control whose attempt has no storable identity instead of sending it unidentified", async () => {
    const user = userEvent.setup();
    const harness = renderWorkDetail();
    await openDetail();
    // A browser whose session storage refuses every access: the attempt has no
    // durable identity, and a request without one could be applied twice by a
    // double click or a lost response, so the surface must refuse it instead.
    vi.stubGlobal("sessionStorage", {
      getItem: () => { throw new Error("storage disabled"); },
      setItem: () => { throw new Error("storage disabled"); },
      removeItem: () => { throw new Error("storage disabled"); }
    });
    try {
      await user.click(screen.getByRole("button", { name: "Cancel run" }));
      const card = await waitFor(() => {
        const found = receiptCards("Not sent");
        expect(found).toHaveLength(1);
        return found[0];
      });
      expect(card).toHaveAttribute("data-outcome", "not_sent");
      expect(card).toHaveTextContent("This browser cannot store an attempt identity, so the request was not sent.");
      // The missing identity, not the request, decides: nothing was sent, and a
      // second press cannot degrade into sending one unidentified either.
      await user.click(screen.getByRole("button", { name: "Cancel run" }));
      expect(harness.posts()).toHaveLength(0);
    } finally {
      vi.unstubAllGlobals();
    }
  });
});

describe("capability-bound controls", () => {
  it("renders cancel and retry only from the served signals, never from the live state", async () => {
    const user = userEvent.setup();
    renderWorkDetail({
      detailReads: (readIndex) => jsonResponse(detailBody({
        // The same live state both reads; only the server's own signals change.
        execution: { task_id: taskId, run_id: null, generation: 1, state: "running", can_cancel: readIndex === 1, can_retry: readIndex === 1 },
      })),
    });
    await openDetail();
    expect(await screen.findByRole("button", { name: "Cancel run" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Retry" })).toBeInTheDocument();
    expect(document.querySelector(".control-bar .chip")).toHaveTextContent("Running");

    // The refresh serves the same state with both operations withheld (the
    // runtime advertises neither). A state-derived surface would keep the
    // controls live; this one renders only the served signal.
    await user.click(screen.getByRole("button", { name: "Refresh" }));
    await waitFor(() => { expect(screen.queryByRole("button", { name: "Cancel run" })).toBeNull(); });
    expect(screen.queryByRole("button", { name: "Retry" })).toBeNull();
    expect(document.querySelector(".control-bar .chip")).toHaveTextContent("Running");
  });
});

describe("control receipts", () => {
  it("renders an accepted start on a queued run with its own headline, never an acknowledgement", async () => {
    const user = userEvent.setup();
    renderWorkDetail({
      detailReads: (readIndex) => jsonResponse(readIndex === 1 ? idleDetail() : queuedDetail()),
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
      detailReads: (readIndex) => (readIndex === 1 ? jsonResponse(runningDetail()) : held.promise),
      command: () => HttpResponse.error(),
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

  it("keeps a local attempt rendered when the served latest command is not on the rendered page", async () => {
    const user = userEvent.setup();
    const commandId = "99999999-9999-4999-8999-999999999999";
    renderWorkDetail({
      // Read 1 serves the live cancel control; read 2 serves the snapshot that
      // names the command as `latest_command` while the rendered timeline page
      // does not carry its entry.
      detailReads: (readIndex) => jsonResponse(readIndex === 1
        ? runningDetail()
        : detailBody({
          execution: {
            task_id: taskId,
            run_id: null,
            generation: 1,
            state: "cancelling",
            latest_command: {
              command_id: commandId,
              task_id: taskId,
              run_id: null,
              generation: null,
              kind: "cancel",
              payload: {},
              state: "pending",
              issued_at: "2026-11-16T10:00:00Z",
              applied_at: null,
              acknowledgement_id: null,
              acknowledgement_outcome: null,
              acknowledged_at: null,
            },
          },
          timeline: [commandEntry("bbbbbbb2-0000-4000-8000-000000000002", "2026-11-16T09:00:00Z", "guidance", "acknowledged", "applied")],
        })),
      command: () => accepted({
        disposition: "created",
        command: { command_id: commandId, task_id: taskId, kind: "cancel", state: "pending", acknowledgement_outcome: null },
        execution: { task_id: taskId, run_id: null, generation: 1, state: "cancelling" },
      }),
    });
    await openDetail();
    await user.click(screen.getByRole("button", { name: "Cancel run" }));
    // The refetched first page is what names the served command without
    // rendering it; once its own receipt row is on screen the attempt has
    // settled.
    await screen.findByText("Applied");
    // The refreshed check compares millisecond snapshot timestamps, and the
    // press's own refetch may already have landed when the attempt settled;
    // the explicit refresh below makes the snapshot the attempt re-reads
    // strictly newer than its baseline.
    const pressGap = Promise.withResolvers<void>();
    setTimeout(pressGap.resolve, 50);
    await pressGap.promise;
    await user.click(screen.getByRole("button", { name: "Refresh" }));
    // The refreshed snapshot names the command but does not render it: the
    // attempt must keep its own receipt instead of vanishing unaccounted.
    await waitFor(() => {
      const rows = screen.queryAllByText("cancel · generation 1")
        .map((node) => node.closest(".receipt"))
        .filter((node): node is HTMLElement => node !== null);
      expect(rows).toHaveLength(1);
      expect(rows[0]).toHaveAttribute("data-outcome", "unknown");
    });
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
    expect(await screen.findByText("Applied by the control plane")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Load older history" }));
    expect(await screen.findByText("Applied")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Refresh" }));
    await waitFor(() => { expect(harness.detailReads()).toBeGreaterThanOrEqual(2); });
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
    expect(await screen.findByText("Applied by the control plane")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Load older history" }));
    expect(await screen.findByText("Applied")).toBeInTheDocument();
    expect(harness.cursors).toEqual(["cursor-1"]);
    await user.click(screen.getByRole("button", { name: "Refresh" }));
    await waitFor(() => { expect(harness.detailReads()).toBeGreaterThanOrEqual(2); });
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
  seedPortal();
  portal.serve((request) => {
    if (request.method !== "GET") return undefined;
    if (request.url.pathname === `/portal/api/work-items/${subjectId}`) {
      detailReads += 1;
      return jsonResponse(options.detail(detailReads));
    }
    if (request.url.pathname === `/portal/api/work-items/${subjectId}/timeline`) {
      const before = request.url.searchParams.get("before");
      expect(before).toBeTruthy();
      cursors.push(before ?? "");
      return jsonResponse(options.timeline(before ?? ""));
    }
    return undefined;
  });
  const { application } = renderRoute(`/portal/work/${subjectId}`);
  applications.push(application);
  return { cursors, detailReads: () => detailReads };
}
