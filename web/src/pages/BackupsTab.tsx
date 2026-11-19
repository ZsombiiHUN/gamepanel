import { useState, type FormEvent } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { Cloud, Database, Download, Loader2, Lock, LockOpen, Plus, RotateCcw, Trash2 } from "lucide-react";
import { api, call } from "../api/client";
import { backupsQuery, invalidates, keys, serverQuery } from "../api/queries";
import { ErrorBox, Field } from "../components/ui";
import { formatBytes } from "../lib/format";

export function BackupsTab({ uuid }: { uuid: string }) {
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

  const submit = (e: FormEvent) => {
    e.preventDefault();
    create.mutate();
  };
  const doRestore = (id: string, n: string) => {
    const warn = truncate
      ? `Restore "${n}" and DELETE all current files first? This cannot be undone.`
      : `Restore "${n}" over the current files? Files with the same name are overwritten.`;
    if (window.confirm(warn)) restore.mutate(id);
  };

  const kept = list.data?.length ?? 0;
  const max = server.data?.backup_limit ?? 0;
  const iconBtn =
    "inline-flex h-9 w-9 items-center justify-center rounded-lg text-zinc-400 hover:bg-zinc-800 hover:text-zinc-100 disabled:cursor-not-allowed disabled:opacity-40 focus-visible:outline-2 focus-visible:outline-indigo-500";
  return (
    <div className="space-y-4">
      <section className="card flex flex-wrap items-center gap-x-10 gap-y-4 !rounded-[14px]" aria-label="Backups kept">
        <div className="min-w-64 flex-1">
          <div className="flex justify-between text-sm text-zinc-400">
            <span>Backups kept</span>
            <span className="font-mono text-zinc-100">
              {kept}
              {max ? ` of ${max}` : ""}
            </span>
          </div>
          {max > 0 && max <= 40 && (
            <div className="mt-3 flex gap-[3px]" aria-hidden>
              {Array.from({ length: max }, (_, i) => (
                <span
                  key={i}
                  className={`h-2 flex-1 rounded-[2px] ${i < kept ? (kept >= max ? "bg-warn" : "bg-indigo-400") : "bg-line-strong"}`}
                />
              ))}
            </div>
          )}
        </div>
        <details className="relative">
          <summary className="btn btn-primary cursor-pointer list-none [&::-webkit-details-marker]:hidden">
            <Plus className="h-4 w-4" aria-hidden /> Create backup
          </summary>
          <form
            onSubmit={submit}
            className="absolute right-0 z-20 mt-2 w-[min(90vw,380px)] space-y-3 rounded-2xl border border-zinc-700 bg-zinc-900 p-4 shadow-xl"
            aria-label="New backup"
          >
            <Field label="Name (optional)">
              {(id) => <input id={id} className="input" maxLength={64} value={name} onChange={(e) => setName(e.target.value)} />}
            </Field>
            <Field label="Leave out" hint="One pattern per line, e.g. logs, *.tmp, cache/">
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
              {create.isPending && <Loader2 className="h-4 w-4 animate-spin" aria-hidden />} Create backup
            </button>
          </form>
        </details>
      </section>

      <ErrorBox error={list.error ?? remove.error ?? lock.error ?? restore.error} />

      <section className="overflow-hidden rounded-2xl border border-zinc-800 bg-zinc-900" aria-label="Your backups">
        <h2 className="px-5 py-4 text-[15px] font-semibold">Your backups</h2>
        {list.data?.length === 0 && (
          <p className="border-t border-line px-5 py-5 text-sm text-zinc-400">No backups yet. Create one before you try something risky.</p>
        )}
        <ul>
          {list.data?.map((b) => (
            <li key={b.uuid} className="flex flex-wrap items-center gap-x-4 gap-y-2 border-t border-line px-5 py-3.5 hover:bg-row-hover">
              <span
                className="flex h-[38px] w-[38px] shrink-0 items-center justify-center rounded-[10px] bg-chip text-indigo-400"
                aria-hidden
              >
                <Database className="h-[17px] w-[17px]" />
              </span>
              <div className="min-w-0 flex-1 basis-56">
                <div className="flex flex-wrap items-center gap-2.5">
                  <span className="text-[15px] font-medium">{b.name}</span>
                  {b.disk === "s3" && (
                    <span
                      className="inline-flex min-h-6 items-center gap-1.5 rounded-full bg-chip px-2.5 text-xs text-zinc-300"
                      title="Stored in S3, off the node"
                    >
                      <Cloud className="h-3 w-3" aria-hidden />
                      S3
                    </span>
                  )}
                  {b.locked && (
                    <span className="inline-flex min-h-6 items-center gap-1.5 rounded-full bg-chip px-2.5 text-xs text-zinc-300">
                      <Lock className="h-3 w-3" aria-hidden />
                      Locked
                    </span>
                  )}
                  {b.status === "running" && (
                    <span className="inline-flex items-center gap-1.5 text-xs text-warn-text">
                      <Loader2 className="h-3 w-3 animate-spin" aria-hidden />
                      Running
                    </span>
                  )}
                  {b.status !== "ok" && b.status !== "running" && <span className="text-xs text-bad-text">{b.status}</span>}
                  {b.restore_status === "running" && (
                    <span role="status" className="inline-flex items-center gap-1.5 text-xs text-warn-text">
                      <Loader2 className="h-3 w-3 animate-spin" aria-hidden />
                      Restoring
                    </span>
                  )}
                  {b.restore_status === "ok" && (
                    <span role="status" className="text-xs text-good-text">
                      Restored {new Date(b.restored_at * 1000).toLocaleString()}
                    </span>
                  )}
                </div>
                <div className="mt-0.5 text-[13px] text-zinc-400">
                  {new Date(b.created_at * 1000).toLocaleString()}
                  {b.status === "ok" && (
                    <span className="ml-2 font-mono text-xs text-zinc-500" title="SHA-256">
                      {b.sha256.slice(0, 12)}
                    </span>
                  )}
                </div>
                {b.error && <div className="mt-0.5 text-xs text-bad-text">{b.error}</div>}
                {b.restore_status === "failed" && <div className="mt-0.5 text-xs text-bad-text">Restore failed: {b.restore_error}</div>}
              </div>
              <div className="w-20 text-right font-mono text-[13px] text-zinc-300">{b.status === "ok" ? formatBytes(b.size) : "-"}</div>
              <div className="flex items-center gap-1">
                <button
                  type="button"
                  className="btn !min-h-9"
                  aria-label={`Restore ${b.name}`}
                  title="Restore (stop the server first)"
                  disabled={b.status !== "ok" || restore.isPending || restoring}
                  onClick={() => doRestore(b.uuid, b.name)}
                >
                  <RotateCcw className="h-4 w-4" aria-hidden /> Restore
                </button>
                {canDownload && b.status === "ok" && (
                  <a
                    className={iconBtn}
                    href={`/api/servers/${uuid}/backups/${b.uuid}/download`}
                    download
                    aria-label={`Download ${b.name}`}
                    title="Download"
                  >
                    <Download className="h-4 w-4" aria-hidden />
                  </a>
                )}
                <button
                  type="button"
                  className={iconBtn}
                  aria-label={b.locked ? `Unlock ${b.name}` : `Lock ${b.name}`}
                  title={b.locked ? "Locked: cannot be deleted" : "Lock"}
                  onClick={() => lock.mutate({ backup: b.uuid, locked: !b.locked })}
                >
                  {b.locked ? <Lock className="h-4 w-4" aria-hidden /> : <LockOpen className="h-4 w-4" aria-hidden />}
                </button>
                <button
                  type="button"
                  className={`${iconBtn} hover:!text-bad-text`}
                  aria-label={`Delete ${b.name}`}
                  title="Delete"
                  disabled={b.locked || b.status === "running" || remove.isPending}
                  onClick={() => window.confirm(`Delete backup "${b.name}"?`) && remove.mutate(b.uuid)}
                >
                  <Trash2 className="h-4 w-4" aria-hidden />
                </button>
              </div>
            </li>
          ))}
        </ul>
        <label className="flex items-center gap-2 border-t border-line px-5 py-3 text-[13px] text-zinc-400">
          <input type="checkbox" checked={truncate} onChange={(e) => setTruncate(e.target.checked)} />
          When restoring, delete all current files first
        </label>
      </section>
      <p className="text-[12.5px] text-zinc-500">
        Restoring replaces the server&rsquo;s files with the backup. Locked backups are never deleted automatically.
      </p>
    </div>
  );
}
