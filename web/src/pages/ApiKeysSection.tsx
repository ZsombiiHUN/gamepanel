import { useState, type SubmitEvent } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { Loader2, Trash2 } from "lucide-react";
import { api, call } from "../api/client";
import { apiKeysQuery, invalidates, keys, meQuery } from "../api/queries";
import { CopyButton, ErrorBox, Field, Section } from "../components/ui";
import { PasswordField, MfaCodeField } from "../components/AuthFields";

const when = (t: number) => (t ? new Date(t * 1000).toLocaleString() : "never");

export function ApiKeysSection() {
  const [name, setName] = useState("");
  const [days, setDays] = useState("0");
  const [password, setPassword] = useState("");
  const [code, setCode] = useState("");
  const [secret, setSecret] = useState<string | null>(null);
  const list = useQuery(apiKeysQuery);
  // A key outlives this session, so making one asks for the password (and a
  // code, with two-factor on) again.
  const mfa = useQuery(meQuery).data?.totp_enabled ?? false;

  const create = useMutation({
    mutationFn: () => call(api.POST("/auth/keys", { body: { name, expires_in_days: Number(days), password, ...(mfa ? { code } : {}) } })),
    meta: invalidates(keys.apiKeys),
    onSuccess: (r) => {
      setSecret(r.token);
      setName("");
      setPassword("");
      setCode("");
    },
  });
  const del = useMutation({
    mutationFn: (id: number) => call(api.DELETE("/auth/keys/{id}", { params: { path: { id } } })),
    meta: invalidates(keys.apiKeys),
  });

  const submit = (e: SubmitEvent) => {
    e.preventDefault();
    create.mutate();
  };

  return (
    <Section
      title="API keys"
      id="keys-h"
      desc={
        <>
          Keys act as you. Send one as <code className="rounded bg-zinc-800 px-1 py-0.5 text-xs">Authorization: Bearer &lt;key&gt;</code>.
          They cannot change your password, two-factor settings or create other keys.
        </>
      }
    >
      <div className="space-y-4">
        {secret && (
          <div role="status" className="space-y-2 rounded-xl border border-warn/30 bg-warn-bg p-4">
            <p className="text-sm text-warn-text">Copy this key now. It is not shown again.</p>
            <code className="block break-all rounded-lg bg-well p-3 font-mono text-[13px] text-good-text select-all">{secret}</code>
            <CopyButton text={secret} label="Copy key" />
          </div>
        )}

        {list.data && list.data.length > 0 && (
          <div className="relative overflow-x-auto rounded-xl border border-line" tabIndex={0} role="region" aria-label="API keys">
            <table className="tbl">
              <thead>
                <tr>
                  <th>Name</th>
                  <th>Key</th>
                  <th>Last used</th>
                  <th>Expires</th>
                  <th>
                    <span className="sr-only">Actions</span>
                  </th>
                </tr>
              </thead>
              <tbody>
                {list.data.map((k) => (
                  <tr key={k.id}>
                    <td className="text-zinc-200">{k.name}</td>
                    <td className="font-mono text-[13px] text-zinc-400">{k.prefix}…</td>
                    <td className="text-zinc-400">{when(k.last_used_at)}</td>
                    <td className="text-zinc-400">{when(k.expires_at)}</td>
                    <td className="text-right">
                      <button
                        type="button"
                        className="btn btn-ghost !px-2.5 text-zinc-500 hover:text-bad-text"
                        aria-label={`Revoke ${k.name}`}
                        disabled={del.isPending}
                        onClick={() => del.mutate(k.id)}
                      >
                        <Trash2 className="h-4 w-4" aria-hidden />
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
        <ErrorBox error={list.error ?? del.error} />

        <form onSubmit={submit} className="flex flex-wrap items-end gap-3 border-t border-line pt-4">
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
          <PasswordField label="Your password" value={password} onChange={setPassword} />
          {mfa && <MfaCodeField value={code} onChange={setCode} />}
          <button type="submit" className="btn btn-primary" disabled={create.isPending}>
            {create.isPending && <Loader2 className="h-4 w-4 animate-spin" aria-hidden />} Create key
          </button>
        </form>
        <ErrorBox error={create.error} />
      </div>
    </Section>
  );
}
