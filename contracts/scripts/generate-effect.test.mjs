import assert from "node:assert/strict";
import { Buffer } from "node:buffer";
import test from "node:test";
import Ajv from "ajv";
import { Schema } from "effect";
import typescript from "typescript";
import { generateEffectSchemas } from "./generate-effect.mjs";
import {
  conditionalObject,
  notSchema,
  openObject,
  strictObject,
  strictOptions,
  withObjectPropertyBounds,
} from "../ts/schema-helpers.ts";

const schema = (overrides = {}) => ({
  file: "example.schema.json",
  key: "example",
  name: "Example",
  schema: {
    type: "object",
    additionalProperties: false,
    required: ["kind", "ids"],
    properties: {
      kind: { enum: ["first", "second"] },
      ids: {
        type: "array",
        minItems: 1,
        uniqueItems: true,
        items: { $ref: "#/definitions/Identifier" },
      },
    },
    definitions: {
      Identifier: { type: "string", minLength: 1, pattern: "^[a-z]+$" },
    },
    oneOf: [
      { properties: { kind: { const: "first" } }, required: ["kind"] },
      { properties: { kind: { const: "second" } }, required: ["kind"] },
    ],
    allOf: [{ not: { required: ["forbidden"] } }],
    ...overrides,
  },
});

const loadGeneratedGraph = async (entry, graphName) => {
  const [{ contents }] = generateEffectSchemas([entry]);
  const source = contents
    .replace('from "effect";', `from ${JSON.stringify(import.meta.resolve("effect"))};`)
    .replace(
      'from "../../ts/schema-helpers.ts";',
      `from ${JSON.stringify(new URL("../ts/schema-helpers.ts", import.meta.url).href)};`,
    );
  const transpiled = typescript.transpileModule(source, {
    compilerOptions: {
      module: typescript.ModuleKind.ESNext,
      target: typescript.ScriptTarget.ES2022,
      verbatimModuleSyntax: true,
    },
  });
  const generated = await import(
    `data:text/javascript;base64,${Buffer.from(transpiled.outputText).toString("base64")}`
  );
  return generated[graphName];
};

test("generates one native Effect graph with reusable definitions and typed facades", () => {
  const [{ fileName, contents }] = generateEffectSchemas([
    schema(),
    {
      file: "second.schema.json",
      key: "second",
      name: "Second",
      schema: {
        type: "object",
        properties: { identifier: { $ref: "#/definitions/Identifier" } },
        definitions: { Identifier: { type: "string", minLength: 1, pattern: "^[a-z]+$" } },
      },
    },
  ]);

  assert.equal(fileName, "index.ts");
  assert.match(contents, /export const DefinitionIdentifierGraph = S\.suspend/u);
  assert.equal((contents.match(/export const DefinitionIdentifierGraph/g) ?? []).length, 1);
  assert.match(contents, /S\.Struct\(/u);
  assert.match(contents, /S\.Array\(/u);
  assert.match(contents, /S\.Literal\(/u);
  assert.match(contents, /S\.optionalWith\([^\n]+\{ exact: true \}\)/u);
  assert.match(contents, /S\.Union\(/u);
  assert.match(contents, /exactOneOf\(/u);
  assert.match(contents, /allOf\(/u);
  assert.match(contents, /notSchema\(/u);
  assert.match(contents, /notSchema\(conditionalObject\(/u);
  assert.match(contents, /export const ExampleSchema: S\.Schema<SymmetryExampleV1, unknown>/u);
  assert.match(contents, /S\.filter\(isExample\)/u);
});

test("keeps a root graph distinct from an equally named common definition", () => {
  const [{ contents }] = generateEffectSchemas([
    {
      file: "plan-proposal.schema.json",
      key: "plan-proposal",
      name: "PlanProposal",
      schema: {
        type: "object",
        properties: { previous: { $ref: "#/definitions/PlanProposal" } },
        definitions: { PlanProposal: { type: "string" } },
      },
    },
  ]);

  assert.match(contents, /export const DefinitionPlanProposalGraph = S\.suspend/u);
  assert.match(contents, /export const PlanProposalGraph = S\.suspend/u);
  assert.equal((contents.match(/DefinitionPlanProposalGraph/g) ?? []).length, 2);
  assert.equal((contents.match(/\bPlanProposalGraph\b/g) ?? []).length, 2);
});

test("required-only schemas remain vacuous for non-objects before not negates them", () => {
  const requiresForbidden = conditionalObject(
    withObjectPropertyBounds(
      openObject(Schema.Struct({ forbidden: Schema.Unknown })),
      undefined,
      undefined,
    ),
  );
  const excludesForbidden = notSchema(requiresForbidden);
  const matches = Schema.is(excludesForbidden, strictOptions);

  assert.equal(matches("scalar"), false);
  assert.equal(matches(null), false);
  assert.equal(matches([]), false);
  assert.equal(matches({}), true);
  assert.equal(matches({ forbidden: true }), false);
});

test("exact optional properties reject explicit undefined", () => {
  const optionalProperty = strictObject(
    Schema.Struct({ optional: Schema.optionalWith(Schema.String, { exact: true }) }),
  );
  const matches = Schema.is(optionalProperty, strictOptions);

  assert.equal(matches({}), true);
  assert.equal(matches({ optional: undefined }), false);
});

test("required names outside properties obey additionalProperties", async () => {
  const oracle = new Ajv({ strict: false });
  for (const additionalProperties of [false, { type: "string" }]) {
    const source = {
      type: "object",
      additionalProperties,
      required: ["required"],
      properties: {},
    };
    const graph = await loadGeneratedGraph(
      {
        file: "additional.schema.json",
        key: "additional",
        name: "Additional",
        schema: source,
      },
      "AdditionalGraph",
    );
    const matches = Schema.is(graph, strictOptions);
    const validate = oracle.compile(source);
    for (const input of [{}, { required: "value" }, { required: 1 }, null, []]) {
      assert.equal(
        matches(input),
        validate(input),
        `generated graph mismatch for ${JSON.stringify(input)}`,
      );
    }
  }
});

test("rejects validation keywords outside the approved Draft 7 subset", () => {
  assert.throws(
    () => generateEffectSchemas([schema({ minContains: 1 })]),
    /minContains: unsupported JSON Schema keyword/u,
  );
});

test("rejects unsupported reference scope and validation siblings", () => {
  assert.throws(
    () =>
      generateEffectSchemas([
        {
          file: "reference-sibling.schema.json",
          key: "reference-sibling",
          name: "ReferenceSibling",
          schema: {
            definitions: { Identifier: { type: "string" } },
            $ref: "#/definitions/Identifier",
            minLength: 1,
          },
        },
      ]),
    /\$ref with validation siblings/u,
  );
  assert.throws(
    () =>
      generateEffectSchemas([schema({ properties: { child: { type: "string", $id: "child" } } })]),
    /nested \$id/u,
  );
  assert.throws(
    () =>
      generateEffectSchemas([
        schema({
          properties: { child: { type: "object", definitions: { Nested: { type: "string" } } } },
        }),
      ]),
    /nested definitions/u,
  );
});

test("allows rewritten root reference containers without validation siblings", () => {
  assert.doesNotThrow(() =>
    generateEffectSchemas([
      {
        file: "root-reference.schema.json",
        key: "root-reference",
        name: "RootReference",
        schema: {
          $schema: "http://json-schema.org/draft-07/schema#",
          $id: "contracts/v1/root-reference.schema.json",
          definitions: { Identifier: { type: "string" } },
          $ref: "#/definitions/Identifier",
        },
      },
    ]),
  );
});
