import { useState, type FormEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Loader2, Trash2 } from "lucide-react";
import { api, call } from "../api/client";
import { CopyButton, ErrorBox, Field, Section } from "../components/ui";

const when = (t: number) => (t ? new Date(t * 1000).toLocaleString() : "never");

export function ApiKeysSection() {
  const qc = useQueryClient();
  const [name, setName] = useState("");
  const [days, setDays] = useState("0");
  const [secret, setSecret] = useState<string | null>(null);
  const keys = useQuery({ queryKey: ["apikeys"], queryFn: () => call(api.GET("/auth/keys")) });

  const create = useMutation({
    mutationFn: () => call(api.POST("/auth/keys", { body: { name, expires_in_days: Number(days) } })),
    onSuccess: async (r) => {
      setSecret(r.token);
      setName("");
      await qc.invalidateQueries({ queryKey: ["apikeys"] });
    },
  });
  const del = useMutation({
    mutationFn: (id: number) => call(api.DELETE("/auth/keys/{id}", { params: { path: { id } } })),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["apikeys"] }),
  });

  const submit = (e: FormEvent) => {
    e.preventDefault();
    create.mutate();
  };

  return (
    <Section
      title="API keys"
      id="keys-h"
      desc={<>Keys act as you. Send one as <code className="rounded bg-zinc-800 px-1 py-0.5 text-xs">Authorization: Bearer &lt;key&gt;</code>. They cannot change your password, two-factor settings or create other keys.</>}
    >
      <div className="space-y-4">
        {secret && (
          <div role="status" className="space-y-2 rounded-xl border border-amber-500/40 bg-amber-950/20 p-4">
            <p className="text-sm text-amber-200">Copy this key now. It is not shown again.</p>
            <code className="block break-all rounded-lg bg-well p-3 font-mono text-[13px] text-emerald-300 select-all">{secret}</code>
            <CopyButton text={secret} label="Copy key" />
          </div>
        )}

        {keys.data && keys.data.length > 0 && (
          <div className="overflow-x-auto rounded-xl border border-zinc-800">
            <table className="w-full text-left text-sm">
              <thead className="border-b border-zinc-800 text-xs uppercase tracking-wider text-zinc-500">
                <tr><th className="px-3.5 py-2.5 font-medium">Name</th><th className="px-3.5 py-2.5 font-medium">Key</th><th className="px-3.5 py-2.5 font-medium">Last used</th><th className="px-3.5 py-2.5 font-medium">Expires</th><th><span className="sr-only">Actions</span></th></tr>
              </thead>
              <tbody className="divide-y divide-zinc-800/70">
                {keys.data.map((k) => (
                  <tr key={k.id}>
                    <td className="px-3.5 py-2.5 text-zinc-200">{k.name}</td>
                    <td className="px-3.5 py-2.5 font-mono text-[13px] text-zinc-400">{k.prefix}…</td>
                    <td className="px-3.5 py-2.5 text-zinc-400">{when(k.last_used_at)}</td>
                    <td className="px-3.5 py-2.5 text-zinc-400">{when(k.expires_at)}</td>
                    <td className="px-2 text-right">
                      <button type="button" className="btn btn-ghost !px-2.5 text-zinc-500 hover:text-red-300" aria-label={`Revoke ${k.name}`} disabled={del.isPending} onClick={() => del.mutate(k.id)}>
                        <Trash2 className="h-4 w-4" aria-hidden />
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
        <ErrorBox error={keys.error ?? del.error} />

        <form onSubmit={submit} className="flex flex-wrap items-end gap-3 border-t border-zinc-800 pt-4">
          <Field label="Name">
            {(id) => <input id={id} className="input" required maxLength={64} value={name} onChange={(e) => setName(e.target.value)} />}
          </Field>
          <Field label="Expires">
            {(id) => (
              <select id={id} className="input" value={days} onChange={(e) => setDays(e.target.value)}>
                <option value="0">Never</option>
                <option value="30">30 days</option>
                <option value="90">90 days</option>
                <option value="365">1 year</option>
              </select>
            )}
          </Field>
          <button type="submit" className="btn btn-primary" disabled={create.isPending}>
            {create.isPending && <Loader2 className="h-4 w-4 animate-spin" aria-hidden />} Create key
          </button>
        </form>
        <ErrorBox error={create.error} />
      </div>
    </Section>
  );
}
