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
