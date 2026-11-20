import { describe, expect, it } from "vitest";
import type { WorkItem } from "../../services/api";
import { boardColumns, filterWorkItems, filtersOf, ownerOptions, sameWorkSearch, stateOptions, summarizeWork, workItemSearch, workSearch, workSummaryText, type WorkSearch } from "./filters";

const firstProjectWork: WorkItem = { id: "11111111-1111-4111-8111-111111111111", key: "ONE-1", title: "Ship the read surface", status: "ready", blocked: false, blocker: null, assignee: { type: "agent", name: null, agent_profile: "default" }, updated_at: null };
const secondProjectWork: WorkItem = { id: "22222222-2222-4222-8222-222222222222", key: "ONE-2", title: "Review the migration", status: "in_progress", blocked: true, blocker: "Waiting on review", assignee: { type: "human", name: "Ada", agent_profile: null }, updated_at: null };
const unknownStateWork: WorkItem = { id: "33333333-3333-4333-8333-333333333333", key: "ONE-3", title: "Imported item", status: "future_state", blocked: false, blocker: null, assignee: { type: "unassigned", name: null, agent_profile: null }, updated_at: null };
const unnamedAgentWork: WorkItem = { id: "44444444-4444-4444-8444-444444444444", key: "ONE-4", title: "Unnamed agent row", status: "ready", blocked: false, blocker: null, assignee: { type: "agent", name: null, agent_profile: null }, updated_at: null };
const unnamedHumanWork: WorkItem = { id: "55555555-5555-4555-8555-555555555555", key: "ONE-5", title: "Unnamed human row", status: "ready", blocked: false, blocker: null, assignee: { type: "human", name: null, agent_profile: null }, updated_at: null };
const recordedSentinelNameWork: WorkItem = { id: "66666666-6666-4666-8666-666666666666", key: "ONE-6", title: "A row named unrecorded", status: "ready", blocked: false, blocker: null, assignee: { type: "agent", name: "unrecorded", agent_profile: null }, updated_at: null };
const work = [firstProjectWork, secondProjectWork, unknownStateWork];
const sentinelWork = [unnamedAgentWork, unnamedHumanWork, recordedSentinelNameWork];

describe("work URL state", () => {
  it("normalises the values the router hands back after JSON parsing", () => {
    expect(workSearch({ view: "board", q: 123, blocked: 1 })).toEqual({ view: "board", q: "123", blocked: "1" });
    expect(workSearch({ blocked: true })).toEqual({ blocked: "1" });
    expect(workSearch({ owner: "human:Ada", state: "in_progress" })).toEqual({ owner: "human:Ada", state: "in_progress" });
  });

  it("drops empty and unrecognised parameters instead of rendering a silent filter", () => {
    expect(workSearch({ view: "grid", q: "   ", state: "", owner: null, blocked: "0" })).toEqual({});
    expect(workSearch({ q: "123" })).toEqual({ q: "123" });
    // The work item route validates the same filter plus the project the URL
    // claims: an empty claim is dropped, a real one is kept as written.
    expect(workItemSearch({ view: "grid", q: "   ", state: "", owner: null, blocked: "0", projectId: "" })).toEqual({});
    expect(workItemSearch({ q: "123", projectId: "ONE" })).toEqual({ q: "123", projectId: "ONE" });
    expect(workItemSearch({ projectId: 42 })).toEqual({ projectId: "42" });
  });

  it("renders JSON-shaped query text from a foreign URL as the literal it encodes", () => {
    expect(workSearch({ q: null })).toEqual({ q: "null" });
    expect(workSearch({ q: { a: 1 } })).toEqual({ q: '{"a":1}' });
    expect(workSearch({ q: [1, 2] })).toEqual({ q: "[1,2]" });
    // Text typed by the reader stays a plain string and is never re-encoded.
    expect(workSearch({ q: '{"a":1}' })).toEqual({ q: '{"a":1}' });
  });

  it("treats an absent view as the list and compares the exact list state", () => {
    expect(sameWorkSearch({}, { view: "list" })).toBe(true);
    expect(sameWorkSearch({ q: "a" }, { q: "b" })).toBe(false);
    // Each clause the saved-view select compares must discriminate on its own:
    // a search is the same only when view, q, state, owner and blocked match.
    const active: WorkSearch = { view: "board", q: "read", state: "ready", owner: "agent:default", blocked: "1" };
    expect(sameWorkSearch(active, { ...active })).toBe(true);
    expect(sameWorkSearch(active, { ...active, view: "list" })).toBe(false);
    expect(sameWorkSearch(active, { ...active, q: "blocked" })).toBe(false);
    expect(sameWorkSearch(active, { ...active, state: "in_progress" })).toBe(false);
    expect(sameWorkSearch(active, { ...active, owner: "unassigned" })).toBe(false);
    // The last comparison differs only in blocked.
    expect(sameWorkSearch(active, { view: "board", q: "read", state: "ready", owner: "agent:default" })).toBe(false);
  });
});

