import createClient, { type Middleware } from "openapi-fetch";
import type { paths } from "./schema";
import { formatProblem, statusMessage } from "../lib/problem";

export const api = createClient<paths>({ baseUrl: "/api", credentials: "same-origin" });

let onUnauthorized: () => void = () => {};
export function setUnauthorizedHandler(fn: () => void) {
  onUnauthorized = fn;
}
/** Report that the session ended outside a fetch (e.g. the console socket was closed for it). */
export function notifyUnauthorized() {
  onUnauthorized();
}

const authMiddleware: Middleware = {
  onResponse({ request, response }) {
    // Sign-in and the session probe handle their own 401; any other one means the session ended mid-use.
    const path = new URL(request.url).pathname;
    if (response.status === 401 && !path.endsWith("/auth/login") && !path.endsWith("/auth/me")) {
      onUnauthorized();
    }
  },
};
api.use(authMiddleware);

export class ApiError extends Error {
  status: number;
  /** The reply's raw detail, for code that reacts to a specific one (e.g. "totp_required"). Never shown. */
  detail: string;
  constructor(message: string, status: number, detail = "") {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.detail = detail;
  }
}

const detailOf = (body: unknown) =>
  body && typeof body === "object" && typeof (body as { detail?: unknown }).detail === "string" ? (body as { detail: string }).detail : "";

/**
 * Unwrap an openapi-fetch result, throwing ApiError with a message meant for
 * people on failure. A request that never got an answer (offline, panel down)
 * is an ApiError with status 0.
 */
export async function call<T>(
  p: Promise<{ data?: T; error?: unknown; response: Response }>,
): Promise<T> {
  let r: { data?: T; error?: unknown; response: Response };
  try {
    r = await p;
  } catch (err) {
    if (err instanceof DOMException && err.name === "AbortError") throw err;
    console.error(err);
    throw new ApiError(statusMessage(0), 0);
  }
  if (r.error !== undefined || !r.response.ok) {
    throw new ApiError(formatProblem(r.error, r.response.status), r.response.status, detailOf(r.error));
  }
  return r.data as T;
}

export type { components } from "./schema";
