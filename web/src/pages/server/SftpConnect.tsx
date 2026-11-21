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

/**
 * Everything needed to open a server in an SFTP client: one button that hands
 * an sftp:// link to the installed client, the details to type in by hand,
 * and a way to make the API key that serves as the password.
 */
export function SftpConnect({ uuid, serverName }: { uuid: string; serverName: string }) {
  const info = useQuery(sftpQuery(uuid));
  const me = useQuery(meQuery);
  // The key exists only in this page's memory: shown once, never stored.
  const [key, setKey] = useState<string | null>(null);
  const [making, setMaking] = useState(false);

  if (info.isPending) return <Spinner label="Loading SFTP details" />;
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
        <Pill>{i.write && i.read ? "Read and write" : i.write ? "Write only" : "Read only"}</Pill>
      </div>

      <div className="rounded-xl border border-line bg-well p-4">
        <div className="flex flex-wrap items-center gap-2">
          {ready ? (
            <a className="btn btn-primary !min-h-11 !px-5 font-semibold" href={link}>
              <Plug className="h-[17px] w-[17px]" aria-hidden /> {key ? "Open and sign in" : "Open in SFTP client"}
            </a>
          ) : (
            <button type="button" className="btn btn-primary !min-h-11 !px-5 font-semibold" disabled>
              <Plug className="h-[17px] w-[17px]" aria-hidden /> Open in SFTP client
            </button>
          )}
          {ready && <CopyIcon text={sftpLink(host, i.port, i.username)} label="Copy link" wide />}
        </div>
        <p className="mt-2.5 text-[13px] text-zinc-500">
          {ready ? (
            key ? (
              "Your new key goes along in the link, so the client signs straight in. It may offer to save it."
            ) : (
              <>
                Opens WinSCP, FileZilla, Cyberduck or whichever SFTP client you have installed. When it asks for a password, use an API key.
                Need a client?{" "}
                <a className={ext} href="https://winscp.net/eng/download.php" target="_blank" rel="noreferrer">
                  WinSCP
                  <ExternalLink className="ml-0.5 inline h-3 w-3 align-[-1px]" aria-hidden />
                </a>{" "}
                for Windows,{" "}
                <a className={ext} href="https://cyberduck.io/download/" target="_blank" rel="noreferrer">
                  Cyberduck
                  <ExternalLink className="ml-0.5 inline h-3 w-3 align-[-1px]" aria-hidden />
                </a>{" "}
                for Mac.
              </>
            )
          ) : i.status === "off" ? (
            isAdmin ? (
              "The daemon on this server's node runs with SFTP turned off (GAMEPANEL_SFTP_ADDR=off), or its port was taken when it started."
            ) : (
              "SFTP is turned off on the machine this server runs on. Ask an administrator to turn it on."
            )
          ) : (
            "The node this server runs on is offline. SFTP works again once it reconnects."
          )}
        </p>
      </div>

      <dl className="divide-y divide-line text-sm">
        <Row
          label="Host"
          value={host}
          copy={host}
          note={
            guessed ? (
              isAdmin ? (
                <>
                  Same as this panel.{" "}
                  <Link to="/nodes" className="underline underline-offset-2 hover:text-zinc-300">
                    Set the node's address
                  </Link>{" "}
                  if it differs.
                </>
              ) : (
                "Same as this panel."
              )
            ) : undefined
          }
        />
        <Row label="Port" value={i.port ? String(i.port) : "unknown while offline"} copy={i.port ? String(i.port) : undefined} />
        <Row label="Username" value={i.username} copy={i.username} />
        <Row
          label="Password"
          plain
          value={key ? <code className="font-mono text-[13px] text-good-text">{key.slice(0, 10)}…</code> : "An API key"}
          note={key ? undefined : "Not your account password."}
          copy={key ?? undefined}
          action={
            !key && !making ? (
              <button type="button" className="btn !py-1 text-xs" onClick={() => setMaking(true)}>
                <KeyRound className="h-3.5 w-3.5" aria-hidden /> Create one
              </button>
            ) : undefined
          }
        />
      </dl>

      {key && (
        <div role="status" className="space-y-2 rounded-xl border border-warn/30 bg-warn-bg p-4">
          <p className="text-sm text-warn-text">
            Copy this key now. It is not shown again. You can revoke it on your{" "}
            <Link to="/account" className="underline underline-offset-2">
              Account page
            </Link>
            .
          </p>
          <code className="block break-all rounded-lg bg-well p-3 font-mono text-[13px] text-good-text select-all">{key}</code>
          <CopyIcon text={key} label="Copy key" wide />
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

/** The SFTP details in a modal, for the Files tab. */
export function SftpDialog({ open, uuid, serverName, onClose }: { open: boolean; uuid: string; serverName: string; onClose: () => void }) {
  return (
    <Modal open={open} title="Connect with SFTP" onClose={onClose}>
      <SftpConnect uuid={uuid} serverName={serverName} />
    </Modal>
  );
}

function Status({ status }: { status: string }) {
  const [dot, text] =
    status === "ready"
      ? ["bg-good", "Ready"]
      : status === "off"
        ? ["bg-warn", "SFTP is off on this node"]
        : ["bg-bad-text", "Node offline"];
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
  return (
    <div className="flex flex-wrap items-center justify-between gap-x-6 gap-y-1 py-2.5 first:pt-0 last:pb-0">
      <dt className="text-zinc-500">{label}</dt>
      <dd className="flex min-w-0 flex-col items-end gap-0.5 text-right">
        <span className="flex min-w-0 items-center gap-1.5">
          <span className={`min-w-0 text-zinc-200 ${plain ? "break-words" : "break-all font-mono text-[13px]"}`}>{value}</span>
          {copy && <CopyIcon text={copy} label={`Copy ${label.toLowerCase()}`} />}
          {action}
        </span>
        {note && <span className="text-xs text-zinc-500">{note}</span>}
      </dd>
    </div>
  );
}

function CopyIcon({ text, label, wide = false }: { text: string; label: string; wide?: boolean }) {
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
        {icon} {done ? "Copied" : err ? "Copy failed - select it" : label}
      </button>
    );
  }
  return (
    <button
      type="button"
      className="inline-flex h-7 w-7 shrink-0 items-center justify-center rounded-md text-zinc-400 hover:bg-row-hover hover:text-zinc-100 focus-visible:outline-2 focus-visible:outline-indigo-500"
      aria-label={done ? "Copied" : label}
      title={err ? "Copy failed - select it" : label}
      onClick={() => void copy()}
    >
      {icon}
    </button>
  );
}

/** Makes an API key for SFTP. A key outlives the session, so this asks for the password (and a code with two-factor on). */
function MakeKey({ name, mfa, onMade, onCancel }: { name: string; mfa: boolean; onMade: (key: string) => void; onCancel: () => void }) {
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
        Creates an API key named <span className="font-medium text-zinc-100">&ldquo;{name}&rdquo;</span> that lasts a year. It can do what
        you can on the panel, so treat it like a password.
      </p>
      <div className="flex flex-wrap items-end gap-3">
        <PasswordField label="Your account password" value={password} onChange={setPassword} autoFocus />
        {mfa && <MfaCodeField value={code} onChange={setCode} />}
      </div>
      <ErrorBox error={create.error} />
      <div className="flex gap-2">
        <button type="submit" className="btn btn-primary" disabled={create.isPending || !password}>
          {create.isPending ? <Loader2 className="h-4 w-4 animate-spin" aria-hidden /> : <KeyRound className="h-4 w-4" aria-hidden />}{" "}
          Create key
        </button>
        <button type="button" className="btn btn-ghost" onClick={onCancel}>
          Cancel
        </button>
      </div>
    </form>
  );
}