describe("work filtering and projections", () => {
  it("matches title, key and owner text, state, owner key and blocked flag", () => {
    expect(filterWorkItems(work, filtersOf(workSearch({ q: "migration" }))).map((item) => item.key)).toEqual(["ONE-2"]);
    expect(filterWorkItems(work, filtersOf(workSearch({ q: "ada" }))).map((item) => item.key)).toEqual(["ONE-2"]);
    expect(filterWorkItems(work, filtersOf(workSearch({ state: "future_state" }))).map((item) => item.key)).toEqual(["ONE-3"]);
    expect(filterWorkItems(work, filtersOf(workSearch({ owner: "unassigned" }))).map((item) => item.key)).toEqual(["ONE-3"]);
    expect(filterWorkItems(work, filtersOf(workSearch({ blocked: "1" }))).map((item) => item.key)).toEqual(["ONE-2"]);
    expect(filterWorkItems(work, filtersOf(workSearch({ q: "migration", blocked: "1", state: "ready" })))).toEqual([]);
  });

  it("renders every supported board column, orders them canonically and keeps unknown states last", () => {
    // Every supported state renders even with no item: the empty column is the
    // movement destination a board whose items all sit in one state lacks.
    expect(boardColumns(work).map((column) => column.state)).toEqual(["backlog", "ready", "in_progress", "review", "done", "future_state"]);
    expect(boardColumns([]).map((column) => column.state)).toEqual(["backlog", "ready", "in_progress", "review", "done"]);
    expect(boardColumns([]).every((column) => column.items.length === 0)).toBe(true);
    expect(boardColumns(work).map((column) => column.items.map((item) => item.key))).toEqual([[], ["ONE-1"], ["ONE-2"], [], [], ["ONE-3"]]);
    expect(stateOptions(work)).toEqual(["ready", "in_progress", "future_state"]);
  });

  it("offers distinct owner options and summarises the served counts", () => {
    expect(ownerOptions(work)).toEqual([
      { value: "human:Ada", label: "Ada" },
      { value: "agent:default", label: "Agent · default" },
      { value: "unassigned", label: "Unassigned" },
    ]);
    expect(summarizeWork(work)).toEqual({ total: 3, blocked: 1 });
    expect(workSummaryText(3, 1)).toBe("3 work items · 1 blocked");
    expect(workSummaryText(1, 0)).toBe("1 work item · 0 blocked");
  });

  it("re-points the legacy sentinel bookmarks to the keys the encoder mints now", () => {
    // Pre-#111 the encoder spelled an absent value `unrecorded`; it now mints
    // the empty-value sentinel, and `ownerKeyLabel` already reads the legacy
    // word as the absence it named. A bookmark carrying it must reach the filter
    // as the same key, or a URL that names a row selects no row at all.
    expect(workSearch({ owner: "agent:unrecorded" })).toEqual({ owner: "agent:" });
    expect(workSearch({ owner: "human:unrecorded" })).toEqual({ owner: "human:" });
    expect(filterWorkItems(sentinelWork, filtersOf(workSearch({ owner: "agent:unrecorded" }))).map((item) => item.key)).toEqual(["ONE-4"]);
    expect(filterWorkItems(sentinelWork, filtersOf(workSearch({ owner: "human:unrecorded" }))).map((item) => item.key)).toEqual(["ONE-5"]);
    // The filter reads its key through the same reader, so a search that never
    // passed the URL parser (a stored or hand-built one) re-points as well.
    expect(filterWorkItems(sentinelWork, filtersOf({ owner: "agent:unrecorded" })).map((item) => item.key)).toEqual(["ONE-4"]);
    expect(filterWorkItems(sentinelWork, filtersOf({ owner: "human:unrecorded" })).map((item) => item.key)).toEqual(["ONE-5"]);
    // The minted keys are fixed points, so the reader stays idempotent at the
    // second call site.
    expect(workSearch({ owner: "agent:" })).toEqual({ owner: "agent:" });
    expect(workSearch({ owner: "human:" })).toEqual({ owner: "human:" });
    // #111's escape is a recorded name, not the legacy word: it keeps matching
    // its own row and never collapses onto the unnamed one.
    expect(filterWorkItems(sentinelWork, filtersOf(workSearch({ owner: "agent::unrecorded" }))).map((item) => item.key)).toEqual(["ONE-6"]);
    // A key that merely resembles the sentinel is not it: an unknown owner type
    // literally named `unrecorded` and a name that starts with the legacy word
    // travel as written through both readers.
    expect(workSearch({ owner: "type:unrecorded" })).toEqual({ owner: "type:unrecorded" });
    expect(filtersOf({ owner: "type:unrecorded" }).owner).toBe("type:unrecorded");
    expect(workSearch({ owner: "agent:unrecorded-bot" })).toEqual({ owner: "agent:unrecorded-bot" });
    expect(filtersOf({ owner: "agent:unrecorded-bot" }).owner).toBe("agent:unrecorded-bot");
    // Every other key travels as written through both readers.
    expect(workSearch({ owner: "agent:builder" })).toEqual({ owner: "agent:builder" });
  });
});
