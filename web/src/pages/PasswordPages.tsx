import { useState, type FormEvent } from "react";
import { Link, useNavigate } from "@tanstack/react-router";
import { useMutation, useQuery } from "@tanstack/react-query";
import { CheckCircle2, Loader2 } from "lucide-react";
import { api, call } from "../api/client";
import { authOptionsQuery, inviteQuery, invalidates, keys } from "../api/queries";
import { AuthLayout, tokenFromHash } from "../components/AuthLayout";
import { ErrorBox, Field } from "../components/ui";
import { SSOButtons } from "./LoginPage";

export function ForgotPage() {
  const [email, setEmail] = useState("");
  const send = useMutation({ mutationFn: () => call(api.POST("/auth/forgot", { body: { email: email.trim() } })) });
  if (send.isSuccess) {
    return (
      <AuthLayout title="Check your email" sub="">
        <p role="status" className="flex gap-2 text-sm text-zinc-300">
          <CheckCircle2 className="h-5 w-5 shrink-0 text-emerald-400" aria-hidden />
          If {email.trim()} has an account, a link to choose a new password is on its way. It works for an hour.
        </p>
        <Link to="/login" className="link text-sm">
          Back to sign in
        </Link>
      </AuthLayout>
    );
  }
  return (
    <AuthLayout title="Forgot your password?" sub="We will email you a link to choose a new one.">
      <form
        className="space-y-6"
        aria-labelledby="auth-title"
        onSubmit={(e: FormEvent) => {
          e.preventDefault();
          send.mutate();
        }}
      >
        <ErrorBox error={send.error} />
        <Field label="Email">
          {(id) => (
            <input
              id={id}
              className="input"
              type="email"
              autoComplete="username"
              required
              autoFocus
              value={email}
              onChange={(e) => setEmail(e.target.value)}
            />
          )}
        </Field>
        <button type="submit" className="btn btn-primary w-full" disabled={send.isPending}>
          {send.isPending && <Loader2 className="h-4 w-4 animate-spin" aria-hidden />} Send the link
        </button>
        <Link to="/login" className="link block text-center text-sm">
          Back to sign in
        </Link>
      </form>
    </AuthLayout>
  );
}

/** Choose a password twice; the second must match before the form is sent. */
function NewPassword({
  password,
  confirm,
  onPassword,
  onConfirm,
}: {
  password: string;
  confirm: string;
  onPassword: (v: string) => void;
  onConfirm: (v: string) => void;
}) {
  return (
    <>
      <Field label="New password" hint="At least 12 characters. A few unrelated words make a strong one.">
        {(id) => (
          <input
            id={id}
            className="input"
            type="password"
            autoComplete="new-password"
            required
            minLength={12}
            maxLength={256}
            value={password}
            onChange={(e) => onPassword(e.target.value)}
          />
        )}
      </Field>
      <Field label="Repeat it" hint={confirm && confirm !== password ? "The two do not match yet." : undefined}>
        {(id) => (
          <input
            id={id}
            className="input"
            type="password"
            autoComplete="new-password"
            required
            value={confirm}
            onChange={(e) => onConfirm(e.target.value)}
          />
        )}
      </Field>
    </>
  );
}

export function ResetPage() {
  const [token] = useState(() => tokenFromHash());
  const [password, setPassword] = useState("");
  const [confirm, setConfirm] = useState("");
  const reset = useMutation({ mutationFn: () => call(api.POST("/auth/reset", { body: { token, password } })) });
  if (!token) {
    return (
      <AuthLayout title="This link is incomplete" sub="Open the link from the email again, or ask for a new one.">
        <Link to="/forgot" className="link text-sm">
          Ask for a new link
        </Link>
      </AuthLayout>
    );
  }
  if (reset.isSuccess) {
    return (
      <AuthLayout title="Password changed" sub="You were signed out everywhere. Sign in with your new password.">
        <Link to="/login" className="btn btn-primary w-full">
          Sign in
        </Link>
      </AuthLayout>
    );
  }
  return (
    <AuthLayout title="Choose a new password" sub="Every device signed in to your account is signed out.">
      <form
        className="space-y-6"
        aria-labelledby="auth-title"
        onSubmit={(e: FormEvent) => {
          e.preventDefault();
          if (password === confirm) reset.mutate();
        }}
      >
        <ErrorBox error={reset.error} />
        <NewPassword password={password} confirm={confirm} onPassword={setPassword} onConfirm={setConfirm} />
        <button type="submit" className="btn btn-primary w-full" disabled={reset.isPending || password !== confirm}>
          {reset.isPending && <Loader2 className="h-4 w-4 animate-spin" aria-hidden />} Save new password
        </button>
      </form>
    </AuthLayout>
  );
}

export function RegisterPage() {
  const [token] = useState(() => tokenFromHash());
  const invite = useQuery({ ...inviteQuery(token), enabled: !!token });
  const options = useQuery(authOptionsQuery);
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [confirm, setConfirm] = useState("");
  const navigate = useNavigate();
  const register = useMutation({
    mutationFn: () => call(api.POST("/auth/register", { body: token ? { token, password } : { email: email.trim(), password } })),
    meta: invalidates(keys.me),
    onSuccess: () => void navigate({ to: "/" }),
  });
  const inv = invite.data;
  const sub = inv
    ? inv.server_name
      ? `You were invited to the game server ${inv.server_name}.`
      : "You were invited to make an account."
    : "Make an account to manage game servers.";
  return (
    <AuthLayout title={inv ? "You are invited" : "Create an account"} sub={sub}>
      {token && invite.isPending && <Loader2 className="h-5 w-5 animate-spin text-zinc-400" aria-label="Checking the invitation" />}
      {token && invite.error ? (
        <>
          <ErrorBox error={invite.error} />
          <Link to="/login" className="link text-sm">
            Back to sign in
          </Link>
        </>
      ) : (
        (!token || inv) && (
          <form
            className="space-y-6"
            aria-labelledby="auth-title"
            onSubmit={(e: FormEvent) => {
              e.preventDefault();
              if (password === confirm) register.mutate();
            }}
          >
            <ErrorBox error={register.error} />
            <Field label="Email" hint={inv ? "The invitation is for this address." : undefined}>
              {(id) => (
                <input
                  id={id}
                  className="input"
                  type="email"
                  autoComplete="username"
                  required
                  readOnly={!!inv}
                  value={inv?.email ?? email}
                  onChange={(e) => setEmail(e.target.value)}
                />
              )}
            </Field>
            <NewPassword password={password} confirm={confirm} onPassword={setPassword} onConfirm={setConfirm} />
            <button type="submit" className="btn btn-primary w-full" disabled={register.isPending || password !== confirm}>
              {register.isPending && <Loader2 className="h-4 w-4 animate-spin" aria-hidden />} Create account
            </button>
            {(options.data?.sso.length ?? 0) > 0 && (
              <>
                <div className="flex items-center gap-3 text-xs text-zinc-500" aria-hidden>
                  <span className="h-px flex-1 bg-zinc-800" />
                  or, without a password
                  <span className="h-px flex-1 bg-zinc-800" />
                </div>
                <SSOButtons providers={options.data!.sso} query={token ? { invite: token } : {}} />
              </>
            )}
            <p className="text-center text-sm text-zinc-400">
              Already have one?{" "}
              <Link to="/login" className="link">
                Sign in
              </Link>
            </p>
          </form>
        )
      )}
    </AuthLayout>
  );
}
