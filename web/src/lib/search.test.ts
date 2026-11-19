import { describe, expect, it } from "vitest";
import { oneOf, page, text } from "./search";

describe("search param validators", () => {
  it("text keeps non-blank strings and drops everything else", () => {
    expect([text("abc"), text(" a "), text(""), text("   "), text(5), text(undefined), text(["a"])]).toEqual([
      "abc",
      " a ",
      undefined,
      undefined,
      undefined,
      undefined,
      undefined,
    ]);
  });
  it("oneOf accepts only listed values", () => {
    expect([oneOf("files", ["files", "console"] as const), oneOf("nope", ["files"] as const), oneOf(1, ["files"] as const)]).toEqual([
      "files",
      undefined,
      undefined,
    ]);
  });
  it("page accepts positive whole numbers, as numbers or numeric strings", () => {
    expect([page(2), page("3"), page(0), page(-1), page(1.5), page("x"), page(undefined), page(NaN)]).toEqual([
      2,
      3,
      undefined,
      undefined,
      undefined,
      undefined,
      undefined,
      undefined,
    ]);
  });
});
