import { useEffect, useMemo, useState, type FormEvent } from "react";
import { useNavigate } from "@tanstack/react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Loader2 } from "lucide-react";
import { api, call } from "../api/client";
import { eggsQuery, nodesQuery } from "../api/queries";
import { AdminOnly } from "../components/AdminOnly";
import { ErrorBox, Field, Spinner } from "../components/ui";

export function CreateServerPage() {
  return <AdminOnly><CreateServer /></AdminOnly>;
}

function CreateServer() {
  const nodes = useQuery(nodesQuery);
  const eggs = useQuery(eggsQuery);
  const qc = useQueryClient();
  const navigate = useNavigate();

  const [name, setName] = useState("");
  const [nodeId, setNodeId] = useState("");
  const [eggId, setEggId] = useState("");
  const [memory, setMemory] = useState(1024);
  const [cpu, setCpu] = useState("");
  const [port, setPort] = useState("");
  const [env, setEnv] = useState<Record<string, string>>({});

  const egg = useMemo(() => eggs.data?.find((e) => String(e.id) === eggId), [eggs.data, eggId]);

  // Reset env overrides to the egg's defaults whenever the egg changes.
  useEffect(() => {
    setEnv(Object.fromEntries((egg?.variables ?? []).map((v) => [v.env, v.default])));
  }, [egg]);

  const create = useMutation({
    mutationFn: () =>
      call(
        api.POST("/servers", {
          body: {
            name,
            node_id: Number(nodeId),
            egg_id: Number(eggId),
            memory_mb: memory,
            cpu_pct: cpu === "" ? 0 : Number(cpu),
            port: port === "" ? 0 : Number(port),
            env,
          },
        }),
      ),
    onSuccess: async (s) => {
      await qc.invalidateQueries({ queryKey: ["servers"] });
      void navigate({ to: "/servers/$uuid", params: { uuid: s.uuid }, search: { tab: "console" } });
    },
  });

  const submit = (e: FormEvent) => {
    e.preventDefault();
    create.mutate();
  };

  if (nodes.isPending || eggs.isPending) return <Spinner />;
  const loadErr = nodes.error ?? eggs.error;
  if (loadErr) return <ErrorBox error={loadErr} />;
  const nodeList = nodes.data ?? [];
  const eggList = eggs.data ?? [];

  return (
    <form onSubmit={submit} className="card mx-auto max-w-2xl space-y-4" aria-labelledby="create-h">
      <h1 id="create-h" className="text-2xl font-semibold">Create server</h1>
      <Field label="Name">{(id) => <input id={id} className="input" required maxLength={64} value={name} onChange={(e) => setName(e.target.value)} />}</Field>
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label="Node" hint={nodeList.length === 0 ? "Create a node first." : undefined}>
          {(id) => (
            <select id={id} className="input" required value={nodeId} onChange={(e) => setNodeId(e.target.value)}>
              <option value="" disabled>Select a node</option>
              {nodeList.map((n) => <option key={n.id} value={n.id}>{n.name}{n.online ? "" : " (offline)"}</option>)}
            </select>
          )}
        </Field>
        <Field label="Egg" hint={eggList.length === 0 ? "Import an egg first." : undefined}>
          {(id) => (
            <select id={id} className="input" required value={eggId} onChange={(e) => setEggId(e.target.value)}>
              <option value="" disabled>Select an egg</option>
              {eggList.map((x) => <option key={x.id} value={x.id}>{x.name}</option>)}
            </select>
          )}
        </Field>
      </div>
      <div className="grid gap-4 sm:grid-cols-3">
        <Field label="Memory (MB)">{(id) => <input id={id} className="input" type="number" min={64} max={1048576} required value={memory} onChange={(e) => setMemory(Number(e.target.value))} />}</Field>
        <Field label="CPU limit (%)" hint="100 = one core. Empty = unlimited.">{(id) => <input id={id} className="input" type="number" min={0} max={6400} value={cpu} onChange={(e) => setCpu(e.target.value)} />}</Field>
        <Field label="Port" hint="Empty = auto-allocate.">{(id) => <input id={id} className="input" type="number" min={1} max={65535} value={port} onChange={(e) => setPort(e.target.value)} />}</Field>
      </div>

      {egg && (egg.variables ?? []).length > 0 && (
        <fieldset className="space-y-3 rounded-md border border-zinc-800 p-3">
          <legend className="px-1 text-sm font-medium text-zinc-300">Environment variables</legend>
          {(egg.variables ?? []).map((v) => (
            <Field key={v.env} label={`${v.name} (${v.env})`} hint={v.description || undefined}>
              {(id) => <input id={id} className="input font-mono" value={env[v.env] ?? ""} onChange={(e) => setEnv((prev) => ({ ...prev, [v.env]: e.target.value }))} />}
            </Field>
          ))}
        </fieldset>
      )}

      <ErrorBox error={create.error} />
      <button type="submit" className="btn btn-primary" disabled={create.isPending}>
        {create.isPending && <Loader2 className="h-4 w-4 animate-spin" aria-hidden />} Create server
      </button>
    </form>
  );
}
