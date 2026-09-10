import fs from "node:fs/promises";
import path from "node:path";
import { createHash } from "node:crypto";
import { fileURLToPath } from "node:url";
import Ajv from "ajv";
import addFormats from "ajv-formats";
import { spawn } from "node:child_process";

const SCRIPT_DIR = path.dirname(fileURLToPath(import.meta.url));
const ROOT_DIR = path.resolve(SCRIPT_DIR, "../..");
const V1_DIR = path.join(ROOT_DIR, "contracts", "v1");
const MANIFEST_PATH = path.join(ROOT_DIR, "contracts", "fixtures", "manifest.json");

const jsonNumberPattern = /-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?/y;
const maxSafeInteger = BigInt(Number.MAX_SAFE_INTEGER);
const minSafeInteger = BigInt(Number.MIN_SAFE_INTEGER);
const utf8Decoder = new TextDecoder("utf-8", { fatal: true });

const findUnsafeJsonNumber = (source) => {
  let inString = false;
  let escaped = false;

  for (let index = 0; index < source.length; index += 1) {
    const character = source[index];
    if (inString) {
      if (escaped) {
        escaped = false;
      } else if (character === "\\") {
        escaped = true;
      } else if (character === '"') {
        inString = false;
      }
      continue;
    }
    if (character === '"') {
      inString = true;
      continue;
    }
    if (character !== "-" && (character < "0" || character > "9")) continue;

    jsonNumberPattern.lastIndex = index;
    const match = jsonNumberPattern.exec(source);
    if (!match) continue;
    const lexeme = match[0];
    if (lexeme.includes(".") || /[eE]/u.test(lexeme)) {
      return {lexeme, reason: "fractional or exponent number is not canonical"};
    }
    const integer = BigInt(lexeme);
    if (integer > maxSafeInteger || integer < minSafeInteger) {
      return {lexeme, reason: "integer exceeds the JSON safe-integer range"};
    }
    index += lexeme.length - 1;
  }
  return null;
};

const readUTF8 = async (filePath) => {
  const bytes = await fs.readFile(filePath);
  try {
    return utf8Decoder.decode(bytes);
  } catch {
    throw new Error(`${path.relative(ROOT_DIR, filePath)} is not valid UTF-8`);
  }
};

const findUnpairedSurrogate = (value, path = "$") => {
  if (typeof value === "string") {
    for (let index = 0; index < value.length; index += 1) {
      const codeUnit = value.charCodeAt(index);
      if (codeUnit >= 0xd800 && codeUnit <= 0xdbff) {
        const next = value.charCodeAt(index + 1);
        if (!Number.isInteger(next) || next < 0xdc00 || next > 0xdfff) {
          return `${path} contains an unpaired high surrogate`;
        }
        index += 1;
      } else if (codeUnit >= 0xdc00 && codeUnit <= 0xdfff) {
        return `${path} contains an unpaired low surrogate`;
      }
    }
    return null;
  }
  if (Array.isArray(value)) {
    for (let index = 0; index < value.length; index += 1) {
      const issue = findUnpairedSurrogate(value[index], `${path}[${index}]`);
      if (issue) return issue;
    }
    return null;
  }
  if (value && typeof value === "object") {
    for (const [key, child] of Object.entries(value)) {
      const issue = findUnpairedSurrogate(child, `${path}.${key}`);
      if (issue) return issue;
    }
  }
  return null;
};

const readJson = async (filePath) => {
  const source = await readUTF8(filePath);
  const issue = findUnsafeJsonNumber(source);
  if (issue) throw new Error(`${path.relative(ROOT_DIR, filePath)} contains ${issue.reason}: ${issue.lexeme}`);
  const value = JSON.parse(source);
  const unicodeIssue = findUnpairedSurrogate(value);
  if (unicodeIssue) throw new Error(`${path.relative(ROOT_DIR, filePath)} ${unicodeIssue}`);
  return value;
};

const readFixtureJson = async (filePath) => {
  const source = await readUTF8(filePath);
  const issue = findUnsafeJsonNumber(source);
  if (issue) return {value: null, issue};
  const value = JSON.parse(source);
  return {value, issue: null, unicodeIssue: findUnpairedSurrogate(value)};
};

const runGeneratorCheck = () =>
  new Promise((resolve, reject) => {
    const child = spawn(process.execPath, [path.join(SCRIPT_DIR, "generate.mjs"), "--check"], {
      cwd: ROOT_DIR,
      stdio: "inherit"
    });
    child.once("error", reject);
    child.once("exit", (code) => (code === 0 ? resolve() : reject(new Error(`generator check exited with ${code}`))));
  });

const uuidPattern = /^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/u;

