import assert from "node:assert/strict";
import fs from "node:fs/promises";
import { fileURLToPath } from "node:url";
import { Effect, Either, Schema } from "effect";
import * as Contracts from "../ts/index.ts";
import { parseWireJson, WireJsonError } from "../ts/wire-json.ts";

const fixtureRoot = new URL("../fixtures/", import.meta.url);
const decoders = {
  "adapter-capabilities": Contracts.AdapterCapabilities,
  admission: Contracts.Admission,
  "context-snapshot": Contracts.ContextSnapshot,
  decision: Contracts.Decision,
  evidence: Contracts.Evidence,
  "goal-command": Contracts.GoalCommand,
  "goal-create": Contracts.GoalCreate,
  "goal-revision": Contracts.GoalRevision,
  "plan-proposal": Contracts.PlanProposal,
  "task-result": Contracts.TaskResult,
  usage: Contracts.Usage,
};
const schemas = {
  "adapter-capabilities": Contracts.AdapterCapabilitiesSchema,
  admission: Contracts.AdmissionSchema,
  "context-snapshot": Contracts.ContextSnapshotSchema,
  decision: Contracts.DecisionSchema,
  evidence: Contracts.EvidenceSchema,
  "goal-command": Contracts.GoalCommandSchema,
  "goal-create": Contracts.GoalCreateSchema,
  "goal-revision": Contracts.GoalRevisionSchema,
  "plan-proposal": Contracts.PlanProposalSchema,
  "task-result": Contracts.TaskResultSchema,
  usage: Contracts.UsageSchema,
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

  let data;
  let structuralValid = false;
  try {
    data = parseWireJson(bytes);
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
