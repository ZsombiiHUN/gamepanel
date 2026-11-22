import { useState, type SubmitEvent, type ReactNode } from "react";
import { Link } from "@tanstack/react-router";
import { useMutation, useQuery } from "@tanstack/react-query";
import { Check, Copy, ExternalLink, KeyRound, Loader2, Plug } from "lucide-react";
import { api, call } from "../../api/client";
import { invalidates, keys, meQuery, sftpQuery } from "../../api/queries";
import { Modal } from "../../components/Modal";
import { ErrorBox, Pill, Spinner } from "../../components/ui";
import { PasswordField, MfaCodeField } from "../../components/AuthFields";
import { sftpHost, sftpLink } from "../../lib/sftp";
import { copyText } from "../../lib/clipboard";
import { Trans, useTranslation } from "react-i18next";

/**
 * Everything needed to open a server in an SFTP client: one button that hands
 * an sftp:// link to the installed client, the details to type in by hand,
 * and a way to make the API key that serves as the password.
 */
export function SftpConnect({ uuid, serverName }: { uuid: string; serverName: string }) {
  const { t } = useTranslation();
  const info = useQuery(sftpQuery(uuid));
  const me = useQuery(meQuery);
  // The key exists only in this page's memory: shown once, never stored.
  const [key, setKey] = useState<string | null>(null);
  const [making, setMaking] = useState(false);

  if (info.isPending) return <Spinner label={t("sftpConnect.loadingSftpDetails")} />;
  if (info.error) return <ErrorBox error={info.error} />;
  const i = info.data;
  const { host, guessed } = sftpHost(i.host);
  const ready = i.status === "ready" && i.port > 0;
  const link = ready ? sftpLink(host, i.port, i.username, key ?? undefined) : "";
  const isAdmin = !!me.data?.is_admin;

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-2">
        <Status status={i.status} />
        <Pill>{i.write && i.read ? t("sftpConnect.readAndWrite") : i.write ? t("sftpConnect.writeOnly") : t("sftpConnect.readOnly")}</Pill>
      </div>

      <div className="rounded-xl border border-line bg-well p-4">
        <div className="flex flex-wrap items-center gap-2">
          {ready ? (
            <a className="btn btn-primary !min-h-11 !px-5 font-semibold" href={link}>
              <Plug className="h-[17px] w-[17px]" aria-hidden /> {key ? t("sftpConnect.openAndSignIn") : t("sftpConnect.openInSftpClient")}
            </a>
          ) : (
            <button type="button" className="btn btn-primary !min-h-11 !px-5 font-semibold" disabled>
              <Plug className="h-[17px] w-[17px]" aria-hidden /> {t("sftpConnect.openInSftpClient")}
            </button>
          )}
          {ready && <CopyIcon text={sftpLink(host, i.port, i.username)} label={t("sftpConnect.copyLink")} wide />}
        </div>
        <p className="mt-2.5 text-[13px] text-zinc-500">
          {ready ? (
            key ? (
              t("sftpConnect.keyInLink")
            ) : (
              <Trans
                i18nKey="sftpConnect.opensClient"
                components={{
                  winscp: <ExtLink href="https://winscp.net/eng/download.php" />,
                  cyberduck: <ExtLink href="https://cyberduck.io/download/" />,
                }}
              />
            )
          ) : i.status === "off" ? (
            isAdmin ? (
              t("sftpConnect.offAdmin")
            ) : (
              t("sftpConnect.off")
            )
          ) : (
            t("sftpConnect.nodeOffline")
          )}
        </p>
      </div>

      <dl className="divide-y divide-line text-sm">
        <Row
          label={t("sftpConnect.host")}
          value={host}
          copy={host}
          note={
            guessed ? (
              isAdmin ? (
                <Trans
                  i18nKey="sftpConnect.sameHostAdmin"
                  components={{ nodes: <Link to="/nodes" className="underline underline-offset-2 hover:text-zinc-300" /> }}
                />
              ) : (
                t("sftpConnect.sameHost")
              )
            ) : undefined
          }
        />
        <Row
          label={t("common.port")}
          value={i.port ? String(i.port) : t("sftpConnect.portUnknown")}
          copy={i.port ? String(i.port) : undefined}
        />
        <Row label={t("common.username")} value={i.username} copy={i.username} />
        <Row
          label={t("common.password")}
          plain
          value={key ? <code className="font-mono text-[13px] text-good-text">{key.slice(0, 10)}…</code> : t("sftpConnect.anApiKey")}
          note={key ? undefined : t("sftpConnect.notYourAccountPassword")}
          copy={key ?? undefined}
          action={
            !key && !making ? (
              <button type="button" className="btn !py-1 text-xs" onClick={() => setMaking(true)}>
                <KeyRound className="h-3.5 w-3.5" aria-hidden /> {t("sftpConnect.createOne")}
              </button>
            ) : undefined
          }
        />
      </dl>

      {key && (
        <div role="status" className="space-y-2 rounded-xl border border-warn/30 bg-warn-bg p-4">
          <p className="text-sm text-warn-text">
            <Trans
              i18nKey="sftpConnect.copyKeyNow"
              components={{ account: <Link to="/account" className="underline underline-offset-2" /> }}
            />
          </p>
          <code className="block break-all rounded-lg bg-well p-3 font-mono text-[13px] text-good-text select-all">{key}</code>
          <CopyIcon text={key} label={t("sftpConnect.copyKey")} wide />
        </div>
      )}
      {making && !key && (
        <MakeKey
          name={`SFTP: ${serverName}`.slice(0, 64)}
          mfa={!!me.data?.totp_enabled}
          onMade={(k) => {
            setKey(k);
            setMaking(false);
          }}
          onCancel={() => setMaking(false)}
        />
      )}
    </div>
  );
}

