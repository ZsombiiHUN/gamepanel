import { useState, type SubmitEvent } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { Archive, Cloud, Download, Loader2, Lock, LockOpen, Plus, RotateCcw, Trash2 } from "lucide-react";
import { api, call } from "../api/client";
import { backupsQuery, invalidates, keys, serverQuery } from "../api/queries";
import { ErrorBox, Field, Stat, StatRow } from "../components/ui";
import { relativeTime } from "../lib/activity";
import { formatBytes } from "../lib/format";
import { useTranslation } from "react-i18next";

export function BackupsTab({ uuid }: { uuid: string }) {
  const { t } = useTranslation();
  const server = useQuery(serverQuery(uuid));
  const meta = invalidates(keys.backups(uuid));
  const [name, setName] = useState("");
  const [ignore, setIgnore] = useState("");
  const [truncate, setTruncate] = useState(false);
  const list = useQuery(backupsQuery(uuid));
  const canDownload = !!server.data?.permissions.includes("backups.download");

  const create = useMutation({
    mutationFn: () => call(api.POST("/servers/{uuid}/backups", { params: { path: { uuid } }, body: { name, ignore } })),
    meta,
    onSuccess: () => setName(""),
  });
  const remove = useMutation({
    mutationFn: (backup: string) => call(api.DELETE("/servers/{uuid}/backups/{backup}", { params: { path: { uuid, backup } } })),
    meta,
  });
  const lock = useMutation({
    mutationFn: (v: { backup: string; locked: boolean }) =>
      call(api.PUT("/servers/{uuid}/backups/{backup}/lock", { params: { path: { uuid, backup: v.backup } }, body: { locked: v.locked } })),
    meta,
  });
  // Restores run in the background; the list shows progress and the outcome.
  const restore = useMutation({
    mutationFn: (backup: string) =>
      call(api.POST("/servers/{uuid}/backups/{backup}/restore", { params: { path: { uuid, backup } }, body: { truncate } })),
    meta,
  });
  const restoring = list.data?.some((b) => b.restore_status === "running") ?? false;

  const submit = (e: SubmitEvent) => {
    e.preventDefault();
    create.mutate();
  };
  const doRestore = (id: string, n: string) => {
    const warn = truncate ? t("backupsTab.confirmRestoreClean", { name: n }) : t("backupsTab.confirmRestore", { name: n });
    if (window.confirm(warn)) restore.mutate(id);
  };

  const kept = list.data?.length ?? 0;
  const used = list.data?.reduce((n, b) => n + (b.status === "ok" ? b.size : 0), 0) ?? 0;
  const locked = list.data?.filter((b) => b.locked).length ?? 0;
  const latest = list.data?.reduce<(typeof list.data)[number] | undefined>(
    (a, b) => (!a || b.created_at > a.created_at ? b : a),
    undefined,
  );
  const max = server.data?.backup_limit ?? 0;
  const iconBtn =
    "inline-flex h-8 w-8 items-center justify-center rounded-lg text-zinc-400 hover:bg-row-hover hover:text-zinc-100 disabled:cursor-not-allowed disabled:opacity-40 focus-visible:outline-2 focus-visible:outline-indigo-500";
  return (
    <div className="space-y-4">
      <StatRow label={t("backupsTab.backupsKept")}>
        <Stat
          label={t("backupsTab.backupsKept")}
          value={kept}
          unit={max ? `of ${max}` : undefined}
          hint={kept >= max && max > 0 ? t("backupsTab.fullTheOldestUnlockedOne") : undefined}
          tone={kept >= max && max > 0 ? "warn" : undefined}
        >
          {max > 0 && (
            <div className="h-1.5 overflow-hidden rounded-full bg-line" aria-hidden>
              <div
                className={`h-full rounded-full ${kept >= max ? "bg-warn" : "bg-gradient-to-r from-zinc-300 to-zinc-100"}`}
                style={{ width: `${Math.min(100, (kept / max) * 100)}%` }}
              />
            </div>
          )}
        </Stat>
        <Stat
          label={t("backupsTab.storageUsed")}
          value={formatBytes(used).split(" ")[0]}
          unit={formatBytes(used).split(" ")[1]}
          hint={t("backupsTab.countsAgainstTheNodeS")}
        />
        <Stat
          label={t("backupsTab.latest")}
          value={latest ? relativeTime(latest.created_at) : t("backupsTab.noneYet")}
          hint={latest ? latest.name : t("backupsTab.createOneBeforeYouTry")}
        />
        <Stat label={t("backupsTab.locked")} value={locked} hint={t("backupsTab.neverDeletedAutomatically")} />
      </StatRow>

      <div className="flex flex-wrap items-center justify-between gap-3 pt-1">
        <h2 className="text-[15px] font-semibold">{t("backupsTab.history")}</h2>
        <details className="relative">
          <summary className="btn btn-primary cursor-pointer list-none [&::-webkit-details-marker]:hidden">
            <Plus className="h-4 w-4" aria-hidden /> {t("backupsTab.createBackup")}
          </summary>
          <form
            onSubmit={submit}
            className="absolute right-0 z-20 mt-2 w-[min(90vw,380px)] space-y-3 rounded-xl border border-line-strong bg-zinc-900 p-4 shadow-xl"
            aria-label={t("backupsTab.newBackup")}
          >
            <Field label={t("backupsTab.nameOptional")}>
              {(id) => <input id={id} className="input" maxLength={64} value={name} onChange={(e) => setName(e.target.value)} />}
            </Field>
            <Field label={t("backupsTab.leaveOut")} hint={t("backupsTab.onePatternPerLineE")}>
              {(id) => (
                <textarea
                  id={id}
                  className="input h-20 font-mono"
                  maxLength={2000}
                  value={ignore}
                  onChange={(e) => setIgnore(e.target.value)}
                />
              )}
            </Field>
            <ErrorBox error={create.error} />
            <button type="submit" className="btn btn-primary w-full" disabled={create.isPending}>
              {create.isPending && <Loader2 className="h-4 w-4 animate-spin" aria-hidden />} {t("backupsTab.createBackup")}
            </button>
          </form>
        </details>
      </div>

      <ErrorBox error={list.error ?? remove.error ?? lock.error ?? restore.error} />

      <section className="overflow-hidden rounded-xl border border-line bg-zinc-900" aria-label={t("backupsTab.yourBackups")}>
        {list.data?.length === 0 && <p className="px-5 py-6 text-sm text-zinc-400">{t("backupsTab.noBackupsYetCreateOne")}</p>}
        <ul>
          {list.data?.map((b) => (
            <li key={b.uuid} className="flex flex-wrap items-center gap-x-4 gap-y-2 border-b border-line px-4 py-3 hover:bg-row-hover">
              <span
                className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg border border-line bg-thead text-zinc-400"
                aria-hidden
              >
                <Archive className="h-4 w-4" />
              </span>
              <div className="min-w-0 flex-1 basis-56">
                <div className="flex flex-wrap items-center gap-2.5">
                  <span className="text-[14px] font-medium">{b.name}</span>
                  {b.disk === "s3" && (
                    <span
                      className="inline-flex min-h-6 items-center gap-1.5 rounded-full bg-chip px-2.5 text-xs text-zinc-300"
                      title={t("backupsTab.storedInS3OffThe")}
                    >
                      <Cloud className="h-3 w-3" aria-hidden />
                      S3
                    </span>
                  )}
                  {b.locked && (
                    <span className="inline-flex h-[22px] items-center gap-1.5 rounded-full border border-line-strong px-2 text-xs text-zinc-300">
                      <Lock className="h-3 w-3" aria-hidden />
                      {t("backupsTab.locked")}
                    </span>
                  )}
                  {b.status === "running" && (
                    <span className="inline-flex items-center gap-1.5 text-xs text-warn-text">
                      <Loader2 className="h-3 w-3 animate-spin" aria-hidden />
                      {t("backupsTab.running")}
                    </span>
                  )}
                  {b.status !== "ok" && b.status !== "running" && <span className="text-xs text-bad-text">{b.status}</span>}
                  {b.restore_status === "running" && (
                    <span role="status" className="inline-flex items-center gap-1.5 text-xs text-warn-text">
                      <Loader2 className="h-3 w-3 animate-spin" aria-hidden />
                      {t("backupsTab.restoring")}
                    </span>
                  )}
                  {b.restore_status === "ok" && (
                    <span role="status" className="text-xs text-good-text">
                      {t("backupsTab.restored", { when: new Date(b.restored_at * 1000).toLocaleString() })}
                    </span>
                  )}
                </div>
                <div className="mt-0.5 text-[12.5px] text-zinc-500">
                  {new Date(b.created_at * 1000).toLocaleString()}
                  {b.status === "ok" && (
                    <span className="ml-2 font-mono text-xs text-zinc-500" title={t("backupsTab.sha256")}>
                      {b.sha256.slice(0, 12)}
                    </span>
                  )}
                </div>
                {b.error && <div className="mt-0.5 text-xs text-bad-text">{b.error}</div>}
                {b.restore_status === "failed" && (
                  <div className="mt-0.5 text-xs text-bad-text">{t("backupsTab.restoreFailed", { error: b.restore_error })}</div>
                )}
              </div>
              <div className="w-20 text-right font-mono text-[13px] text-zinc-300">{b.status === "ok" ? formatBytes(b.size) : "-"}</div>
              <div className="flex items-center gap-1">
                <button
                  type="button"
                  className="btn !min-h-8"
                  aria-label={t("backupsTab.restoreName", { name: b.name })}
                  title={t("backupsTab.restoreStopTheServerFirst")}
                  disabled={b.status !== "ok" || restore.isPending || restoring}
                  onClick={() => doRestore(b.uuid, b.name)}
                >
                  <RotateCcw className="h-4 w-4" aria-hidden /> {t("backupsTab.restore")}
                </button>
                {canDownload && b.status === "ok" && (
                  <a
                    className={iconBtn}
                    href={`/api/servers/${uuid}/backups/${b.uuid}/download`}
                    download
                    aria-label={t("backupsTab.downloadName", { name: b.name })}
                    title={t("common.download")}
                  >
                    <Download className="h-4 w-4" aria-hidden />
                  </a>
                )}
                <button
                  type="button"
                  className={iconBtn}
                  aria-label={b.locked ? t("backupsTab.unlockName", { name: b.name }) : t("backupsTab.lockName", { name: b.name })}
                  title={b.locked ? t("backupsTab.lockedCannotBeDeleted") : t("backupsTab.lock")}
                  onClick={() => lock.mutate({ backup: b.uuid, locked: !b.locked })}
                >
                  {b.locked ? <Lock className="h-4 w-4" aria-hidden /> : <LockOpen className="h-4 w-4" aria-hidden />}
                </button>
                <button
                  type="button"
                  className={`${iconBtn} hover:!text-bad-text`}
                  aria-label={t("backupsTab.deleteName", { name: b.name })}
                  title={t("common.delete")}
                  disabled={b.locked || b.status === "running" || remove.isPending}
                  onClick={() => window.confirm(t("backupsTab.confirmDelete", { name: b.name })) && remove.mutate(b.uuid)}
                >
                  <Trash2 className="h-4 w-4" aria-hidden />
                </button>
              </div>
            </li>
          ))}
        </ul>
        <label className="flex items-center gap-2 bg-thead px-4 py-2.5 text-[13px] text-zinc-400">
          <input type="checkbox" checked={truncate} onChange={(e) => setTruncate(e.target.checked)} />
          {t("backupsTab.whenRestoringDeleteAllCurrent")}
        </label>
      </section>
      <p className="text-[12.5px] text-zinc-500">{t("backupsTab.restoringReplacesTheServerS")}</p>
    </div>
  );
}
