const SUPPORTED_KEYWORDS = new Set([
  "$comment",
  "$id",
  "$ref",
  "$schema",
  "additionalProperties",
  "allOf",
  "const",
  "default",
  "definitions",
  "description",
  "enum",
  "format",
  "items",
  "maxItems",
  "maxLength",
  "maxProperties",
  "maximum",
  "minItems",
  "minLength",
  "minProperties",
  "minimum",
  "not",
  "oneOf",
  "pattern",
  "properties",
  "required",
  "title",
  "type",
  "uniqueItems",
]);

const TYPES = new Set(["array", "boolean", "integer", "null", "number", "object", "string"]);

const REFERENCE_METADATA_KEYWORDS = new Set([
  "$comment",
  "$ref",
  "default",
  "description",
  "title",
]);

const ROOT_REFERENCE_CONTAINER_KEYWORDS = new Set(["$id", "$schema", "definitions"]);

const isObject = (value) => value !== null && typeof value === "object" && !Array.isArray(value);

const stableJson = (value) => {
  if (Array.isArray(value)) return `[${value.map(stableJson).join(",")}]`;
  if (isObject(value)) {
    return `{${Object.keys(value)
      .sort()
      .map((key) => `${JSON.stringify(key)}:${stableJson(value[key])}`)
      .join(",")}}`;
  }
  return JSON.stringify(value);
};

const fail = (path, message) => {
  throw new Error(`${path}: ${message}`);
};

const assertObject = (value, path) => {
  if (!isObject(value)) fail(path, "schema must be an object");
  return value;
};

const assertNonNegativeInteger = (value, path) => {
  if (!Number.isSafeInteger(value) || value < 0) fail(path, "must be a non-negative safe integer");
  return value;
};

const assertFiniteNumber = (value, path) => {
  if (typeof value !== "number" || !Number.isFinite(value)) fail(path, "must be a finite number");
  return value;
};

const assertLiteral = (value, path) => {
  if (
    value !== null &&
    typeof value !== "string" &&
    typeof value !== "boolean" &&
    (typeof value !== "number" || !Number.isFinite(value))
  ) {
    fail(path, "only primitive JSON literals are supported");
  }
  return value;
};

const typeName = (name, path) => {
  if (typeof name !== "string" || !/^[A-Za-z_$][A-Za-z0-9_$]*$/u.test(name)) {
    fail(path, "must be a TypeScript identifier");
  }
  return name;
};

const definitionSymbol = (name, path) => `Definition${typeName(name, path)}Graph`;

const requiredObjectKeywords = new Set([
  "additionalProperties",
  "maxProperties",
  "minProperties",
  "properties",
  "required",
]);

const arrayKeywords = new Set(["items", "maxItems", "minItems", "uniqueItems"]);

const hasAny = (schema, keys) => [...keys].some((key) => Object.hasOwn(schema, key));

const isEvidenceSchemaReference = (reference) =>
  reference === "evidence.schema.json" ||
  reference === "./evidence.schema.json" ||
  reference === "evidence.schema.json#" ||
  reference === "./evidence.schema.json#";

