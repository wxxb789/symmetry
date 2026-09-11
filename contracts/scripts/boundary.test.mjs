import assert from "node:assert/strict";
import fs from "node:fs/promises";
import test from "node:test";
import Ajv from "ajv";
import addFormats from "ajv-formats";
import { Effect, Either, Schema } from "effect";
import {
  Admission,
  GoalCreate,
  PlanProposal,
  TaskResult,
  Usage,
  SemanticError,
  WireJsonError,
} from "../ts/index.ts";
import {
  allOf,
  arrayConstraints,
  exactOneOf,
  openObject,
  openObjectWithAdditionalProperties,
  strictObject,
  stringConstraints,
  withObjectPropertyBounds,
} from "../ts/schema-helpers.ts";

const fixture = async (name) =>
  JSON.parse(await fs.readFile(new URL(`../fixtures/valid/${name}.json`, import.meta.url), "utf8"));
const ajv = new Ajv({ strict: false });
addFormats(ajv);

function agreesWithDraft7(jsonSchema, graph, inputs) {
  const oracle = ajv.compile(jsonSchema);
  const matches = Schema.is(graph);
  for (const input of inputs) {
    assert.equal(matches(input), oracle(input), `Draft 7 mismatch for ${JSON.stringify(input)}`);
  }
}

test("raw decoding rejects unsafe numeric lexemes before schema validation", async () => {
  for (const lexeme of ["9007199254740993", "1.0000000000000001", "1e0", "-9007199254740992"]) {
    const result = await Effect.runPromise(
      Effect.either(Admission.decodeJson(`{"goal_revision":${lexeme}}`)),
    );
    assert.ok(Either.isLeft(result));
    assert.ok(result.left instanceof WireJsonError);
    assert.equal(result.left.code, "noncanonical_number");
  }
});

test("raw decoding classifies malformed JSON before schema validation", async () => {
  for (const source of ["", "{", "{} trailing", '{"key":}']) {
    const result = await Effect.runPromise(Effect.either(Admission.decodeJson(source)));
    assert.ok(Either.isLeft(result));
    assert.ok(result.left instanceof WireJsonError);
    assert.equal(result.left.code, "invalid_json");
  }
});

test("raw decoding preserves numeric-looking lexemes inside escaped strings", async () => {
  const goal = await fixture("goal-create.basic");
  goal.title = 'Keep "9007199254740993" and \\1e0 as text.';
  const decoded = await Effect.runPromise(GoalCreate.decodeJson(JSON.stringify(goal)));
  assert.equal(decoded.title, goal.title);
});

test("raw decoding rejects malformed UTF-8 instead of substituting replacement text", async () => {
  const result = await Effect.runPromise(
    Effect.either(Admission.decodeJson(new Uint8Array([0xc0, 0xaf]))),
  );
  assert.ok(Either.isLeft(result));
  assert.ok(result.left instanceof WireJsonError);
  assert.equal(result.left.code, "invalid_utf8");
});

test("typed boundaries reject unpaired surrogates in values and record keys", async () => {
  const goal = await fixture("goal-create.basic");
  goal.title = "\ud800";
  const admission = await fixture("admission.provider-scope");
  admission.provider_scope.operations_by_resource["\udfff"] = ["resource.sync"];
  for (const operation of [GoalCreate.decodeUnknown(goal), Admission.decodeUnknown(admission)]) {
    const result = await Effect.runPromise(Effect.either(operation));
    assert.ok(Either.isLeft(result));
    assert.ok(result.left instanceof SemanticError);
    assert.equal(result.left.code, "unpaired_surrogate");
  }
});

test("decoders preserve decimal strings and omitted optional fields", async () => {
  const usage = await fixture("usage.microusd-max-int64");
  const decodedUsage = await Effect.runPromise(Usage.decodeJson(JSON.stringify(usage)));
  assert.deepEqual(decodedUsage, usage);
  assert.equal(typeof decodedUsage.cost_microusd, "string");
  const proposal = await fixture("plan-proposal.integration-omitted");
  const decoded = await Effect.runPromise(PlanProposal.decodeUnknown(proposal));
  assert.deepEqual(decoded, proposal);
  assert.equal(Object.hasOwn(decoded.items[0], "integration"), false);
});

