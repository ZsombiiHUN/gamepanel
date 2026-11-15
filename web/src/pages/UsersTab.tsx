import { useState, type FormEvent } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { ChevronDown, Loader2, Trash2, UserPlus } from "lucide-react";
import { api, call } from "../api/client";
import { invalidates, keys, permissionsQuery, subusersQuery } from "../api/queries";
import { Avatar, ConfirmDialog, Empty, ErrorBox, Field, Pill, Section } from "../components/ui";

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

  const add = useMutation({
    mutationFn: () => call(api.POST("/servers/{uuid}/users", { params: { path: { uuid } }, body: { email, permissions: picked } })),
    meta,
    onSuccess: () => setEmail(""),
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
        desc="Subusers can only do what you tick. They never see the server's environment variables, and cannot manage access or delete the server."
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
          desc="They need an existing account on this panel."
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
            <Field label="Their account email">
              {(id) => <input id={id} className="input max-w-md" type="email" required value={email} onChange={(e) => setEmail(e.target.value)} />}
            </Field>
            <PermPicker all={all} value={picked} onChange={setPicked} />
            <ErrorBox error={add.error} />
          </div>
        </Section>
      </form>

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
