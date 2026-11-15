import { useState, type FormEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Loader2, Plus, Trash2 } from "lucide-react";
import { api, call, type components } from "../api/client";
import { nodesQuery } from "../api/queries";
import { AdminOnly } from "../components/AdminOnly";
import { ConfirmDialog, CopyButton, ErrorBox, Field, OnlineDot, Spinner } from "../components/ui";

type NodeView = components["schemas"]["NodeView"];

export function NodesPage() {
  return <AdminOnly><Nodes /></AdminOnly>;
}

function Nodes() {
  const qc = useQueryClient();
  const nodes = useQuery(nodesQuery);
  const [name, setName] = useState("");
  const [start, setStart] = useState(25565);
  const [end, setEnd] = useState(25665);
  const [capMem, setCapMem] = useState("0");
  const [capDisk, setCapDisk] = useState("0");
  const [capOver, setCapOver] = useState("0");
  const [editing, setEditing] = useState<{ id: number; mem: string; disk: string; over: string } | null>(null);
  const [created, setCreated] = useState<{ name: string; token: string } | null>(null);
  const [toDelete, setToDelete] = useState<NodeView | null>(null);

  const create = useMutation({
    mutationFn: () => call(api.POST("/nodes", { body: { name, port_start: start, port_end: end, memory_mb: Number(capMem) || 0, disk_mb: Number(capDisk) || 0, overallocate_pct: Number(capOver) || 0 } })),
    onSuccess: async (r) => {
      setCreated({ name: r.node.name, token: r.token });
      setName("");
      await qc.invalidateQueries({ queryKey: ["nodes"] });
    },
  });
  const saveCap = useMutation({
    mutationFn: (v: { id: number; mem: string; disk: string; over: string }) =>
      call(api.PUT("/nodes/{id}/capacity", { params: { path: { id: v.id } }, body: { memory_mb: Number(v.mem) || 0, disk_mb: Number(v.disk) || 0, overallocate_pct: Number(v.over) || 0 } })),
    onSuccess: async () => {
      setEditing(null);
      await qc.invalidateQueries({ queryKey: ["nodes"] });
    },
  });
  const del = useMutation({
    mutationFn: (id: number) => call(api.DELETE("/nodes/{id}", { params: { path: { id } } })),
    onSuccess: async () => {
      setToDelete(null);
      await qc.invalidateQueries({ queryKey: ["nodes"] });
    },
  });

  const submit = (e: FormEvent) => {
    e.preventDefault();
    setCreated(null);
    create.mutate();
  };
  const envLine = created ? `GAMEPANEL_NODE_TOKEN=${created.token}` : "";

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-semibold">Nodes</h1>

      {created && (
        <section role="alert" className="rounded-lg border-2 border-amber-500 bg-amber-950/40 p-4" aria-labelledby="token-h">
          <h2 id="token-h" className="text-lg font-semibold text-amber-200">Node "{created.name}" created - save this token now</h2>
          <p className="mt-1 text-sm text-amber-100/80">This token is shown only once. Set it on the daemon as an environment variable:</p>
          <pre className="mt-3 overflow-x-auto rounded-md bg-zinc-950 p-3 font-mono text-sm text-emerald-300 select-all"><code>{envLine}</code></pre>
          <div className="mt-3 flex gap-2">
            <CopyButton text={envLine} label="Copy env line" />
            <CopyButton text={created.token} label="Copy token only" />
            <button type="button" className="btn btn-ghost" onClick={() => setCreated(null)}>Dismiss</button>
          </div>
        </section>
      )}

      <form onSubmit={submit} className="card grid gap-4 sm:grid-cols-[2fr_1fr_1fr_1fr_auto] sm:items-end" aria-labelledby="new-node-h">
        <h2 id="new-node-h" className="text-lg font-medium sm:col-span-5">Add node</h2>
        <Field label="Name">{(id) => <input id={id} className="input" required maxLength={64} value={name} onChange={(e) => setName(e.target.value)} />}</Field>
        <Field label="Port range start">{(id) => <input id={id} className="input" type="number" min={1024} max={65535} required value={start} onChange={(e) => setStart(Number(e.target.value))} />}</Field>
        <Field label="Port range end">{(id) => <input id={id} className="input" type="number" min={1024} max={65535} required value={end} onChange={(e) => setEnd(Number(e.target.value))} />}</Field>
        <Field label="Memory capacity (MB)" hint="0 = not tracked">{(id) => <input id={id} className="input" type="number" min={0} value={capMem} onChange={(e) => setCapMem(e.target.value)} />}</Field>
        <Field label="Disk capacity (MB)" hint="0 = not tracked">{(id) => <input id={id} className="input" type="number" min={0} value={capDisk} onChange={(e) => setCapDisk(e.target.value)} />}</Field>
        <Field label="Over-allocate (%)" hint="Allow limits to add up to capacity plus this much.">{(id) => <input id={id} className="input" type="number" min={0} max={1000} value={capOver} onChange={(e) => setCapOver(e.target.value)} />}</Field>
        <button type="submit" className="btn btn-primary" disabled={create.isPending}>
          {create.isPending ? <Loader2 className="h-4 w-4 animate-spin" aria-hidden /> : <Plus className="h-4 w-4" aria-hidden />} Create
        </button>
        <div className="sm:col-span-5"><ErrorBox error={create.error} /></div>
      </form>

      {nodes.isPending && <Spinner />}
      <ErrorBox error={nodes.error} />
      {nodes.data && (
        <div className="overflow-x-auto rounded-lg border border-zinc-800">
          <table className="w-full text-sm">
            <caption className="sr-only">Nodes</caption>
            <thead className="bg-zinc-900 text-left text-zinc-400">
              <tr><th scope="col" className="px-3 py-2">Name</th><th scope="col" className="px-3 py-2">Status</th><th scope="col" className="px-3 py-2">Ports</th><th scope="col" className="px-3 py-2">Memory allocated</th><th scope="col" className="px-3 py-2">Disk allocated</th><th scope="col" className="px-3 py-2"><span className="sr-only">Actions</span></th></tr>
            </thead>
            <tbody className="divide-y divide-zinc-800">
              {nodes.data.length === 0 && <tr><td colSpan={6} className="px-3 py-3 text-zinc-500">No nodes yet.</td></tr>}
              {nodes.data.flatMap((n) => [
                <tr key={n.id}>
                  <td className="px-3 py-2">{n.name} <span className="text-zinc-500">#{n.id}</span></td>
                  <td className="px-3 py-2"><OnlineDot online={n.online} label="Daemon" /></td>
                  <td className="px-3 py-2 font-mono">{n.port_start}-{n.port_end}</td>
                  <td className="px-3 py-2">{n.allocated_memory_mb} / {n.memory_mb ? `${n.memory_mb} MB` : "not tracked"}</td>
                  <td className="px-3 py-2">{n.allocated_disk_mb} / {n.disk_mb ? `${n.disk_mb} MB` : "not tracked"}</td>
                  <td className="px-3 py-2 text-right whitespace-nowrap">
                    <button type="button" className="btn btn-ghost" onClick={() => setEditing(editing?.id === n.id ? null : { id: n.id, mem: String(n.memory_mb), disk: String(n.disk_mb), over: String(n.overallocate_pct) })}>Capacity</button>
                    <button type="button" className="btn btn-ghost text-red-300" onClick={() => setToDelete(n)} aria-label={`Delete node ${n.name}`}><Trash2 className="h-4 w-4" aria-hidden /></button>
                  </td>
                </tr>,
                editing?.id === n.id && (
                  <tr key={`${n.id}-cap`}>
                    <td colSpan={6} className="bg-zinc-900/50 px-3 py-3">
                      <form onSubmit={(e) => { e.preventDefault(); saveCap.mutate(editing); }} className="flex flex-wrap items-end gap-3">
                        <Field label="Memory capacity (MB)">{(id) => <input id={id} className="input" type="number" min={0} value={editing.mem} onChange={(e) => setEditing({ ...editing, mem: e.target.value })} />}</Field>
                        <Field label="Disk capacity (MB)">{(id) => <input id={id} className="input" type="number" min={0} value={editing.disk} onChange={(e) => setEditing({ ...editing, disk: e.target.value })} />}</Field>
                        <Field label="Over-allocate (%)">{(id) => <input id={id} className="input" type="number" min={0} max={1000} value={editing.over} onChange={(e) => setEditing({ ...editing, over: e.target.value })} />}</Field>
                        <button type="submit" className="btn btn-primary" disabled={saveCap.isPending}>Save</button>
                        <p className="basis-full text-xs text-zinc-500">Existing servers are not changed. Only new servers and limit edits are checked.</p>
                        <ErrorBox error={saveCap.error} />
                      </form>
                    </td>
                  </tr>
                ),
              ])}
            </tbody>
          </table>
        </div>
      )}

      <ConfirmDialog
        open={!!toDelete}
        title={`Delete node ${toDelete?.name ?? ""}?`}
        body="The node's token stops working. Nodes that still host servers may be refused."
        confirmLabel="Delete node"
        pending={del.isPending}
        error={del.error}
        onConfirm={() => toDelete && del.mutate(toDelete.id)}
        onCancel={() => { setToDelete(null); del.reset(); }}
      />
    </div>
  );
}
