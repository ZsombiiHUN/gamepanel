import { useEffect, useState, type SubmitEvent } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { Download, Loader2 } from "lucide-react";
import QRCode from "qrcode";
import { api, call } from "../api/client";
import { invalidates, keys, meQuery, recoveryStatusQuery } from "../api/queries";
import { CopyButton, ErrorBox, Field, Pill, Section } from "../components/ui";
import { PasswordField, MfaCodeField } from "../components/AuthFields";
import { useTranslation } from "react-i18next";

function Qr({ uri }: { uri: string }) {
  const { t } = useTranslation();
  const [src, setSrc] = useState("");
  useEffect(() => {
    let alive = true;
    QRCode.toDataURL(uri, { margin: 1, width: 192 }).then(
      (s) => {
        if (alive) setSrc(s);
      },
      // No QR code: the setup key below still works.
      () => undefined,
    );
    return () => {
      alive = false;
    };
  }, [uri]);
  // White background on purpose: scanners need contrast regardless of theme.
  return src ? (
    <img src={src} width={192} height={192} alt={t("totpSection.qrCodeForYourAuthenticator")} className="rounded bg-white" />
  ) : null;
}

function RecoveryCodes({ codes }: { codes: string[] }) {
  const { t } = useTranslation();
  const text = codes.join("\n") + "\n";
  const download = () => {
    const url = URL.createObjectURL(
      new Blob([`${t("totpSection.fileHeader", { host: location.host })}\n\n${text}`], { type: "text/plain" }),
    );
    const a = document.createElement("a");
    a.href = url;
    a.download = `gamepanel-recovery-codes-${location.host}.txt`;
    a.click();
    URL.revokeObjectURL(url);
  };
  return (
    <div className="space-y-3 rounded-xl border border-warn/30 bg-warn-bg p-4" role="status">
      <p className="text-sm text-warn-text">{t("totpSection.saveTheseRecoveryCodesSomewhere")}</p>
      <ul
        className="grid gap-x-6 gap-y-1 rounded-lg bg-well p-3 font-mono text-sm text-zinc-200 sm:grid-cols-2"
        aria-label={t("totpSection.recoveryCodes")}
      >
        {codes.map((c) => (
          <li key={c}>{c}</li>
        ))}
      </ul>
      <div className="flex flex-wrap gap-2">
        <CopyButton text={text} label={t("totpSection.copyCodes")} />
        <button type="button" className="btn" onClick={download}>
          <Download className="h-4 w-4" aria-hidden /> {t("common.download")}
        </button>
      </div>
    </div>
  );
}

/** Remaining recovery codes, with a nudge when they are running out or predate 120-bit codes. */
function RecoveryStatus() {
  const { t } = useTranslation();
  const st = useQuery(recoveryStatusQuery).data;
  if (!st) return null;
  if (st.outdated) {
    return (
      <p className="text-sm text-warn-text" role="note">
        {t("totpSection.yourRecoveryCodesAreFrom")}
      </p>
    );
  }
  if (st.remaining <= 3) {
    return (
      <p className="text-sm text-warn-text" role="note">
        {st.remaining === 0 ? t("totpSection.youHaveNoRecoveryCodes") : t("totpSection.onlyLeft", { count: st.remaining })}{" "}
        {t("totpSection.makeNewOnes")}
      </p>
    );
  }
  return <p className="text-sm text-zinc-500">{t("totpSection.unused", { count: st.remaining })}</p>;
}

export function TotpSection() {
  const { t } = useTranslation();
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
    meta: invalidates(keys.me, keys.recoveryStatus),
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
    meta: invalidates(keys.recoveryStatus),
    onSuccess: (r) => {
      setCodes(r.codes ?? []);
      setPassword("");
      setCode("");
    },
  });

  const stop = (fn: () => void) => (e: SubmitEvent) => {
    e.preventDefault();
    fn();
  };

  return (
    <Section title={t("totpSection.twoFactorAuthentication")} desc={t("totpSection.requireACodeFromAn")} id="totp-h">
      <div className="space-y-4">
        <div>{enabled ? <Pill tone="good">{t("totpSection.on")}</Pill> : <Pill>{t("totpSection.off")}</Pill>}</div>

        {!enabled && !setup.data && (
          <form onSubmit={stop(() => setup.mutate())} className="max-w-md space-y-3">
            <PasswordField label={t("totpSection.yourPassword")} value={password} onChange={setPassword} />
            <ErrorBox error={setup.error} />
            <button type="submit" className="btn btn-primary" disabled={setup.isPending}>
              {setup.isPending && <Loader2 className="h-4 w-4 animate-spin" aria-hidden />} {t("totpSection.setUp")}
            </button>
          </form>
        )}

        {!enabled && setup.data && (
          <form onSubmit={stop(() => enable.mutate())} className="space-y-4">
            <p className="text-sm text-zinc-400">{t("totpSection.scanTheQrCodeWith")}</p>
            <div className="flex flex-wrap items-start gap-5">
              <Qr uri={setup.data.uri} />
              <div className="min-w-0 space-y-3">
                <div>
                  <div className="text-[11px] font-semibold uppercase tracking-wider text-zinc-500">{t("totpSection.setupKey")}</div>
                  <p className="mt-1 break-all font-mono text-sm text-zinc-200" aria-label={t("totpSection.setupKey")}>
                    {setup.data.secret}
                  </p>
                </div>
                <Field label={t("totpSection.6DigitCode")}>
                  {(id) => (
                    <input
                      id={id}
                      className="input w-40 font-mono tracking-widest"
                      inputMode="numeric"
                      autoComplete="one-time-code"
                      pattern="[0-9]{6}"
                      maxLength={6}
                      required
                      value={code}
                      onChange={(e) => setCode(e.target.value)}
                    />
                  )}
                </Field>
                <ErrorBox error={enable.error} />
                <button type="submit" className="btn btn-primary" disabled={enable.isPending}>
                  {enable.isPending && <Loader2 className="h-4 w-4 animate-spin" aria-hidden />} {t("totpSection.turnOn")}
                </button>
              </div>
            </div>
          </form>
        )}

        {codes && <RecoveryCodes codes={codes} />}

        {enabled && !codes && <RecoveryStatus />}

        {enabled && (
          <form onSubmit={stop(() => disable.mutate())} className="space-y-4">
            <p className="text-sm text-zinc-400">{t("totpSection.toTurnItOffOr")}</p>
            <div className="grid max-w-xl gap-4 sm:grid-cols-2">
              <PasswordField label={t("totpSection.yourPassword")} value={password} onChange={setPassword} />
              <MfaCodeField value={code} onChange={setCode} fullWidth />
            </div>
            <ErrorBox error={disable.error ?? regen.error} />
            <div className="flex gap-2">
              <button type="button" className="btn" disabled={regen.isPending} onClick={() => regen.mutate()}>
                {regen.isPending && <Loader2 className="h-4 w-4 animate-spin" aria-hidden />} {t("totpSection.newRecoveryCodes")}
              </button>
              <button type="submit" className="btn btn-danger" disabled={disable.isPending}>
                {disable.isPending && <Loader2 className="h-4 w-4 animate-spin" aria-hidden />} {t("totpSection.turnOff")}
              </button>
            </div>
          </form>
        )}
      </div>
    </Section>
  );
}
