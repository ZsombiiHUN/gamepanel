import { useState, type FormEvent } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { Loader2 } from "lucide-react";
import { api, call } from "../api/client";
import { meQuery } from "../api/queries";
import { ErrorBox, Field } from "../components/ui";
import { ApiKeysSection } from "./ApiKeysSection";
import { TotpSection } from "./TotpSection";

export function AccountPage() {
  const me = useQuery(meQuery);
  const [current, setCurrent] = useState("");
  const [next, setNext] = useState("");
  const [again, setAgain] = useState("");
  const mismatch = again !== "" && next !== again;

  const change = useMutation({
    mutationFn: () => call(api.POST("/auth/password", { body: { current_password: current, new_password: next } })),
    onSuccess: () => {
      setCurrent("");
      setNext("");
      setAgain("");
    },
  });
  const revoke = useMutation({
    mutationFn: () => call(api.POST("/auth/sessions/revoke-others")),
  });

  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (!mismatch) change.mutate();
  };

  return (
    <div className="max-w-xl space-y-6">
      <h1 className="text-2xl font-semibold">Account</h1>
      <p className="text-sm text-zinc-400">Signed in as {me.data?.email}</p>

      <form onSubmit={submit} className="card space-y-4" aria-labelledby="pw-h">
        <h2 id="pw-h" className="text-lg font-medium">Change password</h2>
        <Field label="Current password">
          {(id) => <input id={id} className="input" type="password" autoComplete="current-password" required value={current} onChange={(e) => setCurrent(e.target.value)} />}
        </Field>
        <Field label="New password" hint="12 to 256 characters. Other browsers are signed out when you change it.">
          {(id) => <input id={id} className="input" type="password" autoComplete="new-password" required minLength={12} maxLength={256} value={next} onChange={(e) => setNext(e.target.value)} />}
        </Field>
        <Field label="Repeat new password">
          {(id) => <input id={id} className="input" type="password" autoComplete="new-password" required value={again} onChange={(e) => setAgain(e.target.value)} aria-invalid={mismatch} />}
        </Field>
        {mismatch && <p role="alert" className="text-sm text-red-400">The passwords do not match.</p>}
        {change.error && <ErrorBox error={change.error} />}
        {change.isSuccess && <p role="status" className="text-sm text-emerald-400">Password changed. Your other sessions were signed out.</p>}
        <button type="submit" className="btn btn-primary" disabled={change.isPending || mismatch}>
          {change.isPending && <Loader2 className="h-4 w-4 animate-spin" aria-hidden />} Change password
        </button>
      </form>

      <TotpSection />
      <ApiKeysSection />

      <section className="card space-y-3" aria-labelledby="sess-h">
        <h2 id="sess-h" className="text-lg font-medium">Sessions</h2>
        <p className="text-sm text-zinc-400">Sign out every other browser or device that is logged in to your account.</p>
        {revoke.error && <ErrorBox error={revoke.error} />}
        {revoke.isSuccess && <p role="status" className="text-sm text-emerald-400">Other sessions signed out.</p>}
        <button type="button" className="btn" onClick={() => revoke.mutate()} disabled={revoke.isPending}>
          {revoke.isPending && <Loader2 className="h-4 w-4 animate-spin" aria-hidden />} Sign out other sessions
        </button>
      </section>
    </div>
  );
}
