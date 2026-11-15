import { describe, expect, it } from "vitest";
import { blockIcon } from "./icon";

describe("blockIcon", () => {
  it("gives the same icon for the same name", () => {
    expect(blockIcon("Survival SMP")).toEqual(blockIcon("Survival SMP"));
  });
  it("gives different icons to different names", () => {
    const names = ["Survival SMP", "Creative Plots", "Skyblock", "Rust Wipe Friday", "Server 2", "Server 10"];
    expect(new Set(names.map((n) => JSON.stringify(blockIcon(n)))).size).toBe(names.length);
  });
  it("is mirrored left to right, so it reads as a badge", () => {
    for (const name of ["a", "Lobby 1", "Valheim Friends"]) {
      const xs = new Set(blockIcon(name).blocks.map((b) => `${b.y}:${b.x}`));
      for (const b of blockIcon(name).blocks) expect(xs.has(`${b.y}:${3 + (4 - (b.x - 3) / 7) * 7}`)).toBe(true);
    }
  });
  it("stays inside the 38-unit grid and uses a valid hue", () => {
    const { hue, blocks } = blockIcon("anything at all");
    expect(hue).toBeGreaterThanOrEqual(0);
    expect(hue).toBeLessThan(360);
    for (const b of blocks) {
      expect(b.x).toBeGreaterThanOrEqual(3);
      expect(b.x + 6).toBeLessThanOrEqual(38);
      expect(b.y + 6).toBeLessThanOrEqual(38);
    }
  });
  it("copes with an empty name", () => {
    expect(() => blockIcon("")).not.toThrow();
  });
});
