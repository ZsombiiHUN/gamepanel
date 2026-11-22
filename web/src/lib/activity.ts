import i18n from "../i18n";
import en from "../i18n/locales/en/translation.json";
/** Plain words for audit actions (keys in activity.actions, dots as underscores); unknown ones fall back to the raw action. */
const WORDS = en.activity.actions;
type ActionId = keyof typeof WORDS;

const POWER = ["start", "stop", "restart", "kill"] as const;

export function describeAction(action: string, detail?: string) {
  const id = action.replaceAll(".", "_");
  if (!(id in WORDS)) return action;
  const w = i18n.t(`activity.actions.${id as ActionId}`);
  // Power actions read better with the action itself: "sent a power action: restart" -> "restarted".
  if (action === "server.power" && detail)
    return (POWER as readonly string[]).includes(detail) ? i18n.t(`activity.power.${detail as (typeof POWER)[number]}`) : w;
  return w;
}

/** "3 minutes ago", "yesterday", or a date. */
export function relativeTime(unix: number, now = Date.now()) {
  const s = Math.round((now - unix * 1000) / 1000);
  if (s < 45) return i18n.t("activity.justNow");
  const rtf = new Intl.RelativeTimeFormat(i18n.language, { numeric: "auto" });
  const m = Math.round(s / 60);
  if (m < 60) return rtf.format(-m, "minute");
  const h = Math.round(m / 60);
  if (h < 24) return rtf.format(-h, "hour");
  const d = Math.round(h / 24);
  if (d < 7) return rtf.format(-d, "day");
  return new Date(unix * 1000).toLocaleDateString(i18n.language);
}

export type ActivityKind = "power" | "console" | "files" | "backups" | "schedules" | "access" | "databases" | "server";

/** Which kind of thing an action touched, for its icon and the filter chips. */
export function activityKind(action: string): ActivityKind {
  if (action === "server.power") return "power";
  if (action === "server.command") return "console";
  const prefix = action.split(".")[0];
  return (
    (
      {
        file: "files",
        mod: "files",
        sftp: "files",
        backup: "backups",
        schedule: "schedules",
        subuser: "access",
        invite: "access",
        database: "databases",
      } as Record<string, ActivityKind>
    )[prefix ?? ""] ?? "server"
  );
}

/**
 * Filter chips. `q` is the word sent to the server search (which also looks in
 * details), so results are checked again with activityKind.
 */
export const ACTIVITY_FILTERS: { id: ActivityKind; label: string; q: string }[] = [
  {
    id: "power",
    get label() {
      return i18n.t("activity.filters.power");
    },
    q: "server.power",
  },
  {
    id: "console",
    get label() {
      return i18n.t("activity.filters.console");
    },
    q: "server.command",
  },
  {
    id: "files",
    get label() {
      return i18n.t("activity.filters.files");
    },
    q: "file.",
  },
  {
    id: "backups",
    get label() {
      return i18n.t("activity.filters.backups");
    },
    q: "backup.",
  },
  {
    id: "schedules",
    get label() {
      return i18n.t("activity.filters.schedules");
    },
    q: "schedule.",
  },
  {
    id: "access",
    get label() {
      return i18n.t("activity.filters.access");
    },
    q: "subuser.",
  },
  {
    id: "databases",
    get label() {
      return i18n.t("activity.filters.databases");
    },
    q: "database.",
  },
];

const dayKey = (d: Date) => `${d.getFullYear()}-${d.getMonth()}-${d.getDate()}`;

/** "Today", "Yesterday", or a full date, for grouping entries by day. */
export function dayLabel(unix: number, now = Date.now()) {
  const d = new Date(unix * 1000);
  const today = new Date(now);
  const yesterday = new Date(now);
  yesterday.setDate(today.getDate() - 1);
  if (dayKey(d) === dayKey(today)) return i18n.t("activity.today");
  if (dayKey(d) === dayKey(yesterday)) return i18n.t("activity.yesterday");
  return d.toLocaleDateString(i18n.language, {
    weekday: "long",
    month: "long",
    day: "numeric",
    ...(d.getFullYear() !== today.getFullYear() ? { year: "numeric" } : {}),
  });
}

/** Split newest-first entries into runs that share a day label. */
export function groupByDay<T extends { at: number }>(rows: T[], now = Date.now()): { label: string; rows: T[] }[] {
  const out: { label: string; rows: T[] }[] = [];
  for (const r of rows) {
    const label = dayLabel(r.at, now);
    const last = out[out.length - 1];
    if (last?.label === label) last.rows.push(r);
    else out.push({ label, rows: [r] });
  }
  return out;
}
