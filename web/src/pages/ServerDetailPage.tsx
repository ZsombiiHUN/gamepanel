import { lazy, Suspense, useEffect, useRef, useState, type FormEvent } from "react";
import { useParams, useSearch } from "@tanstack/react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Check, Copy, Eraser, Expand, Loader2, MoreVertical, Play, RotateCw, Send, Shrink, Skull, Square } from "lucide-react";
import { api, call } from "../api/client";
import { serverQuery } from "../api/queries";
import { useServerSocket, type LiveStats } from "../api/ws";
import { ConsoleTerminal, type TerminalHandle } from "../components/Terminal";
import { ErrorBox, OnlineDot, ServerIcon, Spinner, StateBadge } from "../components/ui";
import { UsageChart } from "../components/UsageChart";
const FilesTab = lazy(() => import("./FilesTab").then((m) => ({ default: m.FilesTab })));
import { SettingsTab } from "./SettingsTab";
import { BackupsTab } from "./BackupsTab";
import { SchedulesTab } from "./SchedulesTab";
import { UsersTab } from "./UsersTab";

export type Tab = "console" | "files" | "backups" | "schedules" | "users" | "settings";
type PowerAction = "start" | "stop" | "restart" | "kill";

export function ServerDetailPage() {
  const { uuid } = useParams({ from: "/app/servers/$uuid" });
  const { tab } = useSearch({ from: "/app/servers/$uuid" });
  const qc = useQueryClient();
  const server = useQuery(serverQuery(uuid));
  const term = useRef<TerminalHandle>(null);
  const ws = useServerSocket(uuid, {
    onConsole: (l) => term.current?.writeln(l),
    onReset: () => term.current?.clear(),
  });

  const power = useMutation({
    mutationFn: (action: PowerAction) =>
      call(api.POST("/servers/{uuid}/power", { params: { path: { uuid } }, body: { action } })),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["server", uuid] }),
  });

  const reinstall = useMutation({
    mutationFn: () => call(api.POST("/servers/{uuid}/reinstall", { params: { path: { uuid } } })),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["server", uuid] }),
  });

  if (server.isPending) return <Spinner />;
  if (server.error) return <ErrorBox error={server.error} />;
  const s = server.data;
  const state = ws.state ?? s.state;
  const stats = ws.stats ?? s.stats ?? null;

  const can = (need?: string) => !need || (need === "owner" ? s.owner : s.permissions.includes(need));
  const notReady = state === "installing" || state === "install_failed";
  const btn = (action: PowerAction, label: string, icon: React.ReactNode, extra = "") => (
    <button
      type="button"
      className={`btn !min-h-11 !px-5 ${extra}`}
      disabled={power.isPending || !s.node_online || !can("power") || (notReady && (action === "start" || action === "restart"))}
      onClick={() => power.mutate(action)}
    >
      {power.isPending && power.variables === action ? <Loader2 className="h-4 w-4 animate-spin" aria-hidden /> : icon}
      {label}
    </button>
  );

  const running = state === "running" || state === "starting";
  return (
    <div className="space-y-6">
      <header className="flex flex-wrap items-center gap-x-5 gap-y-4">
        <ServerIcon name={s.name} size={76} />
        <div className="min-w-0 flex-1 basis-72">
          <div className="flex flex-wrap items-center gap-3">
            <h1 className="h-display text-[30px] font-bold leading-tight">{s.name}</h1>
            <StateBadge state={state} />
          </div>
          <div className="mt-2 flex flex-wrap items-center gap-x-5 gap-y-1 text-sm text-zinc-400">
            <CopyChip text={String(s.port)} label={`Port ${s.port}`} />
            <OnlineDot online={s.node_online} label="Node" />
            <span aria-live="polite" className="text-xs text-zinc-500">Live: {ws.status === "open" ? "connected" : ws.status}</span>
          </div>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          {!running && btn("start", "Start", <Play className="h-[17px] w-[17px]" aria-hidden />, "!bg-[#4cc27a] !border-[#4cc27a] !text-[#04150b] font-semibold hover:!bg-[#5fd28b]")}
          {running && btn("restart", "Restart", <RotateCw className="h-[17px] w-[17px]" aria-hidden />, "!bg-[#f0a53a] !border-[#f0a53a] !text-[#1a1203] font-semibold hover:!bg-[#f5b455]")}
          {running && btn("stop", "Stop", <Square className="h-[17px] w-[17px]" aria-hidden />, "!bg-[#f0685a] !border-[#f0685a] !text-[#1f0705] font-semibold hover:!bg-[#f58074]")}
          <MoreMenu>
            <button type="button" role="menuitem" className="menu-item" disabled={power.isPending || !s.node_online || !can("power")} onClick={() => power.mutate("kill")}><Skull className="h-4 w-4" aria-hidden /> Kill</button>
            <button
              type="button"
              role="menuitem"
              className="menu-item"
              disabled={reinstall.isPending || !s.node_online || !can("settings.reinstall") || state === "installing" || state === "running" || state === "starting"}
              onClick={() => {
                if (window.confirm("Re-run the install script? Files it writes may overwrite your changes.")) reinstall.mutate();
              }}
            >
              <RotateCw className="h-4 w-4" aria-hidden /> Reinstall
            </button>
          </MoreMenu>
        </div>
      </header>
      <ErrorBox error={power.error ?? reinstall.error} />

      {tab === "console" && (
        <UsageRow stats={state === "running" ? stats : null} diskStats={stats} memoryMb={s.memory_mb} diskMb={s.disk_mb} cpuLimit={s.cpu_pct} />
      )}

      {/* Console stays mounted so the buffer survives tab switches */}
      <div hidden={tab !== "console"}>
        <ConsolePanel uuid={uuid} termRef={term} active={tab === "console"} canSend={can("console.send")} status={ws.status} />
      </div>
      {tab === "files" && (
        <Suspense fallback={<Spinner />}>
          <FilesTab uuid={uuid} />
        </Suspense>
      )}
      {tab === "backups" && <BackupsTab uuid={uuid} />}
      {tab === "schedules" && <SchedulesTab uuid={uuid} />}
      {tab === "users" && s.owner && <UsersTab uuid={uuid} />}
      {tab === "settings" && <SettingsTab server={s} />}
    </div>
  );
}

