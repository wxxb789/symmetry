import fs from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import Ajv from "ajv";
import addFormats from "ajv-formats";
import { spawn } from "node:child_process";
import {
  SemanticError,
  validateAdmissionSemantics,
  validateContextSnapshotSemantics,
  validateDecisionSemantics,
  validateEvidenceSemantics,
  validateGoalCommandSemantics,
  validateGoalCreateSemantics,
  validateGoalRevisionSemantics,
  validatePlanProposalSemantics,
  validateTaskResultSemantics,
} from "../ts/semantics.ts";
import { assertUnicode, parseWireJson, WireJsonError } from "../ts/wire-json.ts";

const SCRIPT_DIR = path.dirname(fileURLToPath(import.meta.url));
const ROOT_DIR = path.resolve(SCRIPT_DIR, "../..");
const V1_DIR = path.join(ROOT_DIR, "contracts", "v1");
const MANIFEST_PATH = path.join(ROOT_DIR, "contracts", "fixtures", "manifest.json");

const relativePath = (filePath) => path.relative(ROOT_DIR, filePath);

const formatReadError = (filePath, error) => {
  const message = error instanceof Error ? error.message : String(error);
  return new Error(`${relativePath(filePath)} ${message}`);
};

const readJson = async (filePath) => {
  try {
    const value = parseWireJson(await fs.readFile(filePath));
    assertUnicode(value);
    return value;
  } catch (error) {
    throw formatReadError(filePath, error);
  }
};

const numberIssueFromError = (error) => {
  const marker = ": ";
  const separator = error.message.lastIndexOf(marker);
  if (separator < 0) return { lexeme: "", reason: error.message };
  return {
    reason: error.message.slice(0, separator),
    lexeme: error.message.slice(separator + marker.length),
  };
};

const readFixtureJson = async (filePath) => {
  let value;
  try {
    value = parseWireJson(await fs.readFile(filePath));
  } catch (error) {
    if (error instanceof WireJsonError && error.code === "noncanonical_number") {
      return { value: null, issue: numberIssueFromError(error) };
    }
    throw formatReadError(filePath, error);
  }

  try {
    assertUnicode(value);
  } catch (error) {
    if (error instanceof SemanticError && error.code === "unpaired_surrogate") {
      return { value, issue: null, unicodeIssue: error.message };
    }
    throw formatReadError(filePath, error);
  }
  return { value, issue: null, unicodeIssue: null };
};

const runGeneratorCheck = () =>
  new Promise((resolve, reject) => {
    const child = spawn(process.execPath, [path.join(SCRIPT_DIR, "generate.mjs"), "--check"], {
      cwd: ROOT_DIR,
      stdio: "inherit",
    });
    child.once("error", reject);
    child.once("exit", (code) =>
      code === 0 ? resolve() : reject(new Error(`generator check exited with ${code}`)),
    );
  });

const semanticValidators = {
  admission: validateAdmissionSemantics,
  "context-snapshot": validateContextSnapshotSemantics,
  decision: validateDecisionSemantics,
  evidence: validateEvidenceSemantics,
  "goal-command": validateGoalCommandSemantics,
  "goal-create": validateGoalCreateSemantics,
  "goal-revision": validateGoalRevisionSemantics,
  "plan-proposal": validatePlanProposalSemantics,
  "task-result": validateTaskResultSemantics,
};

