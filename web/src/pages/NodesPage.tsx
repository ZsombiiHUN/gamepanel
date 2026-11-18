import { useState, type FormEvent } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { HardDrive, Loader2, Plus, Settings2, Trash2, X } from "lucide-react";
import { api, call, type components } from "../api/client";
import { invalidates, keys, nodesQuery } from "../api/queries";
import { ConfirmDialog, CopyButton, Empty, ErrorBox, Field, Meter, OnlineDot, PageHeader, Section, Spinner } from "../components/ui";

type NodeView = components["schemas"]["NodeView"];

export function NodesPage() {
  const nodes = useQuery(nodesQuery);
  const [name, setName] = useState("");
  const [start, setStart] = useState(25565);
  const [end, setEnd] = useState(25665);
  const [capMem, setCapMem] = useState("0");
  const [capDisk, setCapDisk] = useState("0");
  const [capOver, setCapOver] = useState("0");
  const [editing, setEditing] = useState<{ id: number; mem: string; disk: string; over: string; host: string; port: string } | null>(null);
  const [adding, setAdding] = useState(false);
  const [created, setCreated] = useState<{ name: string; token: string } | null>(null);
  const [toDelete, setToDelete] = useState<NodeView | null>(null);

  const create = useMutation({
    mutationFn: () => call(api.POST("/nodes", { body: { name, port_start: start, port_end: end, memory_mb: Number(capMem) || 0, disk_mb: Number(capDisk) || 0, overallocate_pct: Number(capOver) || 0 } })),
    meta: invalidates(keys.nodes),
    onSuccess: (r) => {
      setCreated({ name: r.node.name, token: r.token });
      setName("");
      setAdding(false);
    },
  });
  const saveCap = useMutation({
    mutationFn: (v: { id: number; mem: string; disk: string; over: string }) =>
      call(api.PUT("/nodes/{id}/capacity", { params: { path: { id: v.id } }, body: { memory_mb: Number(v.mem) || 0, disk_mb: Number(v.disk) || 0, overallocate_pct: Number(v.over) || 0 } })),
    meta: invalidates(keys.nodes),
    onSuccess: () => {
      setEditing(null);
    },
  });
  const saveSftp = useMutation({
    mutationFn: (v: { id: number; host: string; port: string }) =>
      call(api.PUT("/nodes/{id}/sftp", { params: { path: { id: v.id } }, body: { host: v.host.trim(), port: Number(v.port) || 0 } })),
    meta: invalidates(keys.nodes),
  });
  const del = useMutation({
    mutationFn: (id: number) => call(api.DELETE("/nodes/{id}", { params: { path: { id } } })),
    meta: invalidates(keys.nodes),
    onSuccess: () => {
      setToDelete(null);
    },
  });

  const submit = (e: FormEvent) => {
    e.preventDefault();
    setCreated(null);
    create.mutate();
  };
  const envLine = created ? `GAMEPANEL_NODE_TOKEN=${created.token}` : "";

  const pct = (used: number, cap: number) => (cap > 0 ? (used / cap) * 100 : null);
  const fmt = (mb: number) => (mb >= 1024 ? `${(mb / 1024).toFixed(mb % 1024 ? 1 : 0)} GB` : `${mb} MB`);

  return (
    <div className="mx-auto max-w-5xl">
      <PageHeader
        title="Nodes"
        sub="Machines that run your game servers. Each one runs the daemon, which connects out to this panel."
        actions={
          <button type="button" className="btn btn-primary" aria-expanded={adding} onClick={() => setAdding(!adding)}>
            {adding ? <X className="h-4 w-4" aria-hidden /> : <Plus className="h-4 w-4" aria-hidden />} {adding ? "Cancel" : "Add node"}
          </button>
        }
      />

      <div className="space-y-5">
        {created && (
          <section role="alert" aria-labelledby="token-h" className="rounded-2xl border border-amber-500/60 bg-amber-950/30 p-5">
            <h2 id="token-h" className="text-[15px] font-semibold text-amber-200">Node &quot;{created.name}&quot; created. Save its token now.</h2>
            <p className="mt-1 text-sm text-amber-100/70">It is shown only once. Set it on the daemon as an environment variable:</p>
            <pre tabIndex={0} aria-label="Daemon environment" className="mt-3 overflow-x-auto rounded-lg border border-zinc-800 bg-well p-3 font-mono text-[13px] text-emerald-300 select-all"><code>{envLine}</code></pre>
            <div className="mt-3 flex flex-wrap gap-2">
              <CopyButton text={envLine} label="Copy env line" />
              <CopyButton text={created.token} label="Copy token only" />
              <button type="button" className="btn btn-ghost" onClick={() => setCreated(null)}>Dismiss</button>
            </div>
          </section>
        )}

        {adding && (
          <form onSubmit={submit}>
            <Section
              title="Add a node"
              desc="Creating a node gives you a token for its daemon. Capacity is optional; leave it at 0 to skip tracking."
              footer={
                <>
                  <span>Servers on this node get a port from the range.</span>
                  <button type="submit" className="btn btn-primary" disabled={create.isPending}>
                    {create.isPending ? <Loader2 className="h-4 w-4 animate-spin" aria-hidden /> : <Plus className="h-4 w-4" aria-hidden />} Create node
                  </button>
                </>
              }
            >
              <div className="grid gap-4 sm:grid-cols-3">
                <div className="sm:col-span-3 sm:max-w-md"><Field label="Name">{(id) => <input id={id} className="input" required autoFocus maxLength={64} value={name} onChange={(e) => setName(e.target.value)} />}</Field></div>
                <Field label="Port range start">{(id) => <input id={id} className="input" type="number" min={1024} max={65535} required value={start} onChange={(e) => setStart(Number(e.target.value))} />}</Field>
                <Field label="Port range end">{(id) => <input id={id} className="input" type="number" min={1024} max={65535} required value={end} onChange={(e) => setEnd(Number(e.target.value))} />}</Field>
                <span className="hidden sm:block" />
                <Field label="Memory capacity (MB)" hint="0 = not tracked">{(id) => <input id={id} className="input" type="number" min={0} value={capMem} onChange={(e) => setCapMem(e.target.value)} />}</Field>
                <Field label="Disk capacity (MB)" hint="0 = not tracked">{(id) => <input id={id} className="input" type="number" min={0} value={capDisk} onChange={(e) => setCapDisk(e.target.value)} />}</Field>
                <Field label="Over-allocate (%)" hint="Limits may add up to capacity plus this much.">{(id) => <input id={id} className="input" type="number" min={0} max={1000} value={capOver} onChange={(e) => setCapOver(e.target.value)} />}</Field>
              </div>
              <div className="mt-3"><ErrorBox error={create.error} /></div>
            </Section>
          </form>
        )}

        {nodes.isPending && <Spinner />}
        <ErrorBox error={nodes.error} />
        {nodes.data?.length === 0 && !adding && (
          <Empty icon={<HardDrive className="h-5 w-5" aria-hidden />} title="No nodes yet">
            Add a node, then start the daemon on that machine with the token you get.
          </Empty>
        )}

        <ul className="grid gap-4 lg:grid-cols-2">
          {nodes.data?.map((n) => {
            const open = editing?.id === n.id;
            return (
              <li key={n.id} className="rounded-2xl border border-zinc-800 bg-zinc-900 p-5">
                <div className="flex items-start gap-3">
                  <div className="grid h-10 w-10 shrink-0 place-content-center rounded-xl bg-zinc-800 text-zinc-400"><HardDrive className="h-5 w-5" aria-hidden /></div>
                  <div className="min-w-0 flex-1">
                    <h2 className="truncate text-[15px] font-semibold text-zinc-100">{n.name} <span className="font-normal text-zinc-500">#{n.id}</span></h2>
                    <div className="mt-0.5 flex flex-wrap items-center gap-x-3 text-[13px] text-zinc-400">
                      <OnlineDot online={n.online} label="Daemon" />
                      <span className="font-mono">ports {n.port_start}-{n.port_end}</span>
                      <span title="Where SFTP clients connect">SFTP: <span className="font-mono">{n.sftp_host || "panel's host"}{n.sftp_port ? `:${n.sftp_port}` : ""}</span></span>
                    </div>
                  </div>
                  <button type="button" className="btn btn-ghost !px-2.5" aria-expanded={open} aria-label={`Settings of ${n.name}`} title="Capacity and SFTP address" onClick={() => { saveCap.reset(); saveSftp.reset(); setEditing(open ? null : { id: n.id, mem: String(n.memory_mb), disk: String(n.disk_mb), over: String(n.overallocate_pct), host: n.sftp_host, port: n.sftp_port ? String(n.sftp_port) : "" }); }}>
                    <Settings2 className="h-4 w-4" aria-hidden />
                  </button>
                  <button type="button" className="btn btn-ghost !px-2.5 text-zinc-500 hover:text-red-300" onClick={() => setToDelete(n)} aria-label={`Delete node ${n.name}`}>
                    <Trash2 className="h-4 w-4" aria-hidden />
                  </button>
                </div>
                <div className="mt-5 grid gap-4 sm:grid-cols-2">
                  <Meter label="Memory allocated" text={n.memory_mb ? `${fmt(n.allocated_memory_mb)} / ${fmt(n.memory_mb)}` : `${fmt(n.allocated_memory_mb)} (no cap)`} pct={pct(n.allocated_memory_mb, n.memory_mb)} />
                  <Meter label="Disk allocated" text={n.disk_mb ? `${fmt(n.allocated_disk_mb)} / ${fmt(n.disk_mb)}` : `${fmt(n.allocated_disk_mb)} (no cap)`} pct={pct(n.allocated_disk_mb, n.disk_mb)} />
                </div>
                {open && editing && (
                  <form onSubmit={(e) => { e.preventDefault(); saveCap.mutate(editing); }} className="mt-5 space-y-3 border-t border-zinc-800 pt-4">
                    <div className="grid gap-3 sm:grid-cols-3">
                      <Field label="Memory (MB)">{(id) => <input id={id} className="input" type="number" min={0} value={editing.mem} onChange={(e) => setEditing({ ...editing, mem: e.target.value })} />}</Field>
                      <Field label="Disk (MB)">{(id) => <input id={id} className="input" type="number" min={0} value={editing.disk} onChange={(e) => setEditing({ ...editing, disk: e.target.value })} />}</Field>
                      <Field label="Over-allocate (%)">{(id) => <input id={id} className="input" type="number" min={0} max={1000} value={editing.over} onChange={(e) => setEditing({ ...editing, over: e.target.value })} />}</Field>
                    </div>
                    <p className="text-xs text-zinc-500">Existing servers are not changed. Only new servers and limit edits are checked.</p>
                    <ErrorBox error={saveCap.error} />
                    <button type="submit" className="btn btn-primary" disabled={saveCap.isPending}>Save capacity</button>
                  </form>
                )}
                {open && editing && (
                  <form onSubmit={(e) => { e.preventDefault(); saveSftp.mutate(editing); }} className="mt-5 space-y-3 border-t border-zinc-800 pt-4">
                    <div className="grid gap-3 sm:grid-cols-3">
                      <div className="sm:col-span-2">
                        <Field label="SFTP host" hint="Host name or IP address players' clients reach this node on. Empty = this panel's host.">
                          {(id) => <input id={id} className="input font-mono" placeholder="node1.example.com" maxLength={253} value={editing.host} onChange={(e) => setEditing({ ...editing, host: e.target.value })} />}
                        </Field>
                      </div>
                      <Field label="SFTP port" hint="Empty = what the daemon listens on.">
                        {(id) => <input id={id} className="input" type="number" min={0} max={65535} placeholder="2022" value={editing.port} onChange={(e) => setEditing({ ...editing, port: e.target.value })} />}
                      </Field>
                    </div>
                    <ErrorBox error={saveSftp.error} />
                    <div className="flex items-center gap-3">
                      <button type="submit" className="btn btn-primary" disabled={saveSftp.isPending}>Save SFTP address</button>
                      {saveSftp.isSuccess && <span role="status" className="text-sm text-good-text">Saved.</span>}
                    </div>
                  </form>
                )}
              </li>
            );
          })}
        </ul>
      </div>

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
