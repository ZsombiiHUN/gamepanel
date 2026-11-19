import { useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { Fingerprint, Loader2, Trash2 } from "lucide-react";
import { api, call } from "../api/client";
import { authOptionsQuery, identitiesQuery, invalidates, keys, passkeysQuery } from "../api/queries";
import { ErrorBox, Field, FormDialog, Section } from "../components/ui";
import { createPasskey, passkeyError, passkeysSupported } from "../lib/webauthn";

export function PasskeysSection() {
  const options = useQuery(authOptionsQuery);
  const list = useQuery({ ...passkeysQuery, enabled: !!options.data?.passkeys });
  const ids = useQuery(identitiesQuery);
  const [adding, setAdding] = useState(false);
  const remove = useMutation({
    mutationFn: (id: string) => call(api.DELETE("/auth/passkeys/{id}", { params: { path: { id } } })),
    meta: invalidates(keys.passkeys),
  });
  if (!options.data?.passkeys) return null;
  const supported = passkeysSupported();
  return (
    <Section
      title="Passkeys"
      desc="Sign in with your fingerprint, face or device PIN instead of a password. Passkeys cannot be phished, and need no second factor."
      footer={
        <>
          <span>{supported ? "Each device or password manager can hold its own passkey." : "This browser does not support passkeys."}</span>
          <button type="button" className="btn btn-primary" disabled={!supported} onClick={() => setAdding(true)}>
            <Fingerprint className="h-4 w-4" aria-hidden /> Add a passkey
          </button>
        </>
      }
    >
      {list.data?.length === 0 && <p className="text-sm text-zinc-400">No passkeys yet.</p>}
      {!!list.data?.length && (
        <ul className="divide-y divide-line rounded-xl border border-line">
          {list.data.map((p) => (
            <li key={p.id} className="flex items-center gap-3 p-3.5">
              <Fingerprint className="h-5 w-5 shrink-0 text-indigo-300" aria-hidden />
              <div className="min-w-0 flex-1">
                <div className="truncate text-sm font-medium text-zinc-100">{p.name}</div>
                <div className="text-xs text-zinc-500">
                  Added {new Date(p.created_at * 1000).toLocaleDateString()} ·{" "}
                  {p.last_used_at ? `last used ${new Date(p.last_used_at * 1000).toLocaleDateString()}` : "never used"}
                </div>
              </div>
              <button
                type="button"
                className="inline-flex h-9 w-9 items-center justify-center rounded-md text-zinc-500 hover:bg-row-hover hover:text-bad-text"
                aria-label={`Remove ${p.name}`}
                onClick={() => {
                  if (window.confirm(`Remove the passkey "${p.name}"? That device can no longer sign in with it.`)) remove.mutate(p.id);
                }}
              >
                <Trash2 className="h-4 w-4" aria-hidden />
              </button>
            </li>
          ))}
        </ul>
      )}
      <div className="mt-3">
        <ErrorBox error={list.error ?? remove.error} />
      </div>
      {adding && <AddPasskey needsPassword={ids.data?.has_password ?? true} onClose={() => setAdding(false)} />}
    </Section>
  );
}

function AddPasskey({ needsPassword, onClose }: { needsPassword: boolean; onClose: () => void }) {
  const [password, setPassword] = useState("");
  const [name, setName] = useState(() => guessDevice());
  const add = useMutation({
    mutationFn: async () => {
      const begin = await call(api.POST("/auth/passkeys/register/begin", { body: needsPassword ? { password } : {} }));
      let credential;
      try {
        credential = await createPasskey(begin.options);
      } catch (err) {
        throw passkeyError(err);
      }
      return call(
        api.POST("/auth/passkeys/register/finish", {
          body: { name: name.trim(), credential: credential },
        }),
      );
    },
    meta: invalidates(keys.passkeys, keys.me),
    onSuccess: onClose,
  });
  return (
    <FormDialog
      open
      title="Add a passkey"
      submitLabel="Continue"
      pending={add.isPending}
      error={add.error}
      disabled={(needsPassword && !password) || !name.trim()}
      onSubmit={() => add.mutate()}
      onClose={onClose}
    >
      <Field label="Name" hint="So you can tell your passkeys apart later.">
        {(id) => <input id={id} className="input" maxLength={64} value={name} onChange={(e) => setName(e.target.value)} />}
      </Field>
      {needsPassword && (
        <Field label="Your password" hint="Adding a way into your account needs your password.">
          {(id) => (
            <input
              id={id}
              className="input"
              type="password"
              autoComplete="current-password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              autoFocus
            />
          )}
        </Field>
      )}
      {add.isPending && (
        <p role="status" className="flex items-center gap-2 text-sm text-zinc-400">
          <Loader2 className="h-4 w-4 animate-spin" aria-hidden /> Follow your browser's prompt.
        </p>
      )}
    </FormDialog>
  );
}

function guessDevice() {
  const ua = typeof navigator === "undefined" ? "" : navigator.userAgent;
  if (/iPhone|iPad/.test(ua)) return "iPhone or iPad";
  if (ua.includes("Android")) return "Android phone";
  if (ua.includes("Mac")) return "Mac";
  if (ua.includes("Windows")) return "Windows PC";
  return "Passkey";
}
