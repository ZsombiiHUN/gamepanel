import { describe, expect, it, vi } from "vitest";
import { searchFiles, type Entry } from "./fileSearch";

const f = (name: string): Entry => ({ name, dir: false, size: 1, mod_unix: 0 });
const d = (name: string): Entry => ({ name, dir: true, size: 0, mod_unix: 0 });

/** An in-memory file tree: directory path -> entries. */
function tree(t: Record<string, Entry[]>) {
  const list = vi.fn(async (dir: string) => {
    if (!(dir in t)) throw new Error(`no such dir ${dir}`);
    return t[dir]!;
  });
  return list;
}

const mc = {
  "/": [d("config"), d("plugins"), d("logs"), f("server.properties"), f("ops.json")],
  "/config": [f("paper-global.yml")],
  "/plugins": [d("Essentials"), d("WorldEdit")],
  "/plugins/Essentials": [f("config.yml"), f("userdata.json")],
  "/plugins/WorldEdit": [f("config.yml"), d("schematics")],
  "/plugins/WorldEdit/schematics": [f("spawn.schem")],
  "/logs": [f("latest.log")],
};

describe("searchFiles", () => {
  it("finds matches in nested folders, with full paths", async () => {
    const r = await searchFiles(tree(mc), "/", "config");
    expect(r.hits.map((h) => h.path)).toEqual(["/config", "/plugins/Essentials/config.yml", "/plugins/WorldEdit/config.yml"]);
    expect(r.truncated).toBe(false);
  });
  it("is case-insensitive and trims the query", async () => {
    const r = await searchFiles(tree(mc), "/", "  ESSENTIALS ");
    expect(r.hits.map((h) => h.path)).toEqual(["/plugins/Essentials"]);
  });
  it("lists shallow matches before deep ones", async () => {
    const r = await searchFiles(tree(mc), "/", "e");
    const depth = r.hits.map((h) => h.path.split("/").length);
    expect(depth).toEqual([...depth].sort((a, b) => a - b));
  });
  it("keeps the folder flag so the UI can open it", async () => {
    const r = await searchFiles(tree(mc), "/", "schematics");
    expect(r.hits).toMatchObject([{ path: "/plugins/WorldEdit/schematics", dir: true }]);
  });
  it("searches only below the starting folder", async () => {
    const r = await searchFiles(tree(mc), "/plugins", "config");
    expect(r.hits.map((h) => h.path)).toEqual(["/plugins/Essentials/config.yml", "/plugins/WorldEdit/config.yml"]);
  });
  it("returns nothing, without listing anything, for a blank query", async () => {
    const list = tree(mc);
    expect((await searchFiles(list, "/", "   ")).hits).toEqual([]);
    expect(list).not.toHaveBeenCalled();
  });
  it("returns no hits for a query that matches nothing", async () => {
    const r = await searchFiles(tree(mc), "/", "nope");
    expect(r).toEqual({ hits: [], scanned: 7, truncated: false });
  });
  it("skips a folder that cannot be listed instead of failing", async () => {
    const t = { ...mc };
    delete (t as Record<string, Entry[]>)["/plugins/Essentials"];
    const r = await searchFiles(tree(t), "/", "config");
    expect(r.hits.map((h) => h.path)).toEqual(["/config", "/plugins/WorldEdit/config.yml"]);
  });
  it("stops at the folder limit and says so", async () => {
    const list = tree(mc);
    const r = await searchFiles(list, "/", "zzz", { maxDirs: 3 });
    expect(list).toHaveBeenCalledTimes(3);
    expect(r.scanned).toBe(3);
    expect(r.truncated).toBe(true);
  });
  it("stops at the hit limit and says so", async () => {
    const many = { "/": Array.from({ length: 50 }, (_, i) => f(`log-${i}.txt`)) };
    const r = await searchFiles(tree(many), "/", "log", { maxHits: 10 });
    expect(r.hits).toHaveLength(10);
    expect(r.truncated).toBe(true);
  });
  it("does not list more folders at once than the concurrency allows", async () => {
    let live = 0;
    let peak = 0;
    const wide: Record<string, Entry[]> = { "/": Array.from({ length: 12 }, (_, i) => d(`d${i}`)) };
    for (let i = 0; i < 12; i++) wide[`/d${i}`] = [];
    const list = async (dir: string) => {
      live++;
      peak = Math.max(peak, live);
      await new Promise((r) => setTimeout(r, 5));
      live--;
      return wide[dir]!;
    };
    await searchFiles(list, "/", "x", { concurrency: 3 });
    expect(peak).toBeLessThanOrEqual(3);
  });
  it("stops listing once aborted", async () => {
    const ctl = new AbortController();
    const list = vi.fn(async (dir: string) => {
      ctl.abort();
      return (mc as Record<string, Entry[] | undefined>)[dir] ?? [];
    });
    await searchFiles(list, "/", "config", { signal: ctl.signal, concurrency: 1 });
    expect(list.mock.calls.length).toBeLessThan(7);
  });
});
