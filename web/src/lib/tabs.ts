export const TABS = ["console", "files", "backups", "schedules", "users", "settings"] as const;
export type Tab = (typeof TABS)[number];
