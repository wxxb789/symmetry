import assert from "node:assert/strict";
import fs from "node:fs/promises";
import { fileURLToPath } from "node:url";
import { Effect, Either, Schema } from "effect";
import * as Contracts from "../ts/index.ts";
import { parsePresentationJson, parseWireJson, WireJsonError } from "../ts/wire-json.ts";

const fixtureRoot = new URL("../fixtures/", import.meta.url);

// A class this decoder accepts today must be declared in the row's runtime_divergence.
const wireErrorCodes = new Map([
  ["duplicate_json_member", "duplicate_json_member"],
  ["noncanonical_number", "noncanonical_number"],
  ["unsafe_integer", "noncanonical_number"],
  ["leading_zero", "invalid_json"],
  ["nonfinite_number", "nonfinite_number"],
  ["invalid_utf8", "invalid_utf8"],
  ["trailing_value", "invalid_json"],
  ["trailing_garbage", "invalid_json"],
]);

// The corpus vocabulary, checked before either acceptance branch: an accepted
// row never consults wireErrorCodes, so an unknown class would pass vacuously.
const wireErrorClasses = new Set([
  "duplicate_json_member",
  "noncanonical_number",
  "unsafe_integer",
  "leading_zero",
  "negative_zero",
  "nonfinite_number",
  "invalid_utf8",
  "bom",
  "trailing_value",
  "trailing_garbage",
]);

const presentationReadSchemas = new Set([
  "attention-read",
  "chat-read",
  "goal-read",
  "work-detail-read",
  "work-timeline-read",
  "workspace-read",
  "connections-read",
]);
const decoders = {
  "adapter-capabilities": Contracts.AdapterCapabilities,
  admission: Contracts.Admission,
  "attention-read": Contracts.AttentionRead,
  "chat-read": Contracts.ChatRead,
  "connections-read": Contracts.ConnectionsRead,
  "context-snapshot": Contracts.ContextSnapshot,
  decision: Contracts.Decision,
  "evidence-batch-conflict-details": Contracts.EvidenceBatchConflictDetails,
  "evidence-batch-response": Contracts.EvidenceBatchResponse,
  "evidence-batch": Contracts.EvidenceBatch,
  evidence: Contracts.Evidence,
  "goal-command": Contracts.GoalCommand,
  "goal-create": Contracts.GoalCreate,
  "goal-read": Contracts.GoalRead,
  "goal-revision": Contracts.GoalRevision,
  "plan-proposal": Contracts.PlanProposal,
  "task-result": Contracts.TaskResult,
  usage: Contracts.Usage,
  "work-detail-read": Contracts.WorkDetailRead,
  "work-timeline-read": Contracts.WorkTimelineRead,
  "workspace-read": Contracts.WorkspaceRead,
};
const schemas = {
  "adapter-capabilities": Contracts.AdapterCapabilitiesSchema,
  admission: Contracts.AdmissionSchema,
  "attention-read": Contracts.AttentionReadSchema,
  "chat-read": Contracts.ChatReadSchema,
  "connections-read": Contracts.ConnectionsReadSchema,
  "context-snapshot": Contracts.ContextSnapshotSchema,
  decision: Contracts.DecisionSchema,
  "evidence-batch-conflict-details": Contracts.EvidenceBatchConflictDetailsSchema,
  "evidence-batch-response": Contracts.EvidenceBatchResponseSchema,
  "evidence-batch": Contracts.EvidenceBatchSchema,
  evidence: Contracts.EvidenceSchema,
  "goal-command": Contracts.GoalCommandSchema,
  "goal-create": Contracts.GoalCreateSchema,
  "goal-read": Contracts.GoalReadSchema,
  "goal-revision": Contracts.GoalRevisionSchema,
  "plan-proposal": Contracts.PlanProposalSchema,
  "task-result": Contracts.TaskResultSchema,
  usage: Contracts.UsageSchema,
  "work-detail-read": Contracts.WorkDetailReadSchema,
  "work-timeline-read": Contracts.WorkTimelineReadSchema,
  "workspace-read": Contracts.WorkspaceReadSchema,
};

const manifest = JSON.parse(await fs.readFile(new URL("manifest.json", fixtureRoot), "utf8"));
assert.equal(manifest.manifest_version, 1);
assert.ok(Array.isArray(manifest.fixtures));
let rawJsonChecks = 0;
let structuralChecks = 0;
const identifiers = new Set();
for (const fixture of manifest.fixtures) {
  assert.ok(!identifiers.has(fixture.id), `duplicate fixture: ${fixture.id}`);
  identifiers.add(fixture.id);
  const decoder = decoders[fixture.schema];
  const schema = schemas[fixture.schema];
  assert.ok(decoder && schema, `unknown schema for ${fixture.id}`);
  if (fixture.wire_error !== undefined)
    assert.ok(
      wireErrorClasses.has(fixture.wire_error),
      `unknown wire_error class for ${fixture.id}: ${fixture.wire_error}`,
    );
  const bytes = await fs.readFile(new URL(fixture.path, fixtureRoot));

  // Every negative fixture reaches the public raw-body decoder, including number lexemes.
  const result = await Effect.runPromise(Effect.either(decoder.decodeJson(bytes)));
  rawJsonChecks++;
  const semanticValid = Either.isRight(result);
  assert.equal(
    semanticValid,
    fixture.semantic_valid ?? fixture.valid,
    `Effect decodeJson semantic validity: ${fixture.id}`,
  );
  if (fixture.wire_error !== undefined) {
    const acceptedByTs = fixture.runtime_divergence?.ts === "accepts";
    assert.equal(semanticValid, acceptedByTs, `Effect raw wire validity: ${fixture.id}`);
    if (!acceptedByTs) {
      assert.ok(Either.isLeft(result), `expected a raw rejection for ${fixture.id}`);
      assert.equal(
        result.left.code,
        wireErrorCodes.get(fixture.wire_error),
        `Effect raw wire code: ${fixture.id}`,
      );
    }
  }

  let data;
  let structuralValid = false;
  try {
    const parseJson = presentationReadSchemas.has(fixture.schema)
      ? parsePresentationJson
      : parseWireJson;
    data = parseJson(bytes);
    structuralValid = Schema.is(schema)(data);
    structuralChecks++;
  } catch (error) {
    if (!(error instanceof WireJsonError)) throw error;
  }
  assert.equal(
    structuralValid,
    fixture.schema_valid ?? fixture.valid,
    `Effect structural validity: ${fixture.id}`,
  );
  if (structuralValid && !semanticValid) {
    assert.ok(Either.isLeft(result));
    assert.ok(
      result.left instanceof Contracts.SemanticError,
      `Effect semantic error type: ${fixture.id}`,
    );
  }
  if (semanticValid) {
    assert.deepEqual(result.right, data, `Effect changed wire values: ${fixture.id}`);
    const unknownResult = await Effect.runPromise(decoder.decodeUnknown(data));
    assert.deepEqual(
      unknownResult,
      data,
      `Effect decodeUnknown changed wire values: ${fixture.id}`,
    );
  }

  const expectedCode = fixture.semantic_error ?? fixture.semantic_error_code;
  if (expectedCode !== undefined && expectedCode !== null) {
    assert.ok(Either.isLeft(result), `expected failure code for ${fixture.id}`);
    assert.equal(result.left.code, expectedCode, `Effect error code: ${fixture.id}`);
  }
}

assert.equal(rawJsonChecks, manifest.fixtures.length);
console.log(
  `Effect decoded all ${rawJsonChecks} raw fixtures; ${structuralChecks} parsed documents reached Schema validation (${fileURLToPath(fixtureRoot)}).`,
);
