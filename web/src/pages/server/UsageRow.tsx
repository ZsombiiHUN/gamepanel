import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { UsageChart } from "../../components/UsageChart";
import { useRollingSamples } from "../../hooks/useRollingSamples";
import { metricsQuery, type MetricRange } from "../../api/queries";
import type { LiveStats } from "../../api/ws";
import { describeUsage, type Limits } from "../../lib/usage";
import { formatBytes } from "../../lib/format";

const MAX_POINTS = 30;
type Range = "live" | MetricRange;
const RANGES: { id: Range; label: string; since: string }[] = [
  { id: "live", label: "Live", since: "earlier" },
  { id: "1h", label: "1 h", since: "1 h ago" },
  { id: "24h", label: "24 h", since: "24 h ago" },
  { id: "7d", label: "7 days", since: "7 days ago" },
  { id: "30d", label: "30 days", since: "30 days ago" },
];

/** CPU, memory and disk: live readings, or the panel's history for a range. */
export function UsageRow({
  uuid,
  running,
  latest,
  limits,
}: {
  uuid: string;
  running: LiveStats | null;
  latest: LiveStats | null;
  limits: Limits;
}) {
  const [range, setRange] = useState<Range>("live");
  const liveCpu = useRollingSamples(running, (s) => s.cpu_pct, MAX_POINTS);
  // Raw bytes; each chart converts to its own unit (MB or GB, chosen from the limit).
  const liveMem = useRollingSamples(running, (s) => s.mem_bytes, MAX_POINTS);
  const liveDisk = useRollingSamples(latest, (s) => s.disk_bytes, MAX_POINTS);
  const history = useQuery({ ...metricsQuery(uuid, range === "live" ? "1h" : range), enabled: range !== "live" });

  const pts = range === "live" ? [] : (history.data?.points ?? []);
  const cpu = range === "live" ? liveCpu : pts.map((p) => (p.running ? p.cpu_avg : 0));
  const mem = range === "live" ? liveMem : pts.map((p) => (p.running ? p.mem_avg : 0));
  const disk = range === "live" ? liveDisk : pts.map((p) => p.disk);
  const [cpuChart, memChart, diskChart] = describeUsage(
    running,
    latest,
    limits,
    range === "live" ? cpu : [...cpu, ...pts.map((p) => p.cpu_max)],
  );
  const since = RANGES.find((r) => r.id === range)?.since ?? "earlier";
  const peakCpu = Math.max(0, ...pts.map((p) => p.cpu_max));
  const peakMem = Math.max(0, ...pts.map((p) => p.mem_max));
  const step = history.data?.step ?? 60;

  return (
    <section aria-label="Usage" className="space-y-2.5">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div role="group" aria-label="Time range" className="inline-flex rounded-lg border border-zinc-800 bg-zinc-900 p-0.5">
          {RANGES.map((r) => (
            <button
              key={r.id}
              type="button"
              aria-pressed={range === r.id}
              onClick={() => setRange(r.id)}
              className={`min-h-8 rounded-md px-3 text-[13px] focus-visible:outline-2 focus-visible:outline-indigo-500 ${range === r.id ? "bg-zinc-800 font-medium text-zinc-100" : "text-zinc-400 hover:text-zinc-100"}`}
            >
              {r.label}
            </button>
          ))}
        </div>
        {range !== "live" && history.data && pts.length > 0 && (
          <p className="text-[12.5px] text-zinc-500">
            Peaks: CPU {peakCpu.toFixed(0)}%, memory {formatBytes(peakMem)}
          </p>
        )}
      </div>
      <div className="grid gap-4 [grid-template-columns:repeat(auto-fit,minmax(min(100%,270px),1fr))]">
        <UsageChart {...cpuChart} values={cpu} since={since} />
        <UsageChart {...memChart} values={mem.map((b) => b / memChart.per)} since={since} />
        <UsageChart {...diskChart} values={disk.map((b) => b / diskChart.per)} since={since} />
      </div>
      <p className="text-[12.5px] text-zinc-500" aria-live="polite">
        {range === "live"
          ? "Live readings since this page was opened. Pick a range to see the history."
          : history.isPending
            ? "Loading history"
            : pts.length === 0
              ? "No history for this range yet. The panel records usage once a minute."
              : `Averages over ${step >= 3600 ? `${step / 3600} h` : `${step / 60} min`}. The panel keeps minutes for 2 days and quarter hours for 30 days.`}
      </p>
    </section>
  );
}
