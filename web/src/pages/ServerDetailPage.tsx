import { lazy, Suspense, useEffect, useRef, useState, type FormEvent } from "react";
import { Link, useParams, useSearch } from "@tanstack/react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Loader2, Play, RotateCw, Send, Skull, Square } from "lucide-react";
import { api, call } from "../api/client";
import { serverQuery } from "../api/queries";
import { useServerSocket } from "../api/ws";
import { ConsoleTerminal, type TerminalHandle } from "../components/Terminal";
import { ErrorBox, OnlineDot, Spinner, StateBadge } from "../components/ui";
const FilesTab = lazy(() => import("./FilesTab").then((m) => ({ default: m.FilesTab })));
import { SettingsTab } from "./SettingsTab";
import { BackupsTab } from "./BackupsTab";
import { SchedulesTab } from "./SchedulesTab";
import { formatBytes } from "../lib/format";

export type Tab = "console" | "files" | "backups" | "schedules" | "settings";
type PowerAction = "start" | "stop" | "restart" | "kill";

const tabs: { id: Tab; label: string }[] = [
  { id: "console", label: "Console" },
  { id: "files", label: "Files" },
  { id: "backups", label: "Backups" },
  { id: "schedules", label: "Schedules" },
  { id: "settings", label: "Settings" },
];

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

  const notReady = state === "installing" || state === "install_failed";
  const btn = (action: PowerAction, label: string, icon: React.ReactNode, extra = "") => (
    <button
      type="button"
      className={`btn ${extra}`}
      disabled={power.isPending || !s.node_online || (notReady && (action === "start" || action === "restart"))}
      onClick={() => power.mutate(action)}
    >
      {power.isPending && power.variables === action ? <Loader2 className="h-4 w-4 animate-spin" aria-hidden /> : icon}
      {label}
    </button>
  );

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-3">
        <Link to="/" className="link text-sm">&larr; Servers</Link>
        <h1 className="text-2xl font-semibold">{s.name}</h1>
        <StateBadge state={state} />
        <OnlineDot online={s.node_online} label="Node" />
        <span className="text-xs text-zinc-500" aria-live="polite">
          Live: {ws.status === "open" ? "connected" : ws.status}
        </span>
      </div>

      <div className="flex flex-wrap items-center gap-2">
        {btn("start", "Start", <Play className="h-4 w-4" aria-hidden />, "btn-primary")}
        {btn("restart", "Restart", <RotateCw className="h-4 w-4" aria-hidden />)}
        {btn("stop", "Stop", <Square className="h-4 w-4" aria-hidden />)}
        {btn("kill", "Kill", <Skull className="h-4 w-4" aria-hidden />, "btn-danger")}
        <button
          type="button"
          className="btn"
          disabled={reinstall.isPending || !s.node_online || state === "installing" || state === "running" || state === "starting"}
          onClick={() => {
            if (window.confirm("Re-run the install script? Files it writes may overwrite your changes.")) reinstall.mutate();
          }}
        >
          {reinstall.isPending ? <Loader2 className="h-4 w-4 animate-spin" aria-hidden /> : <RotateCw className="h-4 w-4" aria-hidden />}
          Reinstall
        </button>
        <div className="ml-auto flex gap-4 text-sm text-zinc-300" aria-label="Live resource usage">
          <span>CPU: {stats ? `${stats.cpu_pct.toFixed(1)}%` : "-"}</span>
          <span>Memory: {stats ? formatBytes(stats.mem_bytes) : "-"} / {s.memory_mb} MB</span>
        </div>
      </div>
      <ErrorBox error={power.error ?? reinstall.error} />

      <nav aria-label="Server sections" className="flex gap-1 border-b border-zinc-800">
        {tabs.map((t) => (
          <Link
            key={t.id}
            to="/servers/$uuid"
            params={{ uuid }}
            search={{ tab: t.id }}
            aria-current={tab === t.id ? "page" : undefined}
            className={`-mb-px border-b-2 px-4 py-2 text-sm focus-visible:outline-2 focus-visible:outline-indigo-400 ${
              tab === t.id ? "border-indigo-500 text-white" : "border-transparent text-zinc-400 hover:text-zinc-200"
            }`}
          >
            {t.label}
          </Link>
        ))}
      </nav>

      {/* Console stays mounted so the buffer survives tab switches */}
      <div hidden={tab !== "console"}>
        <ConsolePanel uuid={uuid} termRef={term} active={tab === "console"} />
      </div>
      {tab === "files" && (
        <Suspense fallback={<Spinner />}>
          <FilesTab uuid={uuid} />
        </Suspense>
      )}
      {tab === "backups" && <BackupsTab uuid={uuid} />}
      {tab === "schedules" && <SchedulesTab uuid={uuid} />}
      {tab === "settings" && <SettingsTab server={s} />}
    </div>
  );
}

function ConsolePanel({ uuid, termRef, active }: { uuid: string; termRef: React.RefObject<TerminalHandle | null>; active: boolean }) {
  const [cmd, setCmd] = useState("");
  const send = useMutation({
    mutationFn: (command: string) =>
      call(api.POST("/servers/{uuid}/command", { params: { path: { uuid } }, body: { command } })),
    onSuccess: () => setCmd(""),
  });
  // Re-fit when the tab becomes visible.
  useEffect(() => {
    if (active) termRef.current?.fit();
  }, [active, termRef]);

  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (cmd.trim()) send.mutate(cmd);
  };

  return (
    <div className="space-y-3">
      <ConsoleTerminal ref={termRef} />
      <form onSubmit={submit} className="flex gap-2">
        <label htmlFor="cmd" className="sr-only">Console command</label>
        <input
          id="cmd"
          className="input font-mono"
          placeholder="Type a command and press Enter"
          autoComplete="off"
          maxLength={2048}
          value={cmd}
          onChange={(e) => setCmd(e.target.value)}
        />
        <button type="submit" className="btn btn-primary" disabled={send.isPending || !cmd.trim()}>
          {send.isPending ? <Loader2 className="h-4 w-4 animate-spin" aria-hidden /> : <Send className="h-4 w-4" aria-hidden />} Send
        </button>
      </form>
      <ErrorBox error={send.error} />
    </div>
  );
}
