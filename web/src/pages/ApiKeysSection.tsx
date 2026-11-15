import { useState, type FormEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Loader2, Trash2 } from "lucide-react";
import { api, call } from "../api/client";
import { ErrorBox, Field } from "../components/ui";

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
    <section className="card space-y-4" aria-labelledby="keys-h">
      <h2 id="keys-h" className="text-lg font-medium">API keys</h2>
      <p className="text-sm text-zinc-400">
        Keys act as you. Send one as <code>Authorization: Bearer &lt;key&gt;</code>. They cannot change your password,
        two-factor settings or create other keys.
      </p>

      {secret && (
        <div role="status" className="space-y-1 rounded-md border border-amber-700 bg-amber-950/40 p-3">
          <p className="text-sm text-amber-300">Copy this key now. It is not shown again.</p>
          <code className="block break-all text-sm">{secret}</code>
        </div>
      )}

      {keys.data && keys.data.length > 0 && (
        <table className="w-full text-left text-sm">
          <thead className="text-zinc-400">
            <tr><th className="py-1 pr-2">Name</th><th className="pr-2">Key</th><th className="pr-2">Last used</th><th className="pr-2">Expires</th><th><span className="sr-only">Actions</span></th></tr>
          </thead>
          <tbody>
            {keys.data.map((k) => (
              <tr key={k.id} className="border-t border-zinc-800">
                <td className="py-1 pr-2">{k.name}</td>
                <td className="pr-2 font-mono">{k.prefix}…</td>
                <td className="pr-2">{when(k.last_used_at)}</td>
                <td className="pr-2">{when(k.expires_at)}</td>
                <td>
                  <button type="button" className="btn btn-danger" aria-label={`Revoke ${k.name}`} disabled={del.isPending} onClick={() => del.mutate(k.id)}>
                    <Trash2 className="h-4 w-4" aria-hidden />
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
      <ErrorBox error={keys.error ?? del.error} />

      <form onSubmit={submit} className="flex flex-wrap items-end gap-3">
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
    </section>
  );
}