function CopyChip({ text, label }: { text: string; label: string }) {
  const [done, setDone] = useState(false);
  return (
    <button
      type="button"
      title="Copy"
      className="inline-flex min-h-8 items-center gap-2 rounded-lg border border-zinc-800 bg-[#101317] px-2.5 font-mono text-[13px] text-zinc-200 hover:bg-zinc-800 focus-visible:outline-2 focus-visible:outline-indigo-500"
      onClick={() => {
        void navigator.clipboard?.writeText(text).then(() => { setDone(true); setTimeout(() => setDone(false), 1800); }).catch(() => undefined);
      }}
    >
      {label}
      {done ? <Check className="h-3.5 w-3.5 text-[#4cc27a]" aria-hidden /> : <Copy className="h-3.5 w-3.5 text-zinc-500" aria-hidden />}
      <span className="sr-only" aria-live="polite">{done ? "Copied" : ""}</span>
    </button>
  );
}

function MoreMenu({ children }: { children: React.ReactNode }) {
  return (
    <details className="relative">
      <summary className="btn !min-h-11 !w-11 cursor-pointer list-none !px-0 [&::-webkit-details-marker]:hidden" aria-label="More actions"><MoreVertical className="h-4 w-4" aria-hidden /></summary>
      <div role="menu" className="absolute right-0 z-20 mt-1 flex min-w-40 flex-col rounded-xl border border-zinc-700 bg-zinc-900 p-1 shadow-xl">{children}</div>
    </details>
  );
}

