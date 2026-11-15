import { describe, expect, it } from "vitest";
import type { WorkItem } from "../../services/api";
import { boardColumns, filterWorkItems, filtersOf, ownerOptions, sameWorkSearch, stateOptions, summarizeWork, workSearch, workSummaryText } from "./filters";

const firstProjectWork: WorkItem = { id: "11111111-1111-4111-8111-111111111111", key: "ONE-1", title: "Ship the read surface", status: "ready", blocked: false, blocker: null, assignee: { type: "agent", name: null, agent_profile: "default" }, updated_at: null };
const secondProjectWork: WorkItem = { id: "22222222-2222-4222-8222-222222222222", key: "ONE-2", title: "Review the migration", status: "in_progress", blocked: true, blocker: "Waiting on review", assignee: { type: "human", name: "Ada", agent_profile: null }, updated_at: null };
const unknownStateWork: WorkItem = { id: "33333333-3333-4333-8333-333333333333", key: "ONE-3", title: "Imported item", status: "future_state", blocked: false, blocker: null, assignee: { type: "unassigned", name: null, agent_profile: null }, updated_at: null };
const work = [firstProjectWork, secondProjectWork, unknownStateWork];

describe("work URL state", () => {
  it("normalises the values the router hands back after JSON parsing", () => {
    expect(workSearch({ view: "board", q: 123, blocked: 1 })).toEqual({ view: "board", q: "123", blocked: "1" });
    expect(workSearch({ blocked: true })).toEqual({ blocked: "1" });
    expect(workSearch({ owner: "human:Ada", state: "in_progress" })).toEqual({ owner: "human:Ada", state: "in_progress" });
  });

  it("drops empty and unrecognised parameters instead of rendering a silent filter", () => {
    expect(workSearch({ view: "grid", q: "   ", state: "", owner: null, blocked: "0", projectId: "" })).toEqual({});
    expect(workSearch({ q: "123" })).toEqual({ q: "123" });
  });

  it("treats an absent view as the list and compares the exact list state", () => {
    expect(sameWorkSearch({}, { view: "list" })).toBe(true);
    expect(sameWorkSearch({ q: "a" }, { q: "b" })).toBe(false);
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

  it("orders board columns canonically and keeps unknown states last", () => {
    expect(boardColumns(work).map((column) => column.state)).toEqual(["ready", "in_progress", "future_state"]);
    expect(boardColumns([])).toEqual([]);
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
});
