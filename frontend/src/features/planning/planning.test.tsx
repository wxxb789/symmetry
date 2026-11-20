import { QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory, RouterProvider } from "@tanstack/react-router";
import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { createApplication, type Application } from "../../app/application";
import { createAppRouter } from "../../app/router";
import { ServerRefusal, type Project, type WorkItem } from "../../services/api";
import { readWorkDetail } from "../../services/api.fixtures";
import { boardMoveRequest, failureMode, providerOwnedControlsOf, refusalNotice, resetPendingWorkItemCreations, workItemDraftOf, workItemFormStateOf, workItemUpdatePayload, type BoardColumnSnapshot } from "./write-policy";

// This suite drives the real component tree against a stateful HTTP double: the
// double keeps a served revision per row and applies the same rules the Control
// changesets hold (move fields refused on update, blocked requires a blocker,
// Goal-managed rows refuse legacy writes), so a test can assert both what the
// surface sends and what it re-reads. The real-API proof lives in the browser
// suite; this suite pins client-side invariants a live round trip is not needed
// to observe.

const sessionToken = "session-token";
const firstProjectId = "11111111-1111-4111-8111-111111111111";
const secondProjectId = "22222222-2222-4222-8222-222222222222";
const firstItemId = "33333333-3333-4333-8333-333333333333";
const secondItemId = "44444444-4444-4444-8444-444444444444";
const goalItemId = "55555555-5555-4555-8555-555555555555";
const providerItemId = "66666666-6666-4666-8666-666666666666";
const repositoryResourceId = "77777777-7777-4777-8777-777777777777";

interface HarnessResource {
  id: string;
  kind?: string;
  name?: string;
}

interface HarnessProject {
  id: string;
  key: string;
  name: string;
  description: string | null;
  status: string;
  version: number;
  default_agent_profile?: string;
  default_workspace?: string;
  resources: HarnessResource[];
  work_items: WorkItem[];
  /** creation_id -> work item id: the server's own creation identity index. */
  creation_ids: Record<string, string>;
}

interface HarnessState {
  projects: HarnessProject[];
  /** Detail bodies served for a work item; the marker may differ from the write-side state. */
  detail: Record<string, Record<string, unknown>>;
}

interface RecordedRequest {
  method: string;
  path: string;
  body: Record<string, unknown> | null;
}

function workItem(overrides: Partial<WorkItem> & { id: string; title: string }): WorkItem {
  return {
    key: "ONE-1",
    project_id: firstProjectId,
    description: null,
    status: "backlog",
    priority: "medium",
    position: 0,
    blocked: false,
    blocker: null,
    version: 1,
    assignee: { type: "unassigned", name: null, agent_profile: null },
    delivery: {
      pull_request: { value: null, source: "none", generation: null, url: null, status: null },
      ci: { value: "unknown", source: "none", generation: null, url: null, status: "unknown" },
      review: { value: "none", source: "none", generation: null, url: null, status: "none" },
    },
    execution: null,
    external: null,
    updated_at: null,
    ...overrides,
  };
}

