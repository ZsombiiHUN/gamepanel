import { describe, expect, it } from "vitest";
import { isArchive, modeString, moveTarget, nameProblem, octal } from "./files";

describe("file helpers", () => {
  it("recognises archives the panel can extract", () => {
    for (const n of ["a.zip", "w.tar.gz", "w.TGZ", "x.tar", "y.tar.bz2", "log.gz"]) expect(isArchive(n)).toBe(true);
    for (const n of ["a.jar", "x.rar", "y.7z", "tar"]) expect(isArchive(n)).toBe(false);
  });
  it("formats permissions", () => {
    expect(modeString(0o750)).toBe("rwxr-x---");
    expect(modeString(0o644)).toBe("rw-r--r--");
    expect(octal(0o640)).toBe("640");
    expect(octal(0o7)).toBe("007");
  });
  it("checks names", () => {
    expect(nameProblem("ok.txt")).toBeNull();
    expect(nameProblem("")).not.toBeNull();
    expect(nameProblem("..")).not.toBeNull();
    expect(nameProblem("a/b")).not.toBeNull();
  });
  it("builds move targets", () => {
    expect(moveTarget("/plugins/a.jar", "/old/")).toBe("/old/a.jar");
    expect(moveTarget("/a.jar", "/")).toBe("/a.jar");
    expect(moveTarget("/x/dir", "backup")).toBe("/backup/dir");
  });
});
