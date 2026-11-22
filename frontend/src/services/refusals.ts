import { Conflict, Forbidden, Network, ServerRefusal, UnexpectedResponse, Unsupported, Validation, errorMessage, ownValue } from "./api";
import { MutationRefused } from "./commands";

const staleServerCodes: readonly string[] = ["stale"];

const terminalStateServerCodes: readonly string[] = ["stale", "state_conflict", "not_found"];

export function isDefinitive(error: unknown): boolean {
  return error instanceof Conflict || error instanceof Validation || error instanceof Forbidden || error instanceof Unsupported ||
    (error instanceof ServerRefusal && error.status >= 400 && error.status < 500);
}

export function isDefinitiveReadRefusal(error: unknown): boolean {
  return error instanceof Conflict || error instanceof Validation ||
    (error instanceof UnexpectedResponse && error.status === 400);
}

export function isUnknownOutcome(error: unknown): boolean {
  return error instanceof Network || error instanceof UnexpectedResponse;
}

export function isStaleRefusal(error: unknown): boolean {
  if (error instanceof MutationRefused) return error.stale;
  return error instanceof ServerRefusal && staleServerCodes.includes(error.code);
}

export function isTerminalStateRefusal(error: unknown): boolean {
  return error instanceof ServerRefusal && terminalStateServerCodes.includes(error.code);
}

type FailureMode = "fields" | "retryable" | "authoritative";

export function failureMode(error: unknown): FailureMode {
  if (error instanceof ServerRefusal) {
    if (error.status >= 500) return "retryable";
    return error.code === "validation_failed" ? "fields" : "authoritative";
  }
  if (error instanceof Network) return "retryable";
  if (error instanceof UnexpectedResponse) return error.transient ? "retryable" : "authoritative";
  return "authoritative";
}

export const resourceRefusalGuidance: Record<string, string> = {
  stale: "Reload to see the server's current values.",
  state_conflict: "Reload and resolve the changed state.",
  not_found: "Reload the page to see the server's current state.",
  forbidden: "Grant the required provider permissions, then test the connection again.",
  not_connected: "Bind the resource to a connection first.",
  capability_not_granted: "Grant the connection the capability this resource kind needs, then try again.",
  provider_unauthorized: "Reauthenticate the provider session on this server, then test the connection again.",
  provider_failure: "Check provider availability, then test the connection again.",
  validation_failed: "The server rejected this submission; re-read the current values and try again.",
};

export const planningRefusalGuidance: Record<string, string> = {
  stale: "The server's current state is shown instead.",
  state_conflict: "The server's current state is shown instead.",
  provider_owned: "Fields owned by the external work tracker are edited in that tracker.",
  goal_authority_required: "Goal-managed work is changed through Goal admission, not this surface.",
  not_found: "The server's current state is shown instead.",
  validation_failed: "Correct the highlighted fields and submit again.",
  invalid_request: "This request cannot be corrected here; the editor closes and the server's current state is shown.",
  forbidden: "Refresh the page and sign in again if the session was renewed elsewhere.",
};

export const assignmentRefusalGuidance: Record<string, string> = {
  provider_owned: "The provider keeps the title, description and priority; only the assignment fields here are editable.",
  stale: "The work item changed elsewhere; close the editor and reopen it from the re-read detail.",
  state_conflict: "This item or its project is in a state that refuses the write; re-read both, then retry.",
  validation_failed: "Correct the highlighted fields and submit again.",
};

export function refusalNotice(error: unknown, guidance: Record<string, string> = planningRefusalGuidance): string {
  if (!(error instanceof ServerRefusal)) return errorMessage(error);
  const step = ownValue(guidance, error.code);
  return `${error.message} (${error.code})${step ? ` — ${step}` : ""}`;
}
