import type { components } from "../api/client";

type ServerView = components["schemas"]["ServerView"];
export type SearchableServer = Pick<ServerView, "name" | "state" | "port"> & { egg_name?: string; node_name?: string };

export const stateLabel: Record<string, string> = {
  running: "Running",
  starting: "Starting",
  stopping: "Stopping",
  installing: "Installing",
  install_failed: "Install failed",
  crashed: "Crashed",
  offline: "Stopped",
  unknown: "Unknown",
  suspended: "Suspended",
};

/** The state to show: a suspension outranks whatever the node reports. */
export function shownState(s: { state: string; suspended?: boolean }, live?: string | null): string {
  return s.suspended ? "suspended" : (live ?? s.state);
}

export const GROUPS = ["running", "busy", "problem", "stopped"] as const;
export type Group = (typeof GROUPS)[number];
export const groupLabel: Record<Group, string> = {
  running: "Running",
  stopped: "Stopped",
  busy: "Starting or installing",
  problem: "Needs attention",
};

/** Coarse bucket for the status filter chips. */
export function groupOf(state: string): Group {
  if (state === "running") return "running";
  if (state === "starting" || state === "stopping" || state === "installing") return "busy";
  if (state === "install_failed" || state === "crashed" || state === "unknown" || state === "suspended") return "problem";
  return "stopped";
}

/**
 * True when every whitespace-separated word of `query` appears (case-insensitively)
 * in the server's name, game (egg), node, port or status as the user sees it.
 */
export function matchesServer(s: SearchableServer, query: string): boolean {
  const words = query.toLowerCase().split(/\s+/).filter(Boolean);
  if (words.length === 0) return true;
  const hay = [s.name, s.egg_name, s.node_name, String(s.port), s.state, stateLabel[s.state]].filter(Boolean).join("\n").toLowerCase();
  return words.every((w) => hay.includes(w));
}

/** Alphabetical, with numbers in order ("Server 2" before "Server 10"). Stable, so cards do not jump when a server starts. */
export function sortServers<T extends { name: string }>(list: readonly T[]): T[] {
  return [...list].sort((a, b) => a.name.localeCompare(b.name, undefined, { numeric: true, sensitivity: "base" }));
}

export function countGroups(list: readonly { state: string }[]): Record<Group, number> {
  const c: Record<Group, number> = { running: 0, stopped: 0, busy: 0, problem: 0 };
  for (const s of list) c[groupOf(s.state)]++;
  return c;
}

/** "1 running, 5 stopped" style summary, skipping empty groups. */
export function summarize(list: readonly { state: string }[]): string {
  const c = countGroups(list);
  const parts: string[] = [];
  if (c.running) parts.push(`${c.running} running`);
  if (c.busy) parts.push(`${c.busy} starting or installing`);
  if (c.problem) parts.push(`${c.problem} need attention`);
  if (c.stopped) parts.push(`${c.stopped} stopped`);
  return parts.join(", ");
}
