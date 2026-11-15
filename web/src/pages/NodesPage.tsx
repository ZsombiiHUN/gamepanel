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
  const [created, setCreated] = useState<{ name: string; token: string } | null>(null);
  const [toDelete, setToDelete] = useState<NodeView | null>(null);

  const create = useMutation({
    mutationFn: () => call(api.POST("/nodes", { body: { name, port_start: start, port_end: end } })),
    onSuccess: async (r) => {
      setCreated({ name: r.node.name, token: r.token });
      setName("");
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

      <form onSubmit={submit} className="card grid gap-4 sm:grid-cols-[2fr_1fr_1fr_auto] sm:items-end" aria-labelledby="new-node-h">
        <h2 id="new-node-h" className="text-lg font-medium sm:col-span-4">Add node</h2>
        <Field label="Name">{(id) => <input id={id} className="input" required maxLength={64} value={name} onChange={(e) => setName(e.target.value)} />}</Field>
        <Field label="Port range start">{(id) => <input id={id} className="input" type="number" min={1024} max={65535} required value={start} onChange={(e) => setStart(Number(e.target.value))} />}</Field>
        <Field label="Port range end">{(id) => <input id={id} className="input" type="number" min={1024} max={65535} required value={end} onChange={(e) => setEnd(Number(e.target.value))} />}</Field>
        <button type="submit" className="btn btn-primary" disabled={create.isPending}>
          {create.isPending ? <Loader2 className="h-4 w-4 animate-spin" aria-hidden /> : <Plus className="h-4 w-4" aria-hidden />} Create
        </button>
        <div className="sm:col-span-4"><ErrorBox error={create.error} /></div>
      </form>

      {nodes.isPending && <Spinner />}
      <ErrorBox error={nodes.error} />
      {nodes.data && (
        <div className="overflow-x-auto rounded-lg border border-zinc-800">
          <table className="w-full text-sm">
            <caption className="sr-only">Nodes</caption>
            <thead className="bg-zinc-900 text-left text-zinc-400">
              <tr><th scope="col" className="px-3 py-2">Name</th><th scope="col" className="px-3 py-2">Status</th><th scope="col" className="px-3 py-2">Ports</th><th scope="col" className="px-3 py-2"><span className="sr-only">Actions</span></th></tr>
            </thead>
            <tbody className="divide-y divide-zinc-800">
              {nodes.data.length === 0 && <tr><td colSpan={4} className="px-3 py-3 text-zinc-500">No nodes yet.</td></tr>}
              {nodes.data.map((n) => (
                <tr key={n.id}>
                  <td className="px-3 py-2">{n.name} <span className="text-zinc-500">#{n.id}</span></td>
                  <td className="px-3 py-2"><OnlineDot online={n.online} label="Daemon" /></td>
                  <td className="px-3 py-2 font-mono">{n.port_start}-{n.port_end}</td>
                  <td className="px-3 py-2 text-right">
                    <button type="button" className="btn btn-ghost text-red-300" onClick={() => setToDelete(n)} aria-label={`Delete node ${n.name}`}><Trash2 className="h-4 w-4" aria-hidden /></button>
                  </td>
                </tr>
              ))}
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
