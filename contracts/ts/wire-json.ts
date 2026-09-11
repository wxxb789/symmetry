import { SemanticError } from "./semantics.ts";

export class WireJsonError extends Error {
  readonly _tag = "WireJsonError" as const;
  readonly code: string;

  constructor(code: string, message: string) {
    super(message);
    this.name = "WireJsonError";
    this.code = code;
  }
}

export type JsonObject = Record<string, unknown>;

export const isJsonObject = (value: unknown): value is JsonObject =>
  value !== null && typeof value === "object" && !Array.isArray(value);

export const isJsonArray = (value: unknown): value is unknown[] => Array.isArray(value);

const jsonNumberPattern = /-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?/y;
const maxSafeIntegerDigits = "9007199254740991";
const utf8Decoder = new TextDecoder("utf-8", { fatal: true });

type NumberIssue = {
  lexeme: string;
  reason:
    | "fractional or exponent number is not canonical"
    | "integer exceeds the JSON safe-integer range";
};

const isSafeIntegerLexeme = (lexeme: string): boolean => {
  const digits = lexeme.startsWith("-") ? lexeme.slice(1) : lexeme;
  if (digits.length !== maxSafeIntegerDigits.length)
    return digits.length < maxSafeIntegerDigits.length;
  return digits <= maxSafeIntegerDigits;
};

const findNonCanonicalNumber = (source: string): NumberIssue | null => {
  let inString = false;
  let escaped = false;

  for (let index = 0; index < source.length; index += 1) {
    const character = source[index];
    if (character === undefined) continue;
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
      return { lexeme, reason: "fractional or exponent number is not canonical" };
    }
    if (!isSafeIntegerLexeme(lexeme)) {
      return { lexeme, reason: "integer exceeds the JSON safe-integer range" };
    }
    index += lexeme.length - 1;
  }
  return null;
};

export const parseWireJson = (input: string | Uint8Array): unknown => {
  let source: string;
  if (typeof input === "string") {
    source = input;
  } else {
    try {
      source = utf8Decoder.decode(input);
    } catch {
      throw new WireJsonError("invalid_utf8", "JSON input is not valid UTF-8");
    }
  }

  const numberIssue = findNonCanonicalNumber(source);
  if (numberIssue) {
    throw new WireJsonError("noncanonical_number", `${numberIssue.reason}: ${numberIssue.lexeme}`);
  }

  try {
    return JSON.parse(source) as unknown;
  } catch {
    throw new WireJsonError("invalid_json", "JSON input is invalid");
  }
};

const findUnpairedSurrogate = (value: string): "high" | "low" | null => {
  for (let index = 0; index < value.length; index += 1) {
    const codeUnit = value.charCodeAt(index);
    if (codeUnit >= 0xd800 && codeUnit <= 0xdbff) {
      const next = value.charCodeAt(index + 1);
      if (!Number.isInteger(next) || next < 0xdc00 || next > 0xdfff) return "high";
      index += 1;
    } else if (codeUnit >= 0xdc00 && codeUnit <= 0xdfff) {
      return "low";
    }
  }
  return null;
};

const findUnicodeIssue = (value: unknown, path = "$"): string | null => {
  if (typeof value === "string") {
    const kind = findUnpairedSurrogate(value);
    return kind === null ? null : `${path} contains an unpaired ${kind} surrogate`;
  }
  if (isJsonArray(value)) {
    for (let index = 0; index < value.length; index += 1) {
      const issue = findUnicodeIssue(value[index], `${path}[${index}]`);
      if (issue) return issue;
    }
    return null;
  }
  if (isJsonObject(value)) {
    for (const [key, child] of Object.entries(value)) {
      const keyKind = findUnpairedSurrogate(key);
      if (keyKind !== null)
        return `${path} contains an unpaired ${keyKind} surrogate in an object key`;
      const issue = findUnicodeIssue(child, `${path}.${key}`);
      if (issue) return issue;
    }
  }
  return null;
};

export const assertUnicode = (value: unknown): void => {
  const issue = findUnicodeIssue(value);
  if (issue !== null) throw new SemanticError("unpaired_surrogate", issue);
};
