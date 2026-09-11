import fs from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { spawn } from "node:child_process";
import { compile } from "json-schema-to-typescript";
import { generateEffectSchemas } from "./generate-effect.mjs";
import { FetchingJSONSchemaStore, InputData, JSONSchemaInput, quicktype } from "quicktype-core";

const SCRIPT_DIR = path.dirname(fileURLToPath(import.meta.url));
const ROOT_DIR = path.resolve(SCRIPT_DIR, "../..");
const V1_DIR = path.join(ROOT_DIR, "contracts", "v1");
const GENERATED_TS_DIR = path.join(ROOT_DIR, "contracts", "generated", "ts");
const GENERATED_EFFECT_DIR = path.join(ROOT_DIR, "contracts", "generated", "effect");
const GENERATED_GO_DIR = path.join(ROOT_DIR, "daemon", "internal", "contracts");

const toPascalCase = (value) =>
  value
    .split(/[-_]/u)
    .filter(Boolean)
    .map((part) => part[0].toUpperCase() + part.slice(1))
    .join("");

const clone = (value) => JSON.parse(JSON.stringify(value));

const rewriteCommonRefs = (value) => {
  if (Array.isArray(value)) return value.map(rewriteCommonRefs);
  if (!value || typeof value !== "object") return value;

  const result = {};
  for (const [key, child] of Object.entries(value)) {
    if (
      key === "$ref" &&
      typeof child === "string" &&
      child.startsWith("common.schema.json#/definitions/")
    ) {
      result[key] = `#/definitions/${child.slice("common.schema.json#/definitions/".length)}`;
    } else {
      result[key] = rewriteCommonRefs(child);
    }
  }
  return result;
};

const readJson = async (filePath) => JSON.parse(await fs.readFile(filePath, "utf8"));

const loadSchemas = async () => {
  const common = await readJson(path.join(V1_DIR, "common.schema.json"));
  const files = (await fs.readdir(V1_DIR))
    .filter((file) => file.endsWith(".schema.json") && file !== "common.schema.json")
    .sort();

  const schemas = [];
  for (const file of files) {
    const key = file.slice(0, -".schema.json".length);
    const schema = rewriteCommonRefs(await readJson(path.join(V1_DIR, file)));
    schema.definitions = {
      ...clone(common.definitions),
      ...(schema.definitions ?? {}),
    };
    schema.definitions = rewriteCommonRefs(schema.definitions);
    schemas.push({ file, key, name: toPascalCase(key), schema });
  }
  return schemas;
};

const expectedFiles = (schemas) => [
  "index.ts",
  ...schemas.map(({ key }) => `${key}.ts`),
  "generated.go",
  "schema_bundle.json",
];

const normalizeGoImports = (contents) => {
  const imports = [];
  const body = [];
  for (const line of contents.split("\n")) {
    if (/^import\s+"[^"]+"$/u.test(line.trim())) imports.push(line.trim());
    else body.push(line);
  }
  if (imports.length === 0)
    return body
      .join("\n")
      .replace(/\n{3,}/gu, "\n\n")
      .trimEnd();
  const packageIndex = body.findIndex((line) => line.startsWith("package "));
  if (packageIndex < 0)
    throw new Error("quicktype Go output did not contain a package declaration");
  body.splice(packageIndex + 1, 0, "", ...imports, "");
  return body
    .join("\n")
    .replace(/\n{3,}/gu, "\n\n")
    .trimEnd();
};

const formatGo = (contents) =>
  new Promise((resolve, reject) => {
    const child = spawn("gofmt", [], { stdio: ["pipe", "pipe", "pipe"] });
    const output = [];
    const errors = [];
    child.stdout.on("data", (chunk) => output.push(chunk));
    child.stderr.on("data", (chunk) => errors.push(chunk));
    child.once("error", reject);
    child.once("exit", (code) => {
      if (code === 0) {
        resolve(Buffer.concat(output).toString("utf8"));
        return;
      }
      reject(new Error(`gofmt exited with ${code}: ${Buffer.concat(errors).toString("utf8")}`));
    });
    child.stdin.end(contents, "utf8");
  });

// JSON Schema date-time is a wire string. Quicktype's Go renderer otherwise
// converts it to time.Time, which changes its JSON form on a marshal round-trip
// and makes canonical hash verification depend on a parsed timestamp.
const preserveGoWireTimestamps = (contents) =>
  contents.replace(/^import "time"\n\n/mu, "").replace(/\btime\.Time\b/gu, "string");

