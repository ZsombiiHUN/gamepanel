import { useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { Database, Eye, EyeOff, KeyRound, Plus, Trash2 } from "lucide-react";
import { api, call, type components } from "../api/client";
import { databasesQuery, invalidates, keys } from "../api/queries";
import { ConfirmDialog, CopyButton, Empty, ErrorBox, Field, FormDialog, Section, Spinner } from "../components/ui";

type DatabasesView = components["schemas"]["DatabasesView"];
type ServerDatabase = components["schemas"]["ServerDatabase"];

export function DatabasesTab({ uuid }: { uuid: string }) {
  const q = useQuery(databasesQuery(uuid));
  if (q.isPending) return <Spinner />;
  if (q.error) return <ErrorBox error={q.error} />;
  return <Databases uuid={uuid} v={q.data} />;
}

function Databases({ uuid, v }: { uuid: string; v: DatabasesView }) {
  const [creating, setCreating] = useState(false);
  // Passwords the person asked to see, by database id. They are never cached.
  const [shown, setShown] = useState<Record<number, string>>({});
  const [toDelete, setToDelete] = useState<ServerDatabase | null>(null);
  const meta = invalidates(keys.databases(uuid));
  const reveal = useMutation({
    mutationFn: (id: number) => call(api.GET("/servers/{uuid}/databases/{id}/password", { params: { path: { uuid, id } } })),
    onSuccess: (d) => setShown((s) => ({ ...s, [d.id]: d.password })),
  });
  const rotate = useMutation({
    mutationFn: (id: number) => call(api.POST("/servers/{uuid}/databases/{id}/rotate", { params: { path: { uuid, id } } })),
    onSuccess: (d) => setShown((s) => ({ ...s, [d.id]: d.password })),
  });
  const remove = useMutation({
    mutationFn: (id: number) => call(api.DELETE("/servers/{uuid}/databases/{id}", { params: { path: { uuid, id } } })),
    meta,
    onSuccess: () => setToDelete(null),
  });
  const full = v.databases.length >= v.limit;

  return (
    <div className="space-y-5">
      <Section
        title="Databases"
        desc="MySQL databases for plugins and mods that need one, such as LuckPerms or CoreProtect. Each has its own user that can reach only that database."
        footer={
          v.can_manage ? (
            <>
              <span>
                {!v.available
                  ? "No database host is set up yet. Ask an administrator."
                  : `${v.databases.length} of ${v.limit} ${v.limit === 1 ? "database" : "databases"} used.`}
              </span>
              <button type="button" className="btn btn-primary" disabled={!v.available || full} onClick={() => setCreating(true)}>
                <Plus className="h-4 w-4" aria-hidden /> New database
              </button>
            </>
          ) : (
            <span>You can see these databases but not their passwords.</span>
          )
        }
      >
        {v.databases.length === 0 ? (
          <Empty icon={<Database className="h-5 w-5" aria-hidden />} title="No databases yet">
            {v.limit === 0
              ? "This server may not have databases. An administrator can raise its limit."
              : "Create one when a plugin asks for MySQL."}
          </Empty>
        ) : (
          <ul className="space-y-3">
            {v.databases.map((d) => (
              <li key={d.id} className="rounded-xl border border-line bg-zinc-950/40 p-4">
                <div className="flex flex-wrap items-center justify-between gap-2">
                  <h3 className="font-mono text-[14px] font-medium text-zinc-100">{d.name}</h3>
                  {v.can_manage && (
                    <span className="flex gap-1">
                      <button
                        type="button"
                        className="btn btn-ghost !min-h-8 !px-2.5 text-xs"
                        disabled={rotate.isPending}
                        onClick={() => {
                          if (window.confirm(`Give ${d.name} a new password? Anything using the old one stops connecting.`))
                            rotate.mutate(d.id);
                        }}
                      >
                        <KeyRound className="h-4 w-4" aria-hidden /> New password
                      </button>
                      <button
                        type="button"
                        className="inline-flex h-8 w-8 items-center justify-center rounded-md text-zinc-500 hover:bg-row-hover hover:text-bad-text"
                        aria-label={`Delete ${d.name}`}
                        onClick={() => setToDelete(d)}
                      >
                        <Trash2 className="h-4 w-4" aria-hidden />
                      </button>
                    </span>
                  )}
                </div>
                <dl className="mt-3 grid gap-x-6 gap-y-2 text-sm sm:grid-cols-2">
                  <Detail label="Host" value={`${d.host}:${d.port}`} />
                  <Detail label="Database" value={d.name} />
                  <Detail label="Username" value={d.username} />
                  <div className="flex items-center justify-between gap-2">
                    <dt className="text-zinc-500">Password</dt>
                    <dd className="flex items-center gap-1 font-mono text-[13px] text-zinc-200">
                      {shown[d.id] ? (
                        <>
                          <span className="break-all">{shown[d.id]}</span>
                          <CopyButton text={shown[d.id] ?? ""} label="Copy" />
                        </>
                      ) : (
                        "••••••••"
                      )}
                      {v.can_manage && (
                        <button
                          type="button"
                          className="inline-flex h-8 w-8 items-center justify-center rounded-md text-zinc-400 hover:bg-row-hover"
                          aria-label={shown[d.id] ? `Hide the password of ${d.name}` : `Show the password of ${d.name}`}
                          onClick={() => (shown[d.id] ? setShown(({ [d.id]: _, ...rest }) => rest) : reveal.mutate(d.id))}
                        >
                          {shown[d.id] ? <EyeOff className="h-4 w-4" aria-hidden /> : <Eye className="h-4 w-4" aria-hidden />}
                        </button>
                      )}
                    </dd>
                  </div>
                  <Detail label="Connections from" value={d.remote === "%" ? "anywhere" : d.remote} />
                  <div className="sm:col-span-2">
                    <dt className="sr-only">JDBC address</dt>
                    <dd className="flex flex-wrap items-center gap-2 font-mono text-xs text-zinc-500">
                      <span className="break-all">
                        jdbc:mysql://{d.host}:{d.port}/{d.name}
                      </span>
                      <CopyButton text={`jdbc:mysql://${d.host}:${d.port}/${d.name}`} label="Copy JDBC" />
                    </dd>
                  </div>
                </dl>
              </li>
            ))}
          </ul>
        )}
        <div className="mt-3">
          <ErrorBox error={reveal.error ?? rotate.error ?? remove.error} />
        </div>
      </Section>
      {creating && (
        <CreateDialog uuid={uuid} onClose={() => setCreating(false)} onCreated={(d) => setShown((s) => ({ ...s, [d.id]: d.password }))} />
      )}
      <ConfirmDialog
        open={!!toDelete}
        title={`Delete ${toDelete?.name ?? ""}?`}
        body="The database, its tables and its user are deleted for good. Plugins using it stop working."
        confirmLabel="Delete database"
        pending={remove.isPending}
        error={remove.error}
        onConfirm={() => toDelete && remove.mutate(toDelete.id)}
        onCancel={() => {
          setToDelete(null);
          remove.reset();
        }}
      />
    </div>
  );
}

function Detail({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex items-center justify-between gap-2">
      <dt className="text-zinc-500">{label}</dt>
      <dd className="break-all text-right font-mono text-[13px] text-zinc-200">{value}</dd>
    </div>
  );
}

function CreateDialog({
  uuid,
  onClose,
  onCreated,
}: {
  uuid: string;
  onClose: () => void;
  onCreated: (d: { id: number; password: string }) => void;
}) {
  const [name, setName] = useState("");
  const [remote, setRemote] = useState("%");
  const valid = /^[A-Za-z0-9_]{1,48}$/.test(name);
  const create = useMutation({
    mutationFn: () =>
      call(api.POST("/servers/{uuid}/databases", { params: { path: { uuid } }, body: { name, remote: remote.trim() || "%" } })),
    meta: invalidates(keys.databases(uuid)),
    onSuccess: (d) => {
      onCreated(d);
      onClose();
    },
  });
  return (
    <FormDialog
      open
      title="New database"
      submitLabel="Create database"
      pending={create.isPending}
      error={create.error}
      disabled={!valid}
      onSubmit={() => create.mutate()}
      onClose={onClose}
    >
      <Field
        label="Name"
        hint={name && !valid ? "Letters, digits and _ only, up to 48." : "The server's prefix is added in front, so names never clash."}
      >
        {(id) => (
          <input
            id={id}
            className="input font-mono"
            value={name}
            onChange={(e) => setName(e.target.value)}
            autoFocus
            placeholder="luckperms"
            spellCheck={false}
          />
        )}
      </Field>
      <Field label="Connections from" hint="% allows any address. Use the game server's address to allow only it.">
        {(id) => (
          <input id={id} className="input font-mono" value={remote} onChange={(e) => setRemote(e.target.value)} spellCheck={false} />
        )}
      </Field>
    </FormDialog>
  );
}