const validateSchema = (schema, path, isRoot = false) => {
  assertObject(schema, path);
  for (const key of Object.keys(schema)) {
    if (!SUPPORTED_KEYWORDS.has(key)) fail(`${path}/${key}`, "unsupported JSON Schema keyword");
  }

  if (!isRoot && Object.hasOwn(schema, "$id")) {
    fail(`${path}/$id`, "nested $id is outside the supported reference scope");
  }
  if (!isRoot && Object.hasOwn(schema, "definitions")) {
    fail(`${path}/definitions`, "nested definitions are outside the supported reference scope");
  }

  if (Object.hasOwn(schema, "$ref")) {
    if (
      typeof schema.$ref !== "string" ||
      (!schema.$ref.startsWith("#/definitions/") && !isEvidenceSchemaReference(schema.$ref))
    ) {
      fail(
        `${path}/$ref`,
        "only local #/definitions or canonical evidence.schema.json references are supported",
      );
    }
    const validationSiblings = Object.keys(schema).filter(
      (key) =>
        !REFERENCE_METADATA_KEYWORDS.has(key) &&
        !(isRoot && ROOT_REFERENCE_CONTAINER_KEYWORDS.has(key)),
    );
    if (validationSiblings.length > 0) {
      fail(
        path,
        `$ref with validation siblings is outside the supported Draft 7 subset: ${validationSiblings.join(", ")}`,
      );
    }
  }

  if (
    Object.hasOwn(schema, "type") &&
    (typeof schema.type !== "string" || !TYPES.has(schema.type))
  ) {
    fail(`${path}/type`, "unsupported JSON Schema type");
  }

  if (Object.hasOwn(schema, "format") && !["date-time", "uuid"].includes(schema.format)) {
    fail(`${path}/format`, "unsupported JSON Schema format");
  }

  if (
    hasAny(schema, new Set(["format", "maxLength", "minLength", "pattern"])) &&
    schema.type !== "string"
  ) {
    fail(path, "string constraints require type: string");
  }
  if (
    hasAny(schema, new Set(["minimum", "maximum"])) &&
    schema.type !== "integer" &&
    schema.type !== "number"
  ) {
    fail(path, "number constraints require type: integer or number");
  }

  for (const keyword of ["minimum", "maximum"]) {
    if (Object.hasOwn(schema, keyword)) assertFiniteNumber(schema[keyword], `${path}/${keyword}`);
  }
  for (const keyword of [
    "minItems",
    "maxItems",
    "minLength",
    "maxLength",
    "minProperties",
    "maxProperties",
  ]) {
    if (Object.hasOwn(schema, keyword))
      assertNonNegativeInteger(schema[keyword], `${path}/${keyword}`);
  }
  if (Object.hasOwn(schema, "uniqueItems") && typeof schema.uniqueItems !== "boolean") {
    fail(`${path}/uniqueItems`, "must be a boolean");
  }
  if (Object.hasOwn(schema, "pattern") && typeof schema.pattern !== "string") {
    fail(`${path}/pattern`, "must be a string");
  }
  if (Object.hasOwn(schema, "const")) assertLiteral(schema.const, `${path}/const`);
  if (Object.hasOwn(schema, "enum")) {
    if (!Array.isArray(schema.enum) || schema.enum.length === 0) {
      fail(`${path}/enum`, "must be a non-empty array");
    }
    schema.enum.forEach((value, index) => assertLiteral(value, `${path}/enum/${index}`));
  }

  if (Object.hasOwn(schema, "definitions")) {
    for (const [name, definition] of Object.entries(
      assertObject(schema.definitions, `${path}/definitions`),
    )) {
      validateSchema(definition, `${path}/definitions/${name}`);
    }
  }
  if (Object.hasOwn(schema, "properties")) {
    for (const [name, property] of Object.entries(
      assertObject(schema.properties, `${path}/properties`),
    )) {
      validateSchema(property, `${path}/properties/${name}`);
    }
  }
  if (Object.hasOwn(schema, "required")) {
    if (
      !Array.isArray(schema.required) ||
      schema.required.some((name) => typeof name !== "string")
    ) {
      fail(`${path}/required`, "must be an array of property names");
    }
  }
  if (Object.hasOwn(schema, "additionalProperties")) {
    if (schema.additionalProperties !== true && schema.additionalProperties !== false) {
      validateSchema(schema.additionalProperties, `${path}/additionalProperties`);
    }
  }
  if (Object.hasOwn(schema, "items")) validateSchema(schema.items, `${path}/items`);
  for (const keyword of ["oneOf", "allOf"]) {
    if (!Object.hasOwn(schema, keyword)) continue;
    if (!Array.isArray(schema[keyword]) || schema[keyword].length === 0) {
      fail(`${path}/${keyword}`, "must be a non-empty schema array");
    }
    schema[keyword].forEach((member, index) =>
      validateSchema(member, `${path}/${keyword}/${index}`),
    );
  }
  if (Object.hasOwn(schema, "not")) validateSchema(schema.not, `${path}/not`);

  if (Object.hasOwn(schema, "items") && schema.type !== "array") {
    fail(`${path}/items`, "is supported only on array schemas");
  }
  if (hasAny(schema, arrayKeywords) && schema.type !== "array") {
    fail(path, "array constraints require type: array");
  }
  if (
    hasAny(schema, requiredObjectKeywords) &&
    schema.type !== "object" &&
    !Object.hasOwn(schema, "type")
  ) {
    return;
  }
  if (hasAny(schema, requiredObjectKeywords) && schema.type !== "object") {
    fail(path, "object constraints require type: object or an untyped object branch");
  }
};

