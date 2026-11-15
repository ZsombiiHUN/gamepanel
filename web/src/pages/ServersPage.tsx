import { useState } from "react";
import { Link } from "@tanstack/react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Loader2, Plus, Search, SquareTerminal } from "lucide-react";
import { api, call, type components } from "../api/client";
import { meQuery, serversQuery } from "../api/queries";
import { ErrorBox, Meter, ServerIcon, Spinner, StateBadge } from "../components/ui";
import { formatBytes } from "../lib/format";

type ServerView = components["schemas"]["ServerView"];

function ServerCard({ s }: { s: ServerView }) {
  const qc = useQueryClient();
  const running = s.state === "running" || s.state === "starting";
  const busy = s.state === "installing" || s.state === "stopping";
  const power = useMutation({
    mutationFn: () => call(api.POST("/servers/{uuid}/power", { params: { path: { uuid: s.uuid } }, body: { action: running ? "stop" : "start" } })),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["servers"] }),
  });
  const live = s.stats && s.state === "running" ? s.stats : null;
  const memPct = live && s.memory_mb ? (live.mem_bytes / (s.memory_mb * 1024 * 1024)) * 100 : null;
  const diskPct = s.stats && s.disk_mb ? (s.stats.disk_bytes / (s.disk_mb * 1024 * 1024)) * 100 : null;
  const cpuPct = live ? (s.cpu_pct ? (live.cpu_pct / s.cpu_pct) * 100 : live.cpu_pct) : null;
  return (
    <article className="card flex flex-col gap-4 !rounded-[14px]">
      <div className="flex items-start gap-3.5">
        <ServerIcon name={s.name} size={44} />
        <div className="min-w-0 flex-1">
          <Link to="/servers/$uuid" params={{ uuid: s.uuid }} search={{ tab: "console" }} className="h-display block truncate text-xl font-bold focus-visible:outline-2 focus-visible:outline-indigo-500">
            {s.name}
          </Link>
          <div className="mt-0.5 text-[13px] text-zinc-500">Port {s.port}{s.node_online ? "" : " · node offline"}</div>
        </div>
        <StateBadge state={s.state} />
      </div>
      <div className="flex flex-col gap-3.5">
        {live && <Meter label="CPU" text={`${live.cpu_pct.toFixed(0)}%`} pct={cpuPct} />}
        {live && <Meter label="Memory" text={`${formatBytes(live.mem_bytes)} / ${formatBytes(s.memory_mb * 1024 * 1024)}`} pct={memPct} />}
        {s.stats && <Meter label="Disk" text={`${formatBytes(s.stats.disk_bytes)} / ${s.disk_mb ? formatBytes(s.disk_mb * 1024 * 1024) : "no limit"}`} pct={diskPct} />}
        {!s.stats && <p className="py-1 text-[13px] text-zinc-500">{s.state === "installing" ? "Setting up files. This takes a few minutes the first time." : "Not running. Start it to see live usage."}</p>}
      </div>
      <div className="mt-auto flex gap-2 border-t border-[#1d2127] pt-4">
        <Link to="/servers/$uuid" params={{ uuid: s.uuid }} search={{ tab: "console" }} className="btn flex-1">
          <SquareTerminal className="h-4 w-4" aria-hidden /> Console
        </Link>
        <button
          type="button"
          className={`btn flex-1 ${running ? "" : "btn-primary"}`}
          disabled={power.isPending || busy || !s.node_online || !s.permissions.includes("power")}
          onClick={() => power.mutate()}
        >
          {power.isPending && <Loader2 className="h-4 w-4 animate-spin" aria-hidden />}
          {s.state === "installing" ? "Installing…" : running ? "Stop" : "Start"}
        </button>
      </div>
      {power.error && <ErrorBox error={power.error} />}
    </article>
  );
}

export function ServersPage() {
  const servers = useQuery(serversQuery);
  const me = useQuery(meQuery);
  const [q, setQ] = useState("");
  const list = (servers.data ?? []).filter((s) => s.name.toLowerCase().includes(q.trim().toLowerCase()));
  const counts = (servers.data ?? []).reduce<Record<string, number>>((m, s) => ((m[s.state] = (m[s.state] ?? 0) + 1), m), {});
  const summary = (["running:running", "installing:installing", "offline:stopped"] as const)
    .map((x) => x.split(":") as [string, string])
    .filter(([k]) => counts[k])
    .map(([k, l]) => `${counts[k]} ${l}`)
    .join(", ");

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div>
          <h1 className="h-display text-4xl font-bold leading-none">Servers</h1>
          {summary && <p className="mt-2 text-zinc-400">{summary}</p>}
        </div>
        <div className="flex gap-2">
          <label className="flex min-h-10 items-center gap-2 rounded-lg border border-zinc-700 bg-[#101317] px-3 text-zinc-500">
            <Search className="h-4 w-4" aria-hidden />
            <span className="sr-only">Filter servers</span>
            <input className="w-36 bg-transparent text-sm text-zinc-100 outline-none placeholder:text-zinc-500" placeholder="Filter servers" value={q} onChange={(e) => setQ(e.target.value)} />
          </label>
          {me.data?.is_admin && <Link to="/servers/new" className="btn btn-primary">New server</Link>}
        </div>
      </div>
      {servers.isPending && <Spinner />}
      <ErrorBox error={servers.error} />
      {servers.data && servers.data.length === 0 && (
        <p className="card text-sm text-zinc-400">No servers yet.{me.data?.is_admin ? " Create one to get started." : " Ask an admin to create one for you."}</p>
      )}
      <ul className="grid gap-[18px] [grid-template-columns:repeat(auto-fill,minmax(min(100%,340px),1fr))]">
        {list.map((s) => (
          <li key={s.uuid} className="flex"><div className="flex-1 [&>article]:h-full"><ServerCard s={s} /></div></li>
        ))}
        {me.data?.is_admin && !q && (
          <li>
            <Link to="/servers/new" className="flex min-h-48 h-full flex-col items-center justify-center gap-2.5 rounded-[14px] border border-dashed border-zinc-700 text-zinc-400 hover:bg-[#14181c] focus-visible:outline-2 focus-visible:outline-indigo-500">
              <span className="flex h-10 w-10 items-center justify-center rounded-full border border-zinc-700 text-zinc-200"><Plus className="h-[18px] w-[18px]" aria-hidden /></span>
              New server
            </Link>
          </li>
        )}
      </ul>
    </div>
  );
}
