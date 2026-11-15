/**
 * Turn an RFC 9457 problem+json body (or any thrown value) into a readable string.
 */
export function formatProblem(err: unknown, fallback = "Request failed"): string {
  if (err == null) return fallback;
  if (typeof err === "string") return err.trim() || fallback;
  if (err instanceof Error) return err.message || fallback;
  if (typeof err !== "object") return String(err);

  const p = err as {
    detail?: unknown;
    title?: unknown;
    errors?: unknown;
  };
  const parts: string[] = [];
  if (typeof p.detail === "string" && p.detail) parts.push(p.detail);
  else if (typeof p.title === "string" && p.title) parts.push(p.title);

  if (Array.isArray(p.errors)) {
    for (const e of p.errors) {
      if (!e || typeof e !== "object") continue;
      const { message, location } = e as { message?: unknown; location?: unknown };
      if (typeof message !== "string" || !message) continue;
      parts.push(typeof location === "string" && location ? `${location}: ${message}` : message);
    }
  }
  return parts.length ? parts.join("; ") : fallback;
}
