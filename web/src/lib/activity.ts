/** Plain words for audit actions; unknown ones fall back to the raw action. */
const WORDS: Record<string, string> = {
  "server.create": "created the server",
  "server.delete": "deleted the server",
  "server.power": "sent a power action",
  "server.command": "ran a console command",
  "server.reinstall": "reinstalled the server",
  "server.limits": "changed the resource limits",
  "server.startup": "changed startup settings",
  "server.rename": "renamed the server",
  "server.owner": "changed the owner",
  "server.port.add": "added a port",
  "server.port.remove": "removed a port",
  "server.port.primary": "changed the primary port",
  "file.write": "saved a file",
  "file.upload": "uploaded a file",
  "file.delete": "deleted files",
  "file.move": "moved or renamed files",
  "file.copy": "copied a file",
  "file.chmod": "changed permissions",
  "file.compress": "made an archive",
  "file.decompress": "extracted an archive",
  "file.mkdir": "created a folder",
  "file.pull": "downloaded a file from a URL",
  "backup.create": "started a backup",
  "backup.delete": "deleted a backup",
  "backup.restore": "restored a backup",
  "backup.download": "downloaded a backup",
  "backup.lock": "locked a backup",
  "backup.unlock": "unlocked a backup",
  "schedule.create": "created a schedule",
  "schedule.update": "changed a schedule",
  "schedule.delete": "deleted a schedule",
  "schedule.run": "ran a schedule",
  "subuser.add": "gave someone access",
  "invite.create": "invited someone",
  "invite.delete": "withdrew an invitation",
  "mod.install": "installed a mod",
  "sftp.login": "connected over SFTP",
  "subuser.update": "changed someone's permissions",
  "subuser.remove": "removed someone's access",
  "database.create": "created a database",
  "database.delete": "deleted a database",
  "database.password.view": "viewed a database password",
  "database.password.rotate": "changed a database password",
};

export function describeAction(action: string, detail?: string) {
  const w = WORDS[action];
  if (!w) return action;
  // Power actions read better with the action itself: "sent a power action: restart" -> "restarted".
  if (action === "server.power" && detail) return { start: "started the server", stop: "stopped the server", restart: "restarted the server", kill: "killed the server" }[detail] ?? w;
  return w;
}

/** "3 minutes ago", "yesterday", or a date. */
export function relativeTime(unix: number, now = Date.now()) {
  const s = Math.round((now - unix * 1000) / 1000);
  if (s < 45) return "just now";
  const m = Math.round(s / 60);
  if (m < 60) return `${m} ${m === 1 ? "minute" : "minutes"} ago`;
  const h = Math.round(m / 60);
  if (h < 24) return `${h} ${h === 1 ? "hour" : "hours"} ago`;
  const d = Math.round(h / 24);
  if (d === 1) return "yesterday";
  if (d < 7) return `${d} days ago`;
  return new Date(unix * 1000).toLocaleDateString();
}

export type ActivityKind = "power" | "console" | "files" | "backups" | "schedules" | "access" | "databases" | "server";

/** Which kind of thing an action touched, for its icon and the filter chips. */
export function activityKind(action: string): ActivityKind {
  if (action === "server.power") return "power";
  if (action === "server.command") return "console";
  const prefix = action.split(".")[0];
  return (
    ({ file: "files", mod: "files", sftp: "files", backup: "backups", schedule: "schedules", subuser: "access", invite: "access", database: "databases" } as Record<string, ActivityKind>)[prefix ?? ""] ??
    "server"
  );
}

/**
 * Filter chips. `q` is the word sent to the server search (which also looks in
 * details), so results are checked again with activityKind.
 */
export const ACTIVITY_FILTERS: { id: ActivityKind; label: string; q: string }[] = [
  { id: "power", label: "Power", q: "server.power" },
  { id: "console", label: "Commands", q: "server.command" },
  { id: "files", label: "Files", q: "file." },
  { id: "backups", label: "Backups", q: "backup." },
  { id: "schedules", label: "Schedules", q: "schedule." },
  { id: "access", label: "Access", q: "subuser." },
  { id: "databases", label: "Databases", q: "database." },
];

const dayKey = (d: Date) => `${d.getFullYear()}-${d.getMonth()}-${d.getDate()}`;

/** "Today", "Yesterday", or a full date, for grouping entries by day. */
export function dayLabel(unix: number, now = Date.now()) {
  const d = new Date(unix * 1000);
  const today = new Date(now);
  const yesterday = new Date(now);
  yesterday.setDate(today.getDate() - 1);
  if (dayKey(d) === dayKey(today)) return "Today";
  if (dayKey(d) === dayKey(yesterday)) return "Yesterday";
  return d.toLocaleDateString(undefined, { weekday: "long", month: "long", day: "numeric", ...(d.getFullYear() !== today.getFullYear() ? { year: "numeric" } : {}) });
}

/** Split newest-first entries into runs that share a day label. */
export function groupByDay<T extends { at: number }>(rows: T[], now = Date.now()): { label: string; rows: T[] }[] {
  const out: { label: string; rows: T[] }[] = [];
  for (const r of rows) {
    const label = dayLabel(r.at, now);
    const last = out[out.length - 1];
    if (last && last.label === label) last.rows.push(r);
    else out.push({ label, rows: [r] });
  }
  return out;
}
