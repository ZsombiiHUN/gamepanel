import { useEffect, useState, type FormEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Loader2 } from "lucide-react";
import QRCode from "qrcode";
import { api, call } from "../api/client";
import { meQuery } from "../api/queries";
import { ErrorBox, Field } from "../components/ui";

function Qr({ uri }: { uri: string }) {
  const [src, setSrc] = useState("");
  useEffect(() => {
    let alive = true;
    QRCode.toDataURL(uri, { margin: 1, width: 192 }).then((s) => alive && setSrc(s), () => {});
    return () => {
      alive = false;
    };
  }, [uri]);
  // White background on purpose: scanners need contrast regardless of theme.
  return src ? <img src={src} width={192} height={192} alt="QR code for your authenticator app" className="rounded bg-white" /> : null;
}

function RecoveryCodes({ codes }: { codes: string[] }) {
  return (
    <div className="space-y-2" role="status">
      <p className="text-sm text-amber-300">
        Save these recovery codes somewhere safe. Each works once if you lose your authenticator. They are not shown again.
      </p>
      <ul className="grid grid-cols-2 gap-1 font-mono text-sm">
        {codes.map((c) => (
          <li key={c}>{c}</li>
        ))}
      </ul>
    </div>
  );
}

export function TotpSection() {
  const qc = useQueryClient();
  const me = useQuery(meQuery);
  const enabled = me.data?.totp_enabled ?? false;
  const [password, setPassword] = useState("");
  const [code, setCode] = useState("");
  const [codes, setCodes] = useState<string[] | null>(null);
  const refresh = () => qc.invalidateQueries({ queryKey: ["me"] });

  const setup = useMutation({
    mutationFn: () => call(api.POST("/auth/totp/setup", { body: { password } })),
    onSuccess: () => setPassword(""),
  });
  const enable = useMutation({
    mutationFn: () => call(api.POST("/auth/totp/enable", { body: { code } })),
    onSuccess: async (r) => {
      setCodes(r.codes ?? []);
      setCode("");
      setup.reset();
      await refresh();
    },
  });
  const disable = useMutation({
    mutationFn: () => call(api.POST("/auth/totp/disable", { body: { password, code } })),
    onSuccess: async () => {
      setPassword("");
      setCode("");
      setCodes(null);
      await refresh();
    },
  });
  const regen = useMutation({
    mutationFn: () => call(api.POST("/auth/totp/recovery-codes", { body: { password, code } })),
    onSuccess: (r) => {
      setCodes(r.codes ?? []);
      setPassword("");
      setCode("");
    },
  });

  const stop = (fn: () => void) => (e: FormEvent) => {
    e.preventDefault();
    fn();
  };

  return (
    <section className="card space-y-4" aria-labelledby="totp-h">
      <h2 id="totp-h" className="text-lg font-medium">Two-factor authentication</h2>

      {!enabled && !setup.data && (
        <form onSubmit={stop(() => setup.mutate())} className="space-y-3">
          <p className="text-sm text-zinc-400">
            Require a code from an authenticator app (or a recovery code) in addition to your password.
          </p>
          <Field label="Your password">
            {(id) => <input id={id} className="input" type="password" autoComplete="current-password" required value={password} onChange={(e) => setPassword(e.target.value)} />}
          </Field>
          <ErrorBox error={setup.error} />
          <button type="submit" className="btn btn-primary" disabled={setup.isPending}>
            {setup.isPending && <Loader2 className="h-4 w-4 animate-spin" aria-hidden />} Set up
          </button>
        </form>
      )}

      {!enabled && setup.data && (
        <form onSubmit={stop(() => enable.mutate())} className="space-y-3">
          <p className="text-sm text-zinc-400">Scan the QR code with your authenticator app, or enter the key by hand, then type the 6-digit code it shows.</p>
          <Qr uri={setup.data.uri} />
          <p className="break-all font-mono text-sm" aria-label="Setup key">{setup.data.secret}</p>
          <Field label="6-digit code">
            {(id) => <input id={id} className="input w-40" inputMode="numeric" autoComplete="one-time-code" pattern="[0-9]{6}" maxLength={6} required value={code} onChange={(e) => setCode(e.target.value)} />}
          </Field>
          <ErrorBox error={enable.error} />
          <button type="submit" className="btn btn-primary" disabled={enable.isPending}>
            {enable.isPending && <Loader2 className="h-4 w-4 animate-spin" aria-hidden />} Turn on
          </button>
        </form>
      )}

      {codes && <RecoveryCodes codes={codes} />}

      {enabled && (
        <form onSubmit={stop(() => disable.mutate())} className="space-y-3">
          <p className="text-sm text-emerald-400" role="status">Two-factor authentication is on.</p>
          <p className="text-sm text-zinc-400">To turn it off or get new recovery codes, enter your password and a current authenticator or recovery code.</p>
          <Field label="Your password">
            {(id) => <input id={id} className="input" type="password" autoComplete="current-password" required value={password} onChange={(e) => setPassword(e.target.value)} />}
          </Field>
          <Field label="Authenticator or recovery code">
            {(id) => <input id={id} className="input w-56" autoComplete="one-time-code" required maxLength={20} value={code} onChange={(e) => setCode(e.target.value)} />}
          </Field>
          <ErrorBox error={disable.error ?? regen.error} />
          <div className="flex gap-2">
            <button type="submit" className="btn btn-danger" disabled={disable.isPending}>
              {disable.isPending && <Loader2 className="h-4 w-4 animate-spin" aria-hidden />} Turn off
            </button>
            <button type="button" className="btn" disabled={regen.isPending} onClick={() => regen.mutate()}>
              {regen.isPending && <Loader2 className="h-4 w-4 animate-spin" aria-hidden />} New recovery codes
            </button>
          </div>
        </form>
      )}
    </section>
  );
}
