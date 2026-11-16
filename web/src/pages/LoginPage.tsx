import { useState, type FormEvent } from "react";
import { Link, useNavigate } from "@tanstack/react-router";
import { useMutation, useQuery } from "@tanstack/react-query";
import { Loader2 } from "lucide-react";
import { api, ApiError, call } from "../api/client";
import { authOptionsQuery, invalidates, keys } from "../api/queries";
import { AuthLayout } from "../components/AuthLayout";
import { ErrorBox, Field } from "../components/ui";

/** The password was right and the account wants its second factor: a step, not an error. */
const needsCode = (err: unknown) => err instanceof ApiError && err.detail === "totp_required";

export function LoginPage() {
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

  const submit = (e: FormEvent) => {
    e.preventDefault();
    login.mutate();
  };

  const totpStep = needCode;
  const shown = needsCode(login.error) ? null : login.error;
  const opts = options.data;

  return (
    <AuthLayout title={totpStep ? "Two-step check" : "Welcome back"} sub={totpStep ? "Your password is right. One more code to go." : "Sign in to manage your game servers."}>
      <form onSubmit={submit} className="space-y-6" aria-labelledby="auth-title">
        <ErrorBox error={shown} />
        <div className="space-y-4">
          <Field label="Email">
            {(id) => <input id={id} className="input" type="email" autoComplete="username" required autoFocus={!totpStep} readOnly={totpStep} value={email} onChange={(e) => setEmail(e.target.value)} />}
          </Field>
          <Field label="Password">
            {(id) => <input id={id} className="input" type="password" autoComplete="current-password" required readOnly={totpStep} value={password} onChange={(e) => setPassword(e.target.value)} />}
          </Field>
          {totpStep && (
            <Field label="Authentication code" hint="From your authenticator app, or a recovery code.">
              {(id) => <input id={id} className="input font-mono tracking-widest" inputMode="text" autoComplete="one-time-code" required autoFocus maxLength={20} value={code} onChange={(e) => setCode(e.target.value)} />}
            </Field>
          )}
        </div>
        <button type="submit" className="btn btn-primary w-full" disabled={login.isPending}>
          {login.isPending && <Loader2 className="h-4 w-4 animate-spin" aria-hidden />} {totpStep ? "Verify and sign in" : "Sign in"}
        </button>
        {totpStep && (
          <button type="button" className="link w-full text-center text-sm" onClick={() => { setNeedCode(false); setCode(""); login.reset(); }}>
            Use a different account
          </button>
        )}
        {!totpStep && opts && (opts.password_reset || opts.signup === "open") && (
          <div className="flex flex-wrap justify-between gap-2 text-sm">
            {opts.password_reset && <Link to="/forgot" className="link">Forgot your password?</Link>}
            {opts.signup === "open" && <Link to="/register" className="link">Create an account</Link>}
          </div>
        )}
      </form>
    </AuthLayout>
  );
}
