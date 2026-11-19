import { describe, expect, it } from "vitest";
import { areaSummary, groupPerms, requires, roleOf, ROLES, setArea, togglePerm } from "./access";

const known = [
  "console.read",
  "console.send",
  "power",
  "files.read",
  "files.write",
  "backups.read",
  "backups.create",
  "backups.restore",
  "backups.delete",
  "backups.download",
  "schedules.manage",
  "settings.reinstall",
  "settings.rename",
  "activity.read",
  "startup.read",
  "startup.update",
  "startup.docker-image",
  "network.read",
  "network.manage",
  "databases.read",
  "databases.manage",
];

describe("access", () => {
  it("ticks what a permission needs, and unticks what needs it", () => {
    expect(togglePerm([], "files.write", known)).toEqual(["files.read", "files.write"]);
    expect(togglePerm(["files.read", "files.write"], "files.read", known)).toEqual([]);
    expect(togglePerm(["console.read"], "power", known)).toEqual(["console.read", "power"]);
  });
  it("ticks and unticks a whole area", () => {
    expect(setArea([], "backups", true, known)).toEqual([
      "backups.read",
      "backups.create",
      "backups.restore",
      "backups.delete",
      "backups.download",
    ]);
    expect(setArea(["console.read", "console.send", "power"], "console", false, known)).toEqual(["power"]);
  });
  it("recognises roles", () => {
    for (const r of ROLES) expect(roleOf(r.perms(known), known)?.id).toBe(r.id);
    expect(roleOf(["power"], known)).toBeNull();
  });
  it("gives every role what its permissions need", () => {
    for (const r of ROLES) {
      const p = r.perms(known);
      for (const id of p) for (const need of requires(id)) expect(p).toContain(need);
    }
  });
  it("groups and summarises by area", () => {
    expect(
      groupPerms(known.map((id) => ({ id, description: "" })))
        .map((g) => g.label)
        .slice(0, 3),
    ).toEqual(["Console", "Power", "Files"]);
    expect(areaSummary(["files.read", "console.read", "power"])).toBe("Console, Power, Files");
  });
});
