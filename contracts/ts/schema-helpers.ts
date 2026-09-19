import { Schema } from "effect";
import { fullFormats } from "ajv-formats/dist/formats.js";

export type Graph = Schema.Schema.AnyNoContext;

export const strictOptions = {
  exact: true,
  onExcessProperty: "error",
} as const;

const preserveExcessPropertyOptions = {
  exact: true,
  onExcessProperty: "preserve",
} as const;

const dateTimeFormat = fullFormats["date-time"];

const hasSynchronousStringValidator = (
  value: unknown,
): value is { readonly validate: RegExp | ((input: string) => boolean) } =>
  typeof value === "object" &&
  value !== null &&
  "validate" in value &&
  (value.validate instanceof RegExp || typeof value.validate === "function");

const isRecord = (value: unknown): value is Record<string, unknown> =>
  typeof value === "object" && value !== null && !Array.isArray(value);

const equalJson = (left: unknown, right: unknown): boolean => {
  if (left === right) return true;
  if (typeof left !== typeof right || left === null || right === null) return false;

  if (Array.isArray(left) && Array.isArray(right)) {
    return (
      left.length === right.length && left.every((value, index) => equalJson(value, right[index]))
    );
  }

  if (isRecord(left) && isRecord(right)) {
    const leftKeys = Object.keys(left);
    const rightKeys = Object.keys(right);
    return (
      leftKeys.length === rightKeys.length &&
      leftKeys.every((key) => Object.hasOwn(right, key) && equalJson(left[key], right[key]))
    );
  }

  return false;
};

const isRfc3339DateTime = (value: string): boolean => {
  if (!hasSynchronousStringValidator(dateTimeFormat)) return false;
  const { validate } = dateTimeFormat;
  if (validate instanceof RegExp) return validate.test(value);
  return validate(value);
};

const fromSchema = (
  schema: Graph,
  options: typeof strictOptions | typeof preserveExcessPropertyOptions = strictOptions,
): Graph => {
  const matches = Schema.is(schema, options);
  return Schema.Unknown.pipe(Schema.filter((value): boolean => matches(value)));
};

const refine = (predicate: (value: unknown) => boolean): Graph =>
  Schema.Unknown.pipe(Schema.filter(predicate));

export const strictObject = <A, I>(schema: Schema.Schema<A, I, never>): Graph =>
  fromSchema(schema, strictOptions);

export const openObject = <A, I>(schema: Schema.Schema<A, I, never>): Graph =>
  fromSchema(schema, preserveExcessPropertyOptions);

// Object-specific Draft 7 keywords are vacuously valid for non-object inputs.
export const conditionalObject = (schema: Graph): Graph => {
  const matches = Schema.is(schema, strictOptions);
  return refine((value) => !isRecord(value) || matches(value));
};

export const openObjectWithAdditionalProperties = <A, I>(
  schema: Schema.Schema<A, I, never>,
  propertyNames: readonly string[],
  additionalProperties: Graph,
): Graph => {
  const matchesObject = Schema.is(schema, preserveExcessPropertyOptions);
  const matchesAdditionalProperty = Schema.is(additionalProperties, strictOptions);
  const knownProperties = new Set(propertyNames);

  return refine(
    (value) =>
      matchesObject(value) &&
      isRecord(value) &&
      Object.keys(value).every(
        (key) => knownProperties.has(key) || matchesAdditionalProperty(value[key]),
      ),
  );
};

export const withObjectPropertyBounds = (
  schema: Graph,
  minimum: number | undefined,
  maximum: number | undefined,
): Graph => {
  const matches = Schema.is(schema, strictOptions);
  return refine((value) => {
    if (!matches(value) || !isRecord(value)) return false;
    const count = Object.keys(value).length;
    return (
      (minimum === undefined || count >= minimum) && (maximum === undefined || count <= maximum)
    );
  });
};

export const stringConstraints = <A extends string, I>(
  schema: Schema.Schema<A, I, never>,
  minimumLength: number | undefined,
  maximumLength: number | undefined,
  format: "date-time" | undefined,
): Graph => {
  const matches = Schema.is(schema, strictOptions);
  return refine((value) => {
    if (!matches(value) || typeof value !== "string") return false;
    const length = Array.from(value).length;
    return (
      (minimumLength === undefined || length >= minimumLength) &&
      (maximumLength === undefined || length <= maximumLength) &&
      (format !== "date-time" || isRfc3339DateTime(value))
    );
  });
};

export const numberConstraints = <A extends number, I>(
  schema: Schema.Schema<A, I, never>,
  minimum: number | undefined,
  maximum: number | undefined,
): Graph => {
  const matches = Schema.is(schema, strictOptions);
  return refine(
    (value) =>
      matches(value) &&
      typeof value === "number" &&
      Number.isFinite(value) &&
      (minimum === undefined || value >= minimum) &&
      (maximum === undefined || value <= maximum),
  );
};

export const arrayConstraints = <A, I>(
  schema: Schema.Schema<A, I, never>,
  minimumLength: number | undefined,
  maximumLength: number | undefined,
  uniqueItems: boolean | undefined,
): Graph => {
  const matches = Schema.is(schema, strictOptions);
  return refine((value) => {
    if (!matches(value) || !Array.isArray(value)) return false;
    if (minimumLength !== undefined && value.length < minimumLength) return false;
    if (maximumLength !== undefined && value.length > maximumLength) return false;
    if (uniqueItems !== true) return true;

    return value.every(
      (item, index) => !value.slice(0, index).some((previous) => equalJson(previous, item)),
    );
  });
};

export const allOf = (first: Graph, ...rest: readonly Graph[]): Graph => {
  const matches = rest.map((schema) => Schema.is(schema, strictOptions));
  return first.pipe(Schema.filter((value): boolean => matches.every((match) => match(value))));
};

export const exactOneOf = (union: Graph, first: Graph, ...rest: readonly Graph[]): Graph => {
  const schemas = [first, ...rest];
  const matches = schemas.map((schema) => Schema.is(schema, strictOptions));
  return union.pipe(
    Schema.filter((value): boolean => matches.filter((match) => match(value)).length === 1),
  );
};

export const notSchema = (schema: Graph): Graph => {
  const matches = Schema.is(schema, strictOptions);
  return refine((value) => !matches(value));
};
