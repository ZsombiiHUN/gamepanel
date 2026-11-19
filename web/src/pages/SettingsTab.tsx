import { useState, type ReactNode } from "react";
import { useNavigate } from "@tanstack/react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Archive,
  Ban,
  Box,
  Check,
  Copy,
  Cpu,
  Crown,
  Fingerprint,
  HardDrive,
  Layers,
  MemoryStick,
  Network,
  Server,
  Tag,
  Trash2,
} from "lucide-react";
import { api, call, type components } from "../api/client";
import { invalidates, keys, meQuery } from "../api/queries";
import { ErrorBox, Field, FormDialog, Section } from "../components/ui";
import { SftpConnect } from "./server/SftpConnect";
import { LimitsFields, formToLimits, limitsToForm, type LimitsForm } from "../components/LimitsFields";

type ServerView = components["schemas"]["ServerView"];

/** 1024 -> "1 GB", 1536 -> "1.5 GB", 512 -> "512 MB". */
function sizeMb(mb: number) {
  if (mb < 1024) return `${mb} MB`;
  const gb = mb / 1024;
  return `${Number.isInteger(gb) ? gb : gb.toFixed(1)} GB`;
}

function CopyIcon({ text, label }: { text: string; label: string }) {
  const [done, setDone] = useState(false);
  return (
    <button
      type="button"
      aria-label={label}
      title={label}
      className="grid h-7 w-7 shrink-0 place-content-center rounded-md text-zinc-500 hover:bg-zinc-800 hover:text-zinc-200 focus-visible:outline-2 focus-visible:outline-indigo-500"
      onClick={async () => {
        try {
          await navigator.clipboard.writeText(text);
          setDone(true);
          setTimeout(() => setDone(false), 1500);
        } catch {
          /* the value stays selectable */
        }
      }}
    >
      {done ? <Check className="h-3.5 w-3.5 text-good-text" aria-hidden /> : <Copy className="h-3.5 w-3.5" aria-hidden />}
    </button>
  );
}

/** A labelled value with an icon; tiles sit in a grid. */
function Tile({
  icon,
  label,
  children,
  mono,
  action,
}: {
  icon: ReactNode;
  label: string;
  children: ReactNode;
  mono?: boolean;
  action?: ReactNode;
}) {
  return (
    <div className="flex min-w-0 items-start gap-3 rounded-xl border border-zinc-800 bg-zinc-950/40 p-3">
      <span className="grid h-8 w-8 shrink-0 place-content-center rounded-lg bg-chip text-zinc-400">{icon}</span>
      <dl className="min-w-0 flex-1">
        <dt className="text-xs text-zinc-500">{label}</dt>
        <dd className={`mt-0.5 truncate text-sm text-zinc-100 ${mono ? "font-mono text-[13px]" : ""}`}>{children}</dd>
      </dl>
      {action}
    </div>
  );
}

const ico = (I: typeof Server) => <I className="h-4 w-4" aria-hidden />;

