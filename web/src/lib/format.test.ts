import { describe, expect, it } from "vitest";
import { formatBytes, joinPath, parentPath } from "./format";

describe("formatBytes", () => {
  it.each([
    [0, "0 B"],
    [1023, "1023 B"],
    [1024, "1.0 KiB"],
    [1536, "1.5 KiB"],
    [10 * 1024 ** 3, "10.0 GiB"],
    [150 * 1024 ** 2, "150 MiB"],
    [5 * 1024 ** 4, "5.0 TiB"],
    [5000 * 1024 ** 4, "5000 TiB"],
  ])("%d -> %s", (n, out) => expect(formatBytes(n)).toBe(out));
  it("shows a dash for nonsense", () => {
    expect(formatBytes(-1)).toBe("-");
    expect(formatBytes(NaN)).toBe("-");
    expect(formatBytes(Infinity)).toBe("-");
  });
});

describe("paths", () => {
  it("joins onto the root and onto folders", () => {
    expect(joinPath("/", "a")).toBe("/a");
    expect(joinPath("", "a")).toBe("/a");
    expect(joinPath("/a", "b")).toBe("/a/b");
    expect(joinPath("/a/", "b")).toBe("/a/b");
  });
  it("finds the parent, stopping at the root", () => {
    expect(parentPath("/a/b/c")).toBe("/a/b");
    expect(parentPath("/a")).toBe("/");
    expect(parentPath("/")).toBe("/");
  });
});