function baseState(): HarnessState {
  return {
    projects: [
      {
        id: firstProjectId,
        key: "ONE",
        name: "Project One",
        description: "First",
        status: "active",
        version: 3,
        default_agent_profile: "default",
        default_workspace: "primary",
        resources: [
          { id: repositoryResourceId, kind: "repository", name: "Repo resource" },
          { id: "88888888-8888-4888-8888-888888888888", kind: "ci", name: "CI resource" },
        ],
        creation_ids: {},
        work_items: [
          workItem({ id: firstItemId, title: "Legacy item", status: "backlog", position: 0 }),
          workItem({ id: secondItemId, title: "Ready item", status: "ready", position: 0 }),
          workItem({ id: goalItemId, title: "Goal item", status: "ready", position: 1, goal_managed: true }),
          workItem({
            id: providerItemId,
            title: "Provider item",
            status: "review",
            position: 0,
            external: {
              provider: "github",
              id: "9001",
              url: null,
              state: "open",
              available: true,
              assignee: null,
              labels: [],
              updated_at: null,
              resource_id: "99999999-9999-4999-8999-999999999999",
              provider_owned_fields: ["title", "description", "priority", "state", "labels", "assignee"],
            },
          }),
        ],
      },
      {
        id: secondProjectId,
        key: "TWO",
        name: "Project Two",
        description: null,
        status: "archived",
        version: 2,
        default_agent_profile: "default",
        default_workspace: "primary",
        resources: [],
        creation_ids: {},
        work_items: [workItem({ id: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", project_id: secondProjectId, key: "TWO-1", title: "Archived item" })],
      },
    ],
    detail: {},
  };
}

function projectOf(state: HarnessState, id: string): HarnessProject | undefined {
  return state.projects.find((candidate) => candidate.id === id);
}

function itemOf(state: HarnessState, id: string): { project: HarnessProject; item: WorkItem } | null {
  for (const project of state.projects) {
    const item = project.work_items.find((candidate) => candidate.id === id);
    if (item !== undefined) return { project, item };
  }
  return null;
}

// The fake control plane echoes request fields back; only string values are
// meaningful, and anything else falls back like the server's defaults.
const text = (value: unknown, fallback: string): string => (typeof value === "string" ? value : fallback);

interface Harness {
  state: HarnessState;
  requests: RecordedRequest[];
  /** The next mutation fails with a transport error before it reaches the double. */
  failNextMutation: { current: boolean };
  /** The next work-item create commits on the double and its response is destroyed at the transport. */
  loseNextCreateResponse: { current: boolean };
  /** Holds mutations until the returned release function runs. */
  holdNextMutation: () => () => void;
  fetcher: typeof fetch;
}

function createHarness(state: HarnessState = baseState()): Harness {
  const requests: RecordedRequest[] = [];
  let gate: { promise: Promise<void>; release: () => void } | null = null;
  const harness: Harness = {
    state,
    requests,
    failNextMutation: { current: false },
    loseNextCreateResponse: { current: false },
    holdNextMutation: () => {
      const resolvers = Promise.withResolvers<void>();
      gate = { promise: resolvers.promise, release: resolvers.resolve };
      return () => { gate = null; resolvers.resolve(); };
    },
    fetcher: undefined as unknown as typeof fetch,
  };
  const json = (body: unknown, status = 200) => new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } });
  const workspaceBody = (selectedId: string | null) => ({
    selected_project_id: selectedId ?? firstProjectId,
    projects: state.projects.map((project) => (project.id === selectedId ? project : { ...project, work_items: [] })),
  });
  harness.fetcher = vi.fn<typeof fetch>().mockImplementation(async (input, init) => {
    const raw = input instanceof Request ? input.url : String(input);
    const url = new URL(raw, "http://localhost");
    const method = (init?.method ?? "GET").toUpperCase();
    const body = typeof init?.body === "string" ? (JSON.parse(init.body) as Record<string, unknown>) : null;
    requests.push({ method, path: url.pathname, body });
    if (url.pathname === "/portal/api/session") return json({ csrf_token: sessionToken });
    if (method === "GET" && url.pathname === "/portal/api/workspace") return json(workspaceBody(url.searchParams.get("project_id")));
    const detailMatch = /^\/portal\/api\/work-items\/([^/]+)$/.exec(url.pathname);
    if (method === "GET" && detailMatch !== undefined && detailMatch !== null) {
      const id = detailMatch[1] ?? "";
      const found = itemOf(state, id);
      const served = found === null ? { id, projectId: firstProjectId, title: "Item" } : { id, projectId: found.project.id, title: found.item.title, status: found.item.status };
      return json(state.detail[id] ?? readWorkDetail(served));
    }
    if (harness.failNextMutation.current) {
      harness.failNextMutation.current = false;
      throw new TypeError("fetch failed");
    }
    if (gate !== null) await gate.promise;
    if (method === "POST" && url.pathname === "/portal/api/projects") {
      const key = text(body?.["key"], "");
      if (state.projects.some((project) => project.key === key)) {
        return json({ error: { code: "validation_failed", message: "request contains invalid fields", fields: { key: ["has already been taken"] } } }, 422);
      }
      const created: HarnessProject = {
        id: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb",
        key,
        name: text(body?.["name"], ""),
        description: text(body?.["description"], ""),
        status: "active",
        version: 1,
        default_agent_profile: text(body?.["default_agent_profile"], "default"),
        default_workspace: text(body?.["default_workspace"], "primary"),
        resources: [],
        creation_ids: {},
        work_items: [],
      };
      state.projects = [...state.projects, created];
      return json({ project: { id: created.id } }, 201);
    }
    const projectMatch = /^\/portal\/api\/projects\/([^/]+)$/.exec(url.pathname);
    if (method === "PATCH" && projectMatch !== undefined && projectMatch !== null) {
      const project = projectOf(state, projectMatch[1] ?? "");
      if (project === undefined) return json({ error: { code: "not_found", message: "not found" } }, 404);
      if (body?.["version"] !== project.version) return json({ error: { code: "stale", message: "resource changed since it was loaded" } }, 409);
      if (project.status === "archived" && Object.keys(body ?? {}).some((key) => key !== "version" && key !== "status")) {
        return json({ error: { code: "state_conflict", message: "state has already advanced" } }, 409);
      }
      if (typeof body?.["name"] === "string") project.name = body["name"];
      if (typeof body?.["description"] === "string") project.description = body["description"];
      if (typeof body?.["status"] === "string") project.status = body["status"];
      project.version += 1;
      return json({ project: { id: project.id } });
    }
    const createItemMatch = /^\/portal\/api\/projects\/([^/]+)\/work-items$/.exec(url.pathname);
    if (method === "POST" && createItemMatch !== undefined && createItemMatch !== null) {
      const project = projectOf(state, createItemMatch[1] ?? "");
      if (project === undefined) return json({ error: { code: "not_found", message: "not found" } }, 404);
      const title = text(body?.["title"], "");
      if (title.trim() === "") {
        return json({ error: { code: "validation_failed", message: "request contains invalid fields", fields: { title: ["can't be blank"] } } }, 422);
      }
      // The server's creation identity: a replayed identity answers with the
      // row the original submission created instead of inserting a second one.
      const creationId = typeof body?.["creation_id"] === "string" ? body["creation_id"] : "";
      const replayedId = creationId === "" ? undefined : project.creation_ids[creationId];
      if (replayedId !== undefined) return json({ disposition: "replayed", work_item: { id: replayedId } });
      const created = workItem({ id: "cccccccc-cccc-4ccc-8ccc-cccccccccccc", title, project_id: project.id, status: text(body?.["status"], "backlog") });
      project.work_items = [...project.work_items, created];
      if (creationId !== "") project.creation_ids[creationId] = created.id;
      if (harness.loseNextCreateResponse.current) {
        harness.loseNextCreateResponse.current = false;
        // Committed on the double; the answer dies at the transport.
        throw new TypeError("fetch failed");
      }
      return json({ disposition: "created", work_item: { id: created.id } }, 201);
    }
    const moveMatch = /^\/portal\/api\/work-items\/([^/]+)\/move$/.exec(url.pathname);
    if (method === "PATCH" && moveMatch !== undefined && moveMatch !== null) {
      const found = itemOf(state, moveMatch[1] ?? "");
      if (found === null) return json({ error: { code: "not_found", message: "not found" } }, 404);
      if (found.item.goal_managed === true) return json({ error: { code: "goal_authority_required", message: "Goal authority is required for this operation" } }, 403);
      if (body?.["version"] !== found.item.version) return json({ error: { code: "stale", message: "resource changed since it was loaded" } }, 409);
      found.item.status = text(body?.["status"], found.item.status);
      found.item.position = body?.["before_id"] === null ? 99 : 0;
      found.item.version = (found.item.version ?? 0) + 1;
      return json({ work_item: { id: found.item.id } });
    }
    const updateItemMatch = /^\/portal\/api\/work-items\/([^/]+)$/.exec(url.pathname);
    if (method === "PATCH" && updateItemMatch !== undefined && updateItemMatch !== null) {
      const found = itemOf(state, updateItemMatch[1] ?? "");
      if (found === null) return json({ error: { code: "not_found", message: "not found" } }, 404);
      if (body !== null && ("status" in body || "position" in body)) {
        return json({ error: { code: "validation_failed", message: "request contains invalid fields", fields: { status: ["must be changed through a Kanban move"] } } }, 422);
      }
      if (found.item.goal_managed === true) return json({ error: { code: "goal_authority_required", message: "Goal authority is required for this operation" } }, 403);
      if (body?.["version"] !== found.item.version) return json({ error: { code: "stale", message: "resource changed since it was loaded" } }, 409);
      const owned = found.item.external?.provider_owned_fields ?? [];
      if (owned.some((field) => field in (body ?? {}))) return json({ error: { code: "provider_owned", message: "field is authoritative in the external provider" } }, 409);
      if (body?.["blocked"] === true && !text(body?.["blocker"], "").trim()) {
        return json({ error: { code: "validation_failed", message: "request contains invalid fields", fields: { blocker: ["must be present when blocked"] } } }, 422);
      }
      Object.assign(found.item, Object.fromEntries(Object.entries(body ?? {}).filter(([key]) => key !== "version")));
      found.item.version = (found.item.version ?? 0) + 1;
      return json({ work_item: { id: found.item.id } });
    }
    return json({ error: { code: "not_found", message: "not found" } }, 404);
  });
  return harness;
}

