import { describe, expect, it } from "vitest";
import { legacyDestination } from "./legacy";

describe("one-time legacy hash repair", () => {
  it("maps the classic view anchors onto the routes that serve them now", () => {
    expect(legacyDestination("#attention")).toBe("/portal/attention");
    expect(legacyDestination("#health")).toBe("/portal/attention");
    expect(legacyDestination("#projects")).toBe("/portal/projects");
    expect(legacyDestination("#project/p1")).toBe("/portal/projects/p1");
    expect(legacyDestination("#projects", "?project_id=p1")).toBe("/portal/projects/p1");
    expect(legacyDestination("#attention", "?project_id=p1")).toBe("/portal/attention?projectId=p1");
    expect(legacyDestination("")).toBeNull();
    expect(legacyDestination("#")).toBeNull();
  });

  it("sends the classic board and its linked-runs list to the project board", () => {
    expect(legacyDestination("#board")).toBe("/portal/projects");
    expect(legacyDestination("#board", "?project_id=p2")).toBe("/portal/projects/p2");
    expect(legacyDestination("#activity", "?project_id=p2")).toBe("/portal/projects/p2");
  });

  it("sends the classic resources view to the project-resource surface", () => {
    expect(legacyDestination("#resources")).toBe("/portal/connections");
    expect(legacyDestination("#resources", "?project_id=p2")).toBe(
      "/portal/connections?projectId=p2",
    );
  });

  it("carries the classic chat scope and its run into the conversation route", () => {
    expect(legacyDestination("#chat")).toBe("/portal/chat");
    expect(legacyDestination("#chat", "?project_id=p1")).toBe("/portal/chat?scope=project&projectId=p1");
    expect(legacyDestination("#chat", "?chat_scope=workspace")).toBe("/portal/chat?scope=workspace");
    expect(legacyDestination("#chat", "?project_id=p1&chat_scope=run&run_id=r9")).toBe(
      "/portal/chat?scope=run&projectId=p1&runId=r9",
    );
    expect(legacyDestination("#chat", "?run_id=r9")).toBe("/portal/chat?scope=run&runId=r9");
  });

  it("sends the retired connections anchor to the new route", () => {
    expect(legacyDestination("#connections")).toBe("/portal/connections");
    expect(legacyDestination("#connections", "?connectionId=abc")).toBe("/portal/connections?connectionId=abc");
  });

  it("explains unsupported destinations rather than inventing pages", () => {
    expect(legacyDestination("#unknown")).toBe("/portal/attention?unavailable=unknown");
    expect(legacyDestination("#chat_scope=run")).toBe(
      "/portal/attention?unavailable=chat_scope%3Drun",
    );
  });
});
