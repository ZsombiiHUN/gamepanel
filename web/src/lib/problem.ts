import i18n from "../i18n";
/**
 * Error text shown to people. The panel's problem+json replies are written for
 * people (internal failures arrive as a generic sentence with a reference), so
 * their detail is shown. Anything else (an HTML page from a proxy, a network
 * failure, a bug in this app) gets a plain sentence chosen by status, never the
 * raw text.
 */

/** Plain sentences for replies that carry no usable message, by status. */
export function statusMessage(status: number): string {
  if (status === 0) return i18n.t("errors.offline");
  if (status === 401) return i18n.t("errors.unauthorized");
  if (status === 403) return i18n.t("errors.forbidden");
  if (status === 404) return i18n.t("errors.notFound");
  if (status === 408 || status === 504) return i18n.t("errors.timeout");
  if (status === 409) return i18n.t("errors.conflict");
  if (status === 413) return i18n.t("errors.tooLarge");
  if (status === 429) return i18n.t("errors.tooMany");
  if (status === 502 || status === 503) return i18n.t("errors.unavailable");
  if (status >= 500) return i18n.t("errors.server");
  return i18n.t("errors.badRequest");
}

/** "body.limits.memory_mb" -> "Memory mb"; "" when there is no field. */
function fieldName(location: string): string {
  const last =
    location
      .split(".")
      .pop()
      ?.replace(/\[\d+\]$/, "") ?? "";
  if (!last || ["body", "query", "path", "header"].includes(last)) return "";
  const words = last
    .replace(/_/g, " ")
    .replace(/([a-z])([A-Z])/g, "$1 $2")
    .toLowerCase();
  return words.charAt(0).toUpperCase() + words.slice(1);
}

const sentence = (s: string) => s.charAt(0).toUpperCase() + s.slice(1);

/** The message to show for a failed reply with this body and status. */
export function formatProblem(body: unknown, status: number): string {
  // Not problem+json (plain text, an HTML error page, nothing): say what the status means.
  if (body == null || typeof body !== "object" || Array.isArray(body)) return statusMessage(status);

  const p = body as { detail?: unknown; errors?: unknown };
  const fields: string[] = [];
  if (Array.isArray(p.errors)) {
    for (const e of p.errors) {
      if (!e || typeof e !== "object") continue;
      const { message, location } = e as { message?: unknown; location?: unknown };
      if (typeof message !== "string" || !message) continue;
      const name = typeof location === "string" ? fieldName(location) : "";
      fields.push(name ? `${name}: ${message}` : sentence(message));
    }
  }
  // Field errors say more than the generic "validation failed" above them.
  if (fields.length) return fields.join("; ");
  if (typeof p.detail === "string" && p.detail.trim()) return sentence(p.detail.trim());
  return statusMessage(status);
}

/**
 * Text for any thrown value: an API error's message, our own string, or, for
 * anything else (a bug, a library error), a generic sentence. The real error
 * goes to the browser console for whoever is debugging.
 */
export function errorText(err: unknown): string {
  if (typeof err === "string") return err;
  if (err instanceof Error && err.name === "ApiError") return err.message;
  console.error(err);
  return i18n.t("errors.client");
}
