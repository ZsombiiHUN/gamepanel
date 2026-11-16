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
