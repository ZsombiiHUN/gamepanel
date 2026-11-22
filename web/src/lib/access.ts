import i18n from "../i18n";
import { translated } from "../i18n";
/** Subuser permissions as people think about them: areas, roles and what each tick implies. */

export interface Perm {
  id: string;
  description: string;
}

/** Friendly area names, in the order the picker shows them. Unknown prefixes go last under their own name. */
const AREAS: { id: string; label: string }[] = [
  {
    id: "console",
    get label() {
      return i18n.t("access.areas.console");
    },
  },
  {
    id: "power",
    get label() {
      return i18n.t("access.areas.power");
    },
  },
  {
    id: "files",
    get label() {
      return i18n.t("access.areas.files");
    },
  },
  {
    id: "backups",
    get label() {
      return i18n.t("access.areas.backups");
    },
  },
  {
    id: "schedules",
    get label() {
      return i18n.t("access.areas.schedules");
    },
  },
  {
    id: "startup",
    get label() {
      return i18n.t("access.areas.startup");
    },
  },
  {
    id: "network",
    get label() {
      return i18n.t("access.areas.network");
    },
  },
  {
    id: "databases",
    get label() {
      return i18n.t("access.areas.databases");
    },
  },
  {
    id: "activity",
    get label() {
      return i18n.t("access.areas.activity");
    },
  },
  {
    id: "settings",
    get label() {
      return i18n.t("access.areas.settings");
    },
  },
];

const areaOf = (perm: string) => perm.split(".")[0] ?? perm;

function areaLabel(area: string) {
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
const SHORT: Record<string, string> = translated({
  "console.read": "access.perms.console_read",
  "console.send": "access.perms.console_send",
  power: "access.perms.power",
  "files.read": "access.perms.files_read",
  "files.write": "access.perms.files_write",
  "backups.read": "access.perms.backups_read",
  "backups.create": "access.perms.backups_create",
  "backups.restore": "access.perms.backups_restore",
  "backups.delete": "access.perms.backups_delete",
  "backups.download": "access.perms.backups_download",
  "schedules.manage": "access.perms.schedules_manage",
  "settings.reinstall": "access.perms.settings_reinstall",
  "settings.rename": "access.perms.settings_rename",
  "activity.read": "access.perms.activity_read",
  "startup.read": "access.perms.startup_read",
  "startup.update": "access.perms.startup_update",
  "startup.docker-image": "access.perms.startup_docker_image",
  "network.read": "access.perms.network_read",
  "network.manage": "access.perms.network_manage",
  "databases.read": "access.perms.databases_read",
  "databases.manage": "access.perms.databases_manage",
});

export const permLabel = (id: string) => (SHORT as Record<string, string | undefined>)[id] ?? id;

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

export interface Role {
  id: string;
  label: string;
  desc: string;
  perms: (known: string[]) => string[];
}

const pick = (ids: string[]) => (known: string[]) => known.filter((p) => ids.includes(p));

/** Ready-made sets, from least to most. "Custom" is anything else. */
export const ROLES: Role[] = [
  {
    id: "viewer",
    get label() {
      return i18n.t("access.roles.viewer.label");
    },
    get desc() {
      return i18n.t("access.roles.viewer.desc");
    },
    perms: pick(["console.read", "activity.read"]),
  },
  {
    id: "moderator",
    get label() {
      return i18n.t("access.roles.moderator.label");
    },
    get desc() {
      return i18n.t("access.roles.moderator.desc");
    },
    perms: pick(["console.read", "console.send", "power", "activity.read", "backups.read", "backups.create"]),
  },
  {
    id: "manager",
    get label() {
      return i18n.t("access.roles.manager.label");
    },
    get desc() {
      return i18n.t("access.roles.manager.desc");
    },
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
    get label() {
      return i18n.t("access.roles.full.label");
    },
    get desc() {
      return i18n.t("access.roles.full.desc");
    },
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
