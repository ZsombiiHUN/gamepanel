/** Subuser permissions as people think about them: areas, roles and what each tick implies. */

export type Perm = { id: string; description: string };

/** Friendly area names, in the order the picker shows them. Unknown prefixes go last under their own name. */
export const AREAS: { id: string; label: string }[] = [
  { id: "console", label: "Console" },
  { id: "power", label: "Power" },
  { id: "files", label: "Files" },
  { id: "backups", label: "Backups" },
  { id: "schedules", label: "Schedules" },
  { id: "startup", label: "Startup" },
  { id: "network", label: "Network" },
  { id: "databases", label: "Databases" },
  { id: "activity", label: "Activity" },
  { id: "settings", label: "Settings" },
];

export const areaOf = (perm: string) => perm.split(".")[0] ?? perm;

export function areaLabel(area: string) {
  return AREAS.find((a) => a.id === area)?.label ?? area.charAt(0).toUpperCase() + area.slice(1);
}

/** Permissions grouped by area, in AREAS order. */
export function groupPerms(all: Perm[]): { area: string; label: string; perms: Perm[] }[] {
  const groups = new Map<string, Perm[]>();
  for (const p of all) groups.set(areaOf(p.id), [...(groups.get(areaOf(p.id)) ?? []), p]);
  const rank = (a: string) => {
    const i = AREAS.findIndex((x) => x.id === a);
    return i < 0 ? AREAS.length : i;
  };
  return [...groups]
    .sort(([a], [b]) => rank(a) - rank(b) || a.localeCompare(b))
    .map(([area, perms]) => ({ area, label: areaLabel(area), perms }));
}

/** Short names for each permission; the server's description goes under them. */
const SHORT: Record<string, string> = {
  "console.read": "View console",
  "console.send": "Send commands",
  power: "Start, stop and restart",
  "files.read": "Browse and download",
  "files.write": "Upload, edit and delete",
  "backups.read": "See backups",
  "backups.create": "Create",
  "backups.restore": "Restore",
  "backups.delete": "Delete and lock",
  "backups.download": "Download",
  "schedules.manage": "Manage schedules",
  "settings.reinstall": "Reinstall",
  "settings.rename": "Rename",
  "activity.read": "See the activity log",
  "startup.read": "See startup settings",
  "startup.update": "Change variables",
  "startup.docker-image": "Change Docker image",
  "network.read": "See ports",
  "network.manage": "Manage ports",
  "databases.read": "See databases",
  "databases.manage": "Manage databases",
};

export const permLabel = (id: string) => SHORT[id] ?? id;

/**
 * What a permission is useless without. Ticking one ticks these too, and
 * unticking a requirement unticks what depends on it.
 */
const REQUIRES: Record<string, string[]> = {
  "console.send": ["console.read"],
  "files.write": ["files.read"],
  "backups.create": ["backups.read"],
  "backups.restore": ["backups.read"],
  "backups.delete": ["backups.read"],
  "backups.download": ["backups.read"],
  "startup.update": ["startup.read"],
  "startup.docker-image": ["startup.read"],
  "network.manage": ["network.read"],
  "databases.manage": ["databases.read"],
};

export const requires = (id: string) => REQUIRES[id] ?? [];

/** Toggle one permission, keeping requirements consistent. Only touches ids in `known`. */
export function togglePerm(value: string[], id: string, known: string[]): string[] {
  const has = new Set(value);
  if (has.has(id)) {
    has.delete(id);
    // Drop what needs it.
    for (const [p, reqs] of Object.entries(REQUIRES)) if (reqs.includes(id)) has.delete(p);
  } else {
    has.add(id);
    for (const r of requires(id)) if (known.includes(r)) has.add(r);
  }
  return known.filter((p) => has.has(p));
}

/** Tick or untick a whole area at once. */
export function setArea(value: string[], area: string, on: boolean, known: string[]): string[] {
  const has = new Set(value);
  for (const p of known) {
    if (areaOf(p) !== area) continue;
    if (on) has.add(p);
    else has.delete(p);
  }
  if (!on) for (const [p, reqs] of Object.entries(REQUIRES)) if (reqs.some((r) => areaOf(r) === area)) has.delete(p);
  return known.filter((p) => has.has(p));
}

export type Role = { id: string; label: string; desc: string; perms: (known: string[]) => string[] };

const pick = (ids: string[]) => (known: string[]) => known.filter((p) => ids.includes(p));

/** Ready-made sets, from least to most. "Custom" is anything else. */
export const ROLES: Role[] = [
  {
    id: "viewer",
    label: "Viewer",
    desc: "Watches the console and status, nothing else.",
    perms: pick(["console.read", "activity.read"]),
  },
  {
    id: "moderator",
    label: "Moderator",
    desc: "Runs commands, restarts the server and takes backups.",
    perms: pick(["console.read", "console.send", "power", "activity.read", "backups.read", "backups.create"]),
  },
  {
    id: "manager",
    label: "Manager",
    desc: "Also edits files, mods, schedules and startup settings.",
    perms: pick([
      "console.read",
      "console.send",
      "power",
      "activity.read",
      "files.read",
      "files.write",
      "backups.read",
      "backups.create",
      "backups.restore",
      "backups.download",
      "schedules.manage",
      "startup.read",
      "startup.update",
      "network.read",
      "databases.read",
    ]),
  },
  {
    id: "full",
    label: "Full access",
    desc: "Everything a subuser can have. Still cannot manage access or delete the server.",
    perms: (known) => [...known],
  },
];

const same = (a: string[], b: string[]) => a.length === b.length && a.every((x) => b.includes(x));

/** The role these permissions match exactly, or null for a custom set. */
export function roleOf(value: string[], known: string[]): Role | null {
  if (known.length === 0) return null;
  return ROLES.find((r) => same(r.perms(known), value)) ?? null;
}

/** "Console, Files, Backups" for a permission list, in area order. */
export function areaSummary(value: string[]): string {
  const areas = [...new Set(value.map(areaOf))];
  const rank = (a: string) => {
    const i = AREAS.findIndex((x) => x.id === a);
    return i < 0 ? AREAS.length : i;
  };
  return areas
    .sort((a, b) => rank(a) - rank(b))
    .map(areaLabel)
    .join(", ");
}
