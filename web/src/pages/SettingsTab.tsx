import { useState, type ReactNode } from "react";
import { useNavigate } from "@tanstack/react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Archive,
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
import { ErrorBox, Field, FormDialog, Section, SectionsWithNav } from "../components/ui";
import { SftpConnect } from "./server/SftpConnect";
import { DeleteDialog, SuspendSection } from "./server/SuspendDelete";
import { LimitsFields, formToLimits, limitsToForm, type LimitsForm } from "../components/LimitsFields";
import { copyText } from "../lib/clipboard";
import { useTranslation } from "react-i18next";

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
      className="grid h-7 w-7 shrink-0 place-content-center rounded-md text-zinc-500 hover:bg-row-hover hover:text-zinc-200 focus-visible:outline-2 focus-visible:outline-indigo-500"
      onClick={() => {
        void (async () => {
          try {
            await copyText(text);
            setDone(true);
            setTimeout(() => setDone(false), 1500);
          } catch {
            /* the value stays selectable */
          }
        })();
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
    <div className="flex min-w-0 items-start gap-3 rounded-lg border border-line bg-well/60 p-3">
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
  const { t } = useTranslation();
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
    <SectionsWithNav>
      {server.permissions.includes("settings.rename") && <RenameSection server={server} />}

      <Section
        title={t("settingsTab.details")}
        desc={t("settingsTab.whereThisServerLivesAnd")}
        footer={
          isAdmin ? (
            <>
              <span>{t("settingsTab.handingTheServerOverRemoves")}</span>
              <button type="button" className="btn" onClick={() => setOwnerOpen(true)}>
                <Crown className="h-4 w-4" aria-hidden /> {t("settingsTab.changeOwner")}
              </button>
            </>
          ) : undefined
        }
      >
        <div className="grid gap-2.5 sm:grid-cols-2 lg:grid-cols-3">
          <Tile icon={ico(Box)} label={t("settingsTab.game")}>
            {server.egg_name || t("settingsTab.eggId", { id: server.egg_id })}
          </Tile>
          <Tile icon={ico(Server)} label={t("common.node")}>
            {server.node_name || t("settingsTab.nodeId", { id: server.node_id })}
          </Tile>
          <Tile
            icon={ico(Network)}
            label={t("settingsTab.primaryPort")}
            mono
            action={<CopyIcon text={String(server.port)} label={t("settingsTab.copyPort")} />}
          >
            {server.port}
          </Tile>
          <Tile icon={ico(Crown)} label={t("common.owner")}>
            {server.owner_email ?? t("settingsTab.userId", { id: server.owner_id })}
          </Tile>
          <div className="sm:col-span-2">
            <Tile
              icon={ico(Fingerprint)}
              label={t("settingsTab.serverId")}
              mono
              action={<CopyIcon text={server.uuid} label={t("settingsTab.copyServerId")} />}
            >
              {server.uuid}
            </Tile>
          </div>
          {isAdmin && server.external_id && (
            <Tile
              icon={ico(Tag)}
              label={t("settingsTab.externalIdShop")}
              mono
              action={<CopyIcon text={server.external_id} label={t("settingsTab.copyExternalId")} />}
            >
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
            title={t("settingsTab.resourceLimits")}
            desc={t("settingsTab.whatThisServerMayUse")}
            footer={
              <>
                <span
                  className={saveLimits.isSuccess && !limitsDirty ? "text-good-text" : limitsDirty ? "text-warn-text" : ""}
                  role={saveLimits.isSuccess && !limitsDirty ? "status" : undefined}
                >
                  {saveLimits.isSuccess && !limitsDirty
                    ? t("settingsTab.limitsSaved")
                    : limitsDirty
                      ? t("settingsTab.youHaveUnsavedChanges")
                      : t("settingsTab.0UsuallyMeansUnlimited")}
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
                      {t("settingsTab.discard")}
                    </button>
                  )}
                  <button type="submit" className="btn btn-primary" disabled={saveLimits.isPending || !limitsDirty}>
                    {t("settingsTab.saveLimits")}
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
        <Section title={t("settingsTab.resourceLimits")} desc={t("settingsTab.setByAnAdministratorAsk")}>
          <div className="grid gap-2.5 sm:grid-cols-2 lg:grid-cols-3">
            <Tile icon={ico(MemoryStick)} label={t("common.memory")}>
              {sizeMb(server.memory_mb)}
              {server.swap_mb !== 0 && (
                <span className="text-zinc-500">
                  {" "}
                  + {server.swap_mb < 0 ? t("settingsTab.unlimited") : sizeMb(server.swap_mb)} {t("settingsTab.swap")}
                </span>
              )}
            </Tile>
            <Tile icon={ico(Cpu)} label={t("common.cpu")}>
              {server.cpu_pct
                ? t("settingsTab.cpu", { pct: server.cpu_pct, cores: (server.cpu_pct / 100).toFixed(server.cpu_pct % 100 ? 1 : 0) })
                : t("settingsTab.unlimited2")}
              {server.cpuset && <span className="text-zinc-500"> · {t("settingsTab.cpuset", { cpuset: server.cpuset })}</span>}
            </Tile>
            <Tile icon={ico(HardDrive)} label={t("common.disk")}>
              {server.disk_mb ? sizeMb(server.disk_mb) : t("settingsTab.unlimited2")}
            </Tile>
            <Tile icon={ico(Archive)} label={t("settingsTab.backupsKept")}>
              {server.backup_limit}
            </Tile>
            <Tile icon={ico(Layers)} label={t("settingsTab.maxProcesses")}>
              {server.pids_limit}
            </Tile>
          </div>
        </Section>
      )}

      {canSftp && (
        <Section title={t("settingsTab.sftp")} desc={t("settingsTab.moveBigUploadsWholeFolders")}>
          <SftpConnect uuid={server.uuid} serverName={server.name} />
        </Section>
      )}

      {isAdmin && <SuspendSection server={server} />}

      {isAdmin && (
        <Section
          danger
          title={t("settingsTab.deleteThisServer")}
          desc={t("settingsTab.removesTheServerFromThe")}
          footer={
            <>
              <span>{t("settingsTab.backupsStoredOnTheNode")}</span>
              <button type="button" className="btn btn-danger" onClick={() => setConfirm(true)}>
                <Trash2 className="h-4 w-4" aria-hidden /> {t("settingsTab.deleteServer")}
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
    </SectionsWithNav>
  );
}

function RenameSection({ server }: { server: ServerView }) {
  const { t } = useTranslation();
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
        title={t("common.name")}
        desc={t("settingsTab.whatEveryoneWithAccessSees")}
        footer={
          <>
            <span
              className={save.isSuccess && !dirty ? "text-good-text" : dirty ? "text-warn-text" : ""}
              role={save.isSuccess && !dirty ? "status" : undefined}
            >
              {save.isSuccess && !dirty
                ? t("settingsTab.saved")
                : empty
                  ? t("settingsTab.theNameCannotBeEmpty")
                  : dirty
                    ? t("settingsTab.youHaveUnsavedChanges")
                    : ""}
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
                  {t("settingsTab.discard")}
                </button>
              )}
              <button type="submit" className="btn btn-primary" disabled={!dirty || empty || save.isPending}>
                {t("common.save")}
              </button>
            </div>
          </>
        }
      >
        <div className="grid gap-4 sm:grid-cols-2">
          <Field label={t("common.name")}>
            {(id) => (
              <input id={id} className="input" maxLength={64} value={name ?? server.name} onChange={(e) => setName(e.target.value)} />
            )}
          </Field>
          <Field label={t("common.description")} hint={t("settingsTab.optionalEGTheSeason")}>
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
  const { t } = useTranslation();
  const [email, setEmail] = useState("");
  const change = useMutation({
    mutationFn: () => call(api.PUT("/servers/{uuid}/owner", { params: { path: { uuid: server.uuid } }, body: { email: email.trim() } })),
    meta: invalidates(keys.server(server.uuid), keys.servers, keys.subusers(server.uuid)),
    onSuccess: onClose,
  });
  return (
    <FormDialog
      open
      title={t("settingsTab.changeTheOwnerOfName", { name: server.name })}
      submitLabel={t("settingsTab.changeOwner")}
      pending={change.isPending}
      error={change.error}
      disabled={!email.includes("@")}
      onSubmit={() => change.mutate()}
      onClose={onClose}
    >
      <Field label={t("settingsTab.newOwnerSEmail")} hint={t("settingsTab.theyNeedAnAccountAlready")}>
        {(id) => <input id={id} type="email" className="input" value={email} onChange={(e) => setEmail(e.target.value)} autoFocus />}
      </Field>
      <p className="text-xs text-zinc-500">
        {server.owner_email ? t("settingsTab.ownerLosesAccessEmail", { email: server.owner_email }) : t("settingsTab.ownerLosesAccess")}
      </p>
    </FormDialog>
  );
}
