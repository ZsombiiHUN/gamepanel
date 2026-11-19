import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiError, call } from "./client";

const reply = (status: number, body: string, type = "application/problem+json") => ({
  error: type.includes("json") ? (JSON.parse(body) as unknown) : body,
  response: new Response(body, { status, headers: { "content-type": type } }),
});

/** The ApiError call() throws for p. */
async function failure(p: Parameters<typeof call>[0]): Promise<ApiError> {
  try {
    await call(p);
  } catch (e) {
    return e as ApiError;
  }
  throw new Error("call did not fail");
}

describe("call", () => {
  afterEach(() => vi.restoreAllMocks());

  it("turns a failed reply into an ApiError with a readable message and the raw detail", async () => {
    const err = await failure(Promise.resolve(reply(401, '{"status":401,"detail":"totp_required"}')));
    expect(err).toBeInstanceOf(ApiError);
    expect(err.status).toBe(401);
    expect(err.detail).toBe("totp_required");
  });

  it("reports an unreachable panel in plain words, not the browser's fetch error", async () => {
    vi.spyOn(console, "error").mockImplementation(() => {});
    const err = await failure(Promise.reject(new TypeError("NetworkError when attempting to fetch resource.")));
    expect(err).toBeInstanceOf(ApiError);
    expect(err.status).toBe(0);
    expect(err.message).toMatch(/Can't reach the panel/);
    expect(err.message).not.toMatch(/NetworkError|fetch/);
  });

  it("does not show an HTML error page", async () => {
    const err = await failure(Promise.resolve(reply(502, "<h1>502 Bad Gateway</h1>nginx", "text/html")));
    expect(err.message).toMatch(/not responding/);
    expect(err.message).not.toMatch(/nginx|<h1>/);
  });
});