// Quicktype flattens the ExecutionPolicy oneOf branches and can lose the
// top-level nullable Microusd alternatives. Keep those wire-nullable fields as
// pointers so generated DTO decoding distinguishes null from an empty string.
const preserveGoNullableMicrousd = (contents) =>
  contents.replace(
    /^(\s*)(BudgetLimitMicrousd|PerRunCostLimitMicrousd)(\s+)string(\s+`json:[^`]+`)/gmu,
    "$1$2$3*string$4",
  );

// json-schema-to-typescript preserves the Draft 7 allOf validation shape for
// Admission as an intersection, which otherwise leaves the handoff-only source
// field optional in every TypeScript branch. Preserve the wire schema and emit
// its session discriminator directly in the generated public type.
const preserveAdmissionSessionDiscriminator = (contents) => {
  const declaration = "export type SymmetryAdmissionV1 =";
  if (!contents.includes(declaration)) {
    throw new Error("Admission TypeScript output did not contain its expected type declaration");
  }

  return `${contents.replace(declaration, "type SymmetryAdmissionV1Base =")}
export type SymmetryAdmissionV1 = SymmetryAdmissionV1Base &
  (
    | {
        session_mode: "fresh";
        requested_session_id: null;
        handoff_source_run_id?: never;
      }
    | {
        session_mode: "resume";
        requested_session_id: UUID;
        handoff_source_run_id?: never;
      }
    | {
        session_mode: "handoff";
        requested_session_id: null;
        handoff_source_run_id: UUID;
        purpose: "implement" | "validate" | "observe" | "chat";
        work_item_id: UUID;
      }
  );
`;
};

const canonicalize = (value) => {
  if (Array.isArray(value)) return value.map(canonicalize);
  if (!value || typeof value !== "object") return value;

  return Object.fromEntries(
    Object.keys(value)
      .sort()
      .map((key) => [key, canonicalize(value[key])]),
  );
};

const writeOrCheck = async (filePath, contents, check) => {
  if (check) {
    let current;
    try {
      current = await fs.readFile(filePath, "utf8");
    } catch {
      throw new Error(`generated file is missing: ${path.relative(ROOT_DIR, filePath)}`);
    }
    if (current !== contents) {
      throw new Error(`generated file is out of date: ${path.relative(ROOT_DIR, filePath)}`);
    }
    return;
  }
  await fs.mkdir(path.dirname(filePath), { recursive: true });
  await fs.writeFile(filePath, contents, "utf8");
};

const assertNoUnexpectedGeneratedFiles = async (schemas, check) => {
  if (!check) return;
  const expected = new Set(expectedFiles(schemas));
  const files = [
    ...(await fs.readdir(GENERATED_TS_DIR).catch(() => [])),
    ...(await fs.readdir(GENERATED_GO_DIR).catch(() => [])).filter(
      (file) => file === "generated.go" || file === "schema_bundle.json",
    ),
  ];
  const unexpected = files.filter((file) => !expected.has(file));
  if (unexpected.length > 0) {
    throw new Error(`unexpected generated files: ${unexpected.join(", ")}`);
  }
};

const generate = async (check = false) => {
  const schemas = await loadSchemas();
  if (schemas.length !== 11) {
    throw new Error(`expected 11 v1 envelope schemas, found ${schemas.length}`);
  }

  const effectFiles = generateEffectSchemas(schemas);

  const generatedTypes = [];
  for (const { key, name, schema } of schemas) {
    const types = await compile(schema, name, {
      bannerComment: "/* Code generated by contracts/scripts/generate.mjs; DO NOT EDIT. */",
      format: true,
      style: { bracketSpacing: true },
      strictIndexSignatures: true,
    });
    const contents = key === "admission" ? preserveAdmissionSessionDiscriminator(types) : types;
    generatedTypes.push({ key, contents: contents.endsWith("\n") ? contents : `${contents}\n` });
  }

  const indexContents = [
    "/* Code generated by contracts/scripts/generate.mjs; DO NOT EDIT. */",
    ...schemas.map(({ key, name }) => `export type { Symmetry${name}V1 } from \"./${key}.js\";`),
    "",
  ].join("\n");

  const schemaInput = new JSONSchemaInput(new FetchingJSONSchemaStore());
  for (const { name, schema } of schemas) {
    await schemaInput.addSource({ name, schema: JSON.stringify(schema) });
  }
  const inputData = new InputData();
  inputData.addInput(schemaInput);
  const goResult = await quicktype({
    inputData,
    lang: "go",
    rendererOptions: {
      package: "contracts",
      "just-types-and-package": "true",
    },
  });
  const normalizedGo = preserveGoNullableMicrousd(
    preserveGoWireTimestamps(normalizeGoImports(goResult.lines.join("\n"))),
  );
  const goContents = await formatGo(
    [
      "// Code generated by contracts/scripts/generate.mjs; DO NOT EDIT.",
      "",
      normalizedGo,
      "",
    ].join("\n"),
  );
  const schemaBundleContents = `${JSON.stringify(
    Object.fromEntries(schemas.map(({ file, schema }) => [file, canonicalize(schema)])),
    null,
    2,
  )}\n`;

  for (const { key, contents } of generatedTypes) {
    await writeOrCheck(path.join(GENERATED_TS_DIR, `${key}.ts`), contents, check);
  }
  await writeOrCheck(path.join(GENERATED_TS_DIR, "index.ts"), indexContents, check);
  for (const { fileName, contents } of effectFiles) {
    await writeOrCheck(path.join(GENERATED_EFFECT_DIR, fileName), contents, check);
  }
  const expectedEffect = new Set(effectFiles.map(({ fileName }) => fileName));
  for (const file of await fs.readdir(GENERATED_EFFECT_DIR)) {
    if (expectedEffect.has(file)) continue;
    if (check) throw new Error(`unexpected generated Effect file: ${file}`);
    if (file.endsWith(".ts")) await fs.unlink(path.join(GENERATED_EFFECT_DIR, file));
  }
  await writeOrCheck(path.join(GENERATED_GO_DIR, "generated.go"), goContents, check);
  await writeOrCheck(
    path.join(GENERATED_GO_DIR, "schema_bundle.json"),
    schemaBundleContents,
    check,
  );
  await assertNoUnexpectedGeneratedFiles(schemas, check);

  if (!check) {
    const expectedTs = new Set(["index.ts", ...generatedTypes.map(({ key }) => `${key}.ts`)]);
    for (const file of await fs.readdir(GENERATED_TS_DIR)) {
      if (file.endsWith(".ts") && !expectedTs.has(file))
        await fs.unlink(path.join(GENERATED_TS_DIR, file));
    }
  }
};

const check = process.argv.includes("--check");
try {
  await generate(check);
  console.log(check ? "Generated contract DTOs are up to date." : "Generated contract DTOs.");
} catch (error) {
  console.error(error instanceof Error ? error.message : error);
  process.exitCode = 1;
}