const ext = "whitespace-nowrap underline decoration-zinc-600 underline-offset-2 hover:text-zinc-300";

/** An outside link with its icon; Trans fills in the text. */
function ExtLink({ href, children }: { href: string; children?: ReactNode }) {
  return (
    <a className={ext} href={href} target="_blank" rel="noreferrer">
      {children}
      <ExternalLink className="ml-0.5 inline h-3 w-3 align-[-1px]" aria-hidden />
    </a>
  );
}

/** The SFTP details in a modal, for the Files tab. */
export function SftpDialog({ open, uuid, serverName, onClose }: { open: boolean; uuid: string; serverName: string; onClose: () => void }) {
  const { t } = useTranslation();
  return (
    <Modal open={open} title={t("sftpConnect.connectWithSftp")} onClose={onClose}>
      <SftpConnect uuid={uuid} serverName={serverName} />
    </Modal>
  );
}

function Status({ status }: { status: string }) {
  const { t } = useTranslation();
  const [dot, text] =
    status === "ready"
      ? ["bg-good", t("sftpConnect.ready")]
      : status === "off"
        ? ["bg-warn", t("sftpConnect.offOnNode")]
        : ["bg-bad-text", t("sftpConnect.nodeOfflineShort")];
  return (
    <span className="inline-flex items-center gap-1.5 rounded-full bg-zinc-800 px-2.5 py-0.5 text-xs font-medium text-zinc-200">
      <span className={`h-2 w-2 rounded-full ${dot}`} aria-hidden /> {text}
    </span>
  );
}

function Row({
  label,
  value,
  copy,
  note,
  action,
  plain = false,
}: {
  label: string;
  value: ReactNode;
  copy?: string;
  note?: ReactNode;
  action?: ReactNode;
  plain?: boolean;
}) {
  const { t } = useTranslation();
  return (
    <div className="flex flex-wrap items-center justify-between gap-x-6 gap-y-1 py-2.5 first:pt-0 last:pb-0">
      <dt className="text-zinc-500">{label}</dt>
      <dd className="flex min-w-0 flex-col items-end gap-0.5 text-right">
        <span className="flex min-w-0 items-center gap-1.5">
          <span className={`min-w-0 text-zinc-200 ${plain ? "break-words" : "break-all font-mono text-[13px]"}`}>{value}</span>
          {copy && <CopyIcon text={copy} label={t("sftpConnect.copyField", { field: label.toLowerCase() })} />}
          {action}
        </span>
        {note && <span className="text-xs text-zinc-500">{note}</span>}
      </dd>
    </div>
  );
}

function CopyIcon({ text, label, wide = false }: { text: string; label: string; wide?: boolean }) {
  const { t } = useTranslation();
  const [done, setDone] = useState(false);
  const [err, setErr] = useState(false);
  const copy = async () => {
    try {
      await copyText(text);
      setDone(true);
      setErr(false);
      setTimeout(() => setDone(false), 1500);
    } catch {
      setErr(true);
    }
  };
  const icon = done ? <Check className="h-3.5 w-3.5 text-good-text" aria-hidden /> : <Copy className="h-3.5 w-3.5" aria-hidden />;
  if (wide) {
    return (
      <button type="button" className="btn" onClick={() => void copy()}>
        {icon} {done ? t("common.copied") : err ? t("sftpConnect.copyFailedSelectIt") : label}
      </button>
    );
  }
  return (
    <button
      type="button"
      className="inline-flex h-7 w-7 shrink-0 items-center justify-center rounded-md text-zinc-400 hover:bg-row-hover hover:text-zinc-100 focus-visible:outline-2 focus-visible:outline-indigo-500"
      aria-label={done ? t("common.copied") : label}
      title={err ? t("sftpConnect.copyFailedSelectIt") : label}
      onClick={() => void copy()}
    >
      {icon}
    </button>
  );
}

/** Makes an API key for SFTP. A key outlives the session, so this asks for the password (and a code with two-factor on). */
function MakeKey({ name, mfa, onMade, onCancel }: { name: string; mfa: boolean; onMade: (key: string) => void; onCancel: () => void }) {
  const { t } = useTranslation();
  const [password, setPassword] = useState("");
  const [code, setCode] = useState("");
  const create = useMutation({
    mutationFn: () => call(api.POST("/auth/keys", { body: { name, expires_in_days: 365, password, ...(mfa ? { code } : {}) } })),
    meta: invalidates(keys.apiKeys),
    onSuccess: (r) => onMade(r.token),
  });
  const submit = (e: SubmitEvent) => {
    e.preventDefault();
    create.mutate();
  };
  return (
    <form onSubmit={submit} className="space-y-3 rounded-xl border border-line p-4">
      <p className="text-sm text-zinc-300">
        <Trans i18nKey="sftpConnect.createsKey" values={{ name }} components={{ name: <span className="font-medium text-zinc-100" /> }} />
      </p>
      <div className="flex flex-wrap items-end gap-3">
        <PasswordField label={t("sftpConnect.yourAccountPassword")} value={password} onChange={setPassword} autoFocus />
        {mfa && <MfaCodeField value={code} onChange={setCode} />}
      </div>
      <ErrorBox error={create.error} />
      <div className="flex gap-2">
        <button type="submit" className="btn btn-primary" disabled={create.isPending || !password}>
          {create.isPending ? <Loader2 className="h-4 w-4 animate-spin" aria-hidden /> : <KeyRound className="h-4 w-4" aria-hidden />}{" "}
          {t("sftpConnect.createKey")}
        </button>
        <button type="button" className="btn btn-ghost" onClick={onCancel}>
          {t("common.cancel")}
        </button>
      </div>
    </form>
  );
}
