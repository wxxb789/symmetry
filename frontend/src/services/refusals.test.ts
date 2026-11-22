import { describe, expect, it } from "vitest";
import {
  Conflict,
  Forbidden,
  Network,
  ServerRefusal,
  Unauthenticated,
  UnexpectedResponse,
  Unsupported,
  Validation,
} from "./api";
import { MutationRefused } from "./commands";
import {
  assignmentRefusalGuidance,
  failureMode,
  isDefinitive,
  isDefinitiveReadRefusal,
  isStaleRefusal,
  isTerminalStateRefusal,
  isUnknownOutcome,
  planningRefusalGuidance,
  refusalNotice,
  resourceRefusalGuidance,
} from "./refusals";

type TruthRow = readonly [label: string, error: unknown, expected: boolean];
type StepRow = readonly [code: string, step: string];

const serverRefusal = (status: number, code: string): ServerRefusal =>
  new ServerRefusal({ status, code, message: "the server refused this write", fields: null });

const classed = (rows: readonly TruthRow[], predicate: (error: unknown) => boolean) =>
  rows.map(([label, error]) => [label, predicate(error)] as const);

const pinned = (rows: readonly TruthRow[]) =>
  rows.map(([label, , expected]) => [label, expected] as const);

const steps = (rows: readonly StepRow[], guidance: Record<string, string>) =>
  rows.map(([code]) => refusalNotice(serverRefusal(409, code), guidance));

const sentences = (rows: readonly StepRow[]) =>
  rows.map(([code, step]) => `the server refused this write (${code}) — ${step}`);

describe("isDefinitive", () => {
  it("reproduces definitiveRefusal as chat/use-chat-scope-state.ts and work/control.tsx defined it (mutant: Forbidden dropped from the set)", () => {
    const rows: readonly TruthRow[] = [
      ["Conflict", new Conflict({ message: "conflict" }), true],
      ["Validation", new Validation({ message: "invalid" }), true],
      ["Forbidden", new Forbidden({ message: "refused" }), true],
      ["Unsupported", new Unsupported({ message: "unsupported" }), true],
      ["ServerRefusal 400", serverRefusal(400, "invalid_request"), true],
      ["ServerRefusal 499", serverRefusal(499, "invalid_request"), true],
      ["ServerRefusal 500", serverRefusal(500, "internal_error"), false],
      ["Unauthenticated", new Unauthenticated({ message: "signed out" }), false],
      ["Network", new Network({ message: "offline" }), false],
      ["UnexpectedResponse", new UnexpectedResponse({ message: "unreadable", status: 400, transient: false }), false],
      ["MutationRefused", new MutationRefused(409, "stale", "resource changed", {}), false],
      ["Error", new Error("boom"), false],
    ];
    expect(classed(rows, isDefinitive)).toEqual(pinned(rows));
  });
});

describe("isDefinitiveReadRefusal", () => {
  it("keeps the read-cursor policy distinct from isDefinitive (mutant: the status === 400 condition dropped, widening the set to any 4xx)", () => {
    const rows: readonly TruthRow[] = [
      ["Conflict", new Conflict({ message: "conflict" }), true],
      ["Validation", new Validation({ message: "invalid" }), true],
      ["UnexpectedResponse 400", new UnexpectedResponse({ message: "unreadable", status: 400, transient: false }), true],
      ["UnexpectedResponse 404", new UnexpectedResponse({ message: "unreadable", status: 404, transient: false }), false],
      ["UnexpectedResponse 500", new UnexpectedResponse({ message: "unreadable", status: 500, transient: true }), false],
      ["Forbidden", new Forbidden({ message: "refused" }), false],
      ["ServerRefusal 404", serverRefusal(404, "not_found"), false],
      ["ServerRefusal 400", serverRefusal(400, "invalid_request"), false],
      ["Network", new Network({ message: "offline" }), false],
      ["Error", new Error("boom"), false],
    ];
    expect(classed(rows, isDefinitiveReadRefusal)).toEqual(pinned(rows));
  });
});