const applications: Application[] = [];
afterEach(() => { for (const application of applications) application.queryClient.clear(); applications.length = 0; localStorage.clear(); resetPendingWorkItemCreations(); });

function renderWorkspace(path: string, harness: Harness) {
  const application = createApplication({ fetcher: harness.fetcher, location: { href: `http://localhost${path}`, replace: vi.fn() } });
  applications.push(application);
  const history = createMemoryHistory({ initialEntries: [path] });
  const router = createAppRouter(application, history);
  render(<QueryClientProvider client={application.queryClient}><RouterProvider router={router} /></QueryClientProvider>);
  return { application, history };
}

const postsOf = (harness: Harness, path: string) => harness.requests.filter((request) => request.method === "POST" && request.path === path);
const patchesOf = (harness: Harness, path: string) => harness.requests.filter((request) => request.method === "PATCH" && request.path === path);

function formStateOf(item: WorkItem): ReturnType<typeof workItemFormStateOf> {
  const project: Project = { id: firstProjectId, key: "ONE", name: "Project One", description: null, status: "active", work_items: [] };
  return workItemFormStateOf(project, item);
}

describe("planning write policy", () => {
  it("maps the served provider-owned fields onto the editor controls", () => {
    expect([...providerOwnedControlsOf({ external: { provider_owned_fields: ["title", "state", "assignee", "labels"] } })]).toEqual(["title", "status", "assignee_type", "assignee_name"]);
    expect(providerOwnedControlsOf({ external: null }).size).toBe(0);
    expect(providerOwnedControlsOf({}).size).toBe(0);
  });

  it("builds board move intents only where a neighbour exists", () => {
    const columns: BoardColumnSnapshot[] = [
      { state: "backlog", items: [workItem({ id: "a", title: "A" }), workItem({ id: "b", title: "B" })] },
      { state: "ready", items: [workItem({ id: "c", title: "C" })] },
    ];
    const label = (state: string) => state.toUpperCase();
    const first = columns[0]?.items[0];
    const second = columns[0]?.items[1];
    if (first === undefined || second === undefined) throw new Error("fixture missing");
    expect(boardMoveRequest({ direction: "left", item: first, columns, stateLabel: label, version: 4 })).toBeNull();
    // An empty rendered column is a destination too: the move targets the
    // column, not a card that happens to sit in it.
    expect(boardMoveRequest({ direction: "right", item: first, columns: [{ state: "backlog", items: [first] }, { state: "ready", items: [] }], stateLabel: label, version: 4 })).toEqual({
      intent: { version: 4, status: "ready", beforeId: null },
      announcement: "Moved to READY",
    });
    expect(boardMoveRequest({ direction: "right", item: first, columns, stateLabel: label, version: 4 })).toEqual({
      intent: { version: 4, status: "ready", beforeId: null },
      announcement: "Moved to READY",
    });
    expect(boardMoveRequest({ direction: "up", item: second, columns, stateLabel: label, version: 4 })).toEqual({
      intent: { version: 4, status: "backlog", beforeId: "a" },
      announcement: "Moved up",
    });
    expect(boardMoveRequest({ direction: "up", item: first, columns, stateLabel: label, version: 4 })).toBeNull();
    expect(boardMoveRequest({ direction: "down", item: first, columns, stateLabel: label, version: 4 })).toEqual({
      intent: { version: 4, status: "backlog", beforeId: null },
      announcement: "Moved down",
    });
  });

  it("keeps status out of the update draft and provider-owned delivery values unset", () => {
    const form = {
      ...formStateOf(workItem({
        id: "i",
        title: "Title",
        status: "review",
        delivery: {
          pull_request: { value: "https://example.invalid/pr/1", source: "provider", generation: null, url: "https://example.invalid/pr/1", status: null },
          ci: { value: "passed", source: "agent", generation: 1, url: null, status: "passed" },
          review: { value: "approved", source: "provider", generation: null, url: null, status: "approved" },
        },
      })),
      branch: "feat/x",
      blocked: true,
      blocker: "",
      ci_status: "passed",
      review_status: "approved",
      pull_request_url: "https://example.invalid/pr/1",
    };
    const draft = workItemDraftOf(form, { includeStatus: false, deliveryEditable: { pullRequest: false, ci: false, review: false } });
    expect(draft).not.toHaveProperty("status");
    expect(draft).not.toHaveProperty("ci_status");
    expect(draft).not.toHaveProperty("review_status");
    expect(draft).not.toHaveProperty("pull_request_url");
    expect(draft).toMatchObject({ title: "Title", branch: "feat/x", blocked: true });
    expect(failureMode(new Error("x"))).toBe("authoritative");
  });

  it("does not seed delivery controls from the projection's unrecorded fallback", () => {
    // The served source is "none": the unrecorded projection serves "unknown"
    // for CI and "none" for review as values, and an untouched control must
    // stay empty instead of recording what the operator never entered.
    const form = formStateOf(workItem({ id: "i", title: "Title" }));
    expect(form.ci_status).toBe("");
    expect(form.review_status).toBe("");
    expect(form.pull_request_url).toBe("");
  });

  it("clears an empty editable delivery control and leaves an uneditable one alone", () => {
    const form = formStateOf(workItem({ id: "i", title: "Title" }));
    const editable = { pullRequest: true, ci: true, review: true };
    expect(workItemUpdatePayload(form, 2, editable)).toMatchObject({ pull_request_url: null, ci_status: null, review_status: null });
    const evidenceOwned = { pullRequest: false, ci: false, review: false };
    const payload = workItemUpdatePayload(form, 2, evidenceOwned);
    expect(payload).not.toHaveProperty("pull_request_url");
    expect(payload).not.toHaveProperty("ci_status");
    expect(payload).not.toHaveProperty("review_status");
  });

  it("keeps a described server fault retryable and states what an invalid request does", () => {
    // A 5xx is no authoritative answer even when the server's own envelope makes
    // it parseable: the write may have been applied, so it stays retryable.
    expect(failureMode(new ServerRefusal({ status: 500, code: "internal_error", message: "internal server error", fields: null }))).toBe("retryable");
    expect(failureMode(new ServerRefusal({ status: 409, code: "stale", message: "resource changed since it was loaded", fields: null }))).toBe("authoritative");
    // `invalid_request` closes the editor as authoritative, so the guidance
    // states what happens instead of promising a correction this surface cannot take.
    expect(refusalNotice(new ServerRefusal({ status: 400, code: "invalid_request", message: "request is invalid", fields: null })))
      .toBe("request is invalid (invalid_request) — This request cannot be corrected here; the editor closes and the server's current state is shown.");
  });
});

