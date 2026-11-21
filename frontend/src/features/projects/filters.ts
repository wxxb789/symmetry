import type { WorkItem } from "../../services/api";
import { ownerKey, ownerKeyFilter, ownerLabel } from "../work/presentation";

/** View and filter state of the project work surface. The URL owns it: every value here is reloadable. */
export type WorkView = "list" | "board";

export interface WorkSearch {
  view?: WorkView;
  q?: string;
  state?: string;
  owner?: string;
  blocked?: "1";
}

export interface WorkItemSearch extends WorkSearch {
  projectId?: string;
}

// The router's search parser JSON-parses values that look like JSON, so a filter
// such as `q=123` arrives as a number; both forms normalise to the same string.
const textParam = (value: unknown): string | undefined => {
  if (typeof value === "string") return value.trim() === "" ? undefined : value;
  if (typeof value === "number" || typeof value === "boolean") return String(value);
  return undefined;
};

/**
 * The free-text query is whatever text the URL carries. A foreign or older URL
 * can hold JSON-shaped text (`?q=null`, `?q=%7B%22a%22%3A1%7D`) that the
 * parser turns into null or an object; that is still the text the reader asked
 * for, so it is rendered as the literal it encodes instead of being dropped
 * while the URL keeps claiming the filter. Typing cannot produce those values:
 * the router quotes JSON-shaped strings on the way out.
 */
const queryParam = (value: unknown): string | undefined => {
  const text = textParam(value);
  if (text !== undefined) return text;
  if (value === null) return "null";
  if (typeof value === "object") return JSON.stringify(value);
  return undefined;
};

const blockedParam = (value: unknown): "1" | undefined =>
  value === "1" || value === 1 || value === true ? "1" : undefined;

export function workSearch(search: Record<string, unknown>): WorkSearch {
  const view = search.view;
  const q = queryParam(search.q);
  const state = textParam(search.state);
  const ownerParam = textParam(search.owner);
  const owner = ownerParam === undefined ? undefined : ownerKeyFilter(ownerParam);
  const blocked = blockedParam(search.blocked);
  return {
    ...(view === "board" || view === "list" ? { view } : {}),
    ...(q !== undefined ? { q } : {}),
    ...(state !== undefined ? { state } : {}),
    ...(owner !== undefined ? { owner } : {}),
    ...(blocked !== undefined ? { blocked } : {}),
  };
}

export function workItemSearch(search: Record<string, unknown>): WorkItemSearch {
  const projectId = textParam(search.projectId);
  return { ...workSearch(search), ...(projectId !== undefined ? { projectId } : {}) };
}

export const sameWorkSearch = (left: WorkSearch, right: WorkSearch): boolean =>
  (left.view ?? "list") === (right.view ?? "list") &&
  (left.q ?? "") === (right.q ?? "") &&
  (left.state ?? "") === (right.state ?? "") &&
  (left.owner ?? "") === (right.owner ?? "") &&
  (left.blocked ?? "") === (right.blocked ?? "");

interface WorkFilters {
  q: string;
  state: string;
  owner: string;
  blocked: boolean;
}

export function filtersOf(search: WorkSearch): WorkFilters {
  return {
    q: (search.q ?? "").trim().toLocaleLowerCase(),
    state: search.state ?? "",
    // A search built anywhere — a stored view, a hand-built one — reads its key
    // through the same reader the URL uses, so both states always agree.
    owner: ownerKeyFilter(search.owner ?? ""),
    blocked: search.blocked === "1",
  };
}

export function filterWorkItems(items: readonly WorkItem[], filters: WorkFilters): WorkItem[] {
  return items.filter((item) =>
    (filters.q === "" || [item.title, item.key, ownerLabel(item.assignee), item.description ?? ""]
      .some((text) => text.toLocaleLowerCase().includes(filters.q))) &&
    (filters.state === "" || item.status === filters.state) &&
    (filters.owner === "" || ownerKey(item.assignee) === filters.owner) &&
    (!filters.blocked || item.blocked));
}

/** Display order of board columns; unknown states follow alphabetically so served data is never dropped. */
const workStateOrder = ["backlog", "ready", "in_progress", "review", "done"] as const;

const stateRank = (state: string): number => {
  const index = workStateOrder.indexOf(state as (typeof workStateOrder)[number]);
  return index === -1 ? workStateOrder.length : index;
};

interface BoardColumn {
  state: string;
  items: WorkItem[];
}

/**
 * The board's column set. Every supported state always renders, empty or not:
 * an empty column is a real movement destination, and without it a project
 * whose items all sit in one state has no target at all. A served state
 * outside the supported set keeps its own column too (a served state is never
 * dropped) and sorts last, after the canonical order.
 */
export function boardColumns(items: readonly WorkItem[]): BoardColumn[] {
  const columns = new Map<string, WorkItem[]>(workStateOrder.map((state) => [state, []]));
  for (const item of items) {
    const column = columns.get(item.status);
    if (column) column.push(item);
    else columns.set(item.status, [item]);
  }
  return [...columns.entries()]
    .map(([state, column]) => ({ state, items: column }))
    .sort((left, right) => stateRank(left.state) - stateRank(right.state) || left.state.localeCompare(right.state));
}

export function stateOptions(items: readonly WorkItem[]): string[] {
  return [...new Set(items.map((item) => item.status))]
    .sort((left, right) => stateRank(left) - stateRank(right) || left.localeCompare(right));
}

interface OwnerOption {
  value: string;
  label: string;
}

export function ownerOptions(items: readonly WorkItem[]): OwnerOption[] {
  const options = new Map<string, string>();
  for (const item of items) {
    const value = ownerKey(item.assignee);
    if (!options.has(value)) options.set(value, ownerLabel(item.assignee));
  }
  return [...options.entries()].map(([value, label]) => ({ value, label })).sort((left, right) => left.label.localeCompare(right.label));
}

interface WorkSummary {
  total: number;
  blocked: number;
}

export function summarizeWork(items: readonly WorkItem[]): WorkSummary {
  return {
    total: items.length,
    blocked: items.filter((item) => item.blocked).length,
  };
}

export const workSummaryText = (total: number, blocked: number): string =>
  `${total} work ${total === 1 ? "item" : "items"} · ${blocked} blocked`;
