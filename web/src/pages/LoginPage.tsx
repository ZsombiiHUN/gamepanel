import { useState, type FormEvent } from "react";
import { Navigate, useNavigate } from "@tanstack/react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Gamepad2, Loader2 } from "lucide-react";
import { api, call } from "../api/client";
import { meQuery } from "../api/queries";
import { ErrorBox, Field } from "../components/ui";

export function LoginPage() {
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const qc = useQueryClient();
  const navigate = useNavigate();
  const me = useQuery(meQuery);
  const login = useMutation({
    mutationFn: () => call(api.POST("/auth/login", { body: { email, password } })),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: ["me"] });
      void navigate({ to: "/" });
    },
  });

  if (me.data) return <Navigate to="/" />;

  const submit = (e: FormEvent) => {
    e.preventDefault();
    login.mutate();
  };

  return (
    <div className="flex min-h-screen items-center justify-center p-4">
      <form onSubmit={submit} className="card w-full max-w-sm space-y-4" aria-labelledby="login-title">
        <div className="flex items-center gap-2">
          <Gamepad2 className="h-6 w-6 text-indigo-400" aria-hidden />
          <h1 id="login-title" className="text-xl font-semibold">Sign in to GamePanel</h1>
        </div>
        <ErrorBox error={login.error} />
        <Field label="Email">
          {(id) => <input id={id} className="input" type="email" autoComplete="username" required autoFocus value={email} onChange={(e) => setEmail(e.target.value)} />}
        </Field>
        <Field label="Password">
          {(id) => <input id={id} className="input" type="password" autoComplete="current-password" required value={password} onChange={(e) => setPassword(e.target.value)} />}
        </Field>
        <button type="submit" className="btn btn-primary w-full" disabled={login.isPending}>
          {login.isPending && <Loader2 className="h-4 w-4 animate-spin" aria-hidden />} Sign in
        </button>
      </form>
    </div>
  );
}
