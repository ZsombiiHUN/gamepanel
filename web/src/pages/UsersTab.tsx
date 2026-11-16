import { useState, type FormEvent } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { ChevronDown, Loader2, Trash2, UserPlus } from "lucide-react";
import { api, call } from "../api/client";
import { invalidates, keys, permissionsQuery, serverInvitesQuery, subusersQuery } from "../api/queries";
import { Avatar, ConfirmDialog, CopyButton, Empty, ErrorBox, Field, Pill, Section } from "../components/ui";

/** Links come relative when the panel has no public address configured. */
const absolute = (link: string) => (link.startsWith("/") ? window.location.origin + link : link);

type Perm = { id: string; description: string };

/** Permissions grouped by their prefix (console, files, backups ...), one checkbox each. */
function PermPicker({ all, value, onChange }: { all: Perm[]; value: string[]; onChange: (v: string[]) => void }) {
  const toggle = (id: string) => onChange(value.includes(id) ? value.filter((x) => x !== id) : [...value, id]);
  const groups = new Map<string, Perm[]>();
  for (const p of all) {
    const g = p.id.split(".")[0] ?? "other";
    groups.set(g, [...(groups.get(g) ?? []), p]);
  }
  return (
    <fieldset className="grid gap-x-8 gap-y-4 sm:grid-cols-2">
      <legend className="sr-only">Permissions</legend>
      {[...groups].map(([g, perms]) => (
        <div key={g}>
          <div className="mb-1.5 text-[11px] font-semibold uppercase tracking-wider text-zinc-500">{g}</div>
          <div className="space-y-1.5">
            {perms.map((p) => (
              <label key={p.id} className="flex cursor-pointer items-start gap-2.5 text-sm text-zinc-300">
                <input type="checkbox" className="mt-1 accent-indigo-400" checked={value.includes(p.id)} onChange={() => toggle(p.id)} />
                <span>
                  <span className="font-mono text-[13px] text-zinc-200">{p.id}</span>
                  <span className="block text-xs text-zinc-500">{p.description}</span>
                </span>
              </label>
            ))}
          </div>
        </div>
      ))}
    </fieldset>
  );
}

