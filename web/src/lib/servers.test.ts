import { describe, expect, it } from "vitest";
import { countGroups, groupOf, matchesServer, shownState, sortServers, summarize } from "./servers";

const s = (over: Partial<Parameters<typeof matchesServer>[0]> = {}) => ({
  name: "Survival SMP",
  state: "offline",
  port: 25565,
  egg_name: "Paper Minecraft",
  node_name: "eu-west-1",
  ...over,
});

describe("matchesServer", () => {
  it("matches everything for an empty or blank query", () => {
    expect(matchesServer(s(), "")).toBe(true);
    expect(matchesServer(s(), "   ")).toBe(true);
  });
  it.each([
    ["name", "survival"],
    ["name, any case", "SMP"],
    ["game (egg)", "minecraft"],
    ["node", "eu-west"],
    ["port", "25565"],
    ["status as shown to the user", "stopped"],
    ["raw state", "offline"],
  ])("matches by %s", (_n, q) => expect(matchesServer(s(), q)).toBe(true));

  it("requires every word to match, in any field", () => {
    expect(matchesServer(s(), "survival minecraft")).toBe(true);
    expect(matchesServer(s(), "minecraft  eu-west   25565")).toBe(true);
    expect(matchesServer(s(), "survival rust")).toBe(false);
  });
  it("does not match unrelated text", () => {
    expect(matchesServer(s(), "zzz")).toBe(false);
    expect(matchesServer(s(), "99999")).toBe(false);
  });
  it("uses the label for running servers, not just the raw state", () => {
    expect(matchesServer(s({ state: "install_failed" }), "install failed")).toBe(true);
    expect(matchesServer(s({ state: "running" }), "stopped")).toBe(false);
  });
  it("copes with servers that have no egg or node name", () => {
    const bare = { name: "x", state: "offline", port: 1 };
    expect(matchesServer(bare, "x")).toBe(true);
    expect(matchesServer(bare, "undefined")).toBe(false);
  });
});

describe("sortServers", () => {
  it("is alphabetical, ignoring case", () => {
    expect(sortServers([{ name: "b" }, { name: "A" }, { name: "c" }]).map((x) => x.name)).toEqual(["A", "b", "c"]);
  });
  it("orders numbers naturally", () => {
    expect(sortServers([{ name: "Server 10" }, { name: "Server 2" }, { name: "Server 1" }]).map((x) => x.name)).toEqual([
      "Server 1",
      "Server 2",
      "Server 10",
    ]);
  });
  it("does not mutate its input", () => {
    const input = [{ name: "b" }, { name: "a" }];
    sortServers(input);
    expect(input.map((x) => x.name)).toEqual(["b", "a"]);
  });
  it("does not depend on state, so a started server stays where it was", () => {
    const before = sortServers([
      { name: "a", state: "offline" },
      { name: "b", state: "offline" },
    ]).map((x) => x.name);
    const after = sortServers([
      { name: "a", state: "offline" },
      { name: "b", state: "running" },
    ]).map((x) => x.name);
    expect(after).toEqual(before);
  });
});

describe("groups", () => {
  it.each([
    ["running", "running"],
    ["offline", "stopped"],
    ["starting", "busy"],
    ["stopping", "busy"],
    ["installing", "busy"],
    ["install_failed", "problem"],
    ["unknown", "problem"],
    ["something-new", "stopped"],
  ])("%s is in %s", (state, group) => expect(groupOf(state)).toBe(group));

  it("counts and summarizes, skipping empty groups", () => {
    const list = [{ state: "running" }, { state: "offline" }, { state: "offline" }, { state: "install_failed" }];
    expect(countGroups(list)).toEqual({ running: 1, stopped: 2, busy: 0, problem: 1 });
    expect(summarize(list)).toBe("1 running, 1 need attention, 2 stopped");
    expect(summarize([])).toBe("");
  });
});

describe("suspended servers", () => {
  it("show as suspended whatever the node reports, and need attention", () => {
    expect(shownState({ state: "running", suspended: true }, "running")).toBe("suspended");
    expect(shownState({ state: "offline", suspended: false }, "running")).toBe("running");
    expect(shownState({ state: "offline" })).toBe("offline");
    expect(groupOf("suspended")).toBe("problem");
    expect(matchesServer({ name: "a", state: "suspended", port: 1 }, "suspended")).toBe(true);
  });
});
