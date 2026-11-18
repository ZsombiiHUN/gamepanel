import { useEffect, useState, type FormEvent } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { Loader2 } from "lucide-react";
import QRCode from "qrcode";
import { api, call } from "../api/client";
import { invalidates, keys, meQuery } from "../api/queries";
import { ErrorBox, Field, Pill, Section } from "../components/ui";

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
    <div className="space-y-3 rounded-xl border border-amber-500/40 bg-amber-950/20 p-4" role="status">
      <p className="text-sm text-amber-200">
        Save these recovery codes somewhere safe. Each works once if you lose your authenticator. They are not shown again.
      </p>
      <ul className="grid grid-cols-2 gap-x-6 gap-y-1 rounded-lg bg-well p-3 font-mono text-sm text-zinc-200 sm:grid-cols-4">
        {codes.map((c) => (
          <li key={c}>{c}</li>
        ))}
      </ul>
    </div>
  );
}

export function TotpSection() {
  const me = useQuery(meQuery);
  const enabled = me.data?.totp_enabled ?? false;
  const [password, setPassword] = useState("");
  const [code, setCode] = useState("");
  const [codes, setCodes] = useState<string[] | null>(null);

  const setup = useMutation({
    mutationFn: () => call(api.POST("/auth/totp/setup", { body: { password } })),
    onSuccess: () => setPassword(""),
  });
  const enable = useMutation({
    mutationFn: () => call(api.POST("/auth/totp/enable", { body: { code } })),
    meta: invalidates(keys.me),
    onSuccess: (r) => {
      setCodes(r.codes ?? []);
      setCode("");
      setup.reset();
    },
  });
  const disable = useMutation({
    mutationFn: () => call(api.POST("/auth/totp/disable", { body: { password, code } })),
    meta: invalidates(keys.me, keys.sessions),
    onSuccess: () => {
      setPassword("");
      setCode("");
      setCodes(null);
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
    <Section
      title="Two-factor authentication"
      desc="Require a code from an authenticator app (or a recovery code) in addition to your password."
      id="totp-h"
    >
      <div className="space-y-4">
        <div>{enabled ? <Pill tone="good">On</Pill> : <Pill>Off</Pill>}</div>

        {!enabled && !setup.data && (
          <form onSubmit={stop(() => setup.mutate())} className="max-w-md space-y-3">
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
          <form onSubmit={stop(() => enable.mutate())} className="space-y-4">
            <p className="text-sm text-zinc-400">Scan the QR code with your authenticator app, or enter the key by hand, then type the 6-digit code it shows.</p>
            <div className="flex flex-wrap items-start gap-5">
              <Qr uri={setup.data.uri} />
              <div className="min-w-0 space-y-3">
                <div>
                  <div className="text-[11px] font-semibold uppercase tracking-wider text-zinc-500">Setup key</div>
                  <p className="mt-1 break-all font-mono text-sm text-zinc-200" aria-label="Setup key">{setup.data.secret}</p>
                </div>
                <Field label="6-digit code">
                  {(id) => <input id={id} className="input w-40 font-mono tracking-widest" inputMode="numeric" autoComplete="one-time-code" pattern="[0-9]{6}" maxLength={6} required value={code} onChange={(e) => setCode(e.target.value)} />}
                </Field>
                <ErrorBox error={enable.error} />
                <button type="submit" className="btn btn-primary" disabled={enable.isPending}>
                  {enable.isPending && <Loader2 className="h-4 w-4 animate-spin" aria-hidden />} Turn on
                </button>
              </div>
            </div>
          </form>
        )}

        {codes && <RecoveryCodes codes={codes} />}

        {enabled && (
          <form onSubmit={stop(() => disable.mutate())} className="space-y-4">
            <p className="text-sm text-zinc-400">To turn it off or get new recovery codes, enter your password and a current authenticator or recovery code.</p>
            <div className="grid max-w-xl gap-4 sm:grid-cols-2">
              <Field label="Your password">
                {(id) => <input id={id} className="input" type="password" autoComplete="current-password" required value={password} onChange={(e) => setPassword(e.target.value)} />}
              </Field>
              <Field label="Authenticator or recovery code">
                {(id) => <input id={id} className="input font-mono" autoComplete="one-time-code" required maxLength={20} value={code} onChange={(e) => setCode(e.target.value)} />}
              </Field>
            </div>
            <ErrorBox error={disable.error ?? regen.error} />
            <div className="flex gap-2">
              <button type="button" className="btn" disabled={regen.isPending} onClick={() => regen.mutate()}>
                {regen.isPending && <Loader2 className="h-4 w-4 animate-spin" aria-hidden />} New recovery codes
              </button>
              <button type="submit" className="btn btn-danger" disabled={disable.isPending}>
                {disable.isPending && <Loader2 className="h-4 w-4 animate-spin" aria-hidden />} Turn off
              </button>
            </div>
          </form>
        )}
      </div>
    </Section>
  );
}