class SemanticFixtureError extends Error {
  constructor(code, message) {
    super(message);
    this.name = "SemanticFixtureError";
    this.code = code;
  }
}

const canonicalize = (value) => {
  if (Array.isArray(value)) return value.map(canonicalize);
  if (value && typeof value === "object") {
    return Object.fromEntries(Object.keys(value).sort().map((key) => [key, canonicalize(value[key])]));
  }
  return value;
};

const canonicalJson = (value) => JSON.stringify(canonicalize(value));

const canonicalPlanProposal = (proposal) => ({
  ...proposal,
  items: proposal.items.map((item) => ({
    ...item,
    integration: item.integration ?? false,
    change_target: item.change_target ?? null
  }))
});

const subjectHash = (subject) =>
  `sha256:${createHash("sha256").update(canonicalJson(subject), "utf8").digest("hex")}`;

const assertEqual = (fixtureId, label, actual, expected, code = "semantic_mismatch") => {
  if (actual !== expected) {
    throw new SemanticFixtureError(code, `fixture ${fixtureId} ${label} mismatch: ${actual} != ${expected}`);
  }
};

const validateEvidenceSemantics = (fixtureId, evidence) => {
  const expectedHash = subjectHash(evidence.subject);
  assertEqual(fixtureId, "subject_hash", evidence.subject_hash, expectedHash);
  assertEqual(fixtureId, "payload.subject_hash", evidence.payload.subject_hash, evidence.subject_hash);
  assertEqual(fixtureId, "source_ref.subject_hash", evidence.source_ref.subject_hash, evidence.subject_hash);
  if (canonicalJson(evidence.payload.subject) !== canonicalJson(evidence.subject)) {
    throw new Error(`fixture ${fixtureId} payload.subject does not match evidence.subject`);
  }

  switch (evidence.kind) {
    case "check":
      assertEqual(fixtureId, "source_ref.validator_profile", evidence.source_ref.validator_profile, evidence.validator_profile);
      break;
    case "artifact":
      assertEqual(fixtureId, "source_ref.resource_id", evidence.source_ref.resource_id, evidence.payload.resource_id);
      assertEqual(fixtureId, "source_ref.commit", evidence.source_ref.commit, evidence.payload.commit);
      assertEqual(fixtureId, "source_ref.path", evidence.source_ref.path, evidence.payload.path);
      assertEqual(fixtureId, "subject.resource_id", evidence.payload.resource_id, evidence.subject.resource_id);
      assertEqual(fixtureId, "subject.commit", evidence.payload.commit, evidence.subject.commit);
      break;
    case "review":
      assertEqual(fixtureId, "source_ref.review_task_id", evidence.source_ref.review_task_id, evidence.payload.review_task_id);
      break;
    case "observation":
      assertEqual(fixtureId, "source_ref.external_ref", evidence.source_ref.external_ref, evidence.payload.external_ref);
      break;
    default:
      throw new Error(`fixture ${fixtureId} has unsupported evidence kind ${evidence.kind}`);
  }
};

const contextSnapshotHash = (snapshot) => {
  const { content_hash: _contentHash, ...withoutContentHash } = snapshot;
  return `sha256:${createHash("sha256").update(canonicalJson(withoutContentHash), "utf8").digest("hex")}`;
};

const validateContextSnapshotSemantics = (fixtureId, snapshot) => {
  const expectedHash = contextSnapshotHash(snapshot);
  assertEqual(fixtureId, "content_hash", snapshot.content_hash, expectedHash);
  validateAcceptancePredicateIDs(fixtureId, snapshot.work_contract.acceptance);
  validateProviderChangeTarget(fixtureId, snapshot.work_contract.change_target);
};

const validateGoalRevisionSemantics = (fixtureId, revision) => {
  validateGoalRevisionContractSemantics(fixtureId, revision);
};

const validateGoalRevisionContractSemantics = (fixtureId, revision) => {
  validateAcceptancePredicateIDs(fixtureId, revision.acceptance_contract);
  if (
    revision.execution_policy.final_acceptance === "deterministic" &&
    revision.acceptance_contract.predicates.some(
      (predicate) => predicate.kind !== "check" && predicate.kind !== "artifact"
    )
  ) {
    throw new Error(`fixture ${fixtureId} deterministic acceptance includes a non-machine predicate`);
  }
};

const validatePlanProposalSemantics = (fixtureId, proposal) => {
  for (const item of proposal.items) {
    validateAcceptancePredicateIDs(fixtureId, item.acceptance);
    validateProviderChangeTarget(fixtureId, item.change_target);
  }
};

const validateGoalCreateSemantics = (fixtureId, command) => {
  validateGoalRevisionContractSemantics(fixtureId, command.initial_revision);
};

