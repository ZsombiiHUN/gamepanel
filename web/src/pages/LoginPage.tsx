import { useState, type SubmitEvent } from "react";
import { Link, useNavigate, useSearch } from "@tanstack/react-router";
import { useMutation, useQuery } from "@tanstack/react-query";
import { Fingerprint, Loader2, LogIn } from "lucide-react";
import { api, ApiError, call } from "../api/client";
import { authOptionsQuery, invalidates, keys } from "../api/queries";
import { AuthLayout } from "../components/AuthLayout";
import { ErrorBox, Field } from "../components/ui";
import { ssoError, ssoStart } from "../lib/sso";
import { getPasskey, passkeyError, passkeysSupported } from "../lib/webauthn";

/** The password was right and the account wants its second factor: a step, not an error. */
const needsCode = (err: unknown) => err instanceof ApiError && err.detail === "totp_required";

/** "Continue with GitHub" buttons. Plain links: the browser leaves for the provider. */
export function SSOButtons({ providers, query }: { providers: { id: string; name: string }[]; query?: Record<string, string> }) {
  if (providers.length === 0) return null;
  return (
    <div className="space-y-2">
      {providers.map((p) => (
        <a key={p.id} href={ssoStart(p.id, query)} className="btn w-full">
          <LogIn className="h-4 w-4" aria-hidden /> Continue with {p.name}
        </a>
      ))}
    </div>
  );
}

export function LoginPage() {
  const search = useSearch({ from: "/login" });
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [code, setCode] = useState("");
  const [needCode, setNeedCode] = useState(false);
  const navigate = useNavigate();
  const options = useQuery(authOptionsQuery);
  const login = useMutation({
    mutationFn: () => call(api.POST("/auth/login", { body: { email, password, ...(needCode ? { totp_code: code } : {}) } })),
    onError: (err) => {
      // The server answers 401 "totp_required" once the password is right.
      if (needsCode(err)) setNeedCode(true);
    },
    meta: invalidates(keys.me),
    onSuccess: () => void navigate({ to: "/" }),
  });
  // After single sign-on, an account with two-factor still owes its code.
  const ssoTotp = search.sso === "totp";
  const verify = useMutation({
    mutationFn: () => call(api.POST("/auth/sso/verify", { body: { code } })),
    meta: invalidates(keys.me),
    onSuccess: () => void navigate({ to: "/" }),
  });

  const passkey = useMutation({
    mutationFn: async () => {
      const begin = await call(api.POST("/auth/passkeys/login/begin"));
      let credential;
      try {
        credential = await getPasskey(begin.options);
      } catch (err) {
        throw passkeyError(err);
      }
      return call(
        api.POST("/auth/passkeys/login/finish", {
          body: { ticket: begin.ticket, credential: credential },
        }),
      );
    },
    meta: invalidates(keys.me),
    onSuccess: () => void navigate({ to: "/" }),
  });
  const opts = options.data;
  const providers = opts?.sso ?? [];
  const offerPasskey = !!opts?.passkeys && passkeysSupported();

  if (ssoTotp) {
    return (
      <AuthLayout title="Two-step check" sub="You signed in with your provider. Your account also wants its authentication code.">
        <form
          className="space-y-6"
          aria-labelledby="auth-title"
          onSubmit={(e: SubmitEvent) => {
            e.preventDefault();
            verify.mutate();
          }}
        >
          <ErrorBox error={verify.error} />
          <Field label="Authentication code" hint="From your authenticator app, or a recovery code.">
            {(id) => (
              <input
                id={id}
                className="input font-mono tracking-widest"
                autoComplete="one-time-code"
                required
                autoFocus
                maxLength={64}
                value={code}
                onChange={(e) => setCode(e.target.value)}
              />
            )}
          </Field>
          <button type="submit" className="btn btn-primary w-full" disabled={verify.isPending}>
            {verify.isPending && <Loader2 className="h-4 w-4 animate-spin" aria-hidden />} Verify and sign in
          </button>
          <Link to="/login" className="link block text-center text-sm">
            Start over
          </Link>
        </form>
      </AuthLayout>
    );
  }

  const submit = (e: SubmitEvent) => {
    e.preventDefault();
    login.mutate();
  };
  const totpStep = needCode;
  const shown = needsCode(login.error) ? null : login.error;
  const ssoMsg = ssoError(search.sso_error);

  return (
    <AuthLayout
      title={totpStep ? "Two-step check" : "Welcome back"}
      sub={totpStep ? "Your password is right. One more code to go." : "Sign in to manage your game servers."}
    >
      <form onSubmit={submit} className="space-y-6" aria-labelledby="auth-title">
        {ssoMsg && (
          <p role="alert" className="rounded-lg border border-amber-500/30 bg-amber-500/10 px-3 py-2.5 text-sm text-amber-200">
            {ssoMsg}
          </p>
        )}
        <ErrorBox error={shown ?? passkey.error} />
        <div className="space-y-4">
          <Field label="Email">
            {(id) => (
              <input
                id={id}
                className="input"
                type="email"
                autoComplete="username"
                required
                autoFocus={!totpStep}
                readOnly={totpStep}
                value={email}
                onChange={(e) => setEmail(e.target.value)}
              />
            )}
          </Field>
          <Field label="Password">
            {(id) => (
              <input
                id={id}
                className="input"
                type="password"
                autoComplete="current-password"
                required
                readOnly={totpStep}
                value={password}
                onChange={(e) => setPassword(e.target.value)}
              />
            )}
          </Field>
          {totpStep && (
            <Field label="Authentication code" hint="From your authenticator app, or a recovery code.">
              {(id) => (
                <input
                  id={id}
                  className="input font-mono tracking-widest"
                  inputMode="text"
                  autoComplete="one-time-code"
                  required
                  autoFocus
                  maxLength={64}
                  value={code}
                  onChange={(e) => setCode(e.target.value)}
                />
              )}
            </Field>
          )}
        </div>
        <button type="submit" className="btn btn-primary w-full" disabled={login.isPending}>
          {login.isPending && <Loader2 className="h-4 w-4 animate-spin" aria-hidden />} {totpStep ? "Verify and sign in" : "Sign in"}
        </button>
        {totpStep && (
          <button
            type="button"
            className="link w-full text-center text-sm"
            onClick={() => {
              setNeedCode(false);
              setCode("");
              login.reset();
            }}
          >
            Use a different account
          </button>
        )}
        {!totpStep && (providers.length > 0 || offerPasskey) && (
          <>
            <div className="flex items-center gap-3 text-xs text-zinc-500" aria-hidden>
              <span className="h-px flex-1 bg-zinc-800" />
              or
              <span className="h-px flex-1 bg-zinc-800" />
            </div>
            {offerPasskey && (
              <button type="button" className="btn w-full" disabled={passkey.isPending} onClick={() => passkey.mutate()}>
                {passkey.isPending ? (
                  <Loader2 className="h-4 w-4 animate-spin" aria-hidden />
                ) : (
                  <Fingerprint className="h-4 w-4" aria-hidden />
                )}{" "}
                Sign in with a passkey
              </button>
            )}
            <SSOButtons providers={providers} />
          </>
        )}
        {!totpStep && opts && (opts.password_reset || opts.signup === "open") && (
          <div className="flex flex-wrap justify-between gap-2 text-sm">
            {opts.password_reset && (
              <Link to="/forgot" className="link">
                Forgot your password?
              </Link>
            )}
            {opts.signup === "open" && (
              <Link to="/register" className="link">
                Create an account
              </Link>
            )}
          </div>
        )}
      </form>
    </AuthLayout>
  );
}