const MAX_POINTS = 30;
/** Readings since this page opened. The panel keeps no history, so neither do we. */
function UsageRow({ stats, diskStats, memoryMb, diskMb, cpuLimit }: { stats: LiveStats | null; diskStats: LiveStats | null; memoryMb: number; diskMb: number; cpuLimit: number }) {
  const [cpu, setCpu] = useState<number[]>([]);
  const [mem, setMem] = useState<number[]>([]);
  const [disk, setDisk] = useState<number[]>([]);
  const push = (set: React.Dispatch<React.SetStateAction<number[]>>, v: number) => set((a) => [...a.slice(-(MAX_POINTS - 1)), v]);
  useEffect(() => { if (stats) { push(setCpu, stats.cpu_pct); push(setMem, stats.mem_bytes / 1024 ** 3); } }, [stats]);
  useEffect(() => { if (diskStats) push(setDisk, diskStats.disk_bytes / 1024 ** 3); }, [diskStats]);

  const gb = (n: number) => (n >= 10 ? n.toFixed(0) : n.toFixed(1));
  const cpuNow = stats?.cpu_pct ?? null;
  const cpuMax = Math.max(20, Math.ceil(Math.max(0, ...cpu) / 20) * 20 + 20);
  const memMax = Math.max(0.5, memoryMb / 1024);
  const memNow = stats ? stats.mem_bytes / 1024 ** 3 : null;
  const diskLimit = diskMb ? diskMb / 1024 : 0;
  const diskNow = diskStats ? diskStats.disk_bytes / 1024 ** 3 : null;
  const diskMax = diskLimit || Math.max(1, Math.ceil((diskNow ?? 0) * 1.25));
  const diskFull = diskLimit > 0 && diskNow !== null && diskNow / diskLimit >= 0.85;
  return (
    <section aria-label="Live usage" className="space-y-2.5">
      <div className="grid gap-4 [grid-template-columns:repeat(auto-fit,minmax(min(100%,270px),1fr))]">
        <UsageChart title="CPU" value={cpuNow === null ? "-" : `${cpuNow.toFixed(0)}%`} unit={cpuLimit ? `of ${cpuLimit}% limit` : "no limit"} note={cpuLimit ? `About ${((cpuNow ?? 0) / 100).toFixed(1)} of ${cpuLimit / 100} cores in use` : "Share of one core"} values={cpu} max={cpuMax} ticks={[{ at: 0, label: "0" }, { at: cpuMax / 2, label: `${cpuMax / 2}%` }, { at: cpuMax, label: `${cpuMax}%` }]} color="#6fcf97" />
        <UsageChart title="Memory" value={memNow === null ? "-" : gb(memNow)} unit={`GB of ${gb(memMax)} GB`} note={memNow === null ? "Not running" : `${Math.round((memNow / memMax) * 100)}% used`} noteWarn={memNow !== null && memNow / memMax >= 0.85} values={mem} max={memMax} limit={memMax} ticks={[{ at: 0, label: "0" }, { at: memMax / 2, label: `${gb(memMax / 2)} GB` }, { at: memMax, label: `${gb(memMax)} GB` }]} color={memNow !== null && memNow / memMax >= 0.85 ? "#e8a33d" : "#8fa6ff"} />
        <UsageChart title="Storage" value={diskNow === null ? "-" : gb(diskNow)} unit={diskLimit ? `GB of ${gb(diskLimit)} GB` : "GB, no limit"} note={diskFull ? "Almost full. The server stops at its limit." : diskLimit ? `${Math.round(((diskNow ?? 0) / diskLimit) * 100)}% used` : "No quota set"} noteWarn={diskFull} values={disk} max={diskMax} limit={diskLimit || undefined} ticks={[{ at: 0, label: "0" }, { at: diskMax / 2, label: `${gb(diskMax / 2)} GB` }, { at: diskMax, label: `${gb(diskMax)} GB` }]} color={diskFull ? "#e8a33d" : "#8fa6ff"} />
      </div>
      <p className="text-[12.5px] text-zinc-500">Charts show readings since this page was opened. The panel does not keep history.</p>
    </section>
  );
}