const collectDefinitions = (schemas) => {
  const definitions = new Map();
  for (const { file, schema } of schemas) {
    const current = schema.definitions ?? {};
    for (const [name, definition] of Object.entries(assertObject(current, `${file}/definitions`))) {
      const existing = definitions.get(name);
      if (existing !== undefined && stableJson(existing.schema) !== stableJson(definition)) {
        fail(`${file}/definitions/${name}`, `conflicts with the definition from ${existing.file}`);
      }
      if (existing === undefined) definitions.set(name, { file, schema: definition });
    }
  }
  return definitions;
};

const validateReferences = (schema, path, definitions) => {
  if (typeof schema.$ref === "string") {
    if (schema.$ref.startsWith("#/definitions/")) {
      const name = schema.$ref.slice("#/definitions/".length);
      if (!definitions.has(name)) fail(`${path}/$ref`, `unknown definition ${JSON.stringify(name)}`);
    } else if (!isEvidenceSchemaReference(schema.$ref)) {
      fail(`${path}/$ref`, `unknown external reference ${JSON.stringify(schema.$ref)}`);
    }
  }
  if (isObject(schema.definitions)) {
    for (const [name, definition] of Object.entries(schema.definitions)) {
      validateReferences(definition, `${path}/definitions/${name}`, definitions);
    }
  }
  if (isObject(schema.properties)) {
    for (const [name, property] of Object.entries(schema.properties)) {
      validateReferences(property, `${path}/properties/${name}`, definitions);
    }
  }
  if (isObject(schema.additionalProperties)) {
    validateReferences(schema.additionalProperties, `${path}/additionalProperties`, definitions);
  }
  if (isObject(schema.items)) validateReferences(schema.items, `${path}/items`, definitions);
  for (const keyword of ["oneOf", "allOf"]) {
    if (Array.isArray(schema[keyword])) {
      schema[keyword].forEach((member, index) =>
        validateReferences(member, `${path}/${keyword}/${index}`, definitions),
      );
    }
  }
  if (isObject(schema.not)) validateReferences(schema.not, `${path}/not`, definitions);
};

