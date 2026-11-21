import { Schema } from "effect";
import { useState } from "react";
import { workSearch, type WorkSearch } from "./filters";

/**
 * View preferences only: a saved view is a named filter/view combination kept in
 * versioned localStorage under a deployment scope. No authoritative work or
 * evidence data, credentials, or session handles are stored here, and the URL
 * stays the source of truth for what is currently rendered.
 */
export const savedViewStorageKey = "symmetry.portal.saved-views";
const savedViewVersion = 1;

interface SavedWorkView {
  name: string;
  search: WorkSearch;
}

const StoredSearch = Schema.Struct({
  view: Schema.optional(Schema.Literal("list", "board")),
  q: Schema.optional(Schema.String),
  state: Schema.optional(Schema.String),
  owner: Schema.optional(Schema.String),
  blocked: Schema.optional(Schema.Literal("1")),
});
const StoredView = Schema.Struct({ name: Schema.String, search: StoredSearch });
const StoredDocument = Schema.Struct({
  version: Schema.Literal(savedViewVersion),
  scope: Schema.String,
  projects: Schema.Record({ key: Schema.String, value: Schema.Array(StoredView) }),
});
const decodeDocument = Schema.decodeUnknownOption(Schema.parseJson(StoredDocument));

/** The session bootstrap serves no user identity, so preferences are scoped to the deployment origin. */
export function deploymentScope(location: { readonly origin?: string } = window.location): string {
  return location.origin ?? "unknown-deployment";
}

function safeStorage(): Storage | null {
  try {
    return window.localStorage;
  } catch {
    return null;
  }
}

function readDocument(storage: Storage | null, scope: string): Schema.Schema.Type<typeof StoredDocument> | null {
  if (storage === null) return null;
  try {
    const raw = storage.getItem(savedViewStorageKey);
    if (raw === null) return null;
    const decoded = decodeDocument(raw);
    return decoded._tag === "Some" && decoded.value.scope === scope ? decoded.value : null;
  } catch {
    // Unreadable preferences are discarded rather than trusted.
    return null;
  }
}

export function readSavedViews(storage: Storage | null, scope: string, projectId: string): SavedWorkView[] {
  const stored = readDocument(storage, scope)?.projects[projectId];
  if (!stored) return [];
  return stored.flatMap((view) => {
    const name = view.name.trim();
    if (name === "") return [];
    return [{ name, search: workSearch({ ...view.search }) }];
  });
}

export function writeSavedViews(storage: Storage | null, scope: string, projectId: string, views: readonly SavedWorkView[]): void {
  if (storage === null) return;
  const stored = readDocument(storage, scope);
  const document = {
    version: savedViewVersion,
    scope,
    projects: { ...stored?.projects, [projectId]: views },
  };
  try {
    storage.setItem(savedViewStorageKey, JSON.stringify(document));
  } catch {
    // Preferences are disposable: a full or blocked storage must not break the read surface.
  }
}

interface SavedViews {
  views: SavedWorkView[];
  save: (name: string, search: WorkSearch) => void;
  remove: (name: string) => void;
}

export function useSavedViews(projectId: string): SavedViews {
  const [state, setState] = useState<{ projectId: string; views: SavedWorkView[] }>(() => ({
    projectId,
    views: readSavedViews(safeStorage(), deploymentScope(), projectId),
  }));
  if (state.projectId !== projectId) {
    setState({ projectId, views: readSavedViews(safeStorage(), deploymentScope(), projectId) });
  }
  const persist = (views: SavedWorkView[]) => {
    writeSavedViews(safeStorage(), deploymentScope(), projectId, views);
    setState({ projectId, views });
  };
  return {
    views: state.views,
    save: (name: string, search: WorkSearch) => {
      const trimmed = name.trim();
      if (trimmed === "") return;
      persist([...state.views.filter((view) => view.name !== trimmed), { name: trimmed, search }]);
    },
    remove: (name: string) => { persist(state.views.filter((view) => view.name !== name)); },
  };
}
