/**
 * The one home for turning a served value into a rendered one.
 *
 * The read schemas keep open-JSON members `unknown`, so a surface that renders
 * one narrows it here rather than re-deriving a local rule. The module holds two
 * policies that answer different questions and must not be merged:
 *
 * - `asText`/`asRecord`/`asRecords`/`safeUrl` — the work surface's narrowing
 *   primitives: whitespace-only text is absent, and only http(s) URLs are links.
 * - `servedText`/`servedMember` — the goals surface's scalar policy: scalars
 *   render as themselves and the caller names the absence.
 *
 * A surface whose rule genuinely differs keeps a named function beside this
 * module instead of a second copy of these: `presentText`
 * (`features/connections/resources.tsx`, a served field is present when it is
 * not empty), `workFailureText` (`features/work/presentation.ts`, `reason` →
 * `message` → `summary` with the `"Failed: "` prefix) and `chatFailureText`
 * (`features/chat/presentation.ts`, `message` → `summary` → `error`, verbatim).
 */

/**
 * Free-form JSON that the wire carries inside decoded envelopes (agent payloads,
 * task result maps, evidence rows). Those values have no field contract, so
 * readers narrow them here rather than guessing DTO shapes.
 */
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

/**
 * Links in served payloads are untrusted text: only http(s) URLs become anchors,
 * so a tampered provider or agent payload cannot inject a script scheme.
 */
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

/**
 * Scalar text for a served value.
 *
 * The read schemas keep open-JSON members `unknown`, so a surface that renders
 * one must not rely on implicit stringification: an object would render as
 * `[object Object]`, which reads as a value the server never sent. Scalars render
 * as themselves; anything else renders as the explicit absence text.
 */
export function servedText(value: unknown, absence: string): string {
  if (typeof value === "string") return value;
  if (typeof value === "number" || typeof value === "boolean") return String(value);
  return absence;
}

/** One member of a served open-JSON object, with the same scalar rule as {@link servedText}. */
export function servedMember(value: unknown, member: string, absence: string): string {
  if (value === null || typeof value !== "object") return absence;
  if (!(member in value)) return absence;
  return servedText((value as Record<string, unknown>)[member], absence);
}