const createRenderer = () => {
  const regularExpressions = new Map();

  const regularExpression = (pattern) => {
    const existing = regularExpressions.get(pattern);
    if (existing !== undefined) return existing;
    const name = `_regularExpression${regularExpressions.size}`;
    regularExpressions.set(pattern, name);
    return name;
  };

  const join = (parts) => {
    if (parts.length === 0) return "S.Unknown";
    if (parts.length === 1) return parts[0];
    return `allOf(${parts.join(", ")})`;
  };

  const renderStruct = (schema, path, conditional) => {
    const properties = schema.properties ?? {};
    const required = new Set(schema.required ?? []);
    const declaredPropertyNames = Object.keys(properties).sort();
    const names = [...new Set([...declaredPropertyNames, ...required])].sort();
    const undeclaredRequired = names.filter(
      (name) => required.has(name) && !Object.hasOwn(properties, name),
    );
    const fields = names.map((name) => {
      const property = Object.hasOwn(properties, name) ? properties[name] : {};
      const value = render(property, `${path}/properties/${name}`);
      return `${JSON.stringify(name)}: ${required.has(name) ? value : `S.optionalWith(${value}, { exact: true })`}`;
    });
    const struct = `S.Struct({${fields.length === 0 ? "" : `\n${fields.map((field) => `  ${field}`).join(",\n")}\n`}})`;
    const additionalProperties = schema.additionalProperties;
    const base =
      additionalProperties === false && undeclaredRequired.length > 0
        ? "strictObject(S.Never)"
        : additionalProperties === false
          ? `strictObject(${struct})`
          : isObject(additionalProperties)
            ? `openObjectWithAdditionalProperties(${struct}, [${declaredPropertyNames.map(JSON.stringify).join(", ")}], ${render(additionalProperties, `${path}/additionalProperties`)})`
            : `openObject(${struct})`;
    const bounded = `withObjectPropertyBounds(${base}, ${schema.minProperties ?? "undefined"}, ${schema.maxProperties ?? "undefined"})`;
    return conditional ? `conditionalObject(${bounded})` : bounded;
  };

  const renderString = (schema, path) => {
    const base = schema.format === "uuid" ? "S.UUID" : "S.String";
    const patterned = Object.hasOwn(schema, "pattern")
      ? `${base}.pipe(S.pattern(${regularExpression(schema.pattern)}))`
      : base;
    return `stringConstraints(${patterned}, ${schema.minLength ?? "undefined"}, ${schema.maxLength ?? "undefined"}, ${schema.format === "date-time" ? '"date-time"' : "undefined"})`;
  };

  const renderArray = (schema, path) => {
    if (!Object.hasOwn(schema, "items")) fail(`${path}/items`, "array schemas require items");
    return `arrayConstraints(S.Array(${render(schema.items, `${path}/items`)}), ${schema.minItems ?? "undefined"}, ${schema.maxItems ?? "undefined"}, ${schema.uniqueItems ?? "undefined"})`;
  };

  const render = (schema, path) => {
    const parts = [];
    if (typeof schema.$ref === "string") {
      if (isEvidenceSchemaReference(schema.$ref)) {
        parts.push("EvidenceGraph");
      } else {
        parts.push(definitionSymbol(schema.$ref.slice("#/definitions/".length), `${path}/$ref`));
      }
    }
    if (Object.hasOwn(schema, "const")) parts.push(`S.Literal(${JSON.stringify(schema.const)})`);
    if (Object.hasOwn(schema, "enum"))
      parts.push(`S.Literal(${schema.enum.map(JSON.stringify).join(", ")})`);

    const treatsAsObject =
      schema.type === "object" ||
      (!Object.hasOwn(schema, "type") && hasAny(schema, requiredObjectKeywords));
    if (schema.type === "string") parts.push(renderString(schema, path));
    if (schema.type === "integer") {
      parts.push(
        `numberConstraints(S.Int, ${schema.minimum ?? "undefined"}, ${schema.maximum ?? "undefined"})`,
      );
    }
    if (schema.type === "number") {
      parts.push(
        `numberConstraints(S.Number, ${schema.minimum ?? "undefined"}, ${schema.maximum ?? "undefined"})`,
      );
    }
    if (schema.type === "boolean") parts.push("S.Boolean");
    if (schema.type === "null") parts.push("S.Null");
    if (schema.type === "array") parts.push(renderArray(schema, path));
    if (treatsAsObject) parts.push(renderStruct(schema, path, schema.type !== "object"));

    if (Array.isArray(schema.allOf)) {
      parts.push(
        `allOf(${schema.allOf.map((member, index) => render(member, `${path}/allOf/${index}`)).join(", ")})`,
      );
    }
    if (Array.isArray(schema.oneOf)) {
      const members = schema.oneOf.map((member, index) => render(member, `${path}/oneOf/${index}`));
      parts.push(`exactOneOf(S.Union(${members.join(", ")}), ${members.join(", ")})`);
    }
    if (isObject(schema.not)) parts.push(`notSchema(${render(schema.not, `${path}/not`)})`);

    return join(parts);
  };

  return {
    render,
    renderRegularExpressions: () =>
      [...regularExpressions.entries()]
        .map(([pattern, name]) => `const ${name} = new RegExp(${JSON.stringify(pattern)}, "u");`)
        .join("\n"),
  };
};