export function SettingsTab({ server }: { server: ServerView }) {
  const me = useQuery(meQuery);
  const isAdmin = !!me.data?.is_admin;
  const [confirm, setConfirm] = useState(false);
  const [ownerOpen, setOwnerOpen] = useState(false);
  const qc = useQueryClient();
  const navigate = useNavigate();
  const del = useMutation({
    // With the node offline the panel deletes its record now; the node cleans up when it reconnects.
    mutationFn: () =>
      call(
        api.DELETE("/servers/{uuid}", { params: { path: { uuid: server.uuid }, query: server.node_online ? undefined : { force: true } } }),
      ),
    meta: invalidates(keys.servers),
    onSuccess: () => {
      // Drop the deleted server before leaving, or the page behind us refetches a 404.
      qc.removeQueries({ queryKey: keys.server(server.uuid) });
      void navigate({ to: "/" });
    },
  });

  // The form starts from the saved limits and starts over only when they change (after saving, or edited elsewhere).
  const saved = JSON.stringify(limitsToForm(server));
  const [limits, setLimits] = useState<LimitsForm>(() => limitsToForm(server));
  const [seen, setSeen] = useState(saved);
  if (saved !== seen) {
    setSeen(saved);
    setLimits(limitsToForm(server));
  }
  const limitsDirty = JSON.stringify(limits) !== saved;
  const saveLimits = useMutation({
    mutationFn: () => call(api.PUT("/servers/{uuid}/limits", { params: { path: { uuid: server.uuid } }, body: formToLimits(limits) })),
    meta: invalidates(keys.server(server.uuid)),
  });

  const canSftp = server.permissions.includes("files.read") || server.permissions.includes("files.write");

  return (
    <div className="space-y-5">
      {server.permissions.includes("settings.rename") && <RenameSection server={server} />}

      <Section
        title="Details"
        desc="Where this server lives and who owns it."
        footer={
          isAdmin ? (
            <>
              <span>Handing the server over removes the current owner's access.</span>
              <button type="button" className="btn" onClick={() => setOwnerOpen(true)}>
                <Crown className="h-4 w-4" aria-hidden /> Change owner
              </button>
            </>
          ) : undefined
        }
      >
        <div className="grid gap-2.5 sm:grid-cols-2 lg:grid-cols-3">
          <Tile icon={ico(Box)} label="Game">
            {server.egg_name || `Egg #${server.egg_id}`}
          </Tile>
          <Tile icon={ico(Server)} label="Node">
            {server.node_name || `Node #${server.node_id}`}
          </Tile>
          <Tile icon={ico(Network)} label="Primary port" mono action={<CopyIcon text={String(server.port)} label="Copy port" />}>
            {server.port}
          </Tile>
          <Tile icon={ico(Crown)} label="Owner">
            {server.owner_email ?? `User #${server.owner_id}`}
          </Tile>
          <div className="sm:col-span-2">
            <Tile icon={ico(Fingerprint)} label="Server ID" mono action={<CopyIcon text={server.uuid} label="Copy server ID" />}>
              {server.uuid}
            </Tile>
          </div>
          {isAdmin && server.external_id && (
            <Tile icon={ico(Tag)} label="External ID (shop)" mono action={<CopyIcon text={server.external_id} label="Copy external ID" />}>
              {server.external_id}
            </Tile>
          )}
        </div>
      </Section>
      {ownerOpen && <OwnerDialog server={server} onClose={() => setOwnerOpen(false)} />}

      {isAdmin ? (
        <form
          onSubmit={(e) => {
            e.preventDefault();
            saveLimits.mutate();
          }}
        >
          <Section
            title="Resource limits"
            desc="What this server may use on its node. A running server picks up changes the next time it starts."
            footer={
              <>
                <span
                  className={saveLimits.isSuccess && !limitsDirty ? "text-good-text" : limitsDirty ? "text-warn-text" : ""}
                  role={saveLimits.isSuccess && !limitsDirty ? "status" : undefined}
                >
                  {saveLimits.isSuccess && !limitsDirty
                    ? "Limits saved."
                    : limitsDirty
                      ? "You have unsaved changes."
                      : "0 usually means unlimited."}
                </span>
                <div className="flex gap-2">
                  {limitsDirty && (
                    <button
                      type="button"
                      className="btn btn-ghost"
                      onClick={() => {
                        setLimits(limitsToForm(server));
                        saveLimits.reset();
                      }}
                    >
                      Discard
                    </button>
                  )}
                  <button type="submit" className="btn btn-primary" disabled={saveLimits.isPending || !limitsDirty}>
                    Save limits
                  </button>
                </div>
              </>
            }
          >
            <LimitsFields value={limits} onChange={setLimits} />
            <div className="mt-3">
              <ErrorBox error={saveLimits.error} />
            </div>
          </Section>
        </form>
      ) : (
        <Section title="Resource limits" desc="Set by an administrator. Ask them if the server needs more.">
          <div className="grid gap-2.5 sm:grid-cols-2 lg:grid-cols-3">
            <Tile icon={ico(MemoryStick)} label="Memory">
              {sizeMb(server.memory_mb)}
              {server.swap_mb !== 0 && (
                <span className="text-zinc-500"> + {server.swap_mb < 0 ? "unlimited" : sizeMb(server.swap_mb)} swap</span>
              )}
            </Tile>
            <Tile icon={ico(Cpu)} label="CPU">
              {server.cpu_pct ? `${server.cpu_pct}% (${(server.cpu_pct / 100).toFixed(server.cpu_pct % 100 ? 1 : 0)} cores)` : "Unlimited"}
              {server.cpuset && <span className="text-zinc-500"> · cores {server.cpuset}</span>}
            </Tile>
            <Tile icon={ico(HardDrive)} label="Disk">
              {server.disk_mb ? sizeMb(server.disk_mb) : "Unlimited"}
            </Tile>
            <Tile icon={ico(Archive)} label="Backups kept">
              {server.backup_limit}
            </Tile>
            <Tile icon={ico(Layers)} label="Max processes">
              {server.pids_limit}
            </Tile>
          </div>
        </Section>
      )}

      {canSftp && (
        <Section title="SFTP" desc="Move big uploads, whole folders and world saves with a desktop client.">
          <SftpConnect uuid={server.uuid} serverName={server.name} />
        </Section>
      )}

      {isAdmin && <SuspendSection server={server} />}

      {isAdmin && (
        <Section
          danger
          title="Delete this server"
          desc="Removes the server from the panel and from its node, including its files. This cannot be undone."
          footer={
            <>
              <span>Backups stored on the node are deleted with it.</span>
              <button type="button" className="btn btn-danger" onClick={() => setConfirm(true)}>
                <Trash2 className="h-4 w-4" aria-hidden /> Delete server
              </button>
            </>
          }
        />
      )}

      <DeleteDialog
        open={confirm}
        name={server.name}
        offline={!server.node_online}
        pending={del.isPending}
        error={del.error}
        onConfirm={() => del.mutate()}
        onCancel={() => {
          setConfirm(false);
          del.reset();
        }}
      />
    </div>
  );
}

