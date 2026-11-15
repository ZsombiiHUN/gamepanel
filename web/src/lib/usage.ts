export interface Reading { cpu_pct: number; mem_bytes: number; disk_bytes: number }

export interface Limits { memoryMb: number; diskMb: number; cpuPct: number }

export interface ChartSpec {
  title: string;
  value: string;
  unit: string;
  note: string;
  noteWarn: boolean;
  max: number;
  limit?: number;
  ticks: { at: number; label: string }[];
  color: string;
}

const GB = 1024 ** 3;
const WARN_AT = 0.85;
const WARN = "#e8a33d";
const CALM = "#8fa6ff";

/** Whole numbers from 10 up, one decimal below. */
export const gb = (n: number) => (n >= 10 ? n.toFixed(0) : n.toFixed(1));

const ticksOf = (max: number, label: (n: number) => string) => [0, max / 2, max].map((at) => ({ at, label: at === 0 ? "0" : label(at) }));

/**
 * What the three usage charts show. `cpuHistory` only sets the CPU axis, which grows with the
 * readings so a quiet server does not draw a flat line at the bottom of a 400% scale.
 */
export function describeUsage(running: Reading | null, disk: Reading | null, limits: Limits, cpuHistory: readonly number[]): [cpu: ChartSpec, memory: ChartSpec, storage: ChartSpec] {
  const cpuNow = running?.cpu_pct ?? null;
  const cpuMax = Math.max(20, Math.ceil(Math.max(0, ...cpuHistory) / 20) * 20 + 20);
  const cpu: ChartSpec = {
    title: "CPU",
    value: cpuNow === null ? "-" : `${cpuNow.toFixed(0)}%`,
    unit: limits.cpuPct ? `of ${limits.cpuPct}% limit` : "no limit",
    note: limits.cpuPct ? `About ${((cpuNow ?? 0) / 100).toFixed(1)} of ${limits.cpuPct / 100} cores in use` : "Share of one core",
    noteWarn: false,
    max: cpuMax,
    ticks: ticksOf(cpuMax, (n) => `${n}%`),
    color: "#6fcf97",
  };

  const memMax = Math.max(0.5, limits.memoryMb / 1024);
  const memNow = running ? running.mem_bytes / GB : null;
  const memWarn = memNow !== null && memNow / memMax >= WARN_AT;
  const memory: ChartSpec = {
    title: "Memory",
    value: memNow === null ? "-" : gb(memNow),
    unit: `GB of ${gb(memMax)} GB`,
    note: memNow === null ? "Not running" : `${Math.round((memNow / memMax) * 100)}% used`,
    noteWarn: memWarn,
    max: memMax,
    limit: memMax,
    ticks: ticksOf(memMax, (n) => `${gb(n)} GB`),
    color: memWarn ? WARN : CALM,
  };

  const diskLimit = limits.diskMb ? limits.diskMb / 1024 : 0;
  const diskNow = disk ? disk.disk_bytes / GB : null;
  const diskMax = diskLimit || Math.max(1, Math.ceil((diskNow ?? 0) * 1.25));
  const diskFull = diskLimit > 0 && diskNow !== null && diskNow / diskLimit >= WARN_AT;
  const storage: ChartSpec = {
    title: "Storage",
    value: diskNow === null ? "-" : gb(diskNow),
    unit: diskLimit ? `GB of ${gb(diskLimit)} GB` : "GB, no limit",
    note: diskFull ? "Almost full. The server stops at its limit." : diskLimit ? `${Math.round(((diskNow ?? 0) / diskLimit) * 100)}% used` : "No quota set",
    noteWarn: diskFull,
    max: diskMax,
    limit: diskLimit || undefined,
    ticks: ticksOf(diskMax, (n) => `${gb(n)} GB`),
    color: diskFull ? WARN : CALM,
  };
  return [cpu, memory, storage];
}
