import { useEffect, useState, type ReactNode } from "react";
import { useMutation } from "@tanstack/react-query";
import { Check, Copy, Loader2, MoreVertical, Play, RotateCw, Skull, Square } from "lucide-react";
import { api, call, type components } from "../../api/client";
import { invalidates, keys } from "../../api/queries";
import type { WsStatus } from "../../api/ws";
import { ErrorBox, OnlineDot, ServerIcon, StateBadge } from "../../components/ui";
import { shownState } from "../../lib/servers";
import { copyText } from "../../lib/clipboard";
import { useTranslation } from "react-i18next";

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
  const { t } = useTranslation();
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
    const timer = setTimeout(() => setSentIn(null), 15_000);
    return () => clearTimeout(timer);
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
      className={`btn ${extra}`}
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
        <ServerIcon name={s.name} size={48} />
        <div className="min-w-0 flex-1 basis-72">
          <div className="flex flex-wrap items-center gap-2.5">
            <h1 className="h-display text-[22px] leading-tight">{s.name}</h1>
            <StateBadge state={shownState(s, state)} />
          </div>
          {s.description && <p className="mt-0.5 text-[13px] text-zinc-400">{s.description}</p>}
          <div className="mt-1.5 flex flex-wrap items-center gap-x-4 gap-y-1 text-[13px] text-zinc-400">
            <CopyChip text={String(s.port)} label={t("serverHeader.portPort", { port: s.port })} />
            <OnlineDot online={s.node_online} label={t("common.node")} />
            <span aria-live="polite" className="text-xs text-zinc-500">
              {t("serverHeader.live", { status: wsStatus === "open" ? t("serverHeader.connected") : wsStatus })}
            </span>
          </div>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          {!running && btn("start", t("common.start"), <Play className="h-4 w-4" aria-hidden />, "btn-primary")}
          {running && btn("restart", t("common.restart"), <RotateCw className="h-4 w-4 !text-warn" aria-hidden />)}
          {running && btn("stop", t("common.stop"), <Square className="h-4 w-4 !text-bad" aria-hidden />)}
          <MoreMenu>
            <button
              type="button"
              role="menuitem"
              className="menu-item"
              disabled={power.isPending || !killable || !s.node_online || !can("power")}
              onClick={() => send("kill")}
            >
              <Skull className="h-4 w-4" aria-hidden /> {t("common.kill")}
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
                if (window.confirm(t("serverHeader.confirmReinstall"))) reinstall.mutate();
              }}
            >
              <RotateCw className="h-4 w-4" aria-hidden /> {t("serverHeader.reinstall")}
            </button>
          </MoreMenu>
        </div>
      </header>
      {s.suspended && (
        <p role="status" className="mt-4 rounded-xl border border-bad/40 bg-bad-bg px-4 py-3 text-sm text-bad-text">
          {can("owner") && s.permissions.length > 0 ? t("serverHeader.thisServerIsSuspendedIt") : t("serverHeader.thisServerIsSuspendedSo")}
        </p>
      )}
      <ErrorBox error={power.error ?? reinstall.error} />
    </>
  );
}

function CopyChip({ text, label }: { text: string; label: string }) {
  const { t } = useTranslation();
  const [done, setDone] = useState(false);
  return (
    <button
      type="button"
      title={t("common.copy")}
      className="inline-flex min-h-7 items-center gap-2 rounded-md border border-line bg-thead px-2 font-mono text-[12.5px] text-zinc-200 hover:bg-row-hover focus-visible:outline-2 focus-visible:outline-indigo-500"
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
        {done ? t("common.copied") : ""}
      </span>
    </button>
  );
}

function MoreMenu({ children }: { children: React.ReactNode }) {
  const { t } = useTranslation();
  return (
    <details className="relative">
      <summary
        className="btn !w-9 cursor-pointer list-none !px-0 [&::-webkit-details-marker]:hidden"
        aria-label={t("serverHeader.moreActions")}
      >
        <MoreVertical className="h-4 w-4" aria-hidden />
      </summary>
      <div
        role="menu"
        className="absolute right-0 z-30 mt-1 flex min-w-44 flex-col rounded-[10px] border border-line-strong bg-zinc-900 p-1 shadow-xl"
      >
        {children}
      </div>
    </details>
  );
}
