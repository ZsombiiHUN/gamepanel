import { useEffect, useState, type ReactNode } from "react";
import { useMutation } from "@tanstack/react-query";
import { Check, Copy, Loader2, MoreVertical, Play, RotateCw, Skull, Square } from "lucide-react";
import { api, call, type components } from "../../api/client";
import { invalidates, keys } from "../../api/queries";
import type { WsStatus } from "../../api/ws";
import { ErrorBox, OnlineDot, ServerIcon, StateBadge } from "../../components/ui";
import { shownState } from "../../lib/servers";
import { copyText } from "../../lib/clipboard";

type ServerView = components["schemas"]["ServerView"];
type PowerAction = "start" | "stop" | "restart" | "kill";

/** Name, status, port and the power controls. Owns the power and reinstall calls. */
export function ServerHeader({
  server: s,
  state,
  wsStatus,
  can,
}: {
  server: ServerView;
  state: string;
  wsStatus: WsStatus;
  can: (need?: string) => boolean;
}) {
  const uuid = s.uuid;
  const power = useMutation({
    mutationFn: (action: PowerAction) => call(api.POST("/servers/{uuid}/power", { params: { path: { uuid } }, body: { action } })),
    meta: invalidates(keys.server(uuid)),
  });
  const reinstall = useMutation({
    mutationFn: () => call(api.POST("/servers/{uuid}/reinstall", { params: { path: { uuid } } })),
    meta: invalidates(keys.server(uuid)),
  });
  // The panel answers before a slow action (a graceful stop) is done, so the
  // buttons stay held from an accepted action until the state moves on, or
  // for a while at most. Kill stays open: it is how a hung stop is cut short.
  const [sentIn, setSentIn] = useState<string | null>(null);
  if (sentIn !== null && state !== sentIn) setSentIn(null);
  useEffect(() => {
    if (sentIn === null) return;
    const t = setTimeout(() => setSentIn(null), 15_000);
    return () => clearTimeout(t);
  }, [sentIn]);
  const send = (action: PowerAction) => {
    const from = state;
    power.mutate(action, { onSuccess: () => action !== "kill" && setSentIn(from) });
  };
  const notReady = state === "installing" || state === "install_failed" || s.suspended;
  const killable = state === "running" || state === "starting" || state === "stopping";
  const btn = (action: PowerAction, label: string, icon: ReactNode, extra = "") => (
    <button
      type="button"
      className={`btn !min-h-11 !px-5 ${extra}`}
      disabled={
        power.isPending || sentIn !== null || !s.node_online || !can("power") || (notReady && (action === "start" || action === "restart"))
      }
      onClick={() => send(action)}
    >
      {power.isPending && power.variables === action ? <Loader2 className="h-4 w-4 animate-spin" aria-hidden /> : icon}
      {label}
    </button>
  );

  const running = state === "running" || state === "starting";
  return (
    <>
      <header className="flex flex-wrap items-center gap-x-5 gap-y-4">
        <ServerIcon name={s.name} size={76} />
        <div className="min-w-0 flex-1 basis-72">
          <div className="flex flex-wrap items-center gap-3">
            <h1 className="h-display text-[30px] font-bold leading-tight">{s.name}</h1>
            <StateBadge state={shownState(s, state)} />
          </div>
          {s.description && <p className="mt-1 text-sm text-zinc-400">{s.description}</p>}
          <div className="mt-2 flex flex-wrap items-center gap-x-5 gap-y-1 text-sm text-zinc-400">
            <CopyChip text={String(s.port)} label={`Port ${s.port}`} />
            <OnlineDot online={s.node_online} label="Node" />
            <span aria-live="polite" className="text-xs text-zinc-500">
              Live: {wsStatus === "open" ? "connected" : wsStatus}
            </span>
          </div>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          {!running &&
            btn(
              "start",
              "Start",
              <Play className="h-[17px] w-[17px]" aria-hidden />,
              "!bg-good !border-good !text-good-ink font-semibold hover:!bg-good-hover",
            )}
          {running &&
            btn(
              "restart",
              "Restart",
              <RotateCw className="h-[17px] w-[17px]" aria-hidden />,
              "!bg-warn !border-warn !text-warn-ink font-semibold hover:!bg-warn-hover",
            )}
          {running &&
            btn(
              "stop",
              "Stop",
              <Square className="h-[17px] w-[17px]" aria-hidden />,
              "!bg-bad !border-bad !text-bad-ink font-semibold hover:!bg-bad-hover",
            )}
          <MoreMenu>
            <button
              type="button"
              role="menuitem"
              className="menu-item"
              disabled={power.isPending || !killable || !s.node_online || !can("power")}
              onClick={() => send("kill")}
            >
              <Skull className="h-4 w-4" aria-hidden /> Kill
            </button>
            <button
              type="button"
              role="menuitem"
              className="menu-item"
              disabled={
                reinstall.isPending ||
                !s.node_online ||
                !can("settings.reinstall") ||
                state === "installing" ||
                state === "running" ||
                state === "starting"
              }
              onClick={() => {
                if (window.confirm("Re-run the install script? Files it writes may overwrite your changes.")) reinstall.mutate();
              }}
            >
              <RotateCw className="h-4 w-4" aria-hidden /> Reinstall
            </button>
          </MoreMenu>
        </div>
      </header>
      {s.suspended && (
        <p role="status" className="rounded-xl border border-bad/40 bg-bad-bg px-4 py-3 text-sm text-bad-text">
          {can("owner") && s.permissions.length > 0
            ? "This server is suspended: it is stopped, cannot be started, and its owner and subusers cannot use it. Lift the suspension in Settings."
            : "This server is suspended, so it is stopped and its console, files and settings are closed. Contact your host to have it restored."}
        </p>
      )}
      <ErrorBox error={power.error ?? reinstall.error} />
    </>
  );
}

function CopyChip({ text, label }: { text: string; label: string }) {
  const [done, setDone] = useState(false);
  return (
    <button
      type="button"
      title="Copy"
      className="inline-flex min-h-8 items-center gap-2 rounded-lg border border-zinc-800 bg-thead px-2.5 font-mono text-[13px] text-zinc-200 hover:bg-zinc-800 focus-visible:outline-2 focus-visible:outline-indigo-500"
      onClick={() => {
        void copyText(text)
          .then(() => {
            setDone(true);
            setTimeout(() => setDone(false), 1800);
          })
          .catch(() => undefined);
      }}
    >
      {label}
      {done ? <Check className="h-3.5 w-3.5 text-good" aria-hidden /> : <Copy className="h-3.5 w-3.5 text-zinc-500" aria-hidden />}
      <span className="sr-only" aria-live="polite">
        {done ? "Copied" : ""}
      </span>
    </button>
  );
}

function MoreMenu({ children }: { children: React.ReactNode }) {
  return (
    <details className="relative">
      <summary className="btn !min-h-11 !w-11 cursor-pointer list-none !px-0 [&::-webkit-details-marker]:hidden" aria-label="More actions">
        <MoreVertical className="h-4 w-4" aria-hidden />
      </summary>
      <div
        role="menu"
        className="absolute right-0 z-20 mt-1 flex min-w-40 flex-col rounded-xl border border-zinc-700 bg-zinc-900 p-1 shadow-xl"
      >
        {children}
      </div>
    </details>
  );
}