function ConsolePanel({ uuid, termRef, active, canSend, status }: { uuid: string; termRef: React.RefObject<TerminalHandle | null>; active: boolean; canSend: boolean; status: string }) {
  const [cmd, setCmd] = useState("");
  const [expanded, setExpanded] = useState(false);
  const [copied, setCopied] = useState(false);
  const send = useMutation({
    mutationFn: (command: string) =>
      call(api.POST("/servers/{uuid}/command", { params: { path: { uuid } }, body: { command } })),
    onSuccess: () => setCmd(""),
  });
  // Re-fit when the tab becomes visible or the panel resizes.
  useEffect(() => {
    if (active) termRef.current?.fit();
  }, [active, expanded, termRef]);

  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (cmd.trim()) send.mutate(cmd);
  };
  const tool = "inline-flex h-8 w-8 items-center justify-center rounded-md text-zinc-400 hover:bg-zinc-800 hover:text-zinc-100 focus-visible:outline-2 focus-visible:outline-indigo-500";

  return (
    <section aria-label="Console" className="overflow-hidden rounded-2xl border border-zinc-800 bg-well">
      <div className="flex items-center gap-3 border-b border-[#161a1f] py-1.5 pl-[18px] pr-2.5">
        <h2 className="flex items-center gap-2.5 text-[15px] font-semibold">
          <span className={`h-[7px] w-[7px] rounded-full ${status === "open" ? "bg-[#4cc27a]" : "bg-[#e8a33d]"}`} title={status === "open" ? "Following live output" : "Reconnecting"} aria-hidden />
          Console
        </h2>
        <div className="ml-auto flex items-center gap-0.5">
          <button type="button" className={tool} aria-label={copied ? "Copied" : "Copy log"} title="Copy log" onClick={() => { void navigator.clipboard?.writeText(termRef.current?.text() ?? "").then(() => { setCopied(true); setTimeout(() => setCopied(false), 1800); }).catch(() => undefined); }}>
            {copied ? <Check className="h-4 w-4 text-[#4cc27a]" aria-hidden /> : <Copy className="h-4 w-4" aria-hidden />}
          </button>
          <button type="button" className={tool} aria-label="Clear" title="Clear" onClick={() => termRef.current?.clear()}><Eraser className="h-4 w-4" aria-hidden /></button>
          <button type="button" className={tool} aria-label={expanded ? "Shrink" : "Expand"} aria-pressed={expanded} title={expanded ? "Shrink" : "Expand"} onClick={() => setExpanded((v) => !v)}>
            {expanded ? <Shrink className="h-4 w-4" aria-hidden /> : <Expand className="h-4 w-4" aria-hidden />}
          </button>
        </div>
      </div>
      <ConsoleTerminal ref={termRef} expanded={expanded} />
      {canSend && (
        <form onSubmit={submit} className="flex items-center gap-2.5 border-t border-[#161a1f] bg-[#0c0e11] pl-[18px] pr-2.5">
          <span className="font-mono text-zinc-600" aria-hidden>›</span>
          <label htmlFor="cmd" className="sr-only">Console command</label>
          <input id="cmd" className="min-h-12 flex-1 bg-transparent font-mono text-sm outline-none placeholder:text-zinc-600" placeholder="Send a command" autoComplete="off" maxLength={2048} value={cmd} onChange={(e) => setCmd(e.target.value)} />
          <button type="submit" className="btn btn-ghost !min-h-9" disabled={send.isPending || !cmd.trim()}>
            {send.isPending ? <Loader2 className="h-4 w-4 animate-spin" aria-hidden /> : <Send className="h-4 w-4" aria-hidden />} Send
          </button>
        </form>
      )}
      {send.error ? <div className="p-3"><ErrorBox error={send.error} /></div> : null}
    </section>
  );
}
