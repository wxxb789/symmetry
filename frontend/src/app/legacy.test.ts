import { describe, expect, it } from "vitest";
import { legacyDestination } from "./legacy";

describe("one-time legacy navigation", () => {
  it("maps supported views to reloadable shell routes", () => {
    expect(legacyDestination("#attention")).toBe("/portal/attention");
    expect(legacyDestination("#projects")).toBe("/portal/projects");
    expect(legacyDestination("#project/p1")).toBe("/portal/projects/p1");
    expect(legacyDestination("#projects", "?project_id=p1")).toBe("/portal/projects/p1");
    expect(legacyDestination("#attention", "?project_id=p1")).toBe("/portal/attention?projectId=p1");
    expect(legacyDestination("")).toBeNull();
  });

  it("preserves classic project scope without mounting a second runtime", () => {
    expect(legacyDestination("#chat", "?project_id=p1")).toBe("/portal/classic?project_id=p1#chat");
    expect(legacyDestination("#board", "?project_id=p2")).toBe("/portal/classic?project_id=p2#board");
  });

  it("explains unsupported destinations rather than inventing pages", () => {
    expect(legacyDestination("#unknown")).toBe("/portal/attention?unavailable=unknown");
  });
});
