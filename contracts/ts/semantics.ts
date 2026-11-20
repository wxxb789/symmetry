/**
 * Semantic validators receive generated DTOs after structural validation.
 * Public raw inputs must use the boundary decoders exported from index.ts first.
 */
import type {
  SymmetryAdmissionV1,
  Subject as AdmissionSubject,
} from "../generated/ts/admission.js";
import type { SymmetryContextSnapshotV1 } from "../generated/ts/context-snapshot.js";
import type { SymmetryDecisionV1 } from "../generated/ts/decision.js";
import type { SymmetryEvidenceBatchV1 } from "../generated/ts/evidence-batch.js";
import type { SymmetryEvidenceV1 } from "../generated/ts/evidence.js";
import type { SymmetryGoalCommandV1 } from "../generated/ts/goal-command.js";
import type { SymmetryGoalCreateV1 } from "../generated/ts/goal-create.js";
import type { SymmetryGoalRevisionV1 } from "../generated/ts/goal-revision.js";
import type { SymmetryPlanProposalV1 } from "../generated/ts/plan-proposal.js";
import type { SymmetryTaskResultV1 } from "../generated/ts/task-result.js";

const textEncoder = new TextEncoder();

export class SemanticError extends Error {
  readonly _tag = "SemanticError" as const;
  readonly code: string;

  constructor(code: string, message: string) {
    super(message);
    this.name = "SemanticError";
    this.code = code;
  }
}

type JsonObject = Record<string, unknown>;

const isJsonObject = (value: unknown): value is JsonObject =>
  value !== null && typeof value === "object" && !Array.isArray(value);

const canonicalize = (value: unknown): unknown => {
  if (Array.isArray(value)) return value.map(canonicalize);
  if (isJsonObject(value)) {
    return Object.fromEntries(
      Object.keys(value)
        .sort()
        .map((key) => [key, canonicalize(value[key])]),
    );
  }
  return value;
};

// The shorthand escapes Jason emits for the characters that have one; every
// other C0 control is escaped as an uppercase `\u00XX` code point.
const jsonEscapes: Record<number, string> = {
  0x08: "\\b",
  0x09: "\\t",
  0x0a: "\\n",
  0x0c: "\\f",
  0x0d: "\\r",
  0x22: '\\"',
  0x5c: "\\\\",
};

/**
 * One JSON string encoded exactly as Elixir `Jason` encodes it: the shorthand
 * escapes above, an uppercase `\u00XX` for every other C0 control, and every
 * other code point literal (Jason's generated `escape_json/1`,
 * `deps/jason/lib/encode.ex`). `JSON.stringify` escapes the same characters
 * with lowercase hex (`\u000b`), and the control plane hashes the exact text
 * `RequestHash.canonical/1` produces, so the two forms hash as different bytes.
 */
export const encodeJsonString = (value: string): string => {
  let encoded = '"';
  for (let index = 0; index < value.length; index += 1) {
    const code = value.charCodeAt(index);
    if (code < 0x20 || code === 0x22 || code === 0x5c) {
      encoded += jsonEscapes[code] ?? `\\u${code.toString(16).toUpperCase().padStart(4, "0")}`;
    } else {
      encoded += value[index];
    }
  }
  return `${encoded}"`;
};

/**
 * Canonical JSON text: object keys already sorted by `canonicalize`, arrays in
 * order, every string escaped as Jason escapes it. Values JSON cannot carry
 * (functions, symbols, `undefined`) are dropped from objects and rendered as
 * `null` in arrays, exactly as `JSON.stringify` treats them; at the top level
 * they leave nothing to encode.
 */
const encodeCanonicalJson = (value: unknown): string | undefined => {
  if (typeof value === "string") return encodeJsonString(value);
  if (Array.isArray(value)) {
    const items: string[] = [];
    for (let index = 0; index < value.length; index += 1) {
      items.push(encodeCanonicalJson(value[index]) ?? "null");
    }
    return `[${items.join(",")}]`;
  }
  if (isJsonObject(value)) {
    const members: string[] = [];
    for (const key of Object.keys(value)) {
      const member = encodeCanonicalJson(value[key]);
      if (member !== undefined) members.push(`${encodeJsonString(key)}:${member}`);
    }
    return `{${members.join(",")}}`;
  }
  return JSON.stringify(value);
};

export const canonicalJson = (value: unknown): string => {
  const encoded = encodeCanonicalJson(canonicalize(value));
  if (encoded === undefined) {
    throw new SemanticError("semantic_mismatch", "value cannot be encoded as canonical JSON");
  }
  return encoded;
};

const sha256 = async (value: unknown): Promise<string> => {
  const digest = await globalThis.crypto.subtle.digest(
    "SHA-256",
    textEncoder.encode(canonicalJson(value)),
  );
  const bytes = new Uint8Array(digest);
  const hexadecimal = Array.from(bytes, (byte) => byte.toString(16).padStart(2, "0")).join("");
  return `sha256:${hexadecimal}`;
};

