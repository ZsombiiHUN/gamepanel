import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { UsageChart } from "../../components/UsageChart";
import { useRollingSamples } from "../../hooks/useRollingSamples";
import { metricsQuery, type MetricRange } from "../../api/queries";
import type { LiveStats } from "../../api/ws";
import { describeUsage, type Limits } from "../../lib/usage";
import { formatBytes } from "../../lib/format";
import { useTranslation } from "react-i18next";

const MAX_POINTS = 30;
type Range = "live" | MetricRange;
const RANGES: { id: Range }[] = [{ id: "live" }, { id: "1h" }, { id: "24h" }, { id: "7d" }, { id: "30d" }];

/** CPU, memory and disk: live readings, or the panel's history for a range. */
export function UsageRow({
  uuid,
  running,
  latest,
  limits,
  rail = false,
}: {
  uuid: string;
  /** Stack the charts in one narrow card (beside the console on wide screens). */
  rail?: boolean;
  running: LiveStats | null;
  latest: LiveStats | null;
  limits: Limits;
}) {
  const { t } = useTranslation();
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
  const since = t(`usageRow.since.${range}`);
  const peakCpu = Math.max(0, ...pts.map((p) => p.cpu_max));
  const peakMem = Math.max(0, ...pts.map((p) => p.mem_max));
  const step = history.data?.step ?? 60;

  return (
    <section aria-label={t("usageRow.usage")} className="space-y-2.5">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div role="group" aria-label={t("usageRow.timeRange")} className="seg">
          {RANGES.map((r) => (
            <button key={r.id} type="button" aria-pressed={range === r.id} onClick={() => setRange(r.id)}>
              {t(`usageRow.range.${r.id}`)}
            </button>
          ))}
        </div>
        {range !== "live" && history.data && pts.length > 0 && (
          <p className="text-[12.5px] text-zinc-500">{t("usageRow.peaks", { cpu: peakCpu.toFixed(0), memory: formatBytes(peakMem) })}</p>
        )}
      </div>
      <div
        className={
          rail
            ? "divide-y divide-line overflow-hidden rounded-xl border border-line bg-zinc-900"
            : "grid gap-3 [grid-template-columns:repeat(auto-fit,minmax(min(100%,270px),1fr))]"
        }
      >
        <UsageChart {...cpuChart} values={cpu} since={since} compact={rail} />
        <UsageChart {...memChart} values={mem.map((b) => b / memChart.per)} since={since} compact={rail} />
        <UsageChart {...diskChart} values={disk.map((b) => b / diskChart.per)} since={since} compact={rail} />
      </div>
      <p className="text-[12.5px] text-zinc-500" aria-live="polite">
        {range === "live"
          ? t("usageRow.liveReadingsSinceThisPage")
          : history.isPending
            ? t("usageRow.loadingHistory")
            : pts.length === 0
              ? t("usageRow.noHistoryForThisRange")
              : t("usageRow.averages", {
                  step: step >= 3600 ? t("usageRow.hours", { n: step / 3600 }) : t("usageRow.minutes", { n: step / 60 }),
                })}
      </p>
    </section>
  );
}