const main = async () => {
  const common = await readJson(path.join(V1_DIR, "common.schema.json"));
  const schemaFiles = (await fs.readdir(V1_DIR))
    .filter((file) => file.endsWith(".schema.json") && file !== "common.schema.json")
    .sort();

  if (schemaFiles.length !== 11)
    throw new Error(`expected 11 v1 envelope schemas, found ${schemaFiles.length}`);
  if (common.$schema !== "http://json-schema.org/draft-07/schema#")
    throw new Error("common schema must be Draft 7");

  const ajv = new Ajv({ allErrors: true, strict: false, validateFormats: true });
  addFormats(ajv, ["date-time"]);
  ajv.addFormat(
    "uuid",
    /^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/u,
  );
  ajv.addSchema(common, common.$id);

  const schemas = new Map();
  for (const file of schemaFiles) {
    const schema = await readJson(path.join(V1_DIR, file));
    if (schema.$schema !== "http://json-schema.org/draft-07/schema#")
      throw new Error(`${file} must be Draft 7`);
    const commonDefinitionReference =
      typeof schema.$ref === "string"
        ? /^common\.schema\.json#\/definitions\/(.+)$/u.exec(schema.$ref)
        : null;
    const referencedDefinition = commonDefinitionReference
      ? common.definitions[commonDefinitionReference[1]]
      : null;
    const strictEnvelope =
      schema.additionalProperties === false ||
      (Array.isArray(schema.oneOf) &&
        schema.oneOf.length > 0 &&
        schema.oneOf.every((branch) => branch.additionalProperties === false)) ||
      referencedDefinition?.additionalProperties === false;
    if (!strictEnvelope) throw new Error(`${file} must reject unknown control fields`);
    ajv.addSchema(schema, schema.$id);
    schemas.set(file.slice(0, -".schema.json".length), schema);
  }

  const manifest = await readJson(MANIFEST_PATH);
  if (manifest.manifest_version !== 1 || !Array.isArray(manifest.fixtures))
    throw new Error("invalid fixture manifest header");
  const ids = new Set();
  const tags = new Set();
  for (const fixture of manifest.fixtures) {
    if (!fixture || typeof fixture !== "object" || typeof fixture.id !== "string")
      throw new Error("fixture entry requires id");
    if (ids.has(fixture.id)) throw new Error(`duplicate fixture id: ${fixture.id}`);
    ids.add(fixture.id);
    for (const tag of fixture.tags ?? []) tags.add(tag);
    const schema = schemas.get(fixture.schema);
    if (!schema)
      throw new Error(`fixture ${fixture.id} references unknown schema ${fixture.schema}`);
    const fixturePath = path.join(ROOT_DIR, "contracts", "fixtures", fixture.path);
    const fixtureDocument = await readFixtureJson(fixturePath);
    const expectedSchemaValid = fixture.schema_valid ?? fixture.valid;
    if (fixtureDocument.issue) {
      if (expectedSchemaValid) {
        throw new Error(
          `fixture ${fixture.id} expected schema_valid=true, but the fixture was rejected before JSON.parse: ${fixtureDocument.issue.reason}: ${fixtureDocument.issue.lexeme}`,
        );
      }
      continue;
    }
    const data = fixtureDocument.value;
    const validate = ajv.getSchema(schema.$id);
    if (!validate) throw new Error(`validator not registered for ${fixture.schema}`);
    const schemaValid = Boolean(validate(data));
    if (schemaValid !== Boolean(expectedSchemaValid)) {
      const details = (validate.errors ?? [])
        .map((error) => `${error.instancePath || "/"} ${error.message}`)
        .join("; ");
      throw new Error(
        `fixture ${fixture.id} expected schema_valid=${expectedSchemaValid}, got ${schemaValid}: ${details}`,
      );
    }

    let semanticValid = schemaValid;
    let semanticError = null;
    let semanticErrorCode = null;
    if (semanticValid && fixtureDocument.unicodeIssue) {
      semanticValid = false;
      semanticError = fixtureDocument.unicodeIssue;
      semanticErrorCode = "unpaired_surrogate";
    }
    const semanticValidator = semanticValidators[fixture.schema];
    if (semanticValid && semanticValidator) {
      try {
        await semanticValidator(data);
      } catch (error) {
        semanticValid = false;
        semanticError = error instanceof Error ? error.message : String(error);
        semanticErrorCode = error instanceof SemanticError ? error.code : null;
      }
    }
    const expectedSemanticValid = fixture.semantic_valid ?? fixture.valid;
    if (semanticValid !== Boolean(expectedSemanticValid)) {
      throw new Error(
        `fixture ${fixture.id} expected semantic_valid=${expectedSemanticValid}, got ${semanticValid}: ${semanticError || "no semantic error"}`,
      );
    }
    const expectedSemanticError = fixture.semantic_error ?? fixture.semantic_error_code ?? null;
    if (expectedSemanticError !== null) {
      if (semanticValid || semanticErrorCode !== expectedSemanticError) {
        throw new Error(
          `fixture ${fixture.id} expected semantic_error=${expectedSemanticError}, got ${semanticErrorCode || "none"}: ${semanticError || "no semantic error"}`,
        );
      }
    }
  }

  const requiredTags = [
    "missing-control-field",
    "extra-control-field",
    "wrong-identity",
    "unknown-enum",
    "nullable",
    "safe-integer-limit",
    "version-mismatch",
    "max-int64",
    "max-int64-overflow",
    "semantic-consistency",
    "source-identity",
    "context-self-hash",
    "context-self-hash-mismatch",
    "stable-sections",
    "required-reason",
    "candidate-subject",
    "task-result-subject-hash",
    "number-precision",
    "uuid-canonical",
    "uuid-version",
    "uuid-variant",
    "predicate-id-unique",
  ];
  const missingTags = requiredTags.filter((tag) => !tags.has(tag));
  if (missingTags.length > 0)
    throw new Error(`fixture coverage is missing tags: ${missingTags.join(", ")}`);

  await runGeneratorCheck();
  console.log(
    `Validated ${schemaFiles.length} Draft 7 schemas and ${manifest.fixtures.length} fixtures.`,
  );
};

try {
  await main();
} catch (error) {
  console.error(error instanceof Error ? error.message : error);
  process.exitCode = 1;
}
