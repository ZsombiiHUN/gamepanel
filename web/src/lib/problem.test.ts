import { afterEach, describe, expect, it, vi } from "vitest";
import { errorText, formatProblem, statusMessage } from "./problem";
import { ApiError } from "../api/client";

describe("formatProblem", () => {
  it("shows the panel's detail as a sentence", () => {
    expect(formatProblem({ title: "Conflict", detail: "stop the server first" }, 409)).toBe("Stop the server first");
  });
  it("names fields in plain words instead of request paths", () => {
    const msg = formatProblem(
      { detail: "validation failed", errors: [{ location: "body.limits.memory_mb", message: "expected number >= 64" }, { location: "body", message: "unexpected property" }] },
      422,
    );
    expect(msg).toBe("Memory mb: expected number >= 64; Unexpected property");
    expect(msg).not.toContain("body.");
    expect(msg).not.toContain("validation failed");
  });
  it("never shows a body that is not problem+json", () => {
    // What a reverse proxy or a dev server sends when the panel is down.
    const html = "<html><body><h1>502 Bad Gateway</h1><hr>nginx/1.27.0</body></html>";
    expect(formatProblem(html, 502)).toBe(statusMessage(502));
    expect(formatProblem("Error: connect ECONNREFUSED 127.0.0.1:8080", 500)).toBe(statusMessage(500));
    expect(formatProblem("404 page not found", 404)).toBe(statusMessage(404));
    expect(formatProblem(undefined, 429)).toBe(statusMessage(429));
    expect(formatProblem({}, 403)).toBe(statusMessage(403));
  });
  it("has a sentence for every status", () => {
    for (const s of [0, 400, 401, 403, 404, 409, 413, 422, 429, 500, 502, 503, 504, 507]) {
      expect(statusMessage(s)).toMatch(/^[A-Z].+\.$/);
    }
  });
});

describe("errorText", () => {
  afterEach(() => vi.restoreAllMocks());
  it("shows API errors and our own strings", () => {
    expect(errorText(new ApiError("Stop the server first", 409))).toBe("Stop the server first");
    expect(errorText("That is not valid egg JSON.")).toBe("That is not valid egg JSON.");
  });
  it("hides bugs and library errors, logging them for debugging", () => {
    const log = vi.spyOn(console, "error").mockImplementation(() => {});
    const bug = new TypeError("Cannot read properties of undefined (reading 'uuid')");
    const msg = errorText(bug);
    expect(msg).not.toContain("uuid");
    expect(msg).toMatch(/Something went wrong/);
    expect(log).toHaveBeenCalledWith(bug);
  });
});
