import { minecraftKind } from "./minecraft";
import i18n from "../i18n";

export const TABS = [
  "console",
  "minecraft",
  "files",
  "mods",
  "backups",
  "schedules",
  "startup",
  "network",
  "databases",
  "activity",
  "users",
  "settings",
] as const;
export type Tab = (typeof TABS)[number];

/**
 * Every server tab, in sidebar order. `need` is a permission the viewer must
 * hold, or "owner" for sections only the owner and admins see.
 */
export const TAB_INFO: { id: Tab; label: string; need?: string; keywords?: string; minecraft?: boolean }[] = [
  {
    id: "console",
    get label() {
      return i18n.t("tabs.console");
    },
    need: "console.read",
  },
  {
    id: "minecraft",
    get label() {
      return i18n.t("tabs.minecraft");
    },
    need: "files.read",
    minecraft: true,
    keywords: "players whitelist ops bans server.properties motd version world seed icon gamerule",
  },
  {
    id: "files",
    get label() {
      return i18n.t("tabs.files");
    },
    need: "files.read",
  },
  {
    id: "mods",
    get label() {
      return i18n.t("tabs.mods");
    },
    need: "files.write",
    keywords: "plugins modrinth datapacks install",
  },
  {
    id: "backups",
    get label() {
      return i18n.t("tabs.backups");
    },
    need: "backups.read",
  },
  {
    id: "schedules",
    get label() {
      return i18n.t("tabs.schedules");
    },
    need: "schedules.manage",
  },
  {
    id: "startup",
    get label() {
      return i18n.t("tabs.startup");
    },
    need: "startup.read",
    keywords: "variables docker image java version command",
  },
  {
    id: "network",
    get label() {
      return i18n.t("tabs.network");
    },
    need: "network.read",
    keywords: "ports allocations",
  },
  {
    id: "databases",
    get label() {
      return i18n.t("tabs.databases");
    },
    need: "databases.read",
    keywords: "mysql mariadb sql",
  },
  {
    id: "activity",
    get label() {
      return i18n.t("tabs.activity");
    },
    need: "activity.read",
    keywords: "log history who audit",
  },
  {
    id: "users",
    get label() {
      return i18n.t("tabs.users");
    },
    need: "owner",
    keywords: "subusers permissions",
  },
  {
    id: "settings",
    get label() {
      return i18n.t("tabs.settings");
    },
  },
];

/** Whether a viewer with these rights may open a tab. */
function canSee(need: string | undefined, server: { owner: boolean; permissions: string[] }) {
  return !need || (need === "owner" ? server.owner : server.permissions.includes(need));
}

/** Whether a tab shows for this server: the viewer may open it, and it fits the game (the Minecraft tab). */
export function tabShown(
  t: { need?: string; minecraft?: boolean },
  server: { owner: boolean; permissions: string[]; features?: readonly string[] },
) {
  return canSee(t.need, server) && (!t.minecraft || minecraftKind(server.features) !== null);
}
