import { describe, expect, it, vi } from "vitest";
import { createApplication, safeReturnTo, workspaceOptions } from "./application";
import { Conflict, Network, UnexpectedResponse, readRetry } from "../services/api";

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
    await vi.waitFor(() => expect(replace).toHaveBeenCalledTimes(1));
    expect(await cancelled).toBe("cancelled");
    expect(app.queryClient.getQueryCache().getAll()).toHaveLength(0);
    expect(app.auth.getSnapshot()).toBe(true);
    expect(replace).toHaveBeenCalledWith("/portal/login?return_to=%2Fportal%2Fprojects%2Fexpired");
  });

  it("never accepts an external or non-shell login return location", () => {
    expect(safeReturnTo("https://other.invalid/elsewhere")).toBe("/portal/attention");
    expect(safeReturnTo("http://localhost/portal/projects/p1?x=1")).toBe("/portal/projects/p1?x=1");
    expect(safeReturnTo("http://localhost/portal/logout")).toBe("/portal/attention");
  });

  it("retries only bounded transient read errors", () => {
    expect(readRetry(0, new Network({ message: "Offline" }))).toBe(true);
    expect(readRetry(2, new Network({ message: "Offline" }))).toBe(false);
    expect(readRetry(0, new Conflict({ message: "Changed" }))).toBe(false);
    expect(readRetry(0, new UnexpectedResponse({ message: "Bad JSON", status: 200, transient: false }))).toBe(false);
  });
});
