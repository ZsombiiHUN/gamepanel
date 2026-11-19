import { useState, type FormEvent } from "react";
import { useSearch } from "@tanstack/react-router";
import { useMutation, useQuery } from "@tanstack/react-query";
import { Link2, Loader2 } from "lucide-react";
import { api, call } from "../api/client";
import { identitiesQuery, invalidates, keys } from "../api/queries";
import { ErrorBox, Field, Pill, Section } from "../components/ui";
import { ssoError, ssoStart } from "../lib/sso";

/** Linked single sign-on providers, with link and unlink. */
export function SignInMethods() {
  const q = useQuery(identitiesQuery);
  const search = useSearch({ from: "/app/account" });
  const unlink = useMutation({
    mutationFn: (provider: string) => call(api.DELETE("/auth/identities/{provider}", { params: { path: { provider } } })),
    meta: invalidates(keys.identities),
  });
  const providers = q.data?.providers ?? [];
  if (!q.data || (providers.length === 0 && q.data.identities.length === 0)) return null;
  const linked = new Map(q.data.identities.map((i) => [i.provider, i]));
  const msg = ssoError(search.sso_error);
  const justLinked = search.sso_linked ? providers.find((p) => p.id === search.sso_linked)?.name : undefined;

  return (
    <Section
      title="Sign-in methods"
      desc="Sign in with an account you already have elsewhere. Linking happens here, while you are signed in, so nobody can attach their sign-in to your account."
    >
      {msg && (
        <p role="alert" className="mb-3 rounded-lg border border-amber-500/30 bg-amber-500/10 px-3 py-2.5 text-sm text-amber-200">
          {msg}
        </p>
      )}
      {justLinked && (
        <p role="status" className="mb-3 text-sm text-emerald-400">
          {justLinked} is linked. You can now sign in with it.
        </p>
      )}
      <ul className="divide-y divide-zinc-800 rounded-xl border border-zinc-800">
        {providers.map((p) => {
          const id = linked.get(p.id);
          return (
            <li key={p.id} className="flex flex-wrap items-center gap-3 p-3.5">
              <div className="min-w-0 flex-1">
                <div className="text-sm font-medium text-zinc-100">{p.name}</div>
                <div className="text-xs text-zinc-500">{id ? `Linked${id.email ? ` as ${id.email}` : ""}` : "Not linked"}</div>
              </div>
              {id ? (
                <button type="button" className="btn btn-ghost" disabled={unlink.isPending} onClick={() => unlink.mutate(p.id)}>
                  Unlink
                </button>
              ) : (
                <a className="btn" href={ssoStart(p.id, { mode: "link" })}>
                  <Link2 className="h-4 w-4" aria-hidden /> Link
                </a>
              )}
            </li>
          );
        })}
      </ul>
      {!q.data.has_password && (
        <p className="mt-3">
          <Pill tone="warn">No password</Pill>{" "}
          <span className="text-sm text-zinc-400">Set one below so you can still sign in if a provider is unavailable.</span>
        </p>
      )}
      <div className="mt-3">
        <ErrorBox error={unlink.error} />
      </div>
    </Section>
  );
}

/** For accounts made through single sign-on: choose a first password. */
export function FirstPassword() {
  const [pw, setPw] = useState("");
  const [again, setAgain] = useState("");
  const set = useMutation({
    mutationFn: () => call(api.POST("/auth/password/first", { body: { password: pw } })),
    meta: invalidates(keys.identities),
  });
  return (
    <form
      onSubmit={(e: FormEvent) => {
        e.preventDefault();
        if (pw === again) set.mutate();
      }}
    >
      <Section
        title="Password"
        desc="Your account was made through single sign-on and has no password yet."
        footer={
          <>
            <span />
            <button type="submit" className="btn btn-primary" disabled={set.isPending || pw !== again}>
              {set.isPending && <Loader2 className="h-4 w-4 animate-spin" aria-hidden />} Set password
            </button>
          </>
        }
      >
        <div className="grid max-w-md gap-4">
          <Field label="New password">
            {(id) => (
              <input
                id={id}
                className="input"
                type="password"
                autoComplete="new-password"
                required
                minLength={12}
                maxLength={256}
                value={pw}
                onChange={(e) => setPw(e.target.value)}
              />
            )}
          </Field>
          <Field label="Repeat new password" hint={again && pw !== again ? "The two do not match yet." : undefined}>
            {(id) => (
              <input
                id={id}
                className="input"
                type="password"
                autoComplete="new-password"
                required
                value={again}
                onChange={(e) => setAgain(e.target.value)}
              />
            )}
          </Field>
          <ErrorBox error={set.error} />
        </div>
      </Section>
    </form>
  );
}
