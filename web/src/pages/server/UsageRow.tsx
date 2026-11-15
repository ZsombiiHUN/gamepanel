import { UsageChart } from "../../components/UsageChart";
import { useRollingSamples } from "../../hooks/useRollingSamples";
import type { LiveStats } from "../../api/ws";
import { describeUsage, type Limits } from "../../lib/usage";

const MAX_POINTS = 30;

/** Readings since this page opened. The panel keeps no history, so neither do we. */
export function UsageRow({ running, latest, limits }: { running: LiveStats | null; latest: LiveStats | null; limits: Limits }) {
  const cpu = useRollingSamples(running, (s) => s.cpu_pct, MAX_POINTS);
  const mem = useRollingSamples(running, (s) => s.mem_bytes / 1024 ** 3, MAX_POINTS);
  const disk = useRollingSamples(latest, (s) => s.disk_bytes / 1024 ** 3, MAX_POINTS);
  const [cpuChart, memChart, diskChart] = describeUsage(running, latest, limits, cpu);
  return (
    <section aria-label="Live usage" className="space-y-2.5">
      <div className="grid gap-4 [grid-template-columns:repeat(auto-fit,minmax(min(100%,270px),1fr))]">
        <UsageChart {...cpuChart} values={cpu} />
        <UsageChart {...memChart} values={mem} />
        <UsageChart {...diskChart} values={disk} />
      </div>
      <p className="text-[12.5px] text-zinc-500">Charts show readings since this page was opened. The panel does not keep history.</p>
    </section>
  );
}