/** Suspending stops the server and locks its owner out until it is lifted; nothing is deleted. */
function SuspendSection({ server }: { server: ServerView }) {
  const [confirm, setConfirm] = useState(false);
  const change = useMutation({
    mutationFn: () =>
      call(
        server.suspended
          ? api.POST("/servers/{uuid}/unsuspend", { params: { path: { uuid: server.uuid } } })
          : api.POST("/servers/{uuid}/suspend", { params: { path: { uuid: server.uuid } } }),
      ),
    meta: invalidates(keys.server(server.uuid), keys.servers),
    onSuccess: () => setConfirm(false),
  });
  return (
    <>
      <Section
        title="Suspension"
        desc={
          server.suspended
            ? "This server is suspended. Lifting it lets its owner and subusers use it again; it is not started by this."
            : "Stops the server and keeps it stopped. Its owner and subusers still see it but cannot use it until the suspension is lifted. Nothing is deleted."
        }
        footer={
          <>
            <span>{server.suspended ? "Suspended." : "Billing systems do this through the API when an invoice goes unpaid."}</span>
            <button
              type="button"
              className={server.suspended ? "btn btn-primary" : "btn"}
              disabled={change.isPending}
              onClick={() => (server.suspended ? change.mutate() : setConfirm(true))}
            >
              <Ban className="h-4 w-4" aria-hidden /> {server.suspended ? "Lift suspension" : "Suspend server"}
            </button>
          </>
        }
      >
        {server.suspended && change.error ? <ErrorBox error={change.error} /> : null}
      </Section>
      <FormDialog
        open={confirm}
        title={`Suspend ${server.name}?`}
        submitLabel="Suspend server"
        pending={change.isPending}
        error={change.error}
        onSubmit={() => change.mutate()}
        onClose={() => {
          setConfirm(false);
          change.reset();
        }}
      >
        <p>
          The server is stopped now if it is running, and cannot be started until you lift the suspension. Its owner and subusers lose
          access to the console, files, backups and settings in the meantime.
        </p>
      </FormDialog>
    </>
  );
}

/** Deleting asks for the server's name, so it cannot happen by a stray click. */
function DeleteDialog({
  open,
  name,
  offline,
  pending,
  error,
  onConfirm,
  onCancel,
}: {
  open: boolean;
  name: string;
  offline: boolean;
  pending: boolean;
  error: unknown;
  onConfirm: () => void;
  onCancel: () => void;
}) {
  const [typed, setTyped] = useState("");
  const close = () => {
    setTyped("");
    onCancel();
  };
  return (
    <FormDialog
      open={open}
      danger
      title={`Delete ${name}?`}
      submitLabel="Delete server"
      pending={pending}
      error={error}
      disabled={typed.trim() !== name.trim()}
      onSubmit={onConfirm}
      onClose={close}
    >
      <p>This permanently deletes the server, its files and its backups. It cannot be undone.</p>
      {offline && (
        <p className="rounded-lg border border-amber-500/30 bg-amber-500/10 px-3 py-2 text-sm text-amber-200">
          Its node is offline. The server is removed from the panel now, and the node deletes its container and files when it connects
          again.
        </p>
      )}
      <Field label={`Type ${name} to confirm`}>
        {(id) => (
          <input
            id={id}
            className="input"
            autoComplete="off"
            spellCheck={false}
            value={typed}
            onChange={(e) => setTyped(e.target.value)}
            autoFocus
          />
        )}
      </Field>
    </FormDialog>
  );
}

