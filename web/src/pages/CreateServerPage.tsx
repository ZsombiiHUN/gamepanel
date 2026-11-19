import { useMemo, useState, type FormEvent } from "react";
import { useNavigate } from "@tanstack/react-router";
import { useMutation, useQuery } from "@tanstack/react-query";
import { Loader2 } from "lucide-react";
import { api, call } from "../api/client";
import { eggsQuery, invalidates, keys, nodesQuery } from "../api/queries";
import { ErrorBox, Field, PageHeader, Section, ServerIcon, Spinner } from "../components/ui";
import { LimitsFields, defaultLimitsForm, formToLimits, type LimitsForm } from "../components/LimitsFields";

export function CreateServerPage() {
  const nodes = useQuery(nodesQuery);
  const eggs = useQuery(eggsQuery);
  const navigate = useNavigate();

  const [name, setName] = useState("");
  const [nodeId, setNodeId] = useState("");
  const [eggId, setEggId] = useState("");
  const [limits, setLimits] = useState<LimitsForm>(defaultLimitsForm);
  const [port, setPort] = useState("");
  // Variables start from the egg's defaults; what the admin typed is kept per egg, so picking another egg starts clean.
  const [typed, setTyped] = useState<{ egg: string; values: Record<string, string> }>({ egg: "", values: {} });

  const egg = useMemo(() => eggs.data?.find((e) => String(e.id) === eggId), [eggs.data, eggId]);

  const env = {
    ...Object.fromEntries((egg?.variables ?? []).map((v) => [v.env, v.default])),
    ...(typed.egg === eggId ? typed.values : {}),
  };

  const create = useMutation({
    mutationFn: () =>
      call(
        api.POST("/servers", {
          body: {
            name,
            node_id: Number(nodeId),
            egg_id: Number(eggId),
            ...formToLimits(limits),
            port: port === "" ? 0 : Number(port),
            env,
          },
        }),
      ),
    meta: invalidates(keys.servers),
    onSuccess: (s) => {
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
    <form onSubmit={submit} className="mx-auto max-w-3xl" aria-label="Create server">
      <PageHeader title="New server" sub="Pick a node and an egg, set its limits, and it is installed in the background." />
      <div className="space-y-5">
        <Section title="Basics" desc="The name is only for you. The icon is generated from it.">
          <div className="flex items-start gap-4">
            <div className="pt-6">
              <ServerIcon name={name || "new server"} size={52} />
            </div>
            <div className="grid flex-1 gap-4 sm:grid-cols-2">
              <div className="sm:col-span-2">
                <Field label="Name">
                  {(id) => (
                    <input
                      id={id}
                      className="input"
                      required
                      autoFocus
                      maxLength={64}
                      value={name}
                      onChange={(e) => setName(e.target.value)}
                    />
                  )}
                </Field>
              </div>
              <Field label="Node" hint={nodeList.length === 0 ? "Create a node first." : undefined}>
                {(id) => (
                  <select id={id} className="input" required value={nodeId} onChange={(e) => setNodeId(e.target.value)}>
                    <option value="" disabled>
                      Select a node
                    </option>
                    {nodeList.map((n) => (
                      <option key={n.id} value={n.id}>
                        {n.name}
                        {n.online ? "" : " (offline)"}
                      </option>
                    ))}
                  </select>
                )}
              </Field>
              <Field label="Egg" hint={eggList.length === 0 ? "Import an egg first." : egg?.description || undefined}>
                {(id) => (
                  <select id={id} className="input" required value={eggId} onChange={(e) => setEggId(e.target.value)}>
                    <option value="" disabled>
                      Select an egg
                    </option>
                    {eggList.map((x) => (
                      <option key={x.id} value={x.id}>
                        {x.name}
                      </option>
                    ))}
                  </select>
                )}
              </Field>
            </div>
          </div>
        </Section>

        <Section title="Resources" desc="Hard limits for this server. Zero usually means unlimited.">
          <LimitsFields value={limits} onChange={setLimits} />
        </Section>

        <Section title="Network">
          <Field label="Port" hint="Leave empty to take the next free port in the node's range.">
            {(id) => (
              <input
                id={id}
                className="input w-40 font-mono"
                type="number"
                min={1}
                max={65535}
                placeholder="auto"
                value={port}
                onChange={(e) => setPort(e.target.value)}
              />
            )}
          </Field>
        </Section>

        {egg && (egg.variables ?? []).length > 0 && (
          <Section title="Environment variables" desc={`Defaults come from ${egg.name}.`}>
            <div className="grid gap-4 sm:grid-cols-2">
              {(egg.variables ?? []).map((v) => (
                <Field key={v.env} label={v.name} hint={v.description || v.env}>
                  {(id) => (
                    <input
                      id={id}
                      className="input font-mono text-[13px]"
                      value={env[v.env] ?? ""}
                      onChange={(e) => setTyped({ egg: eggId, values: { ...env, [v.env]: e.target.value } })}
                    />
                  )}
                </Field>
              ))}
            </div>
          </Section>
        )}

        <ErrorBox error={create.error} />
        <div className="flex justify-end gap-2 pb-4">
          <button type="button" className="btn" onClick={() => void navigate({ to: "/" })}>
            Cancel
          </button>
          <button type="submit" className="btn btn-primary" disabled={create.isPending}>
            {create.isPending && <Loader2 className="h-4 w-4 animate-spin" aria-hidden />} Create server
          </button>
        </div>
      </div>
    </form>
  );
}
