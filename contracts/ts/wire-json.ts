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

type JsonDuplicateIssue = { key: string; path: string };
type JsonValueScan = { end: number; duplicate: JsonDuplicateIssue | null };

const skipJsonWhitespace = (source: string, start: number): number => {
  let index = start;
  while (index < source.length) {
    const character = source[index];
    if (character !== " " && character !== "\t" && character !== "\n" && character !== "\r") break;
    index += 1;
  }
  return index;
};

const hexDigit = (codeUnit: number): number | null => {
  if (codeUnit >= 0x30 && codeUnit <= 0x39) return codeUnit - 0x30;
  if (codeUnit >= 0x41 && codeUnit <= 0x46) return codeUnit - 0x41 + 10;
  if (codeUnit >= 0x61 && codeUnit <= 0x66) return codeUnit - 0x61 + 10;
  return null;
};

const scanJsonString = (source: string, start: number): { end: number; value: string } | null => {
  let index = start + 1;
  let value = "";
  while (index < source.length) {
    const character = source[index];
    if (character === undefined) return null;
    const codeUnit = character.charCodeAt(0);
    if (codeUnit === 0x22) return { end: index + 1, value };
    if (codeUnit < 0x20) return null;
    if (codeUnit !== 0x5c) {
      value += character;
      index += 1;
      continue;
    }

    const escape = source[index + 1];
    if (escape === undefined) return null;
    switch (escape) {
      case '"':
      case "\\":
      case "/":
        value += escape;
        index += 2;
        continue;
      case "b":
        value += "\b";
        index += 2;
        continue;
      case "f":
        value += "\f";
        index += 2;
        continue;
      case "n":
        value += "\n";
        index += 2;
        continue;
      case "r":
        value += "\r";
        index += 2;
        continue;
      case "t":
        value += "\t";
        index += 2;
        continue;
      case "u": {
        let decoded = 0;
        for (let offset = 2; offset < 6; offset += 1) {
          const digit = hexDigit(source.charCodeAt(index + offset));
          if (digit === null) return null;
          decoded = decoded * 16 + digit;
        }
        value += String.fromCharCode(decoded);
        index += 6;
        continue;
      }
      default:
        return null;
    }
  }
  return null;
};

const scanJsonNumber = (source: string, start: number): number | null => {
  let index = start;
  if (source[index] === "-") index += 1;

  const integer = source[index];
  if (integer === "0") {
    index += 1;
  } else if (integer !== undefined && integer >= "1" && integer <= "9") {
    index += 1;
    while (index < source.length) {
      const digit = source[index];
      if (digit === undefined || digit < "0" || digit > "9") break;
      index += 1;
    }
  } else {
    return null;
  }

  if (source[index] === ".") {
    index += 1;
    const fractionStart = index;
    while (index < source.length) {
      const digit = source[index];
      if (digit === undefined || digit < "0" || digit > "9") break;
      index += 1;
    }
    if (index === fractionStart) return null;
  }

  if (source[index] === "e" || source[index] === "E") {
    index += 1;
    if (source[index] === "+" || source[index] === "-") index += 1;
    const exponentStart = index;
    while (index < source.length) {
      const digit = source[index];
      if (digit === undefined || digit < "0" || digit > "9") break;
      index += 1;
    }
    if (index === exponentStart) return null;
  }

  return index;
};

function scanJsonValue(source: string, start: number, path: string): JsonValueScan | null {
  const index = skipJsonWhitespace(source, start);
  const character = source[index];
  if (character === '"') {
    const string = scanJsonString(source, index);
    return string === null ? null : { end: string.end, duplicate: null };
  }
  if (character === "{") return scanJsonObject(source, index, path);
  if (character === "[") return scanJsonArray(source, index, path);
  if (character === "t" && source.startsWith("true", index))
    return { end: index + 4, duplicate: null };
  if (character === "f" && source.startsWith("false", index))
    return { end: index + 5, duplicate: null };
  if (character === "n" && source.startsWith("null", index))
    return { end: index + 4, duplicate: null };
  if (character === "-" || (character !== undefined && character >= "0" && character <= "9")) {
    const end = scanJsonNumber(source, index);
    return end === null ? null : { end, duplicate: null };
  }
  return null;
}

function scanJsonObject(source: string, start: number, path: string): JsonValueScan | null {
  let index = skipJsonWhitespace(source, start + 1);
  let duplicate: JsonDuplicateIssue | null = null;
  const seen = new Set<string>();
  if (source[index] === "}") return { end: index + 1, duplicate };

  while (true) {
    if (source[index] !== '"') return null;
    const key = scanJsonString(source, index);
    if (key === null) return null;
    if (seen.has(key.value) && duplicate === null) duplicate = { key: key.value, path };
    seen.add(key.value);

    index = skipJsonWhitespace(source, key.end);
    if (source[index] !== ":") return null;
    const value = scanJsonValue(source, index + 1, `${path}.${key.value}`);
    if (value === null) return null;
    if (value.duplicate !== null && duplicate === null) duplicate = value.duplicate;
    index = skipJsonWhitespace(source, value.end);

    const delimiter = source[index];
    if (delimiter === "}") return { end: index + 1, duplicate };
    if (delimiter !== ",") return null;
    index = skipJsonWhitespace(source, index + 1);
  }
}

function scanJsonArray(source: string, start: number, path: string): JsonValueScan | null {
  let index = skipJsonWhitespace(source, start + 1);
  let element = 0;
  let duplicate: JsonDuplicateIssue | null = null;
  if (source[index] === "]") return { end: index + 1, duplicate };

  while (true) {
    const value = scanJsonValue(source, index, `${path}[${element}]`);
    if (value === null) return null;
    if (value.duplicate !== null && duplicate === null) duplicate = value.duplicate;
    index = skipJsonWhitespace(source, value.end);

    const delimiter = source[index];
    if (delimiter === "]") return { end: index + 1, duplicate };
    if (delimiter !== ",") return null;
    element += 1;
    index = skipJsonWhitespace(source, index + 1);
  }
}

const findDuplicateJsonMember = (source: string): JsonDuplicateIssue | null => {
  const value = scanJsonValue(source, 0, "$");
  if (value === null || skipJsonWhitespace(source, value.end) !== source.length) return null;
  return value.duplicate;
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

  const duplicate = findDuplicateJsonMember(source);
  if (duplicate !== null) {
    throw new WireJsonError(
      "duplicate_json_member",
      `duplicate JSON object member ${JSON.stringify(duplicate.key)} at ${duplicate.path}`,
    );
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
