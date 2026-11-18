import { Link, useNavigate, useSearch } from "@tanstack/react-router";
import { useMutation, useQuery } from "@tanstack/react-query";
import { Loader2, Plus, Search, Server, SquareTerminal, X } from "lucide-react";
import { api, call, type components } from "../api/client";
import { invalidates, keys, meQuery, serversQuery } from "../api/queries";
import { SearchInput } from "../components/SearchInput";
import { useSearchText } from "../hooks/useSearchText";
import { Empty, ErrorBox, Meter, ServerIcon, Spinner, StateBadge } from "../components/ui";
import { formatBytes } from "../lib/format";
import { countGroups, GROUPS, groupLabel, groupOf, matchesServer, shownState, sortServers, summarize, type Group } from "../lib/servers";

type ServerView = components["schemas"]["ServerView"];

function ServerCard({ s }: { s: ServerView }) {
  const running = s.state === "running" || s.state === "starting";
  const busy = s.state === "installing" || s.state === "stopping";
  const power = useMutation({
    mutationFn: () => call(api.POST("/servers/{uuid}/power", { params: { path: { uuid: s.uuid } }, body: { action: running ? "stop" : "start" } })),
    meta: invalidates(keys.servers),
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
          <div className="mt-0.5 truncate text-[13px] text-zinc-500">{s.egg_name ? `${s.egg_name} · ` : ""}Port {s.port}{s.node_online ? "" : " · node offline"}</div>
        </div>
        <StateBadge state={s.state} />
      </div>
      <div className="flex flex-col gap-3.5">
        {live && <Meter label="CPU" text={`${live.cpu_pct.toFixed(0)}%`} pct={cpuPct} />}
        {live && <Meter label="Memory" text={`${formatBytes(live.mem_bytes)} / ${formatBytes(s.memory_mb * 1024 * 1024)}`} pct={memPct} />}
        {s.stats && <Meter label="Disk" text={`${formatBytes(s.stats.disk_bytes)} / ${s.disk_mb ? formatBytes(s.disk_mb * 1024 * 1024) : "no limit"}`} pct={diskPct} />}
        {!s.stats && <p className="py-1 text-[13px] text-zinc-500">{s.state === "installing" ? "Setting up files. This takes a few minutes the first time." : s.state === "suspended" ? "Suspended. Contact your host to have it restored." : "Not running. Start it to see live usage."}</p>}
      </div>
      <div className="mt-auto flex gap-2 border-t border-line pt-4">
        <Link to="/servers/$uuid" params={{ uuid: s.uuid }} search={{ tab: "console" }} className="btn flex-1">
          <SquareTerminal className="h-4 w-4" aria-hidden /> Console
        </Link>
        <button
          type="button"
          className={`btn flex-1 ${running ? "" : "btn-primary"}`}
          disabled={power.isPending || busy || s.state === "suspended" || !s.node_online || !s.permissions.includes("power")}
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
  // The filter lives in the address: a link to "only the stopped servers" works, and Back undoes a filter.
  const { q: urlQuery, status } = useSearch({ from: "/app/" });
  const group = status ?? "all";
  const navigate = useNavigate({ from: "/" });
  const setGroup = (g: Group | "all") => void navigate({ search: (prev) => ({ ...prev, status: g === "all" ? undefined : g }), replace: true });
  const [q, setQ] = useSearchText(urlQuery, (text) => void navigate({ search: (prev) => ({ ...prev, q: text || undefined }), replace: true }), 400);
  // A suspended server shows (and filters and counts) as suspended, whatever its node says.
  const all = (servers.data ?? []).map((s) => ({ ...s, state: shownState(s) }));
  const groups = countGroups(all);
  const filtering = q.trim() !== "" || group !== "all";
  const list = sortServers(all).filter((s) => (group === "all" || groupOf(s.state) === group) && matchesServer(s, q));
  const summary = summarize(all);
  const clear = () => { setQ(""); void navigate({ search: {}, replace: true }); };
  const chips: (Group | "all")[] = ["all", ...GROUPS.filter((g) => groups[g] > 0)];

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div>
          <h1 className="h-display text-4xl font-bold leading-none">Servers</h1>
          {summary && <p className="mt-2 text-zinc-400">{summary}</p>}
        </div>
        <div className="flex gap-2">
          <SearchInput
            label="Filter servers"
            placeholder="Filter by name, game, port…"
            value={q}
            onChange={setQ}
            hint={<kbd className="hidden rounded border border-zinc-700 px-1.5 font-mono text-[11px] text-zinc-500 sm:inline" aria-hidden>/</kbd>}
          />
          {me.data?.is_admin && <Link to="/servers/new" className="btn btn-primary">New server</Link>}
        </div>
      </div>
      {chips.length > 2 && (
        <div role="group" aria-label="Filter by status" className="flex flex-wrap gap-2">
          {chips.map((g) => (
            <button
              key={g}
              type="button"
              aria-pressed={group === g}
              onClick={() => setGroup(g)}
              className={`inline-flex min-h-8 items-center gap-2 rounded-full border px-3 text-[13px] focus-visible:outline-2 focus-visible:outline-indigo-500 ${group === g ? "border-indigo-400 bg-indigo-500/15 text-zinc-100" : "border-zinc-700 text-zinc-400 hover:text-zinc-100"}`}
            >
              {g === "all" ? "All" : groupLabel[g]}
              <span className="font-mono text-xs text-zinc-500">{g === "all" ? all.length : groups[g]}</span>
            </button>
          ))}
        </div>
      )}
      {servers.isPending && <Spinner />}
      <ErrorBox error={servers.error} />
      {all.length === 0 && servers.data && (
        <Empty icon={<Server className="h-5 w-5" aria-hidden />} title="No servers yet">{me.data?.is_admin ? "Create one to get started." : "Ask an admin to create one for you."}</Empty>
      )}
      {all.length > 0 && list.length === 0 && (
        <Empty icon={<Search className="h-5 w-5" aria-hidden />} title="No servers match">
          Nothing matches {q.trim() ? <>“{q.trim()}”</> : "that status"}. You can search by name, game, node, port or status.{" "}
          <button type="button" className="link inline-flex items-center gap-1" onClick={clear}><X className="h-3.5 w-3.5" aria-hidden /> Clear filter</button>
        </Empty>
      )}
      <ul className="grid gap-[18px] [grid-template-columns:repeat(auto-fill,minmax(min(100%,340px),1fr))]">
        {list.map((s) => (
          <li key={s.uuid} className="flex min-w-0"><div className="min-w-0 flex-1 [&>article]:h-full"><ServerCard s={s} /></div></li>
        ))}
        {me.data?.is_admin && !filtering && all.length > 0 && (
          <li>
            <Link to="/servers/new" className="flex min-h-48 h-full flex-col items-center justify-center gap-2.5 rounded-[14px] border border-dashed border-zinc-700 text-zinc-400 hover:bg-row-hover focus-visible:outline-2 focus-visible:outline-indigo-500">
              <span className="flex h-10 w-10 items-center justify-center rounded-full border border-zinc-700 text-zinc-200"><Plus className="h-[18px] w-[18px]" aria-hidden /></span>
              New server
            </Link>
          </li>
        )}
      </ul>
    </div>
  );
}