export const subjectHash = (subject: AdmissionSubject): Promise<string> => sha256(subject);

const assertEqual = (
  label: string,
  actual: string,
  expected: string,
  code = "semantic_mismatch",
): void => {
  if (actual !== expected) {
    throw new SemanticError(code, `${label} mismatch: ${actual} != ${expected}`);
  }
};

type AcceptanceContract = SymmetryGoalRevisionV1["acceptance_contract"];
type RevisionContract = Pick<
  SymmetryGoalRevisionV1,
  "acceptance_contract" | "authority_policy" | "execution_policy"
>;
type ProviderChangeTarget = Exclude<
  NonNullable<SymmetryAdmissionV1["provider_scope"]>["change_target"],
  null
>;

/**
 * The proposal document Control hashes and validates for `request_decision`
 * and `accept_plan`: Elixir `canonical_plan_proposal` injects `integration:
 * false` and `change_target: null` per item before the request hash is
 * computed, so a client that presents a digest must present this document
 * (exported for the portal's plan approval, which computes the same digest).
 */
export const canonicalPlanProposal = (proposal: SymmetryPlanProposalV1): JsonObject => ({
  ...proposal,
  items: proposal.items.map((item) => ({
    ...item,
    integration: item.integration ?? false,
    change_target: item.change_target ?? null,
  })),
});

const validateAcceptancePredicateIDs = (acceptance: AcceptanceContract): void => {
  const ids = new Set<string>();
  for (const predicate of acceptance.predicates) {
    if (ids.has(predicate.id)) {
      throw new SemanticError(
        "semantic_mismatch",
        `duplicate acceptance predicate id ${predicate.id}`,
      );
    }
    ids.add(predicate.id);
  }
};

const validateProviderChangeTarget = (target: ProviderChangeTarget | null): void => {
  if (target === null || target.kind !== "branches") return;
  if (target.source_branch === target.target_branch) {
    throw new SemanticError("semantic_mismatch", "provider change target branches must differ");
  }
};

const validateGoalRevisionContractSemantics = (revision: RevisionContract): void => {
  validateAcceptancePredicateIDs(revision.acceptance_contract);
  if (
    revision.execution_policy.final_acceptance === "deterministic" &&
    revision.authority_policy.operator_required_for_completion !== true &&
    revision.acceptance_contract.predicates.some(
      (predicate) => predicate.kind !== "check" && predicate.kind !== "artifact",
    )
  ) {
    throw new SemanticError(
      "semantic_mismatch",
      "deterministic acceptance includes a non-machine predicate",
    );
  }
};

export const validateEvidenceSemantics = async (evidence: SymmetryEvidenceV1): Promise<void> => {
  const expectedHash = await subjectHash(evidence.subject);
  assertEqual("subject_hash", evidence.subject_hash, expectedHash);
  assertEqual("payload.subject_hash", evidence.payload.subject_hash, evidence.subject_hash);
  assertEqual("source_ref.subject_hash", evidence.source_ref.subject_hash, evidence.subject_hash);
  if (canonicalJson(evidence.payload.subject) !== canonicalJson(evidence.subject)) {
    throw new SemanticError("semantic_mismatch", "payload.subject does not match evidence.subject");
  }

  switch (evidence.kind) {
    case "check":
      assertEqual(
        "source_ref.validator_profile",
        evidence.source_ref.validator_profile,
        evidence.validator_profile,
      );
      return;
    case "artifact":
      assertEqual(
        "source_ref.resource_id",
        evidence.source_ref.resource_id,
        evidence.payload.resource_id,
      );
      assertEqual("source_ref.commit", evidence.source_ref.commit, evidence.payload.commit);
      assertEqual("source_ref.path", evidence.source_ref.path, evidence.payload.path);
      assertEqual(
        "subject.resource_id",
        evidence.payload.resource_id,
        evidence.subject.resource_id,
      );
      assertEqual("subject.commit", evidence.payload.commit, evidence.subject.commit);
      return;
    case "review":
      assertEqual("payload.verdict", evidence.payload.verdict, evidence.verdict);
      assertEqual(
        "source_ref.review_task_id",
        evidence.source_ref.review_task_id,
        evidence.payload.review_task_id,
      );
      return;
    case "observation":
      assertEqual(
        "source_ref.external_ref",
        evidence.source_ref.external_ref,
        evidence.payload.external_ref,
      );
      return;
  }
};

export const validateEvidenceBatchSemantics = async (
  batch: SymmetryEvidenceBatchV1,
): Promise<void> => {
  const evidenceKeys = new Set<string>();
  const evidenceIDs = new Set<string>();
  for (const evidence of batch.items) {
    assertEqual("evidence.run_id", evidence.run_id, batch.run_id);
    if (evidenceKeys.has(evidence.evidence_key)) {
      throw new SemanticError(
        "duplicate_evidence_key",
        `duplicate evidence key ${evidence.evidence_key}`,
      );
    }
    evidenceKeys.add(evidence.evidence_key);
    if (evidenceIDs.has(evidence.evidence_id)) {
      throw new SemanticError(
        "duplicate_evidence_id",
        `duplicate evidence id ${evidence.evidence_id}`,
      );
    }
    evidenceIDs.add(evidence.evidence_id);
    await validateEvidenceSemantics(evidence);
  }
};

