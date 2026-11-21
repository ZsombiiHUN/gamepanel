export interface Reading {
  cpu_pct: number;
  mem_bytes: number;
  disk_bytes: number;
}

export interface Limits {
  memoryMb: number;
  diskMb: number;
  cpuPct: number;
}

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
  /** What one chart unit is worth in raw readings (bytes per MB or GB; 1 for CPU). */
  per: number;
}

const MB = 1024 ** 2;
const GB = 1024 ** 3;
const WARN_AT = 0.85;
// Theme colours (index.css), so the charts follow the light and dark themes.
const WARN = "var(--color-chart-warn)";
const CALM = "var(--color-chart-calm)";

/** Whole numbers from 10 up, one decimal below. */
export const gb = (n: number) => (n >= 10 ? n.toFixed(0) : n.toFixed(1));

const ticksOf = (max: number, label: (n: number) => string) => [0, max / 2, max].map((at) => ({ at, label: at === 0 ? "0" : label(at) }));

/** MB for scales under a gigabyte (a 128 MB server is not "0.1 GB"), GB from there. */
function sizeUnit(scaleBytes: number) {
  return scaleBytes < GB ? { name: "MB", per: MB, fmt: (n: number) => n.toFixed(0) } : { name: "GB", per: GB, fmt: gb };
}

/**
 * What the three usage charts show. `cpuHistory` only sets the CPU axis, which grows with the
 * readings so a quiet server does not draw a flat line at the bottom of a 400% scale.
 */
export function describeUsage(
  running: Reading | null,
  disk: Reading | null,
  limits: Limits,
  cpuHistory: readonly number[],
): [cpu: ChartSpec, memory: ChartSpec, storage: ChartSpec] {
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
    color: "var(--color-chart-cpu)",
    per: 1,
  };

  // Every server has a memory limit (the API requires at least 64 MB); measure against it, not a display floor.
  const memLimitBytes = Math.max(limits.memoryMb, 0) * MB;
  const memNowBytes = running ? running.mem_bytes : null;
  const memMaxBytes = memLimitBytes || Math.max(512 * MB, (memNowBytes ?? 0) * 1.25);
  const mu = sizeUnit(memMaxBytes);
  const memWarn = memLimitBytes > 0 && memNowBytes !== null && memNowBytes / memLimitBytes >= WARN_AT;
  const memory: ChartSpec = {
    title: "Memory",
    value: memNowBytes === null ? "-" : mu.fmt(memNowBytes / mu.per),
    unit: memLimitBytes ? `${mu.name} of ${mu.fmt(memLimitBytes / mu.per)} ${mu.name}` : `${mu.name}, no limit`,
    note:
      memNowBytes === null ? "Not running" : memLimitBytes ? `${Math.round((memNowBytes / memLimitBytes) * 100)}% used` : "No limit set",
    noteWarn: memWarn,
    max: memMaxBytes / mu.per,
    limit: memLimitBytes ? memLimitBytes / mu.per : undefined,
    ticks: ticksOf(memMaxBytes / mu.per, (n) => `${mu.fmt(n)} ${mu.name}`),
    color: memWarn ? WARN : CALM,
    per: mu.per,
  };

  const diskLimitBytes = Math.max(limits.diskMb, 0) * MB;
  const diskNowBytes = disk ? disk.disk_bytes : null;
  const diskMaxBytes = diskLimitBytes || Math.max(1, Math.ceil(((diskNowBytes ?? 0) / GB) * 1.25)) * GB;
  const du = sizeUnit(diskMaxBytes);
  const diskFull = diskLimitBytes > 0 && diskNowBytes !== null && diskNowBytes / diskLimitBytes >= WARN_AT;
  const storage: ChartSpec = {
    title: "Storage",
    value: diskNowBytes === null ? "-" : du.fmt(diskNowBytes / du.per),
    unit: diskLimitBytes ? `${du.name} of ${du.fmt(diskLimitBytes / du.per)} ${du.name}` : `${du.name}, no limit`,
    note: diskFull
      ? "Almost full. The server stops at its limit."
      : diskLimitBytes
        ? `${Math.round(((diskNowBytes ?? 0) / diskLimitBytes) * 100)}% used`
        : "No quota set",
    noteWarn: diskFull,
    max: diskMaxBytes / du.per,
    limit: diskLimitBytes ? diskLimitBytes / du.per : undefined,
    ticks: ticksOf(diskMaxBytes / du.per, (n) => `${du.fmt(n)} ${du.name}`),
    color: diskFull ? WARN : CALM,
    per: du.per,
  };
  return [cpu, memory, storage];
}
