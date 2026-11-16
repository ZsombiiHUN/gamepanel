import { useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { Database, Plus, Trash2 } from "lucide-react";
import { api, call } from "../api/client";
import { databaseHostsQuery, invalidates, keys, nodesQuery } from "../api/queries";
import { Empty, ErrorBox, Field, FormDialog, PageHeader, Pill, Spinner } from "../components/ui";

export function DatabaseHostsPage() {
  const hosts = useQuery(databaseHostsQuery);
  const nodes = useQuery(nodesQuery);
  const [adding, setAdding] = useState(false);
  const remove = useMutation({
    mutationFn: (id: number) => call(api.DELETE("/database-hosts/{id}", { params: { path: { id } } })),
    meta: invalidates(keys.databaseHosts),
  });
  return (
    <div className="space-y-6">
      <PageHeader
        title="Database hosts"
        sub="MySQL or MariaDB servers that game servers get their databases on."
        actions={<button type="button" className="btn btn-primary" onClick={() => setAdding(true)}><Plus className="h-4 w-4" aria-hidden /> Add host</button>}
      />
      {hosts.isPending && <Spinner />}
      <ErrorBox error={hosts.error ?? remove.error} />
      {hosts.data?.length === 0 && (
        <Empty icon={<Database className="h-5 w-5" aria-hidden />} title="No database hosts">
          Add one so server owners can create MySQL databases. The panel needs an account there that may create databases and users.
        </Empty>
      )}
      {hosts.data && hosts.data.length > 0 && (
        <ul className="divide-y divide-zinc-800 overflow-hidden rounded-2xl border border-zinc-800 bg-zinc-900">
          {hosts.data.map((h) => (
            <li key={h.id} className="flex flex-wrap items-center gap-x-5 gap-y-2 px-5 py-4">
              <div className="min-w-0 flex-1 basis-60">
                <div className="flex flex-wrap items-center gap-2"><span className="font-medium">{h.name}</span>{h.node_id ? <Pill>for {nodes.data?.find((n) => n.id === h.node_id)?.name ?? `node #${h.node_id}`}</Pill> : null}</div>
                <div className="mt-0.5 font-mono text-[13px] text-zinc-400">{h.username}@{h.host}:{h.port}</div>
              </div>
              <span className="text-sm text-zinc-400">{h.databases} {h.databases === 1 ? "database" : "databases"}{h.max_databases ? ` of ${h.max_databases}` : ""}</span>
              <button type="button" className="inline-flex h-9 w-9 items-center justify-center rounded-md text-zinc-500 hover:bg-zinc-800 hover:text-red-300 disabled:opacity-40" aria-label={`Remove ${h.name}`}
                title={h.databases ? "Delete its databases first" : "Remove"} disabled={h.databases > 0 || remove.isPending}
                onClick={() => { if (window.confirm(`Remove the database host ${h.name}? Its databases are not touched.`)) remove.mutate(h.id); }}>
                <Trash2 className="h-4 w-4" aria-hidden />
              </button>
            </li>
          ))}
        </ul>
      )}
      {adding && <AddHost nodes={nodes.data ?? []} onClose={() => setAdding(false)} />}
    </div>
  );
}

function AddHost({ nodes, onClose }: { nodes: { id: number; name: string }[]; onClose: () => void }) {
  const [f, setF] = useState({ name: "", host: "", port: "3306", username: "", password: "", max: "0", node: "0" });
  const set = (k: keyof typeof f) => (e: React.ChangeEvent<HTMLInputElement | HTMLSelectElement>) => setF({ ...f, [k]: e.target.value });
  const create = useMutation({
    mutationFn: () => call(api.POST("/database-hosts", { body: { name: f.name.trim(), host: f.host.trim(), port: Number(f.port) || 3306, username: f.username, password: f.password, max_databases: Number(f.max) || 0, node_id: Number(f.node) || 0 } })),
    meta: invalidates(keys.databaseHosts),
    onSuccess: onClose,
  });
  return (
    <FormDialog open title="Add a database host" submitLabel="Test and add" pending={create.isPending} error={create.error} disabled={!f.name.trim() || !f.host.trim() || !f.username} onSubmit={() => create.mutate()} onClose={onClose}>
      <Field label="Name">{(id) => <input id={id} className="input" value={f.name} onChange={set("name")} autoFocus placeholder="eu-db-1" />}</Field>
      <div className="grid grid-cols-[1fr_7rem] gap-3">
        <Field label="Address" hint="As game servers reach it.">{(id) => <input id={id} className="input font-mono" value={f.host} onChange={set("host")} placeholder="10.0.0.5" />}</Field>
        <Field label="Port">{(id) => <input id={id} className="input font-mono" inputMode="numeric" value={f.port} onChange={set("port")} />}</Field>
      </div>
      <div className="grid gap-3 sm:grid-cols-2">
        <Field label="Username" hint="Needs CREATE, DROP, CREATE USER and GRANT OPTION.">{(id) => <input id={id} className="input font-mono" value={f.username} onChange={set("username")} autoComplete="off" />}</Field>
        <Field label="Password" hint="Stored encrypted.">{(id) => <input id={id} type="password" className="input" value={f.password} onChange={set("password")} autoComplete="new-password" />}</Field>
      </div>
      <div className="grid gap-3 sm:grid-cols-2">
        <Field label="Most databases" hint="0 = no limit.">{(id) => <input id={id} className="input" inputMode="numeric" value={f.max} onChange={set("max")} />}</Field>
        <Field label="Prefer for node">
          {(id) => (
            <select id={id} className="input" value={f.node} onChange={set("node")}>
              <option value="0">Any node</option>
              {nodes.map((n) => <option key={n.id} value={n.id}>{n.name}</option>)}
            </select>
          )}
        </Field>
      </div>
      <p className="text-xs text-zinc-500">The panel connects before saving, so a wrong address or password is caught now.</p>
    </FormDialog>
  );
}