describe("isUnknownOutcome", () => {
  it("reproduces outcomeUnknown as goals/detail.tsx defined it (mutant: UnexpectedResponse classified as a known outcome)", () => {
    const rows: readonly TruthRow[] = [
      ["Network", new Network({ message: "offline" }), true],
      ["UnexpectedResponse transient", new UnexpectedResponse({ message: "unreadable", status: 503, transient: true }), true],
      ["UnexpectedResponse permanent", new UnexpectedResponse({ message: "unreadable", status: 400, transient: false }), true],
      ["ServerRefusal 500", serverRefusal(500, "internal_error"), false],
      ["Conflict", new Conflict({ message: "conflict" }), false],
      ["MutationRefused", new MutationRefused(409, "stale", "resource changed", {}), false],
      ["Error", new Error("boom"), false],
    ];
    expect(classed(rows, isUnknownOutcome)).toEqual(pinned(rows));
  });
});

describe("isStaleRefusal", () => {
  it("delegates the command surface's staleness to MutationRefused.stale (mutant: stale dropped from the class's code set)", () => {
    const rows: readonly TruthRow[] = [
      ["MutationRefused 409 stale", new MutationRefused(409, "stale", "resource changed", {}), true],
      ["MutationRefused 409 stale_revision", new MutationRefused(409, "stale_revision", "resource changed", {}), true],
      ["MutationRefused 404 stale", new MutationRefused(404, "stale", "resource missing", {}), false],
      ["MutationRefused 409 state_conflict", new MutationRefused(409, "state_conflict", "state moved", {}), false],
      ["MutationRefused 409 not_found", new MutationRefused(409, "not_found", "resource missing", {}), false],
    ];
    expect(classed(rows, isStaleRefusal)).toEqual(pinned(rows));
  });

  it("reproduces the write surfaces' ServerRefusal stale check, status-insensitive as base was (mutants: a status condition added; the code set widened to stale_revision)", () => {
    const rows: readonly TruthRow[] = [
      ["ServerRefusal 409 stale", serverRefusal(409, "stale"), true],
      ["ServerRefusal 500 stale", serverRefusal(500, "stale"), true],
      ["ServerRefusal 409 stale_revision", serverRefusal(409, "stale_revision"), false],
      ["ServerRefusal 409 state_conflict", serverRefusal(409, "state_conflict"), false],
      ["ServerRefusal 404 not_found", serverRefusal(404, "not_found"), false],
      ["ServerRefusal 403 forbidden", serverRefusal(403, "forbidden"), false],
      ["Error", new Error("boom"), false],
    ];
    expect(classed(rows, isStaleRefusal)).toEqual(pinned(rows));
  });
});

describe("isTerminalStateRefusal", () => {
  it("keeps the unbind dialog's own three-code set, never merged with the stale set (mutants: the sets merged; stale_revision added)", () => {
    const rows: readonly TruthRow[] = [
      ["ServerRefusal 409 stale", serverRefusal(409, "stale"), true],
      ["ServerRefusal 409 state_conflict", serverRefusal(409, "state_conflict"), true],
      ["ServerRefusal 404 not_found", serverRefusal(404, "not_found"), true],
      ["ServerRefusal 500 stale", serverRefusal(500, "stale"), true],
      ["ServerRefusal 409 stale_revision", serverRefusal(409, "stale_revision"), false],
      ["ServerRefusal 403 forbidden", serverRefusal(403, "forbidden"), false],
      ["MutationRefused 409 stale", new MutationRefused(409, "stale", "resource changed", {}), false],
      ["Network", new Network({ message: "offline" }), false],
    ];
    expect(classed(rows, isTerminalStateRefusal)).toEqual(pinned(rows));
  });
});

describe("failureMode", () => {
  it("reproduces write-policy.ts's five-way table (mutant: a 5xx ServerRefusal classified as an authoritative answer)", () => {
    const rows: readonly (readonly [label: string, error: unknown, expected: string])[] = [
      ["ServerRefusal 500", serverRefusal(500, "internal_error"), "retryable"],
      ["ServerRefusal 503", serverRefusal(503, "unavailable"), "retryable"],
      ["ServerRefusal 400 validation_failed", serverRefusal(400, "validation_failed"), "fields"],
      ["ServerRefusal 409 stale", serverRefusal(409, "stale"), "authoritative"],
      ["ServerRefusal 404 not_found", serverRefusal(404, "not_found"), "authoritative"],
      ["Network", new Network({ message: "offline" }), "retryable"],
      ["UnexpectedResponse transient", new UnexpectedResponse({ message: "unreadable", status: 503, transient: true }), "retryable"],
      ["UnexpectedResponse permanent", new UnexpectedResponse({ message: "unreadable", status: 400, transient: false }), "authoritative"],
      ["MutationRefused", new MutationRefused(409, "stale", "resource changed", {}), "authoritative"],
      ["Error", new Error("boom"), "authoritative"],
    ];
    expect(rows.map(([label, error]) => [label, failureMode(error)] as const))
      .toEqual(rows.map(([label, , expected]) => [label, expected] as const));
  });
});