describe("project planning", () => {
  it("submits a new project once while pending and lands on the server's project", async () => {
    const user = userEvent.setup();
    const harness = createHarness();
    renderWorkspace("/portal/projects", harness);
    await user.click(await screen.findByRole("button", { name: "New project" }));
    await user.type(screen.getByLabelText("Name"), "Third project");
    await user.type(screen.getByLabelText("Key"), "THR");
    const create = screen.getByRole("button", { name: "Create" });
    fireEvent.click(create);
    fireEvent.click(create);
    await screen.findByText("Project created.");
    expect(postsOf(harness, "/portal/api/projects")).toHaveLength(1);
    expect(await screen.findByRole("heading", { level: 2, name: "Third project" })).toBeVisible();
  });

  it("keeps the editor open with the server's field error and submits after correction", async () => {
    const user = userEvent.setup();
    const harness = createHarness();
    renderWorkspace("/portal/projects", harness);
    await user.click(await screen.findByRole("button", { name: "New project" }));
    await user.type(screen.getByLabelText("Name"), "Duplicate");
    await user.type(screen.getByLabelText("Key"), "ONE");
    await user.click(screen.getByRole("button", { name: "Create" }));
    expect(await screen.findByText("has already been taken")).toBeVisible();
    expect(screen.getByRole("dialog")).toBeVisible();
    await user.clear(screen.getByLabelText("Key"));
    await user.type(screen.getByLabelText("Key"), "DUP");
    await user.click(screen.getByRole("button", { name: "Create" }));
    await screen.findByText("Project created.");
    expect(postsOf(harness, "/portal/api/projects")).toHaveLength(2);
  });

  it("replays the failed intent verbatim on an explicit retry", async () => {
    const user = userEvent.setup();
    const harness = createHarness();
    renderWorkspace(`/portal/projects/${firstProjectId}`, harness);
    await user.click(await screen.findByRole("button", { name: "Project settings" }));
    const name = screen.getByLabelText("Name");
    await user.clear(name);
    await user.type(name, "Renamed once");
    harness.failNextMutation.current = true;
    await user.click(screen.getByRole("button", { name: "Save" }));
    const tryAgain = await screen.findByRole("button", { name: "Try again" });
    await user.click(tryAgain);
    await screen.findByText("Project updated.");
    const attempts = patchesOf(harness, `/portal/api/projects/${firstProjectId}`);
    // The failed attempt never reached the double's handler but was still sent;
    // the retry must carry exactly the same bytes, including the same version.
    expect(attempts).toHaveLength(2);
    expect(attempts[0]?.body).toEqual(attempts[1]?.body);
    expect(attempts[1]?.body).toMatchObject({ version: 3, name: "Renamed once" });
    expect(projectOf(harness.state, firstProjectId)?.name).toBe("Renamed once");
  });

  it("refuses a retried intent whose row advanced instead of rebasing it", async () => {
    const user = userEvent.setup();
    const harness = createHarness();
    renderWorkspace(`/portal/projects/${firstProjectId}`, harness);
    await user.click(await screen.findByRole("button", { name: "Project settings" }));
    const name = screen.getByLabelText("Name");
    await user.clear(name);
    await user.type(name, "Renamed once");
    harness.failNextMutation.current = true;
    await user.click(screen.getByRole("button", { name: "Save" }));
    const tryAgain = await screen.findByRole("button", { name: "Try again" });
    // The row advanced elsewhere while the operator was looking at the error.
    const project = projectOf(harness.state, firstProjectId);
    if (project === undefined) throw new Error("project missing");
    project.version += 1;
    project.name = "Renamed elsewhere";
    await user.click(tryAgain);
    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("resource changed since it was loaded (stale)");
    expect(screen.queryByRole("dialog", { name: "Project settings" })).toBeNull();
    expect(projectOf(harness.state, firstProjectId)?.name).toBe("Renamed elsewhere");
  });

  it("surfaces a stale refusal, closes the editor and re-reads the server's current row", async () => {
    const user = userEvent.setup();
    const harness = createHarness();
    renderWorkspace(`/portal/projects/${firstProjectId}`, harness);
    await user.click(await screen.findByRole("button", { name: "Project settings" }));
    const project = projectOf(harness.state, firstProjectId);
    if (project === undefined) throw new Error("project missing");
    project.name = "Renamed elsewhere";
    project.version += 1;
    await user.clear(screen.getByLabelText("Name"));
    await user.type(screen.getByLabelText("Name"), "Local rename");
    await user.click(screen.getByRole("button", { name: "Save" }));
    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("resource changed since it was loaded (stale)");
    expect(screen.queryByRole("dialog", { name: "Project settings" })).toBeNull();
    expect(await screen.findByRole("heading", { level: 1, name: "Renamed elsewhere" })).toBeVisible();
  });

  it("keeps a draft across a background refresh and submits the revision it was edited from", async () => {
    const user = userEvent.setup();
    const harness = createHarness();
    const { application } = renderWorkspace(`/portal/projects/${firstProjectId}`, harness);
    await user.click(await screen.findByRole("button", { name: "Project settings" }));
    const name = screen.getByLabelText("Name");
    await user.clear(name);
    await user.type(name, "Half-typed");
    const project = projectOf(harness.state, firstProjectId);
    if (project === undefined) throw new Error("project missing");
    // The row advances elsewhere while the editor is open: the refresh serves
    // the new revision, and the frozen intent must still target the revision
    // the editor was opened from.
    project.version += 1;
    project.name = "Renamed elsewhere";
    await act(async () => { await application.queryClient.invalidateQueries({ queryKey: ["workspace"] }); });
    expect(screen.getByLabelText("Name")).toHaveValue("Half-typed");
    expect(screen.getByRole("dialog")).toBeVisible();
    await user.click(screen.getByRole("button", { name: "Save" }));
    // The submission carries the opened-at revision, not the refreshed one, so
    // the server refuses it as stale instead of rebasing onto state the
    // operator never saw.
    expect(await screen.findByRole("alert")).toHaveTextContent("resource changed since it was loaded (stale)");
    const submitted = patchesOf(harness, `/portal/api/projects/${firstProjectId}`)[0];
    expect(submitted?.body).toMatchObject({ version: 3, name: "Half-typed" });
    expect(submitted?.body).not.toHaveProperty("version", 4);
    expect(projectOf(harness.state, firstProjectId)?.name).toBe("Renamed elsewhere");
  });

  it("clears the optional fields the operator empties on the update path", async () => {
    const user = userEvent.setup();
    const harness = createHarness();
    const served = readWorkDetail({ id: firstItemId, projectId: firstProjectId, title: "First item" });
    const servedItem = served["work_item"] as Record<string, unknown>;
    servedItem["version"] = 1;
    servedItem["description"] = "Recorded description";
    servedItem["branch"] = "feat/recorded";
    // Recorded manual delivery values, so every editable delivery control
    // holds a value the operator can empty.
    servedItem["delivery"] = {
      pull_request: { value: "https://example.invalid/pr/1", source: "manual", generation: null, url: "https://example.invalid/pr/1", status: null },
      ci: { value: "passed", source: "manual", generation: null, url: null, status: "passed" },
      review: { value: "approved", source: "manual", generation: null, url: null, status: "approved" },
    };
    harness.state.detail[firstItemId] = served;
    renderWorkspace(`/portal/work/${firstItemId}`, harness);
    await user.click(await screen.findByRole("button", { name: "Edit work item" }));
    await user.clear(screen.getByLabelText("Description"));
    await user.clear(screen.getByLabelText("Branch"));
    await user.clear(screen.getByLabelText("Pull request URL"));
    await user.selectOptions(screen.getByLabelText("CI"), "");
    await user.selectOptions(screen.getByLabelText("Review"), "");
    await user.click(screen.getByRole("button", { name: "Save" }));
    await screen.findByText("Work item updated.");
    // An emptied control is an explicit clear: Ecto reads "" as "no change",
    // so the payload has to carry null for the value to be removed. The
    // delivery controls clear the same way: a nil change is accepted there too.
    const update = patchesOf(harness, `/portal/api/work-items/${firstItemId}`)[0];
    expect(update?.body).toMatchObject({ version: 1, description: null, branch: null, pull_request_url: null, ci_status: null, review_status: null });
    const item = itemOf(harness.state, firstItemId)?.item;
    expect(item?.description).toBeNull();
    expect(item?.pull_request_url).toBeNull();
    expect(item?.ci_status).toBeNull();
    expect(item?.review_status).toBeNull();
  });

  it("restores an archived project through the one write it accepts", async () => {
    const user = userEvent.setup();
    const harness = createHarness();
    renderWorkspace(`/portal/projects/${secondProjectId}`, harness);
    expect(await screen.findByText(/This project is archived/)).toBeVisible();
    expect(screen.queryByRole("button", { name: "New work item" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Project settings" })).toBeNull();
    await user.click(screen.getByRole("button", { name: "Restore project" }));
    await screen.findByText("Project restored.");
    expect(patchesOf(harness, `/portal/api/projects/${secondProjectId}`)[0]?.body).toEqual({ version: 2, status: "active" });
    expect(projectOf(harness.state, secondProjectId)?.status).toBe("active");
  });

  it("does not apply a late board move to the project the operator switched to", async () => {
    const harness = createHarness();
    const history = createMemoryHistory({ initialEntries: [`/portal/projects/${firstProjectId}?view=board`] });
    const application = createApplication({ fetcher: harness.fetcher, location: { href: "http://localhost/portal/projects", replace: vi.fn() } });
    applications.push(application);
    const router = createAppRouter(application, history);
    render(<QueryClientProvider client={application.queryClient}><RouterProvider router={router} /></QueryClientProvider>);
    const card = await screen.findByRole("link", { name: "Legacy item" });
    const release = harness.holdNextMutation();
    card.focus();
    fireEvent.keyDown(card, { key: "ArrowRight", shiftKey: true });
    await waitFor(() => expect(patchesOf(harness, `/portal/api/work-items/${firstItemId}/move`)).toHaveLength(1));
    // A move is not modal, so the operator can leave while it is in flight. The
    // switch keeps the board view, so project two's board — the surface the
    // late move's announcement would have to land on — is mounted and is what
    // the negatives below observe.
    await act(async () => { await router.navigate({ to: "/projects/$projectId", params: { projectId: secondProjectId }, search: { view: "board" } }); });
    await screen.findByRole("heading", { level: 1, name: "Project Two" });
    const board = await screen.findByRole("region", { name: "Work board" });
    expect(within(board).getByRole("link", { name: "Archived item" })).toBeVisible();
    release();
    await new Promise((resolve) => setTimeout(resolve, 20));
    expect(screen.getByRole("heading", { level: 1, name: "Project Two" })).toBeVisible();
    expect(screen.getByText(/This project is archived\. Its work stays visible here/)).toBeVisible();
    expect(screen.getByRole("region", { name: "Work board" })).toBeVisible();
    expect(screen.queryByRole("alert")).toBeNull();
    expect(screen.queryByText("Moved to ready")).toBeNull();
    expect(itemOf(harness.state, firstItemId)?.item.status).toBe("ready");
  });
});

describe("work item planning", () => {
  it("creates a work item from the project toolbar with the chosen resources", async () => {
    const user = userEvent.setup();
    const harness = createHarness();
    renderWorkspace(`/portal/projects/${firstProjectId}`, harness);
    await user.click(await screen.findByRole("button", { name: "New work item" }));
    await user.type(screen.getByLabelText("Title"), "Planned item");
    await user.selectOptions(screen.getByLabelText("Status"), "ready");
    await user.selectOptions(screen.getByLabelText("Repository"), repositoryResourceId);
    await user.click(screen.getByRole("button", { name: "Create" }));
    await screen.findByText("Work item created.");
    expect(postsOf(harness, `/portal/api/projects/${firstProjectId}/work-items`)[0]?.body)
      .toMatchObject({ title: "Planned item", status: "ready", repository_resource_id: repositoryResourceId });
    expect(await screen.findByRole("link", { name: "Planned item" })).toBeVisible();
  });

  it("renders the server's field error for a whitespace title and keeps the draft", async () => {
    const user = userEvent.setup();
    const harness = createHarness();
    renderWorkspace(`/portal/projects/${firstProjectId}`, harness);
    await user.click(await screen.findByRole("button", { name: "New work item" }));
    await user.type(screen.getByLabelText("Title"), " ");
    await user.click(screen.getByRole("button", { name: "Create" }));
    expect(await screen.findByText("can't be blank")).toBeVisible();
    expect(screen.getByLabelText("Title")).toHaveValue(" ");
    expect(within(screen.getByRole("dialog")).getByText(/validation_failed/)).toBeVisible();
  });

  it("mints one creation identity per draft and replays it verbatim on an explicit retry", async () => {
    const user = userEvent.setup();
    const harness = createHarness();
    renderWorkspace(`/portal/projects/${firstProjectId}`, harness);
    await user.click(await screen.findByRole("button", { name: "New work item" }));
    await user.type(screen.getByLabelText("Title"), "Identity item");
    // The first attempt never reaches the double: the operator retries the
    // frozen intent, and both attempts must carry the same identity.
    harness.failNextMutation.current = true;
    await user.click(screen.getByRole("button", { name: "Create" }));
    await user.click(await screen.findByRole("button", { name: "Try again" }));
    await screen.findByText("Work item created.");
    const posts = postsOf(harness, `/portal/api/projects/${firstProjectId}/work-items`);
    expect(posts).toHaveLength(2);
    const firstIdentity = posts[0]?.body?.["creation_id"];
    expect(typeof firstIdentity).toBe("string");
    expect(firstIdentity).not.toBe("");
    expect(posts[1]?.body?.["creation_id"]).toBe(firstIdentity);
    expect(projectOf(harness.state, firstProjectId)?.work_items.filter((item) => item.title === "Identity item")).toHaveLength(1);
  });

  it("answers a committed create whose response was lost with the original row's notice", async () => {
    const user = userEvent.setup();
    const harness = createHarness();
    renderWorkspace(`/portal/projects/${firstProjectId}`, harness);
    await user.click(await screen.findByRole("button", { name: "New work item" }));
    await user.type(screen.getByLabelText("Title"), "Lost answer");
    // The double commits the row and then destroys the response: the retry
    // replays the identity and the server's own disposition says so.
    harness.loseNextCreateResponse.current = true;
    await user.click(screen.getByRole("button", { name: "Create" }));
    await user.click(await screen.findByRole("button", { name: "Try again" }));
    await screen.findByText("Work item already existed; the server returned the original row, so this submission was not applied.");
    expect(projectOf(harness.state, firstProjectId)?.work_items.filter((item) => item.title === "Lost answer")).toHaveLength(1);
    const posts = postsOf(harness, `/portal/api/projects/${firstProjectId}/work-items`);
    expect(posts).toHaveLength(2);
    expect(posts[1]?.body?.["creation_id"]).toBe(posts[0]?.body?.["creation_id"]);
  });

  it("keeps the pending creation identity when the operator reopens the draft", async () => {
    const user = userEvent.setup();
    const harness = createHarness();
    renderWorkspace(`/portal/projects/${firstProjectId}`, harness);
    await user.click(await screen.findByRole("button", { name: "New work item" }));
    await user.type(screen.getByLabelText("Title"), "Reopened draft");
    harness.loseNextCreateResponse.current = true;
    await user.click(screen.getByRole("button", { name: "Create" }));
    await screen.findByText(/Cannot reach the server/);
    // The operator closes the failed draft and starts it again: an identity
    // that has not been acknowledged must survive the dialog, or the reopened
    // submission would become the duplicate the retry is meant to avoid.
    await user.click(screen.getByRole("button", { name: "Cancel" }));
    await user.click(screen.getByRole("button", { name: "New work item" }));
    await user.type(screen.getByLabelText("Title"), "Reopened draft");
    await user.click(screen.getByRole("button", { name: "Create" }));
    await screen.findByText("Work item already existed; the server returned the original row, so this submission was not applied.");
    const posts = postsOf(harness, `/portal/api/projects/${firstProjectId}/work-items`);
    expect(posts).toHaveLength(2);
    expect(posts[1]?.body?.["creation_id"]).toBe(posts[0]?.body?.["creation_id"]);
    expect(projectOf(harness.state, firstProjectId)?.work_items.filter((item) => item.title === "Reopened draft")).toHaveLength(1);
  });

  it("refuses a create visibly when this browser cannot mint an item identity", async () => {
    const user = userEvent.setup();
    const harness = createHarness();
    renderWorkspace(`/portal/projects/${firstProjectId}`, harness);
    await user.click(await screen.findByRole("button", { name: "New work item" }));
    await user.type(screen.getByLabelText("Title"), "Unidentified item");
    // A non-secure origin without Web Crypto: the create has no idempotency
    // identity, and one sent without could be duplicated by a retry.
    vi.stubGlobal("crypto", {});
    try {
      await user.click(screen.getByRole("button", { name: "Create" }));
      expect(await screen.findByRole("alert")).toHaveTextContent("This browser cannot mint an item identity, so the request was not sent.");
      expect(screen.getByLabelText("Title")).toHaveValue("Unidentified item");
      expect(postsOf(harness, `/portal/api/projects/${firstProjectId}/work-items`)).toHaveLength(0);
      // With an identity available again the same press sends and clears the refusal.
      vi.unstubAllGlobals();
      await user.click(screen.getByRole("button", { name: "Create" }));
      await screen.findByText("Work item created.");
      expect(screen.queryByRole("alert")).toBeNull();
      expect(postsOf(harness, `/portal/api/projects/${firstProjectId}/work-items`)).toHaveLength(1);
    } finally {
      vi.unstubAllGlobals();
    }
  });

  it("moves the focused card between columns, keeps focus on it and re-reads the new order", async () => {
    const harness = createHarness();
    renderWorkspace(`/portal/projects/${firstProjectId}?view=board`, harness);
    const card = await screen.findByRole("link", { name: "Legacy item" });
    card.focus();
    fireEvent.keyDown(card, { key: "ArrowRight", shiftKey: true });
    await waitFor(() => expect(patchesOf(harness, `/portal/api/work-items/${firstItemId}/move`)).toHaveLength(1));
    expect(patchesOf(harness, `/portal/api/work-items/${firstItemId}/move`)[0]?.body).toEqual({ version: 1, status: "ready", before_id: null });
    const moved = screen.getByRole("link", { name: "Legacy item" });
    await waitFor(() => expect(document.activeElement).toBe(moved));
    const readyColumn = screen.getByRole("heading", { name: /^ready$/i }).closest(".board-column");
    if (readyColumn === null) throw new Error("ready column missing");
    expect(within(readyColumn as HTMLElement).getByRole("link", { name: "Legacy item" })).toBeVisible();
  });

  it("offers a Goal-managed card no move path and issues no request", async () => {
    const harness = createHarness();
    renderWorkspace(`/portal/projects/${firstProjectId}?view=board`, harness);
    const card = await screen.findByRole("link", { name: "Goal item" });
    expect(within(card).getByText("Goal-managed")).toBeVisible();
    card.focus();
    fireEvent.keyDown(card, { key: "ArrowRight", shiftKey: true });
    expect(await screen.findByText(/Goal-managed work has no board movement/)).toBeVisible();
    expect(harness.requests.filter((request) => request.method === "PATCH")).toHaveLength(0);
    expect(screen.getByRole("link", { name: "Goal item" })).toBeVisible();
  });

  it("keeps the provider-owned fields read-only in the editor and sends only the editable ones", async () => {
    const user = userEvent.setup();
    const harness = createHarness();
    const served = readWorkDetail({ id: providerItemId, projectId: firstProjectId, title: "Provider item" });
    const servedItem = served["work_item"] as Record<string, unknown>;
    servedItem["version"] = 1;
    // The write-side row carries the tracker binding the read serves; the editor
    // must present those fields read-only from the served list.
    const found = itemOf(harness.state, providerItemId);
    if (found === null) throw new Error("item missing");
    servedItem["external"] = found.item.external;
    harness.state.detail[providerItemId] = served;
    renderWorkspace(`/portal/work/${providerItemId}`, harness);
    await user.click(await screen.findByRole("button", { name: "Edit work item" }));
    expect(screen.getByLabelText("Title")).toBeDisabled();
    expect(screen.getByLabelText("Description")).toBeDisabled();
    expect(screen.getByLabelText("Priority")).toBeDisabled();
    expect(screen.getByLabelText("Owner type")).toBeDisabled();
    expect(screen.getByText(/edit them in the tracker/)).toBeVisible();
    await user.clear(screen.getByLabelText("Branch"));
    await user.type(screen.getByLabelText("Branch"), "feat/provider");
    await user.click(screen.getByRole("button", { name: "Save" }));
    await screen.findByText("Work item updated.");
    const update = patchesOf(harness, `/portal/api/work-items/${providerItemId}`)[0];
    expect(update?.body).toMatchObject({ version: 1, branch: "feat/provider" });
    expect(update?.body).not.toHaveProperty("title");
    expect(update?.body).not.toHaveProperty("priority");
    expect(update?.body).not.toHaveProperty("status");
  });

  it("names the fields Control discards for a human owner", async () => {
    const user = userEvent.setup();
    const harness = createHarness();
    const served = readWorkDetail({ id: firstItemId, projectId: firstProjectId, title: "First item" });
    const servedItem = served["work_item"] as Record<string, unknown>;
    servedItem["version"] = 1;
    harness.state.detail[firstItemId] = served;
    renderWorkspace(`/portal/work/${firstItemId}`, harness);
    await user.click(await screen.findByRole("button", { name: "Edit work item" }));
    // The draft carries the recorded agent profile and the project workspace;
    // Control records neither for a human owner, and the note must say so.
    await user.selectOptions(screen.getByLabelText("Owner type"), "human");
    await user.click(screen.getByRole("button", { name: "Save" }));
    const notice = await screen.findByText(/Work item updated\./);
    expect(notice).toHaveTextContent("Control records no agent profile, workspace for a human owner, so those values were not applied.");
  });

  it("names the fields Control discards for an unassigned item", async () => {
    const user = userEvent.setup();
    const harness = createHarness();
    const served = readWorkDetail({ id: firstItemId, projectId: firstProjectId, title: "First item" });
    const servedItem = served["work_item"] as Record<string, unknown>;
    servedItem["version"] = 1;
    // A recorded owner name: every field the unassigned rewrite discards is
    // carried by the payload and named in the fixed owner-name-first order.
    servedItem["assignee"] = { type: "agent", name: "Ada", agent_profile: "default" };
    harness.state.detail[firstItemId] = served;
    renderWorkspace(`/portal/work/${firstItemId}`, harness);
    await user.click(await screen.findByRole("button", { name: "Edit work item" }));
    await user.selectOptions(screen.getByLabelText("Owner type"), "unassigned");
    await user.click(screen.getByRole("button", { name: "Save" }));
    const notice = await screen.findByText(/Work item updated\./);
    expect(notice).toHaveTextContent("Control records no owner name, agent profile, workspace for an unassigned item, so those values were not applied.");
  });

  it("names the fields a mid-edit ownership change kept out of the submission", async () => {
    const user = userEvent.setup();
    const harness = createHarness();
    const served = readWorkDetail({ id: firstItemId, projectId: firstProjectId, title: "First item" });
    const servedItem = served["work_item"] as Record<string, unknown>;
    servedItem["version"] = 1;
    harness.state.detail[firstItemId] = served;
    const { application } = renderWorkspace(`/portal/work/${firstItemId}`, harness);
    await user.click(await screen.findByRole("button", { name: "Edit work item" }));
    await user.clear(screen.getByLabelText("Title"));
    await user.type(screen.getByLabelText("Title"), "Typed before the tracker owned it");
    // The tracker claims the title while the form is open: the field becomes
    // read-only, and the submission may no longer carry the typed value.
    const trackerExternal = itemOf(harness.state, providerItemId)?.item.external;
    if (trackerExternal === undefined) throw new Error("tracker fixture missing");
    servedItem["external"] = { ...trackerExternal, provider_owned_fields: ["title"] };
    await act(async () => { await application.queryClient.refetchQueries({ queryKey: ["work-item", { workItemId: firstItemId }] }); });
    await vi.waitFor(() => expect(screen.getByLabelText("Title")).toBeDisabled());
    await user.click(screen.getByRole("button", { name: "Save" }));
    const notice = await screen.findByText(/Work item updated\./);
    expect(notice).toHaveTextContent(/title/);
    const update = patchesOf(harness, `/portal/api/work-items/${firstItemId}`)[0];
    expect(update?.body).not.toHaveProperty("title");
  });

  it("names a delivery value the server claims while the form is open", async () => {
    const user = userEvent.setup();
    const harness = createHarness();
    const served = readWorkDetail({ id: firstItemId, projectId: firstProjectId, title: "First item" });
    const servedItem = served["work_item"] as Record<string, unknown>;
    servedItem["version"] = 1;
    harness.state.detail[firstItemId] = served;
    const { application } = renderWorkspace(`/portal/work/${firstItemId}`, harness);
    await user.click(await screen.findByRole("button", { name: "Edit work item" }));
    await user.selectOptions(screen.getByLabelText("CI"), "passed");
    // Reported execution claims the CI status while the form is open.
    const servedDelivery = servedItem["delivery"] as { ci: { source: string; value: string | null } };
    servedDelivery.ci.source = "agent";
    servedDelivery.ci.value = "passed";
    await act(async () => { await application.queryClient.refetchQueries({ queryKey: ["work-item", { workItemId: firstItemId }] }); });
    await vi.waitFor(() => expect(screen.queryByRole("combobox", { name: "CI" })).toBeNull());
    await user.click(screen.getByRole("button", { name: "Save" }));
    const notice = await screen.findByText(/Work item updated\./);
    expect(notice).toHaveTextContent(/CI status/);
    const update = patchesOf(harness, `/portal/api/work-items/${firstItemId}`)[0];
    expect(update?.body).not.toHaveProperty("ci_status");
  });

  it("closes an editor whose row stopped serving a revision, and says so", async () => {
    const user = userEvent.setup();
    const harness = createHarness();
    const served = readWorkDetail({ id: firstItemId, projectId: firstProjectId, title: "First item" });
    const servedItem = served["work_item"] as Record<string, unknown>;
    servedItem["version"] = 1;
    harness.state.detail[firstItemId] = served;
    const { application } = renderWorkspace(`/portal/work/${firstItemId}`, harness);
    await user.click(await screen.findByRole("button", { name: "Edit work item" }));
    await user.type(screen.getByLabelText("Branch"), "feat/draft");
    delete servedItem["version"];
    await act(async () => { await application.queryClient.invalidateQueries({ queryKey: ["work-item", { workItemId: firstItemId }] }); });
    expect(await screen.findByRole("alert")).toHaveTextContent(/revision is no longer served/);
    expect(screen.queryByRole("dialog", { name: "Edit work item" })).toBeNull();
    expect(harness.requests.filter((request) => request.method === "PATCH")).toHaveLength(0);
  });

  it("surfaces the server's Goal refusal when the loaded row predates the admission, then re-reads", async () => {
    const user = userEvent.setup();
    const harness = createHarness();
    // The detail read the operator holds was served before the admission and
    // carries no marker; the server's row is Goal-managed, so the legacy write
    // is refused with the authoritative 403 and the surface re-reads.
    const served = readWorkDetail({ id: firstItemId, projectId: firstProjectId, title: "Legacy item" });
    const servedItem = served["work_item"] as Record<string, unknown>;
    servedItem["version"] = 1;
    harness.state.detail[firstItemId] = served;
    const found = itemOf(harness.state, firstItemId);
    if (found === null) throw new Error("item missing");
    found.item.goal_managed = true;
    renderWorkspace(`/portal/work/${firstItemId}`, harness);
    await user.click(await screen.findByRole("button", { name: "Edit work item" }));
    await user.clear(screen.getByLabelText("Title"));
    await user.type(screen.getByLabelText("Title"), "Local rewrite");
    const readsBeforeSave = harness.requests.filter((request) => request.method === "GET" && request.path === "/portal/api/workspace").length;
    await user.click(screen.getByRole("button", { name: "Save" }));
    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("Goal authority is required for this operation (goal_authority_required)");
    expect(alert).toHaveTextContent("Goal-managed work is changed through Goal admission");
    expect(screen.queryByRole("dialog", { name: "Edit work item" })).toBeNull();
    expect(found.item.title).toBe("Legacy item");
    // The authoritative refusal re-reads the server's workspace state instead
    // of leaving the surface on the snapshot the refused write was built from.
    await waitFor(() => expect(harness.requests.filter((request) => request.method === "GET" && request.path === "/portal/api/workspace")).toHaveLength(readsBeforeSave + 1));
  });
});
