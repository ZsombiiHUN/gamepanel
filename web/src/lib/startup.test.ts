import { describe, expect, it } from "vitest";
import { fillCommand, inputKind } from "./startup";

describe("fillCommand", () => {
  it("fills known placeholders and keeps unknown ones", () => {
    expect(fillCommand("java -Xmx{{SERVER_MEMORY}}M -jar {{ JAR }} {{HIDDEN}}", { SERVER_MEMORY: "1024", JAR: "paper.jar" })).toBe(
      "java -Xmx1024M -jar paper.jar {{HIDDEN}}",
    );
  });
  it("does not treat values as patterns", () => {
    expect(fillCommand("{{A}}", { A: "$& $1" })).toBe("$& $1");
  });
});

describe("inputKind", () => {
  it("offers a list for in: rules", () => {
    expect(inputKind("required|string|in:survival,creative", "survival")).toEqual({ type: "select", options: ["survival", "creative"] });
  });
  it("keeps the egg's boolean spelling", () => {
    expect(inputKind("required|boolean", "1")).toMatchObject({ options: ["1", "0"] });
    expect(inputKind("required|boolean", "false")).toMatchObject({ options: ["true", "false"] });
  });
  it("uses a number keyboard for numbers and text otherwise", () => {
    expect(inputKind("required|integer|between:1,64", "20")).toEqual({ type: "number" });
    expect(inputKind("required|regex:/^(a|in:b)$/", "a")).toEqual({ type: "text" });
  });
});
