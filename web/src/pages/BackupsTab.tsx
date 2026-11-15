import { useState, type FormEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Loader2, Lock, LockOpen, RotateCcw, Trash2 } from "lucide-react";
import { api, call } from "../api/client";
import { ErrorBox, Field } from "../components/ui";
import { formatBytes } from "../lib/format";

export function BackupsTab({ uuid }: { uuid: string }) {
  const qc = useQueryClient();
  const key = ["backups", uuid];
  const [name, setName] = useState("");
  const [ignore, setIgnore] = useState("");
  const [truncate, setTruncate] = useState(false);
  const list = useQuery({
    queryKey: key,
    queryFn: () => call(api.GET("/servers/{uuid}/backups", { params: { path: { uuid } } })),
    // Poll only while something is running.
    refetchInterval: (q) => (q.state.data?.some((b) => b.status === "running") ? 2000 : false),
  });
  const refresh = () => qc.invalidateQueries({ queryKey: key });

  const create = useMutation({
    mutationFn: () => call(api.POST("/servers/{uuid}/backups", { params: { path: { uuid } }, body: { name, ignore } })),
    onSuccess: () => {
      setName("");
      return refresh();
    },
  });
  const remove = useMutation({
    mutationFn: (backup: string) => call(api.DELETE("/servers/{uuid}/backups/{backup}", { params: { path: { uuid, backup } } })),
    onSuccess: refresh,
  });
  const lock = useMutation({
    mutationFn: (v: { backup: string; locked: boolean }) =>
      call(api.PUT("/servers/{uuid}/backups/{backup}/lock", { params: { path: { uuid, backup: v.backup } }, body: { locked: v.locked } })),
    onSuccess: refresh,
  });
  const restore = useMutation({
    mutationFn: (backup: string) =>
      call(api.POST("/servers/{uuid}/backups/{backup}/restore", { params: { path: { uuid, backup } }, body: { truncate } })),
  });

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

  return (
    <div className="space-y-4">
      <form onSubmit={submit} className="card space-y-3" aria-labelledby="bk-new">
        <h2 id="bk-new" className="text-lg font-medium">New backup</h2>
        <div className="flex flex-wrap items-end gap-3">
          <Field label="Name (optional)">
            {(id) => <input id={id} className="input" maxLength={64} value={name} onChange={(e) => setName(e.target.value)} />}
          </Field>
          <Field label="Leave out" hint="One pattern per line, e.g. logs, *.tmp, cache/">
            {(id) => <textarea id={id} className="input h-20 font-mono" maxLength={2000} value={ignore} onChange={(e) => setIgnore(e.target.value)} />}
          </Field>
          <button type="submit" className="btn btn-primary" disabled={create.isPending}>
            {create.isPending && <Loader2 className="h-4 w-4 animate-spin" aria-hidden />} Create backup
          </button>
        </div>
        <ErrorBox error={create.error} />
      </form>

      <div className="card space-y-3" aria-labelledby="bk-list">
        <h2 id="bk-list" className="text-lg font-medium">Backups</h2>
        <label className="flex items-center gap-2 text-sm text-zinc-300">
          <input type="checkbox" checked={truncate} onChange={(e) => setTruncate(e.target.checked)} />
          When restoring, delete all current files first
        </label>
        <ErrorBox error={list.error ?? remove.error ?? lock.error ?? restore.error} />
        {restore.isSuccess && <p role="status" className="text-sm text-emerald-400">Backup restored.</p>}
        {list.data?.length === 0 && <p className="text-sm text-zinc-400">No backups yet.</p>}
        {list.data && list.data.length > 0 && (
          <table className="w-full text-left text-sm">
            <thead className="text-zinc-400">
              <tr><th className="py-1 pr-2">Name</th><th className="pr-2">Status</th><th className="pr-2">Size</th><th className="pr-2">Created</th><th><span className="sr-only">Actions</span></th></tr>
            </thead>
            <tbody>
              {list.data.map((b) => (
                <tr key={b.uuid} className="border-t border-zinc-800 align-top">
                  <td className="py-1 pr-2">
                    {b.name}
                    {b.status === "ok" && <div className="break-all font-mono text-xs text-zinc-500" title="SHA-256">{b.sha256.slice(0, 16)}…</div>}
                    {b.error && <div className="text-xs text-red-400">{b.error}</div>}
                  </td>
                  <td className="pr-2">
                    {b.status === "running" ? (
                      <span className="inline-flex items-center gap-1"><Loader2 className="h-3 w-3 animate-spin" aria-hidden /> running</span>
                    ) : b.status}
                  </td>
                  <td className="pr-2">{b.status === "ok" ? formatBytes(b.size) : "-"}</td>
                  <td className="pr-2">{new Date(b.created_at * 1000).toLocaleString()}</td>
                  <td className="whitespace-nowrap">
                    <button type="button" className="btn" aria-label={`Restore ${b.name}`} title="Restore (server must be stopped)" disabled={b.status !== "ok" || restore.isPending} onClick={() => doRestore(b.uuid, b.name)}>
                      <RotateCcw className="h-4 w-4" aria-hidden />
                    </button>{" "}
                    <button type="button" className="btn" aria-label={b.locked ? `Unlock ${b.name}` : `Lock ${b.name}`} title={b.locked ? "Locked: cannot be deleted" : "Lock"} onClick={() => lock.mutate({ backup: b.uuid, locked: !b.locked })}>
                      {b.locked ? <Lock className="h-4 w-4" aria-hidden /> : <LockOpen className="h-4 w-4" aria-hidden />}
                    </button>{" "}
                    <button type="button" className="btn btn-danger" aria-label={`Delete ${b.name}`} disabled={b.locked || b.status === "running" || remove.isPending} onClick={() => window.confirm(`Delete backup "${b.name}"?`) && remove.mutate(b.uuid)}>
                      <Trash2 className="h-4 w-4" aria-hidden />
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </div>
  );
}
