import { useState, type SubmitEvent } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { Check, ChevronDown, Clock, Crown, Loader2, Mail, Trash2, UserPlus } from "lucide-react";
import { api, call } from "../api/client";
import { invalidates, keys, permissionsQuery, serverInvitesQuery, subusersQuery } from "../api/queries";
import { Avatar, ConfirmDialog, CopyButton, Empty, ErrorBox, Field, Pill, Section, Spinner } from "../components/ui";
import { areaSummary, roleOf } from "../lib/access";
import { AccessChooser } from "./server/AccessChooser";

/** Links come relative when the panel has no public address configured. */
const absolute = (link: string) => (link.startsWith("/") ? window.location.origin + link : link);

function daysLeft(unix: number, now = Date.now()) {
  const d = Math.ceil((unix * 1000 - now) / 86_400_000);
  if (d <= 0) return "expires today";
  return d === 1 ? "expires tomorrow" : `expires in ${d} days`;
}

function RoleTag({ perms, known }: { perms: string[]; known: string[] }) {
  const role = roleOf(perms, known);
  return <Pill tone={role?.id === "full" ? "good" : "neutral"}>{role ? role.label : "Custom"}</Pill>;
}

export function UsersTab({ uuid, ownerEmail }: { uuid: string; ownerEmail?: string }) {
  const perms = useQuery(permissionsQuery);
  const list = useQuery(subusersQuery(uuid));
  const invites = useQuery(serverInvitesQuery(uuid));
  const meta = invalidates(keys.subusers(uuid));
  const all = perms.data ?? [];
  const known = all.map((p) => p.id);

  const [email, setEmail] = useState("");
  const [picked, setPicked] = useState<string[]>(["console.read", "activity.read"]);
  const [editing, setEditing] = useState<{ id: number; perms: string[] } | null>(null);
  const [toRemove, setToRemove] = useState<{ id: number; email: string } | null>(null);

  // Adds an existing account at once, or invites the email (link to pass on, and emailed when the panel can).
  const [sent, setSent] = useState<{ email: string; link: string; emailed: boolean; added: boolean } | null>(null);
  const add = useMutation({
    mutationFn: () =>
      call(api.POST("/servers/{uuid}/invites", { params: { path: { uuid } }, body: { email: email.trim(), permissions: picked } })),
    meta: invalidates(keys.subusers(uuid), keys.invites(uuid)),
    onSuccess: (r) => {
      setSent({ email: email.trim(), link: r.link ? absolute(r.link) : "", emailed: r.emailed, added: r.added });
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

  const submit = (e: SubmitEvent) => {
    e.preventDefault();
    add.mutate();
  };

  const people = list.data ?? [];
  const pending = invites.data ?? [];
  const count = people.length + pending.length;

  return (
    <div className="space-y-5">
      <Section
        title="People with access"
        desc="Subusers can only do what you allow. They never see variables the egg hides, and cannot manage access or delete the server."
      >
        <ErrorBox error={list.error ?? remove.error ?? save.error ?? withdraw.error} />
        {list.isPending && <Spinner />}
        {list.data && (ownerEmail || count > 0) && (
          <ul className="divide-y divide-zinc-800 overflow-hidden rounded-xl border border-zinc-800">
            {ownerEmail && (
              <li className="flex items-center gap-3 bg-thead/60 p-3.5">
                <Avatar name={ownerEmail} />
                <div className="min-w-0 flex-1">
                  <div className="truncate text-sm font-medium text-zinc-100">{ownerEmail}</div>
                  <div className="text-xs text-zinc-500">Can do everything, including managing access</div>
                </div>
                <Pill tone="warn">
                  <Crown className="mr-1 h-3 w-3" aria-hidden />
                  Owner
                </Pill>
              </li>
            )}
            {people.map((u) => {
              const open = editing?.id === u.user_id;
              const changed =
                open && (editing.perms.length !== u.permissions.length || editing.perms.some((p) => !u.permissions.includes(p)));
              return (
                <li key={u.user_id} className={open ? "bg-zinc-950/40" : undefined}>
                  <div className="flex flex-wrap items-center gap-3 p-3.5">
                    <Avatar name={u.email} />
                    <div className="min-w-0 flex-1">
                      <div className="flex flex-wrap items-center gap-2">
                        <span className="truncate text-sm font-medium text-zinc-100">{u.email}</span>
                        <RoleTag perms={u.permissions} known={known} />
                      </div>
                      <div className="mt-0.5 truncate text-xs text-zinc-500">{areaSummary(u.permissions) || "No permissions"}</div>
                    </div>
                    <div className="flex items-center gap-1">
                      <button
                        type="button"
                        className="btn"
                        aria-expanded={open}
                        onClick={() => {
                          save.reset();
                          setEditing(open ? null : { id: u.user_id, perms: u.permissions });
                        }}
                      >
                        {open ? "Close" : "Edit"} <ChevronDown className={`h-4 w-4 transition ${open ? "rotate-180" : ""}`} aria-hidden />
                      </button>
                      <button
                        type="button"
                        className="btn btn-ghost !px-2.5 text-zinc-500 hover:text-red-300"
                        aria-label={`Remove ${u.email}`}
                        onClick={() => setToRemove({ id: u.user_id, email: u.email })}
                      >
                        <Trash2 className="h-4 w-4" aria-hidden />
                      </button>
                    </div>
                  </div>
                  {open && (
                    <div className="space-y-4 border-t border-zinc-800 p-3.5 sm:p-4">
                      <AccessChooser all={all} value={editing.perms} onChange={(v) => setEditing({ id: u.user_id, perms: v })} />
                      <div className="flex flex-wrap items-center justify-end gap-2 border-t border-zinc-800 pt-4">
                        {editing.perms.length === 0 && (
                          <span className="mr-auto text-sm text-warn-text">Pick at least one permission, or remove them instead.</span>
                        )}
                        <button type="button" className="btn btn-ghost" onClick={() => setEditing(null)}>
                          Cancel
                        </button>
                        <button
                          type="button"
                          className="btn btn-primary"
                          disabled={save.isPending || !changed || editing.perms.length === 0}
                          onClick={() => save.mutate(editing)}
                        >
                          {save.isPending && <Loader2 className="h-4 w-4 animate-spin" aria-hidden />} Save permissions
                        </button>
                      </div>
                    </div>
                  )}
                </li>
              );
            })}
            {pending.map((i) => (
              <li key={`invite-${i.id}`} className="flex flex-wrap items-center gap-3 p-3.5">
                <span
                  aria-hidden
                  className="grid h-8 w-8 shrink-0 place-content-center rounded-full border border-dashed border-zinc-600 text-zinc-500"
                >
                  <Mail className="h-3.5 w-3.5" />
                </span>
                <div className="min-w-0 flex-1">
                  <div className="flex flex-wrap items-center gap-2">
                    <span className="truncate text-sm font-medium text-zinc-300">{i.email}</span>
                    <Pill tone="warn">
                      <Clock className="mr-1 h-3 w-3" aria-hidden />
                      Invited
                    </Pill>
                    <RoleTag perms={i.permissions} known={known} />
                  </div>
                  <div className="mt-0.5 text-xs text-zinc-500">Waiting for them to sign up, {daysLeft(i.expires_at)}</div>
                </div>
                <button type="button" className="btn btn-ghost" disabled={withdraw.isPending} onClick={() => withdraw.mutate(i.id)}>
                  Withdraw
                </button>
              </li>
            ))}
          </ul>
        )}
        {list.data && count === 0 && (
          <div className="mt-3">
            <Empty icon={<UserPlus className="h-5 w-5" aria-hidden />} title="Only you have access">
              Add a friend or a moderator below and choose what they can do.
            </Empty>
          </div>
        )}
      </Section>

      <form onSubmit={submit}>
        <Section
          title="Add someone"
          desc="If they already have an account here they get access at once. If not, they get an invitation to sign up, and the access comes with it."
          footer={
            <>
              <span>
                {picked.length === 0
                  ? "Choose a role or some permissions."
                  : `${roleOf(picked, known)?.label ?? "Custom"} · ${picked.length} permission${picked.length === 1 ? "" : "s"}`}
              </span>
              <button type="submit" className="btn btn-primary" disabled={add.isPending || picked.length === 0 || !email.trim()}>
                {add.isPending ? <Loader2 className="h-4 w-4 animate-spin" aria-hidden /> : <UserPlus className="h-4 w-4" aria-hidden />}{" "}
                Add person
              </button>
            </>
          }
        >
          <div className="space-y-5">
            {sent && (
              <div role="status" className="space-y-2 rounded-xl border border-line-strong bg-row-active p-4 text-sm text-zinc-200">
                {sent.added || !sent.link ? (
                  <p className="flex items-center gap-2">
                    <Check className="h-4 w-4 text-good-text" aria-hidden /> {sent.email} now has access.
                  </p>
                ) : (
                  <>
                    <p>
                      {sent.email} has no account yet, so they were invited{sent.emailed ? " by email" : ""}.{" "}
                      {sent.emailed ? "You can also send them this link yourself:" : "Send them this link; it works once, for 7 days:"}
                    </p>
                    <div className="flex flex-wrap items-center gap-2">
                      <code className="min-w-0 flex-1 break-all rounded bg-zinc-950 px-2 py-1.5 text-xs text-zinc-300">{sent.link}</code>
                      <CopyButton text={sent.link} label="Copy link" />
                    </div>
                  </>
                )}
              </div>
            )}
            <Field label="Their email">
              {(id) => (
                <input
                  id={id}
                  className="input max-w-md"
                  type="email"
                  required
                  autoComplete="off"
                  placeholder="friend@example.com"
                  value={email}
                  onChange={(e) => {
                    setEmail(e.target.value);
                    if (sent) setSent(null);
                  }}
                />
              )}
            </Field>
            <div>
              <div className="label">What can they do?</div>
              {perms.isPending ? <Spinner /> : <AccessChooser all={all} value={picked} onChange={setPicked} />}
            </div>
            <ErrorBox error={add.error ?? perms.error} />
          </div>
        </Section>
      </form>

      <ConfirmDialog
        open={!!toRemove}
        title={`Remove ${toRemove?.email ?? ""}?`}
        body="They lose all access to this server immediately. You can add them again later."
        confirmLabel="Remove access"
        pending={remove.isPending}
        error={remove.error}
        onConfirm={() => toRemove && remove.mutate(toRemove.id)}
        onCancel={() => {
          setToRemove(null);
          remove.reset();
        }}
      />
    </div>
  );
}