function RenameSection({ server }: { server: ServerView }) {
  const [name, setName] = useState<string | null>(null);
  const [desc, setDesc] = useState<string | null>(null);
  const save = useMutation({
    mutationFn: () =>
      call(
        api.PUT("/servers/{uuid}/details", {
          params: { path: { uuid: server.uuid } },
          body: { name: (name ?? server.name).trim(), description: (desc ?? server.description).trim() },
        }),
      ),
    meta: invalidates(keys.server(server.uuid), keys.servers),
    onSuccess: () => {
      setName(null);
      setDesc(null);
    },
  });
  const dirty = (name !== null && name.trim() !== server.name) || (desc !== null && desc.trim() !== server.description);
  const empty = !(name ?? server.name).trim();
  return (
    <form
      onSubmit={(e) => {
        e.preventDefault();
        save.mutate();
      }}
    >
      <Section
        title="Name"
        desc="What everyone with access sees in their server list."
        footer={
          <>
            <span
              className={save.isSuccess && !dirty ? "text-good-text" : dirty ? "text-warn-text" : ""}
              role={save.isSuccess && !dirty ? "status" : undefined}
            >
              {save.isSuccess && !dirty ? "Saved." : empty ? "The name cannot be empty." : dirty ? "You have unsaved changes." : ""}
            </span>
            <div className="flex gap-2">
              {dirty && (
                <button
                  type="button"
                  className="btn btn-ghost"
                  onClick={() => {
                    setName(null);
                    setDesc(null);
                  }}
                >
                  Discard
                </button>
              )}
              <button type="submit" className="btn btn-primary" disabled={!dirty || empty || save.isPending}>
                Save
              </button>
            </div>
          </>
        }
      >
        <div className="grid gap-4 sm:grid-cols-2">
          <Field label="Name">
            {(id) => (
              <input id={id} className="input" maxLength={64} value={name ?? server.name} onChange={(e) => setName(e.target.value)} />
            )}
          </Field>
          <Field label="Description" hint="Optional, e.g. the season or the modpack version.">
            {(id) => (
              <input
                id={id}
                className="input"
                maxLength={500}
                value={desc ?? server.description}
                onChange={(e) => setDesc(e.target.value)}
              />
            )}
          </Field>
        </div>
        <div className="mt-3">
          <ErrorBox error={save.error} />
        </div>
      </Section>
    </form>
  );
}

function OwnerDialog({ server, onClose }: { server: ServerView; onClose: () => void }) {
  const [email, setEmail] = useState("");
  const change = useMutation({
    mutationFn: () => call(api.PUT("/servers/{uuid}/owner", { params: { path: { uuid: server.uuid } }, body: { email: email.trim() } })),
    meta: invalidates(keys.server(server.uuid), keys.servers, keys.subusers(server.uuid)),
    onSuccess: onClose,
  });
  return (
    <FormDialog
      open
      title={`Change the owner of ${server.name}`}
      submitLabel="Change owner"
      pending={change.isPending}
      error={change.error}
      disabled={!email.includes("@")}
      onSubmit={() => change.mutate()}
      onClose={onClose}
    >
      <Field label="New owner's email" hint="They need an account already. If they were a subuser, they become the owner instead.">
        {(id) => <input id={id} type="email" className="input" value={email} onChange={(e) => setEmail(e.target.value)} autoFocus />}
      </Field>
      <p className="text-xs text-zinc-500">
        The current owner{server.owner_email ? ` (${server.owner_email})` : ""} loses access. Add them as a subuser afterwards if they
        should keep it.
      </p>
    </FormDialog>
  );
}
