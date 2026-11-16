import { useState } from "react";
import { useNavigate } from "@tanstack/react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Trash2 } from "lucide-react";
import { api, call, type components } from "../api/client";
import { eggsQuery, invalidates, keys, meQuery, nodesQuery } from "../api/queries";
import { ConfirmDialog, CopyButton, ErrorBox, Field, FormDialog, InfoList, Section } from "../components/ui";
import { LimitsFields, formToLimits, limitsToForm, type LimitsForm } from "../components/LimitsFields";

type ServerView = components["schemas"]["ServerView"];

export function SettingsTab({ server }: { server: ServerView }) {
  const me = useQuery(meQuery);
  const isAdmin = !!me.data?.is_admin;
  // Nodes/eggs lists are admin endpoints; only ask when admin.
  const nodes = useQuery({ ...nodesQuery, enabled: isAdmin });
  const eggs = useQuery({ ...eggsQuery, enabled: isAdmin });
  const nodeName = nodes.data?.find((n) => n.id === server.node_id)?.name;
  const eggName = eggs.data?.find((e) => e.id === server.egg_id)?.name;
  const [confirm, setConfirm] = useState(false);
  const [ownerOpen, setOwnerOpen] = useState(false);
  const qc = useQueryClient();
  const navigate = useNavigate();
  const del = useMutation({
    mutationFn: () => call(api.DELETE("/servers/{uuid}", { params: { path: { uuid: server.uuid } } })),
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
  const saveLimits = useMutation({
    mutationFn: () => call(api.PUT("/servers/{uuid}/limits", { params: { path: { uuid: server.uuid } }, body: formToLimits(limits) })),
    meta: invalidates(keys.server(server.uuid)),
  });
  const sftpUser = `${me.data?.email ?? ""}.${server.uuid.slice(0, 8)}`;

  return (
    <div className="space-y-5">
      {server.permissions.includes("settings.rename") && <RenameSection server={server} />}

      <Section
        title="Details"
        desc="Where this server lives and who owns it."
        footer={isAdmin ? <><span>Handing the server over removes the current owner's access.</span><button type="button" className="btn" onClick={() => setOwnerOpen(true)}>Change owner</button></> : undefined}
      >
        <InfoList
          rows={[
            { label: "UUID", value: server.uuid },
            { label: "Egg", value: eggName ? `${eggName} (#${server.egg_id})` : `#${server.egg_id}` },
            { label: "Node", value: nodeName ? `${nodeName} (#${server.node_id})` : `#${server.node_id}` },
            { label: "Port", value: String(server.port) },
            { label: "Owner", value: server.owner_email ?? `#${server.owner_id}` },
          ]}
        />
      </Section>
      {ownerOpen && <OwnerDialog server={server} onClose={() => setOwnerOpen(false)} />}

      {isAdmin ? (
        <form onSubmit={(e) => { e.preventDefault(); saveLimits.mutate(); }}>
          <Section
            title="Resource limits"
            desc="What this server may use on its node. A running server picks up changes the next time it starts."
            footer={
              <>
                <span className={saveLimits.isSuccess ? "text-emerald-400" : ""} role={saveLimits.isSuccess ? "status" : undefined}>
                  {saveLimits.isSuccess ? "Limits saved." : "0 usually means unlimited."}
                </span>
                <button type="submit" className="btn btn-primary" disabled={saveLimits.isPending}>Save limits</button>
              </>
            }
          >
            <LimitsFields value={limits} onChange={setLimits} />
            <div className="mt-3"><ErrorBox error={saveLimits.error} /></div>
          </Section>
        </form>
      ) : (
        <Section title="Resource limits" desc="Set by an administrator.">
          <InfoList
            rows={[
              { label: "Memory", value: `${server.memory_mb} MB` },
              { label: "Swap", value: server.swap_mb < 0 ? "unlimited" : `${server.swap_mb} MB` },
              { label: "CPU", value: server.cpu_pct ? `${server.cpu_pct}%` : "unlimited" },
              { label: "Cores", value: server.cpuset || "any" },
              { label: "Disk", value: server.disk_mb ? `${server.disk_mb} MB` : "unlimited" },
              { label: "Max processes", value: String(server.pids_limit) },
              { label: "Backups to keep", value: String(server.backup_limit) },
            ]}
          />
        </Section>
      )}

      {(server.permissions.includes("files.read") || server.permissions.includes("files.write")) && (
        <Section
          title="SFTP"
          desc="Upload and download files with any SFTP client."
          footer={<><span>Your account password does not work for SFTP. Use an API key from your Account page.</span><CopyButton text={sftpUser} label="Copy username" /></>}
        >
          <InfoList
            rows={[
              { label: "Host", value: "your node's address", plain: true },
              { label: "Port", value: "2022 (default)" },
              { label: "Username", value: sftpUser },
              { label: "Password", value: "An API key", plain: true },
            ]}
          />
        </Section>
      )}

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

      <ConfirmDialog
        open={confirm}
        title={`Delete ${server.name}?`}
        body="This permanently deletes the server and cannot be undone."
        confirmLabel="Delete server"
        pending={del.isPending}
        error={del.error}
        onConfirm={() => del.mutate()}
        onCancel={() => { setConfirm(false); del.reset(); }}
      />
    </div>
  );
}

function RenameSection({ server }: { server: ServerView }) {
  const [name, setName] = useState<string | null>(null);
  const [desc, setDesc] = useState<string | null>(null);
  const save = useMutation({
    mutationFn: () => call(api.PUT("/servers/{uuid}/details", { params: { path: { uuid: server.uuid } }, body: { name: (name ?? server.name).trim(), description: (desc ?? server.description).trim() } })),
    meta: invalidates(keys.server(server.uuid), keys.servers),
    onSuccess: () => { setName(null); setDesc(null); },
  });
  const dirty = (name !== null && name.trim() !== server.name) || (desc !== null && desc.trim() !== server.description);
  return (
    <form onSubmit={(e) => { e.preventDefault(); save.mutate(); }}>
      <Section
        title="Name"
        desc="What everyone with access sees in their server list."
        footer={<><span role={save.isSuccess && !dirty ? "status" : undefined}>{save.isSuccess && !dirty ? "Saved." : ""}</span><button type="submit" className="btn btn-primary" disabled={!dirty || !(name ?? server.name).trim() || save.isPending}>Save</button></>}
      >
        <div className="grid gap-4 sm:grid-cols-2">
          <Field label="Name">{(id) => <input id={id} className="input" maxLength={64} value={name ?? server.name} onChange={(e) => setName(e.target.value)} />}</Field>
          <Field label="Description" hint="Optional, e.g. the season or the modpack version.">{(id) => <input id={id} className="input" maxLength={500} value={desc ?? server.description} onChange={(e) => setDesc(e.target.value)} />}</Field>
        </div>
        <div className="mt-3"><ErrorBox error={save.error} /></div>
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
    <FormDialog open title={`Change the owner of ${server.name}`} submitLabel="Change owner" pending={change.isPending} error={change.error} disabled={!email.includes("@")} onSubmit={() => change.mutate()} onClose={onClose}>
      <Field label="New owner's email" hint="They need an account already. If they were a subuser, they become the owner instead.">
        {(id) => <input id={id} type="email" className="input" value={email} onChange={(e) => setEmail(e.target.value)} autoFocus />}
      </Field>
      <p className="text-xs text-zinc-500">The current owner{server.owner_email ? ` (${server.owner_email})` : ""} loses access. Add them as a subuser afterwards if they should keep it.</p>
    </FormDialog>
  );
}
