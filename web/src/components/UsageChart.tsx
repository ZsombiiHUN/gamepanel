import { useId } from "react";

const W = 300;
const H = 120;

/** Area chart of readings since the page opened. `max` fixes the y-axis; `limit` draws a dashed line. */
export function UsageChart({
  title, value, unit, note, noteWarn, values, max, ticks, limit, color, action, since = "earlier",
}: {
  title: string;
  value: string;
  unit: string;
  note: string;
  noteWarn?: boolean;
  values: number[];
  max: number;
  ticks: { at: number; label: string }[];
  limit?: number;
  color: string;
  action?: React.ReactNode;
  /** Left end of the time axis ("earlier" for live readings, "24 h ago" for history). */
  since?: string;
}) {
  const gid = useId();
  const n = values.length;
  const pts = values.map((v, i) => [n > 1 ? (i * W) / (n - 1) : W, H - (Math.min(v, max) / max) * H] as const);
  const line = pts.map(([x, y]) => `${x.toFixed(1)},${y.toFixed(1)}`).join(" ");
  const area = n ? `M0,${H} L${pts.map(([x, y]) => `${x.toFixed(1)},${y.toFixed(1)}`).join(" L")} L${W},${H} Z` : "";
  const y = (v: number) => H - (v / max) * H;
  return (
    <section className="min-w-0 rounded-[14px] border border-zinc-800 bg-zinc-900 px-5 pb-4 pt-[18px]" aria-label={title}>
      <div className="flex items-start justify-between gap-3">
        <h2 className="text-[15px] font-semibold">{title}</h2>
        {action}
      </div>
      <p className={`h-display mt-2.5 text-[32px] font-bold leading-none ${noteWarn ? "text-warn-text" : ""}`}>
        {value}<span className="ml-2 font-sans text-sm font-normal tracking-normal text-zinc-400">{unit}</span>
      </p>
      <p className={`mt-1.5 text-[13px] ${noteWarn ? "text-warn-text" : "text-zinc-500"}`}>{note}</p>
      <div className="mt-3.5 flex gap-2.5">
        <div className="relative h-[120px] w-12 shrink-0" aria-hidden>
          {ticks.map((t) => (
            <span key={t.label} className="absolute right-0 translate-y-1/2 whitespace-nowrap font-mono text-[11.5px] text-zinc-500" style={{ bottom: `${(t.at / max) * 100}%` }}>{t.label}</span>
          ))}
        </div>
        <div className="min-w-0 flex-1">
          <svg viewBox={`0 0 ${W} ${H}`} preserveAspectRatio="none" className="block h-[120px] w-full overflow-visible" role="img" aria-label={`${title} over the last readings`}>
            <defs>
              <linearGradient id={gid} x1="0" y1="0" x2="0" y2="1">
                <stop offset="0" style={{ stopColor: color }} stopOpacity=".28" />
                <stop offset="1" style={{ stopColor: color }} stopOpacity="0" />
              </linearGradient>
            </defs>
            {ticks.map((t) => (
              <line key={t.label} x1="0" x2={W} y1={y(t.at)} y2={y(t.at)} className="stroke-line-strong" strokeWidth="1" vectorEffect="non-scaling-stroke" />
            ))}
            {n > 1 && <path d={area} fill={`url(#${gid})`} />}
            {limit !== undefined && (
              <line x1="0" x2={W} y1={y(limit) + 0.5} y2={y(limit) + 0.5} className="stroke-zinc-500" strokeWidth="1.2" strokeDasharray="4 4" vectorEffect="non-scaling-stroke" />
            )}
            {n > 1 && <polyline points={line} fill="none" style={{ stroke: color }} strokeWidth="2" strokeLinejoin="round" vectorEffect="non-scaling-stroke" />}
          </svg>
          <div className="mt-2 flex justify-between font-mono text-[11.5px] text-zinc-500" aria-hidden><span>{since}</span><span>now</span></div>
        </div>
      </div>
    </section>
  );
}
