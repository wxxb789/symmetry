import { describe, expect, it } from "vitest";
import { deploymentScope, readSavedViews, savedViewStorageKey, writeSavedViews } from "./saved-views";

const projectId = "11111111-1111-4111-8111-111111111111";

function memoryStorage(): Storage {
  const entries = new Map<string, string>();
  return {
    get length() { return entries.size; },
    clear: () => { entries.clear(); },
    getItem: (key) => entries.get(key) ?? null,
    key: (index) => [...entries.keys()][index] ?? null,
    removeItem: (key) => { entries.delete(key); },
    setItem: (key, value) => { entries.set(key, value); },
  };
}

describe("saved view preferences", () => {
  const scope = deploymentScope({ origin: "https://portal.example" });

  it("round-trips views per project under the deployment scope", () => {
    const storage = memoryStorage();
    writeSavedViews(storage, scope, projectId, [{ name: "My ready work", search: { view: "board", state: "ready", blocked: "1" } }]);
    expect(readSavedViews(storage, scope, projectId)).toEqual([{ name: "My ready work", search: { view: "board", state: "ready", blocked: "1" } }]);
    expect(readSavedViews(storage, scope, "22222222-2222-4222-8222-222222222222")).toEqual([]);
    expect(deploymentScope({})).toBe("unknown-deployment");
  });

  it("keeps another project's views when one project is rewritten", () => {
    const storage = memoryStorage();
    const otherProjectId = "22222222-2222-4222-8222-222222222222";
    writeSavedViews(storage, scope, projectId, [{ name: "Mine", search: { q: "a" } }]);
    writeSavedViews(storage, scope, otherProjectId, [{ name: "Theirs", search: { view: "board" } }]);
    expect(readSavedViews(storage, scope, projectId)).toEqual([{ name: "Mine", search: { q: "a" } }]);
    expect(readSavedViews(storage, scope, otherProjectId)).toEqual([{ name: "Theirs", search: { view: "board" } }]);
  });

  it("keeps preferences free of credentials and authoritative records", () => {
    const storage = memoryStorage();
    writeSavedViews(storage, scope, projectId, [{ name: "Mine", search: { q: "read surface" } }]);
    const raw = storage.getItem(savedViewStorageKey) ?? "";
    expect(raw).not.toMatch(/csrf|token|session|password|evidence|accepted/i);
    expect(raw).toContain('"version":1');
    expect(raw).toContain(`"scope":"${scope}"`);
    expect(raw).toContain(`"projects":{"${projectId}":[{"name":"Mine","search":{"q":"read surface"}}]}`);
  });

  it("discards documents from another version, another deployment or a broken shape", () => {
    const storage = memoryStorage();
    writeSavedViews(storage, scope, projectId, [{ name: "Mine", search: {} }]);
    expect(readSavedViews(storage, "https://other.example", projectId)).toEqual([]);
    storage.setItem(savedViewStorageKey, JSON.stringify({ version: 2, scope, projects: { [projectId]: [{ name: "Mine", search: {} }] } }));
    expect(readSavedViews(storage, scope, projectId)).toEqual([]);
    storage.setItem(savedViewStorageKey, "{not json");
    expect(readSavedViews(storage, scope, projectId)).toEqual([]);
    storage.setItem(savedViewStorageKey, JSON.stringify({ version: 1, scope, projects: { [projectId]: [{ name: "   ", search: {} }, { name: 7, search: {} }] } }));
    expect(readSavedViews(storage, scope, projectId)).toEqual([]);
  });

  it("keeps later views when rewriting and tolerates unavailable storage", () => {
    const storage = memoryStorage();
    writeSavedViews(storage, scope, projectId, [{ name: "Mine", search: { q: "a" } }]);
    writeSavedViews(storage, scope, projectId, [{ name: "Mine", search: { q: "b" } }, { name: "Other", search: { view: "board" } }]);
    expect(readSavedViews(storage, scope, projectId)).toEqual([{ name: "Mine", search: { q: "b" } }, { name: "Other", search: { view: "board" } }]);
    expect(readSavedViews(null, scope, projectId)).toEqual([]);
    writeSavedViews(null, scope, projectId, [{ name: "Mine", search: {} }]);
    expect(readSavedViews(storage, scope, projectId)).toHaveLength(2);
  });
});
