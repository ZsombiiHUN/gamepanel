/** Small, total validators for URL search params. Anything unexpected falls back to "not set". */

export function text(v: unknown): string | undefined {
  return typeof v === "string" && v.trim() !== "" ? v : undefined;
}

export function oneOf<T extends string>(v: unknown, allowed: readonly T[]): T | undefined {
  return allowed.find((a) => a === v);
}

/** A page number from the URL. Page 0 is the default, so it is left out of the address. */
export function page(v: unknown): number | undefined {
  const n = typeof v === "number" ? v : typeof v === "string" ? Number(v) : NaN;
  return Number.isInteger(n) && n > 0 ? n : undefined;
}
