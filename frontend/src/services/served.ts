export function asText(value: unknown): string | null {
  return typeof value === "string" && value.trim() !== "" ? value : null;
}

export function asRecord(value: unknown): Record<string, unknown> | null {
  if (typeof value !== "object" || value === null || Array.isArray(value)) return null;
  return value as Record<string, unknown>;
}

export function asRecords(value: unknown): Record<string, unknown>[] {
  return Array.isArray(value) ? value.map(asRecord).filter((entry): entry is Record<string, unknown> => entry !== null) : [];
}

// Links in served payloads are untrusted text: only http(s) URLs become anchors, so
// a tampered provider or agent payload cannot inject a script scheme.
export function safeUrl(value: unknown): string | null {
  const raw = asText(value);
  if (raw === null) return null;
  try {
    const link = new URL(raw);
    return link.protocol === "http:" || link.protocol === "https:" ? raw : null;
  } catch {
    return null;
  }
}

// Scalars render as themselves; anything else renders as the explicit absence text.
// No implicit stringification: an object must never render as `[object Object]`.
export function servedText(value: unknown, absence: string): string {
  if (typeof value === "string") return value;
  if (typeof value === "number" || typeof value === "boolean") return String(value);
  return absence;
}

export function servedMember(value: unknown, member: string, absence: string): string {
  if (value === null || typeof value !== "object") return absence;
  if (!(member in value)) return absence;
  return servedText((value as Record<string, unknown>)[member], absence);
}
