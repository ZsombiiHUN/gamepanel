import { useState } from "react";
import { useNavigate } from "@tanstack/react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Trash2 } from "lucide-react";
import { api, call, type components } from "../api/client";
import { eggsQuery, meQuery, nodesQuery } from "../api/queries";
import { ConfirmDialog } from "../components/ui";

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

      <section className="card" aria-labelledby="env-h">
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
      </section>

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