test("asynchronous hash mismatches remain typed semantic failures", async () => {
  const candidate = await fixture("task-result.candidate");
  candidate.subject_hash = "sha256:" + "f".repeat(64);
  const result = await Effect.runPromise(Effect.either(TaskResult.decodeUnknown(candidate)));
  assert.ok(Either.isLeft(result));
  assert.ok(result.left instanceof SemanticError);
  assert.equal(result.left._tag, "SemanticError");
  assert.equal(result.left.code, "semantic_mismatch");
});

test("TaskResult reason shape is rejected at the schema boundary", async () => {
  const failed = await fixture("task-result.failed");
  failed.reason = null;
  const candidate = await fixture("task-result.candidate");
  candidate.reason = "process_failure";
  for (const invalid of [failed, candidate]) {
    const result = await Effect.runPromise(Effect.either(TaskResult.decodeUnknown(invalid)));
    assert.ok(Either.isLeft(result));
    assert.equal(result.left._tag, "ParseError");
  }
});

test("admission decoding rejects extra fields and discriminator conflicts", async () => {
  const source = await fixture("admission.basic");
  for (const patch of [
    { undeclared_control: true },
    { handoff_source_run_id: undefined },
    { session_mode: "resume", requested_session_id: null },
    { session_mode: "handoff", handoff_source_run_id: source.admission_id, purpose: "plan" },
  ]) {
    const result = await Effect.runPromise(
      Effect.either(Admission.decodeUnknown({ ...source, ...patch })),
    );
    assert.ok(Either.isLeft(result), `accepted invalid admission ${JSON.stringify(patch)}`);
  }
});

test("oneOf rejects overlapping branches while allOf preserves partial-object fields", () => {
  agreesWithDraft7(
    { oneOf: [{ type: "string" }, { const: "overlap" }] },
    exactOneOf(
      Schema.Union(Schema.String, Schema.Literal("overlap")),
      Schema.String,
      Schema.Literal("overlap"),
    ),
    ["one branch", "overlap", 1, null],
  );
  const left = openObject(Schema.Struct({ kind: Schema.Literal("work") }));
  const right = openObject(Schema.Struct({ version: Schema.Int }));
  agreesWithDraft7(
    {
      type: "object",
      allOf: [
        { properties: { kind: { const: "work" } }, required: ["kind"] },
        { properties: { version: { type: "integer" } }, required: ["version"] },
      ],
    },
    allOf(left, right),
    [{ kind: "work", version: 1, extra: true }, { kind: "work" }, { version: 1 }, null],
  );
});

test("additionalProperties validates only undeclared keys and respects property bounds", () => {
  const graph = withObjectPropertyBounds(
    openObjectWithAdditionalProperties(
      Schema.Struct({ name: Schema.String }),
      ["name"],
      Schema.Int,
    ),
    1,
    2,
  );
  agreesWithDraft7(
    {
      type: "object",
      properties: { name: { type: "string" } },
      required: ["name"],
      additionalProperties: { type: "integer" },
      minProperties: 1,
      maxProperties: 2,
    },
    graph,
    [
      { name: "work" },
      { name: "work", attempts: 1 },
      { name: "work", attempts: "1" },
      { name: "work", a: 1, b: 2 },
      {},
    ],
  );
  assert.equal(
    Schema.is(strictObject(Schema.Struct({ name: Schema.String })))({ name: "work", extra: 1 }),
    false,
  );
});

test("string bounds count Unicode code points and dates use the Draft 7 format", () => {
  agreesWithDraft7(
    { type: "string", minLength: 1, maxLength: 1 },
    stringConstraints(Schema.String, 1, 1, undefined),
    ["", "a", "\ud83d\ude80", "ab", "\ud83d\ude80\ud83d\ude80"],
  );
  agreesWithDraft7(
    { type: "string", format: "date-time" },
    stringConstraints(Schema.String, undefined, undefined, "date-time"),
    [
      "2024-02-29T00:00:00Z",
      "2023-02-29T00:00:00Z",
      "2026-09-11T24:00:00Z",
      "2026-09-11T12:00:00+02:00",
      "2016-12-31T23:59:60Z",
    ],
  );
});

test("array bounds and uniqueItems use JSON equality", () => {
  agreesWithDraft7(
    { type: "array", items: {}, minItems: 1, maxItems: 3, uniqueItems: true },
    arrayConstraints(Schema.Array(Schema.Unknown), 1, 3, true),
    [
      [],
      [1],
      [1, 2, 3, 4],
      [0, -0],
      [
        { a: 1, b: 2 },
        { b: 2, a: 1 },
      ],
      [[1], [1]],
      [1, "1"],
    ],
  );
});
