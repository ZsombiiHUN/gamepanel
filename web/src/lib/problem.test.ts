import { describe, expect, it } from "vitest";
import { formatProblem } from "./problem";

describe("formatProblem", () => {
  it("uses detail", () => {
    expect(formatProblem({ title: "Bad Request", detail: "nope" })).toBe("nope");
  });
  it("falls back to title", () => {
    expect(formatProblem({ title: "Unauthorized" })).toBe("Unauthorized");
  });
  it("appends field errors with locations", () => {
    expect(
      formatProblem({
        detail: "validation failed",
        errors: [{ location: "body.name", message: "expected length >= 1" }, { message: "other" }],
      }),
    ).toBe("validation failed; body.name: expected length >= 1; other");
  });
  it("handles strings, errors and empties", () => {
    expect(formatProblem("  boom ")).toBe("boom");
    expect(formatProblem(new Error("x"))).toBe("x");
    expect(formatProblem(undefined, "fb")).toBe("fb");
    expect(formatProblem({}, "fb")).toBe("fb");
  });
});