export const generateEffectSchemas = (inputSchemas) => {
  if (!Array.isArray(inputSchemas) || inputSchemas.length === 0) {
    throw new Error("schemas must be a non-empty array");
  }

  const schemas = inputSchemas
    .map((schema, index) => {
      if (!isObject(schema)) fail(`schemas/${index}`, "entry must be an object");
      if (typeof schema.file !== "string" || typeof schema.key !== "string") {
        fail(`schemas/${index}`, "entry must include file and key strings");
      }
      return {
        ...schema,
        name: typeName(schema.name, `schemas/${index}/name`),
        schema: assertObject(schema.schema, `schemas/${index}/schema`),
      };
    })
    .sort((left, right) => left.file.localeCompare(right.file));

  const files = new Set();
  const names = new Set();
  for (const { file, name, schema } of schemas) {
    if (files.has(file)) fail(file, "duplicate schema file");
    if (names.has(name)) fail(file, "duplicate schema name");
    files.add(file);
    names.add(name);
    validateSchema(schema, file, true);
  }

  const definitions = collectDefinitions(schemas);
  for (const { file, schema } of schemas) validateReferences(schema, file, definitions);
  for (const [name, definition] of definitions) {
    validateReferences(definition.schema, `${definition.file}/definitions/${name}`, definitions);
  }

  const renderer = createRenderer();
  const definitionContents = [...definitions.entries()]
    .map(
      ([name, definition]) =>
        `export const ${definitionSymbol(name, `${definition.file}/definitions/${name}`)} = S.suspend(() => ${renderer.render(definition.schema, `${definition.file}/definitions/${name}`)});`,
    )
    .join("\n\n");
  const rootContents = schemas
    .map(({ file, name, schema }) => {
      const graphName = `${name}Graph`;
      const predicateName = `is${name}`;
      return [
        `export const ${graphName} = S.suspend(() => ${renderer.render(schema, file)});`,
        "",
        `const matches${name} = S.is(${graphName}, strictOptions);`,
        `export const ${predicateName} = (value: unknown): value is Symmetry${name}V1 => matches${name}(value);`,
        `export const ${name}Schema: S.Schema<Symmetry${name}V1, unknown> = S.Unknown.pipe(S.filter(${predicateName}));`,
      ].join("\n");
    })
    .join("\n\n");
  const typeImports = [
    "import type {",
    ...schemas.map(({ name }) => `  Symmetry${name}V1,`),
    '} from "../ts/index.js";',
  ].join("\n");
  const regularExpressions = renderer.renderRegularExpressions();
  const contents = `${[
    "/* Code generated by contracts/scripts/generate-effect.mjs; DO NOT EDIT. */",
    'import { Schema as S } from "effect";',
    'import { allOf, arrayConstraints, conditionalObject, exactOneOf, notSchema, numberConstraints, openObject, openObjectWithAdditionalProperties, strictObject, strictOptions, stringConstraints, withObjectPropertyBounds } from "../../ts/schema-helpers.ts";',
    typeImports,
    regularExpressions,
    definitionContents,
    rootContents,
    "",
  ]
    .filter((section, index) => section !== "" || index === 0)
    .join("\n\n")}\n`;

  return [{ fileName: "index.ts", contents }];
};