const validateGoalCommandSemantics = (fixtureId, command) => {
  switch (command.kind) {
    case "request_plan":
      if (command.payload.subject.resource_id !== command.payload.repository_resource_id) {
        throw new SemanticFixtureError(
          "request_plan_subject_resource_mismatch",
          `fixture ${fixtureId} payload.subject.resource_id mismatch: ${command.payload.subject.resource_id} != ${command.payload.repository_resource_id}`
        );
      }
      return;
    case "amend":
      validateGoalRevisionContractSemantics(fixtureId, command.payload.revision_contract);
      return;
    case "accept_plan": {
      validatePlanProposalSemantics(fixtureId, command.payload.proposal);
      const expectedHash = `sha256:${createHash("sha256")
        .update(canonicalJson(canonicalPlanProposal(command.payload.proposal)), "utf8")
        .digest("hex")}`;
      assertEqual(fixtureId, "payload.proposal_hash", command.payload.proposal_hash, expectedHash);
      return;
    }
    case "request_decision":
      if (command.payload.kind === "plan") {
        validatePlanProposalSemantics(fixtureId, command.payload.proposal);
      }
      return;
    default:
      return;
  }
};

const validateAdmissionSemantics = (fixtureId, admission) => {
  if (admission.provider_scope === null) return;

  validateProviderChangeTarget(fixtureId, admission.provider_scope.change_target);

  const resourceIDs = new Set(admission.provider_scope.resource_ids);
  const operationResourceIDs = Object.keys(admission.provider_scope.operations_by_resource);
  if (resourceIDs.size !== operationResourceIDs.length) {
    throw new Error(`fixture ${fixtureId} provider_scope operation keys do not equal resource_ids`);
  }
  for (const resourceID of operationResourceIDs) {
    if (!resourceIDs.has(resourceID)) {
      throw new Error(`fixture ${fixtureId} provider_scope grants an unscoped resource ${resourceID}`);
    }
  }
};

const validateProviderChangeTarget = (fixtureId, target) => {
  if (target === null || target.kind !== "branches") return;
  if (target.source_branch === target.target_branch) {
    throw new Error(`fixture ${fixtureId} provider change target branches must differ`);
  }
};

const validateAcceptancePredicateIDs = (fixtureId, acceptance) => {
  const ids = new Set();
  for (const predicate of acceptance.predicates) {
    if (ids.has(predicate.id)) {
      throw new Error(`fixture ${fixtureId} contains duplicate acceptance predicate id ${predicate.id}`);
    }
    ids.add(predicate.id);
  }
};

const validateTaskResultSemantics = (fixtureId, result) => {
  assertEqual(fixtureId, "subject_hash", result.subject_hash, subjectHash(result.subject));
  const terminalReasons = new Set([
    "auth",
    "quota",
    "rate_limit",
    "network",
    "unsupported_version",
    "resume_rejected",
    "handoff_unsupported",
    "context_overflow",
    "missing_result",
    "process_failure",
    "cancelled",
    "unknown_outcome"
  ]);
  if (result.kind === "failed") {
    if (!terminalReasons.has(result.reason)) {
      throw new Error(`fixture ${fixtureId} failed result requires a terminal reason`);
    }
  } else if (result.reason !== null) {
    throw new Error(`fixture ${fixtureId} non-failed result reason must be null`);
  }
};

