import createClient, { type Middleware } from "openapi-fetch";
import type { paths } from "./schema";
import { formatProblem } from "../lib/problem";

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
  constructor(message: string, status: number) {
    super(message);
    this.name = "ApiError";
    this.status = status;
  }
}

/** Unwrap an openapi-fetch result, throwing ApiError with a readable message on failure. */
export async function call<T>(
  p: Promise<{ data?: T; error?: unknown; response: Response }>,
): Promise<T> {
  const r = await p;
  if (r.error !== undefined || !r.response.ok) {
    throw new ApiError(formatProblem(r.error, r.response.statusText || "Request failed"), r.response.status);
  }
  return r.data as T;
}

export type { components } from "./schema";
