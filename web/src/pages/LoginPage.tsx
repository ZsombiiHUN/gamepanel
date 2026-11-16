import { useState, type FormEvent } from "react";
import { useNavigate } from "@tanstack/react-router";
import { useMutation } from "@tanstack/react-query";
import { Gamepad2, Loader2 } from "lucide-react";
import { api, ApiError, call } from "../api/client";
import { invalidates, keys } from "../api/queries";
import { ErrorBox, Field, ServerIcon } from "../components/ui";

/** The password was right and the account wants its second factor: a step, not an error. */
const needsCode = (err: unknown) => err instanceof ApiError && err.detail === "totp_required";

export function LoginPage() {
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [code, setCode] = useState("");
  const [needCode, setNeedCode] = useState(false);
  const navigate = useNavigate();
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

  return (
    <div className="grid min-h-screen lg:grid-cols-[minmax(0,1fr)_minmax(0,1.1fr)]">
      <main className="flex items-center justify-center px-6 py-10">
        <form onSubmit={submit} className="w-full max-w-sm space-y-6" aria-labelledby="login-title">
          <div className="flex items-center gap-2.5 text-zinc-100">
            <Gamepad2 className="h-5 w-5 text-indigo-300" aria-hidden />
            <span className="h-display text-lg">GamePanel</span>
          </div>
          <div>
            <h1 id="login-title" className="h-display text-3xl">{totpStep ? "Two-step check" : "Welcome back"}</h1>
            <p className="mt-1.5 text-sm text-zinc-400">
              {totpStep ? "Your password is right. One more code to go." : "Sign in to manage your game servers."}
            </p>
          </div>
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
        </form>
      </main>
      <aside aria-hidden className="relative hidden overflow-hidden border-l border-zinc-800 bg-side lg:block">
        <div className="absolute inset-0 grid place-content-center gap-5 p-12">
          <div className="grid grid-cols-4 gap-5">
            {ICONS.map((n) => (
              <div key={n} className="opacity-90"><ServerIcon name={n} size={84} /></div>
            ))}
          </div>
        </div>
        <div className="absolute inset-x-0 bottom-0 bg-gradient-to-t from-[#0b0d0f] via-[#0b0d0f]/90 to-transparent p-12 pt-24">
          <p className="h-display max-w-md text-2xl leading-snug text-zinc-100">Game servers, without the panel you have to babysit.</p>
        </div>
      </aside>
    </div>
  );
}

const ICONS = ["survival-smp", "creative-build", "valheim-co-op", "rust-weekly", "ark-ascended", "terraria-hub", "cs2-comp", "factorio-main", "vanilla-1.21", "skyblock", "mini-games", "modded-pack"];