const main = async () => {
  const common = await readJson(path.join(V1_DIR, "common.schema.json"));
  const schemaFiles = (await fs.readdir(V1_DIR))
    .filter((file) => file.endsWith(".schema.json") && file !== "common.schema.json")
    .sort();

  if (schemaFiles.length !== 11) throw new Error(`expected 11 v1 envelope schemas, found ${schemaFiles.length}`);
  if (common.$schema !== "http://json-schema.org/draft-07/schema#") throw new Error("common schema must be Draft 7");

  const ajv = new Ajv({ allErrors: true, strict: false, validateFormats: true });
  addFormats(ajv, ["date-time"]);
  ajv.addFormat("uuid", uuidPattern);
  ajv.addSchema(common, common.$id);

  const schemas = new Map();
  for (const file of schemaFiles) {
    const schema = await readJson(path.join(V1_DIR, file));
    if (schema.$schema !== "http://json-schema.org/draft-07/schema#") throw new Error(`${file} must be Draft 7`);
    const commonDefinitionReference = typeof schema.$ref === "string"
      ? /^common\.schema\.json#\/definitions\/(.+)$/u.exec(schema.$ref)
      : null;
    const referencedDefinition = commonDefinitionReference
      ? common.definitions[commonDefinitionReference[1]]
      : null;
    const strictEnvelope = schema.additionalProperties === false ||
      (Array.isArray(schema.oneOf) && schema.oneOf.length > 0 && schema.oneOf.every((branch) => branch.additionalProperties === false)) ||
      referencedDefinition?.additionalProperties === false;
    if (!strictEnvelope) throw new Error(`${file} must reject unknown control fields`);
    ajv.addSchema(schema, schema.$id);
    schemas.set(file.slice(0, -".schema.json".length), schema);
  }

  const manifest = await readJson(MANIFEST_PATH);
  if (manifest.manifest_version !== 1 || !Array.isArray(manifest.fixtures)) throw new Error("invalid fixture manifest header");
  const ids = new Set();
  const tags = new Set();
  for (const fixture of manifest.fixtures) {
    if (!fixture || typeof fixture !== "object" || typeof fixture.id !== "string") throw new Error("fixture entry requires id");
    if (ids.has(fixture.id)) throw new Error(`duplicate fixture id: ${fixture.id}`);
    ids.add(fixture.id);
    for (const tag of fixture.tags ?? []) tags.add(tag);
    const schema = schemas.get(fixture.schema);
    if (!schema) throw new Error(`fixture ${fixture.id} references unknown schema ${fixture.schema}`);
    const fixturePath = path.join(ROOT_DIR, "contracts", "fixtures", fixture.path);
    const fixtureDocument = await readFixtureJson(fixturePath);
    const expectedSchemaValid = fixture.schema_valid ?? fixture.valid;
    if (fixtureDocument.issue) {
      if (expectedSchemaValid) {
        throw new Error(
          `fixture ${fixture.id} expected schema_valid=true, but the fixture was rejected before JSON.parse: ${fixtureDocument.issue.reason}: ${fixtureDocument.issue.lexeme}`
        );
      }
      continue;
    }
    const data = fixtureDocument.value;
    const validate = ajv.getSchema(schema.$id);
    if (!validate) throw new Error(`validator not registered for ${fixture.schema}`);
    const schemaValid = Boolean(validate(data));
    if (schemaValid !== Boolean(expectedSchemaValid)) {
      const details = (validate.errors ?? []).map((error) => `${error.instancePath || "/"} ${error.message}`).join("; ");
      throw new Error(`fixture ${fixture.id} expected schema_valid=${expectedSchemaValid}, got ${schemaValid}: ${details}`);
    }

    let semanticValid = schemaValid;
    let semanticError = null;
    let semanticErrorCode = null;
    if (semanticValid && fixtureDocument.unicodeIssue) {
      semanticValid = false;
      semanticError = fixtureDocument.unicodeIssue;
      semanticErrorCode = "unpaired_surrogate";
    }
    const semanticValidator = {
      admission: validateAdmissionSemantics,
      "context-snapshot": validateContextSnapshotSemantics,
      evidence: validateEvidenceSemantics,
      "goal-command": validateGoalCommandSemantics,
      "goal-create": validateGoalCreateSemantics,
      "goal-revision": validateGoalRevisionSemantics,
      "plan-proposal": validatePlanProposalSemantics,
      "task-result": validateTaskResultSemantics
    }[fixture.schema];
    if (semanticValid && semanticValidator) {
      try {
        semanticValidator(fixture.id, data);
      } catch (error) {
        semanticValid = false;
        semanticError = error instanceof Error ? error.message : String(error);
        semanticErrorCode = error instanceof SemanticFixtureError ? error.code : null;
      }
    }
    const expectedSemanticValid = fixture.semantic_valid ?? fixture.valid;
    if (semanticValid !== Boolean(expectedSemanticValid)) {
      throw new Error(`fixture ${fixture.id} expected semantic_valid=${expectedSemanticValid}, got ${semanticValid}: ${semanticError || "no semantic error"}`);
    }
    const expectedSemanticError = fixture.semantic_error ?? fixture.semantic_error_code ?? null;
    if (expectedSemanticError !== null) {
      if (semanticValid || semanticErrorCode !== expectedSemanticError) {
        throw new Error(
          `fixture ${fixture.id} expected semantic_error=${expectedSemanticError}, got ${semanticErrorCode || "none"}: ${semanticError || "no semantic error"}`
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
    "predicate-id-unique"
  ];
  const missingTags = requiredTags.filter((tag) => !tags.has(tag));
  if (missingTags.length > 0) throw new Error(`fixture coverage is missing tags: ${missingTags.join(", ")}`);

  await runGeneratorCheck();
  console.log(`Validated ${schemaFiles.length} Draft 7 schemas and ${manifest.fixtures.length} fixtures.`);
};

try {
  await main();
} catch (error) {
  console.error(error instanceof Error ? error.message : error);
  process.exitCode = 1;
}