export const validateContextSnapshotSemantics = async (
  snapshot: SymmetryContextSnapshotV1,
): Promise<void> => {
  const withoutContentHash = Object.fromEntries(
    Object.entries(snapshot).filter(([key]) => key !== "content_hash"),
  );
  const expectedHash = await sha256(withoutContentHash);
  assertEqual("content_hash", snapshot.content_hash, expectedHash);
  validateAcceptancePredicateIDs(snapshot.work_contract.acceptance);
  validateProviderChangeTarget(snapshot.work_contract.change_target);
};

export const validateDecisionSemantics = async (decision: SymmetryDecisionV1): Promise<void> => {
  const optionIDs = new Set<string>();
  for (const option of decision.options) {
    if (optionIDs.has(option.id)) {
      throw new SemanticError(
        "duplicate_decision_option_id",
        `duplicate decision option id ${option.id}`,
      );
    }
    optionIDs.add(option.id);
  }

  switch (decision.state) {
    case "open":
    case "superseded":
      if (decision.resolution !== null) {
        throw new SemanticError(
          "decision_resolution_shape",
          `resolution is not allowed for ${decision.state} decisions`,
        );
      }
      return;
    case "resolved":
      if (decision.resolution === null) {
        throw new SemanticError(
          "decision_resolution_shape",
          "resolved decisions require a resolution",
        );
      }
      if (!optionIDs.has(decision.resolution.option_id)) {
        throw new SemanticError(
          "decision_resolution_unknown_option",
          `resolution option ${decision.resolution.option_id} is not present in decision options`,
        );
      }
      return;
  }
};

export const validateGoalRevisionSemantics = async (
  revision: SymmetryGoalRevisionV1,
): Promise<void> => {
  validateGoalRevisionContractSemantics(revision);
};

export const validatePlanProposalSemantics = async (
  proposal: SymmetryPlanProposalV1,
): Promise<void> => {
  for (const item of proposal.items) {
    validateAcceptancePredicateIDs(item.acceptance);
    validateProviderChangeTarget(item.change_target);
  }
};

export const validateGoalCreateSemantics = async (command: SymmetryGoalCreateV1): Promise<void> => {
  validateGoalRevisionContractSemantics(command.initial_revision);
};

export const validateGoalCommandSemantics = async (
  command: SymmetryGoalCommandV1,
): Promise<void> => {
  switch (command.kind) {
    case "request_plan":
      if (command.payload.subject.resource_id !== command.payload.repository_resource_id) {
        throw new SemanticError(
          "request_plan_subject_resource_mismatch",
          `payload.subject.resource_id mismatch: ${command.payload.subject.resource_id} != ${command.payload.repository_resource_id}`,
        );
      }
      return;
    case "amend":
      validateGoalRevisionContractSemantics(command.payload.revision_contract);
      return;
    case "accept_plan": {
      await validatePlanProposalSemantics(command.payload.proposal);
      const expectedHash = await sha256(canonicalPlanProposal(command.payload.proposal));
      assertEqual("payload.proposal_hash", command.payload.proposal_hash, expectedHash);
      return;
    }
    case "request_decision":
      if (command.payload.kind === "plan") {
        await validatePlanProposalSemantics(command.payload.proposal);
      }
      return;
    default:
      return;
  }
};

export const validateAdmissionSemantics = async (admission: SymmetryAdmissionV1): Promise<void> => {
  if (admission.provider_scope === null) return;

  validateProviderChangeTarget(admission.provider_scope.change_target);

  const resourceIDs = new Set(admission.provider_scope.resource_ids);
  const operationResourceIDs = Object.keys(admission.provider_scope.operations_by_resource);
  if (resourceIDs.size !== operationResourceIDs.length) {
    throw new SemanticError(
      "semantic_mismatch",
      "provider_scope operation keys do not equal resource_ids",
    );
  }
  for (const resourceID of operationResourceIDs) {
    if (!resourceIDs.has(resourceID)) {
      throw new SemanticError(
        "semantic_mismatch",
        `provider_scope grants an unscoped resource ${resourceID}`,
      );
    }
  }
};

export const validateTaskResultSemantics = async (result: SymmetryTaskResultV1): Promise<void> => {
  assertEqual("subject_hash", result.subject_hash, await subjectHash(result.subject));
  if ((result.kind === "blocked") !== (result.blocker !== null)) {
    throw new SemanticError("semantic_mismatch", "only blocked results require a blocker");
  }
  if (result.kind === "plan_proposed") {
    await validatePlanProposalSemantics(result.proposal);
  }
};
