export const TABS = ["console", "files", "backups", "schedules", "startup", "network", "users", "settings"] as const;
export type Tab = (typeof TABS)[number];

/**
 * Every server tab, in sidebar order. `need` is a permission the viewer must
 * hold, or "owner" for sections only the owner and admins see.
 */
export const TAB_INFO: { id: Tab; label: string; need?: string; keywords?: string }[] = [
  { id: "console", label: "Console", need: "console.read" },
  { id: "files", label: "Files", need: "files.read" },
  { id: "backups", label: "Backups", need: "backups.read" },
  { id: "schedules", label: "Schedules", need: "schedules.manage" },
  { id: "startup", label: "Startup", need: "startup.read", keywords: "variables docker image java version command" },
  { id: "network", label: "Network", need: "network.read", keywords: "ports allocations" },
  { id: "users", label: "Access", need: "owner", keywords: "subusers permissions" },
  { id: "settings", label: "Settings" },
];

/** Whether a viewer with these rights may open a tab. */
export function canSee(need: string | undefined, server: { owner: boolean; permissions: string[] }) {
  return !need || (need === "owner" ? server.owner : server.permissions.includes(need));
}
