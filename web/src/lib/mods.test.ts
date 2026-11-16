import { describe, expect, it } from "vitest";
import { guessFromEgg } from "./mods";

describe("guessFromEgg", () => {
  it("picks plugins for plugin servers and mods for modded ones", () => {
    expect(guessFromEgg("Paper Minecraft")).toEqual({ type: "plugin", loader: "paper" });
    expect(guessFromEgg("Fabric")).toEqual({ type: "mod", loader: "fabric" });
    expect(guessFromEgg("NeoForge 1.21")).toEqual({ type: "mod", loader: "neoforge" });
    expect(guessFromEgg("Vanilla Minecraft")).toEqual({ type: "datapack", loader: "" });
    expect(guessFromEgg("Valheim")).toEqual({ type: "mod", loader: "" });
  });
});
