import { useEffect, useState } from "react";
import { useNavigate } from "@tanstack/react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Trash2 } from "lucide-react";
import { api, call, type components } from "../api/client";
import { eggsQuery, meQuery, nodesQuery } from "../api/queries";
import { ConfirmDialog, ErrorBox } from "../components/ui";
import { LimitsFields, formToLimits, limitsToForm, type LimitsForm } from "../components/LimitsFields";

type ServerView = components["schemas"]["ServerView"];

export function SettingsTab({ server }: { server: ServerView }) {
  const me = useQuery(meQuery);
  const isAdmin = !!me.data?.is_admin;
  // Nodes/eggs lists are admin endpoints; only ask when admin.
  const nodes = useQuery({ ...nodesQuery, enabled: isAdmin });
  const eggs = useQuery({ ...eggsQuery, enabled: isAdmin });
  const nodeName = nodes.data?.find((n) => n.id === server.node_id)?.name;
  const eggName = eggs.data?.find((e) => e.id === server.egg_id)?.name;
  const [confirm, setConfirm] = useState(false);
  const qc = useQueryClient();
  const navigate = useNavigate();
  const del = useMutation({
    mutationFn: () => call(api.DELETE("/servers/{uuid}", { params: { path: { uuid: server.uuid } } })),
    onSuccess: async () => {
      qc.removeQueries({ queryKey: ["server", server.uuid] });
      await qc.invalidateQueries({ queryKey: ["servers"] });
      void navigate({ to: "/" });
    },
  });

  const [limits, setLimits] = useState<LimitsForm>(() => limitsToForm(server));
  useEffect(() => setLimits(limitsToForm(server)), [server]);
  const saveLimits = useMutation({
    mutationFn: () => call(api.PUT("/servers/{uuid}/limits", { params: { path: { uuid: server.uuid } }, body: formToLimits(limits) })),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["server", server.uuid] }),
  });
  const stopped = server.state === "offline" || server.state === "install_failed";

  const envEntries = Object.entries(server.env ?? {});
  const rows: [string, string][] = [
    ["UUID", server.uuid],
    ["Egg", eggName ? `${eggName} (#${server.egg_id})` : `#${server.egg_id}`],
    ["Node", nodeName ? `${nodeName} (#${server.node_id})` : `#${server.node_id}`],
    ["Port", String(server.port)],
    ["Memory", `${server.memory_mb} MB`],
    ["CPU limit", server.cpu_pct === 0 ? "unlimited" : `${server.cpu_pct}%`],
    ["Owner ID", String(server.owner_id)],
  ];

  return (
    <div className="space-y-6">
      <section className="card" aria-labelledby="details-h">
        <h2 id="details-h" className="mb-3 text-lg font-medium">Details</h2>
        <dl className="grid gap-x-6 gap-y-2 text-sm sm:grid-cols-[10rem_1fr]">
          {rows.map(([k, v]) => (
            <div key={k} className="contents">
              <dt className="text-zinc-400">{k}</dt>
              <dd className="break-all font-mono">{v}</dd>
            </div>
          ))}
        </dl>
      </section>

      <section className="card space-y-3" aria-labelledby="limits-h">
        <h2 id="limits-h" className="text-lg font-medium">Resource limits</h2>
        {isAdmin ? (
          <form onSubmit={(e) => { e.preventDefault(); saveLimits.mutate(); }} className="space-y-3">
            <LimitsFields value={limits} onChange={setLimits} />
            {!stopped && <p className="text-sm text-amber-300">Stop the server to change its limits.</p>}
            <ErrorBox error={saveLimits.error} />
            {saveLimits.isSuccess && <p role="status" className="text-sm text-emerald-400">Limits saved.</p>}
            <button type="submit" className="btn btn-primary" disabled={saveLimits.isPending || !stopped}>Save limits</button>
          </form>
        ) : (
          <dl className="grid gap-x-6 gap-y-1 text-sm sm:grid-cols-[12rem_1fr]">
            {[
              ["Memory", `${server.memory_mb} MB`], ["Swap", server.swap_mb < 0 ? "unlimited" : `${server.swap_mb} MB`],
              ["CPU", server.cpu_pct ? `${server.cpu_pct}%` : "unlimited"], ["Cores", server.cpuset || "any"],
              ["Disk", server.disk_mb ? `${server.disk_mb} MB` : "unlimited"], ["Max processes", String(server.pids_limit)],
              ["Backups to keep", String(server.backup_limit)],
            ].map(([k, v]) => (<div key={k} className="contents"><dt className="text-zinc-400">{k}</dt><dd>{v}</dd></div>))}
          </dl>
        )}
      </section>

      {(server.permissions.includes("files.read") || server.permissions.includes("files.write")) && (
        <section className="card" aria-labelledby="sftp-h">
          <h2 id="sftp-h" className="mb-3 text-lg font-medium">SFTP</h2>
          <dl className="grid gap-x-6 gap-y-2 text-sm sm:grid-cols-[10rem_1fr]">
            <dt className="text-zinc-400">Host</dt>
            <dd className="font-mono">your node&apos;s address, port 2022 (default)</dd>
            <dt className="text-zinc-400">Username</dt>
            <dd className="break-all font-mono">{me.data?.email}.{server.uuid.slice(0, 8)}</dd>
            <dt className="text-zinc-400">Password</dt>
            <dd>An API key from your Account page. Your account password does not work for SFTP.</dd>
          </dl>
        </section>
      )}

      {server.owner && <section className="card" aria-labelledby="env-h">
        <h2 id="env-h" className="mb-3 text-lg font-medium">Environment</h2>
        {envEntries.length === 0 ? (
          <p className="text-sm text-zinc-400">No environment variables.</p>
        ) : (
          <dl className="grid gap-x-6 gap-y-2 text-sm sm:grid-cols-[14rem_1fr]">
            {envEntries.map(([k, v]) => (
              <div key={k} className="contents">
                <dt className="break-all font-mono text-zinc-400">{k}</dt>
                <dd className="break-all font-mono">{v}</dd>
              </div>
            ))}
          </dl>
        )}
      </section>}

      {isAdmin && (
        <section className="card border-red-900" aria-labelledby="danger-h">
          <h2 id="danger-h" className="mb-2 text-lg font-medium text-red-300">Danger zone</h2>
          <p className="mb-3 text-sm text-zinc-400">Deleting a server removes it from the panel and its node.</p>
          <button type="button" className="btn btn-danger" onClick={() => setConfirm(true)}>
            <Trash2 className="h-4 w-4" aria-hidden /> Delete server
          </button>
        </section>
      )}

      <ConfirmDialog
        open={confirm}
        title={`Delete ${server.name}?`}
        body="This permanently deletes the server and cannot be undone."
        confirmLabel="Delete server"
        pending={del.isPending}
        error={del.error}
        onConfirm={() => del.mutate()}
        onCancel={() => { setConfirm(false); del.reset(); }}
      />
    </div>
  );
}