export function UsersTab({ uuid }: { uuid: string }) {
  const perms = useQuery(permissionsQuery);
  const list = useQuery(subusersQuery(uuid));
  const meta = invalidates(keys.subusers(uuid));

  const [email, setEmail] = useState("");
  const [picked, setPicked] = useState<string[]>(["console.read"]);
  const [editing, setEditing] = useState<{ id: number; perms: string[] } | null>(null);
  const [toRemove, setToRemove] = useState<{ id: number; email: string } | null>(null);

  // Adds an existing account at once, or invites the email (link to pass on, and emailed when the panel can).
  const [sent, setSent] = useState<{ email: string; link: string; emailed: boolean } | null>(null);
  const invites = useQuery(serverInvitesQuery(uuid));
  const add = useMutation({
    mutationFn: () => call(api.POST("/servers/{uuid}/invites", { params: { path: { uuid } }, body: { email: email.trim(), permissions: picked } })),
    meta: invalidates(keys.subusers(uuid), keys.invites(uuid)),
    onSuccess: (r) => {
      setSent(r.added || !r.link ? null : { email: email.trim(), link: absolute(r.link), emailed: r.emailed });
      setEmail("");
    },
  });
  const withdraw = useMutation({
    mutationFn: (id: number) => call(api.DELETE("/servers/{uuid}/invites/{id}", { params: { path: { uuid, id } } })),
    meta: invalidates(keys.invites(uuid)),
  });
  const save = useMutation({
    mutationFn: (v: { id: number; perms: string[] }) =>
      call(api.PUT("/servers/{uuid}/users/{user_id}", { params: { path: { uuid, user_id: v.id } }, body: { permissions: v.perms } })),
    meta,
    onSuccess: () => setEditing(null),
  });
  const remove = useMutation({
    mutationFn: (id: number) => call(api.DELETE("/servers/{uuid}/users/{user_id}", { params: { path: { uuid, user_id: id } } })),
    meta,
    onSuccess: () => setToRemove(null),
  });

  const submit = (e: FormEvent) => {
    e.preventDefault();
    add.mutate();
  };
  const all = perms.data ?? [];

  return (
    <div className="space-y-5">
      <Section
        title="People with access"
        desc="Subusers can only do what you tick. They never see variables the egg hides, and cannot manage access or delete the server."
      >
        <ErrorBox error={list.error ?? remove.error ?? save.error} />
        {list.data?.length === 0 && (
          <Empty icon={<UserPlus className="h-5 w-5" aria-hidden />} title="Only you have access">
            Give a friend or a moderator an account below and choose what they can do.
          </Empty>
        )}
        {!!list.data?.length && (
          <ul className="divide-y divide-zinc-800 rounded-xl border border-zinc-800">
            {list.data.map((u) => {
              const open = editing?.id === u.user_id;
              return (
                <li key={u.user_id} className="p-3.5">
                  <div className="flex items-center gap-3">
                    <Avatar name={u.email} />
                    <div className="min-w-0 flex-1">
                      <div className="truncate text-sm font-medium text-zinc-100">{u.email}</div>
                      <div className="mt-0.5"><Pill>{u.permissions.length} of {all.length || u.permissions.length} permissions</Pill></div>
                    </div>
                    <button type="button" className="btn" aria-expanded={open} onClick={() => setEditing(open ? null : { id: u.user_id, perms: u.permissions })}>
                      {open ? "Close" : "Edit"} <ChevronDown className={`h-4 w-4 transition ${open ? "rotate-180" : ""}`} aria-hidden />
                    </button>
                    <button type="button" className="btn btn-ghost !px-2.5 text-zinc-500 hover:text-red-300" aria-label={`Remove ${u.email}`} onClick={() => setToRemove({ id: u.user_id, email: u.email })}>
                      <Trash2 className="h-4 w-4" aria-hidden />
                    </button>
                  </div>
                  {open && editing && (
                    <div className="mt-4 space-y-4 border-t border-zinc-800 pt-4">
                      <PermPicker all={all} value={editing.perms} onChange={(v) => setEditing({ id: u.user_id, perms: v })} />
                      <button type="button" className="btn btn-primary" disabled={save.isPending || editing.perms.length === 0} onClick={() => save.mutate(editing)}>
                        {save.isPending && <Loader2 className="h-4 w-4 animate-spin" aria-hidden />} Save permissions
                      </button>
                    </div>
                  )}
                </li>
              );
            })}
          </ul>
        )}
      </Section>

      <form onSubmit={submit}>
        <Section
          title="Give someone access"
          desc="If they have an account here they get access at once. If not, they get an invitation to make one, and the access comes with it."
          footer={
            <>
              <span>{picked.length} permission{picked.length === 1 ? "" : "s"} selected</span>
              <button type="submit" className="btn btn-primary" disabled={add.isPending || picked.length === 0}>
                {add.isPending && <Loader2 className="h-4 w-4 animate-spin" aria-hidden />} Add person
              </button>
            </>
          }
        >
          <div className="space-y-5">
            {sent && (
              <div role="status" className="space-y-2 rounded-xl border border-indigo-500/30 bg-indigo-500/10 p-4 text-sm text-zinc-200">
                <p>{sent.email} has no account yet, so they were invited{sent.emailed ? " by email" : ""}. {sent.emailed ? "You can also send them this link yourself:" : "Send them this link; it works once, for 7 days:"}</p>
                <div className="flex flex-wrap items-center gap-2">
                  <code className="min-w-0 flex-1 break-all rounded bg-zinc-950 px-2 py-1.5 text-xs text-zinc-300">{sent.link}</code>
                  <CopyButton text={sent.link} label="Copy link" />
                </div>
              </div>
            )}
            <Field label="Their email">
              {(id) => <input id={id} className="input max-w-md" type="email" required value={email} onChange={(e) => setEmail(e.target.value)} />}
            </Field>
            <PermPicker all={all} value={picked} onChange={setPicked} />
            <ErrorBox error={add.error} />
          </div>
        </Section>
      </form>

      {!!invites.data?.length && (
        <Section title="Waiting for sign-up" desc="Invitations nobody has used yet. Withdraw one to make its link stop working.">
          <ul className="divide-y divide-zinc-800 rounded-xl border border-zinc-800">
            {invites.data.map((i) => (
              <li key={i.id} className="flex flex-wrap items-center gap-3 p-3.5">
                <Avatar name={i.email} />
                <div className="min-w-0 flex-1">
                  <div className="truncate text-sm font-medium text-zinc-100">{i.email}</div>
                  <div className="text-xs text-zinc-500">Expires {new Date(i.expires_at * 1000).toLocaleDateString()} · {i.permissions.length} permissions</div>
                </div>
                <button type="button" className="btn btn-ghost" disabled={withdraw.isPending} onClick={() => withdraw.mutate(i.id)}>Withdraw</button>
              </li>
            ))}
          </ul>
          <div className="mt-3"><ErrorBox error={withdraw.error} /></div>
        </Section>
      )}

      <ConfirmDialog
        open={!!toRemove}
        title={`Remove ${toRemove?.email ?? ""}?`}
        body="They lose all access to this server immediately."
        confirmLabel="Remove access"
        pending={remove.isPending}
        error={remove.error}
        onConfirm={() => toRemove && remove.mutate(toRemove.id)}
        onCancel={() => { setToRemove(null); remove.reset(); }}
      />
    </div>
  );
}
