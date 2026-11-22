import { describe, expect, it, vi } from "vitest";
import { createApplication, safeReturnTo, workspaceOptions } from "./application";
import { Conflict, Network, Unauthenticated, UnexpectedResponse, readRetry, type ConnectionDraft } from "../services/api";
import returnTargets from "../../../contracts/fixtures/shell-return-targets.json";

describe("private query lifecycle", () => {
  it.each([401, 302, 303])("clears private records and cancels other reads before login recovery for HTTP %s", async (status) => {
    const replace = vi.fn();
    const fetcher = vi.fn<typeof fetch>().mockImplementation((input) => {
      if ((input instanceof Request ? input.url : String(input)).includes("expired")) return Promise.resolve(new Response("{}", { status }));
      return Promise.withResolvers<Response>().promise;
    });
    const app = createApplication({ fetcher, location: { href: "http://localhost/portal/projects/expired", replace } });
    app.queryClient.setQueryData(["private", "old"], { title: "Private title" });
    const otherRead = app.queryClient.fetchQuery(workspaceOptions(app.api, "pending"));
    const cancelled = otherRead.catch(() => "cancelled");
    await expect(app.queryClient.fetchQuery(workspaceOptions(app.api, "expired"))).rejects.toBeDefined();
    await vi.waitFor(() => { expect(replace).toHaveBeenCalledTimes(1); });
    expect(await cancelled).toBe("cancelled");
    expect(app.queryClient.getQueryCache().getAll()).toHaveLength(0);
    expect(app.auth.getSnapshot()).toBe(true);
    expect(replace).toHaveBeenCalledWith("/portal/login?return_to=%2Fportal%2Fprojects%2Fexpired");
  });

  it("never accepts an external or non-shell login return location", () => {
    expect(safeReturnTo("https://other.invalid/elsewhere")).toBe("/portal/attention");
    // Only the query state the shell itself writes survives; the server's
    // per-path rules are the authority, so a foreign key falls back.
    expect(safeReturnTo("http://localhost/portal/projects/p1?x=1")).toBe("/portal/attention");
    expect(safeReturnTo("http://localhost/portal/goals/goal-123")).toBe("/portal/goals/goal-123");
    expect(safeReturnTo("http://localhost/portal/logout")).toBe("/portal/attention");
    // The Goal path is a single anchored segment, like the work-item path.
    expect(safeReturnTo("http://localhost/portal/goals/goal-123/extra")).toBe("/portal/attention");
    expect(safeReturnTo("http://localhost/portal/goals/../login")).toBe("/portal/attention");
  });

  it.each(returnTargets.cases)("keeps the shared shell return target: $name", (row) => {
    // A malformed row is a corpus error, not a refusal case: fail loudly on a
    // missing name/path and on a missing or non-string, non-null return_to.
    const label = `shared return-target row: ${JSON.stringify(row)}`;
    expect(typeof row.name, label).toBe("string");
    expect(typeof row.path, label).toBe("string");
    expect(Object.hasOwn(row, "return_to"), label).toBe(true);
    expect(row.return_to === null || typeof row.return_to === "string", label).toBe(true);
    expect(safeReturnTo(new URL(row.path, returnTargets.base_url).href)).toBe(row.return_to ?? "/portal/attention");
  });

  // The corpus carries one `path` string per case, so the raw empty-query and
  // bare-fragment spellings stay out of it and are pinned instead per side: the
  // server side pins every shell path (see the reject list of the ExUnit test
  // for explicit internal shell return paths), and this file every shell path
  // but `/portal/attention` (see below). The URL `search`/`hash` getters report
  // an empty query or fragment as "", and `safeReturnTo` emits pathname +
  // search (the parser keeps `?` and `#` in `href`), so the client keeps the
  // location while the server refuses the raw spelling.
  const shellPaths = [
    "/portal",
    // `/portal/attention` is absent: its accepted target equals the refusal fallback, so the pin cannot discriminate.
    "/portal/chat",
    "/portal/connections",
    "/portal/goals/goal-123",
    "/portal/projects",
    "/portal/projects/project-123",
    "/portal/runtimes",
    "/portal/work/work-123",
  ];
  it.each(shellPaths.flatMap((path) => [
    [`${path}?`, path] as const,
    [`${path}#`, path] as const,
  ]))("keeps the shell location for the raw URL %s", (raw, expected) => {
    expect(safeReturnTo(`http://localhost${raw}`)).toBe(expected);
  });

  it("expires the session when a command refuses with 401 even though its mutation has its own error handler", async () => {
    const replace = vi.fn();
    const fetcher = vi.fn<typeof fetch>().mockImplementation((_input, init) =>
      (init?.method ?? "GET") === "GET"
        ? Promise.withResolvers<Response>().promise
        : Promise.resolve(new Response("{}", { status: 401 })));
    const app = createApplication({ fetcher, location: { href: "http://localhost/portal/projects/p1", replace } });
    app.queryClient.setQueryData(["private", "old"], { title: "Private title" });
    const localHandler = vi.fn();
    // Every connection mutation supplies its own onError, which replaces the
    // default mutation handler instead of chaining to it; expiry recovery must
    // not depend on the command's own callback or on a later read also
    // answering 401.
    const mutation = app.queryClient.getMutationCache().build(app.queryClient, {
      mutationFn: (draft: ConnectionDraft) => app.api.createConnection(draft, "csrf"),
      onError: (error) => {
        localHandler(error);
        void app.queryClient.invalidateQueries({ queryKey: ["connections"] });
      },
    });
    await expect(mutation.execute({ provider: "github", name: "Private draft", account_ref: "acme", capabilities: [] }))
      .rejects.toBeInstanceOf(Unauthenticated);
    expect(localHandler).toHaveBeenCalledTimes(1);
    await vi.waitFor(() => { expect(replace).toHaveBeenCalledTimes(1); });
    expect(app.queryClient.getQueryCache().getAll()).toHaveLength(0);
    expect(app.auth.getSnapshot()).toBe(true);
    expect(replace).toHaveBeenCalledWith("/portal/login?return_to=%2Fportal%2Fprojects%2Fp1");
  });

  it("retries only bounded transient read errors", () => {
    expect(readRetry(0, new Network({ message: "Offline" }))).toBe(true);
    expect(readRetry(2, new Network({ message: "Offline" }))).toBe(false);
    expect(readRetry(0, new Conflict({ message: "Changed" }))).toBe(false);
    expect(readRetry(0, new UnexpectedResponse({ message: "Bad JSON", status: 200, transient: false }))).toBe(false);
  });
});