const resourceSteps: readonly StepRow[] = [
  ["stale", "Reload to see the server's current values."],
  ["state_conflict", "Reload and resolve the changed state."],
  ["not_found", "Reload the page to see the server's current state."],
  ["forbidden", "Grant the required provider permissions, then test the connection again."],
  ["not_connected", "Bind the resource to a connection first."],
  ["capability_not_granted", "Grant the connection the capability this resource kind needs, then try again."],
  ["provider_unauthorized", "Reauthenticate the provider session on this server, then test the connection again."],
  ["provider_failure", "Check provider availability, then test the connection again."],
  ["validation_failed", "The server rejected this submission; re-read the current values and try again."],
];

const planningSteps: readonly StepRow[] = [
  ["stale", "The server's current state is shown instead."],
  ["state_conflict", "The server's current state is shown instead."],
  ["provider_owned", "Fields owned by the external work tracker are edited in that tracker."],
  ["goal_authority_required", "Goal-managed work is changed through Goal admission, not this surface."],
  ["not_found", "The server's current state is shown instead."],
  ["validation_failed", "Correct the highlighted fields and submit again."],
  ["invalid_request", "This request cannot be corrected here; the editor closes and the server's current state is shown."],
  ["forbidden", "Refresh the page and sign in again if the session was renewed elsewhere."],
];

const assignmentSteps: readonly StepRow[] = [
  ["provider_owned", "The provider keeps the title, description and priority; only the assignment fields here are editable."],
  ["stale", "The work item changed elsewhere; close the editor and reopen it from the re-read detail."],
  ["state_conflict", "This item or its project is in a state that refuses the write; re-read both, then retry."],
  ["validation_failed", "Correct the highlighted fields and submit again."],
];

describe("refusalNotice rendering", () => {
  it("renders every resource refusal sentence as base did (mutants: a dropped step; a changed joiner or code suffix; an extra table entry)", () => {
    expect(Object.keys(resourceRefusalGuidance).sort()).toEqual(resourceSteps.map(([code]) => code).sort());
    expect(steps(resourceSteps, resourceRefusalGuidance)).toEqual(sentences(resourceSteps));
  });

  it("renders every planning refusal sentence as base did (mutants: a dropped step; a changed joiner or code suffix; an extra table entry)", () => {
    expect(Object.keys(planningRefusalGuidance).sort()).toEqual(planningSteps.map(([code]) => code).sort());
    expect(steps(planningSteps, planningRefusalGuidance)).toEqual(sentences(planningSteps));
  });

  it("renders every assignment refusal sentence as base did (mutants: a dropped step; a changed joiner or code suffix; an extra table entry)", () => {
    expect(Object.keys(assignmentRefusalGuidance).sort()).toEqual(assignmentSteps.map(([code]) => code).sort());
    expect(steps(assignmentSteps, assignmentRefusalGuidance)).toEqual(sentences(assignmentSteps));
  });

  it("serves a one-argument caller the planning table (mutant: the default table swapped for another surface's)", () => {
    expect(refusalNotice(serverRefusal(409, "stale"))).toBe("the server refused this write (stale) — The server's current state is shown instead.");
  });

  it("keeps the frozen fallbacks (mutants: a plain-index step lookup; the suffix or joiner changed)", () => {
    expect(refusalNotice(serverRefusal(409, "unlisted_code"), planningRefusalGuidance)).toBe("the server refused this write (unlisted_code)");
    expect(refusalNotice(serverRefusal(409, "constructor"), planningRefusalGuidance)).toBe("the server refused this write (constructor)");
    expect(refusalNotice(serverRefusal(500, "toString"), resourceRefusalGuidance)).toBe("the server refused this write (toString)");
    expect(refusalNotice(new Network({ message: "the network is unreachable" }))).toBe("the network is unreachable");
    expect(refusalNotice(new Error("boom"))).toBe("The workspace could not be loaded. Try again.");
  });
});
