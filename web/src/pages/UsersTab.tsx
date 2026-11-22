import { useState, type SubmitEvent } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { Check, ChevronDown, Clock, Crown, Loader2, Mail, Trash2, UserPlus } from "lucide-react";
import { api, call } from "../api/client";
import { invalidates, keys, permissionsQuery, serverInvitesQuery, subusersQuery } from "../api/queries";
import { Avatar, ConfirmDialog, CopyButton, Empty, ErrorBox, Field, Pill, Section, Spinner } from "../components/ui";
import { areaSummary, roleOf } from "../lib/access";
import { AccessChooser } from "./server/AccessChooser";
import { useTranslation } from "react-i18next";
import i18n from "../i18n";

/** Links come relative when the panel has no public address configured. */
const absolute = (link: string) => (link.startsWith("/") ? window.location.origin + link : link);

function daysLeft(unix: number, now = Date.now()) {
  const d = Math.ceil((unix * 1000 - now) / 86_400_000);
  if (d <= 0) return i18n.t("usersTab.expiresToday");
  return i18n.t("usersTab.expiresIn", { count: d });
}

function RoleTag({ perms, known }: { perms: string[]; known: string[] }) {
  const { t } = useTranslation();
  const role = roleOf(perms, known);
  return <Pill tone={role?.id === "full" ? "good" : "neutral"}>{role ? role.label : t("usersTab.custom")}</Pill>;
}

export function UsersTab({ uuid, ownerEmail }: { uuid: string; ownerEmail?: string }) {
  const { t } = useTranslation();
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
      <Section title={t("usersTab.peopleWithAccess")} desc={t("usersTab.subusersCanOnlyDoWhat")}>
        <ErrorBox error={list.error ?? remove.error ?? save.error ?? withdraw.error} />
        {list.isPending && <Spinner />}
        {list.data && (ownerEmail || count > 0) && (
          <ul className="divide-y divide-line overflow-hidden rounded-xl border border-line">
            {ownerEmail && (
              <li className="flex items-center gap-3 bg-thead/60 p-3.5">
                <Avatar name={ownerEmail} />
                <div className="min-w-0 flex-1">
                  <div className="truncate text-sm font-medium text-zinc-100">{ownerEmail}</div>
                  <div className="text-xs text-zinc-500">{t("usersTab.canDoEverythingIncludingManaging")}</div>
                </div>
                <Pill tone="warn">
                  <Crown className="mr-1 h-3 w-3" aria-hidden />
                  {t("common.owner")}
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
                      <div className="mt-0.5 truncate text-xs text-zinc-500">
                        {areaSummary(u.permissions) || t("usersTab.noPermissions")}
                      </div>
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
                        {open ? t("common.close") : t("common.edit")}{" "}
                        <ChevronDown className={`h-4 w-4 transition ${open ? "rotate-180" : ""}`} aria-hidden />
                      </button>
                      <button
                        type="button"
                        className="btn btn-ghost !px-2.5 text-zinc-500 hover:text-bad-text"
                        aria-label={t("usersTab.removeEmail", { email: u.email })}
                        onClick={() => setToRemove({ id: u.user_id, email: u.email })}
                      >
                        <Trash2 className="h-4 w-4" aria-hidden />
                      </button>
                    </div>
                  </div>
                  {open && (
                    <div className="space-y-4 border-t border-line p-3.5 sm:p-4">
                      <AccessChooser all={all} value={editing.perms} onChange={(v) => setEditing({ id: u.user_id, perms: v })} />
                      <div className="flex flex-wrap items-center justify-end gap-2 border-t border-line pt-4">
                        {editing.perms.length === 0 && (
                          <span className="mr-auto text-sm text-warn-text">{t("usersTab.pickAtLeastOnePermission")}</span>
                        )}
                        <button type="button" className="btn btn-ghost" onClick={() => setEditing(null)}>
                          {t("common.cancel")}
                        </button>
                        <button
                          type="button"
                          className="btn btn-primary"
                          disabled={save.isPending || !changed || editing.perms.length === 0}
                          onClick={() => save.mutate(editing)}
                        >
                          {save.isPending && <Loader2 className="h-4 w-4 animate-spin" aria-hidden />} {t("usersTab.savePermissions")}
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
                      {t("usersTab.invited")}
                    </Pill>
                    <RoleTag perms={i.permissions} known={known} />
                  </div>
                  <div className="mt-0.5 text-xs text-zinc-500">{t("usersTab.waiting", { expires: daysLeft(i.expires_at) })}</div>
                </div>
                <button type="button" className="btn btn-ghost" disabled={withdraw.isPending} onClick={() => withdraw.mutate(i.id)}>
                  {t("usersTab.withdraw")}
                </button>
              </li>
            ))}
          </ul>
        )}
        {list.data && count === 0 && (
          <div className="mt-3">
            <Empty icon={<UserPlus className="h-5 w-5" aria-hidden />} title={t("usersTab.onlyYouHaveAccess")}>
              {t("usersTab.addAFriendOrA")}
            </Empty>
          </div>
        )}
      </Section>

      <form onSubmit={submit}>
        <Section
          title={t("usersTab.addSomeone")}
          desc={t("usersTab.ifTheyAlreadyHaveAn")}
          footer={
            <>
              <span>
                {picked.length === 0
                  ? t("usersTab.chooseARoleOrSome")
                  : `${roleOf(picked, known)?.label ?? t("accessChooser.custom")} · ${t("usersTab.permissions", { count: picked.length })}`}
              </span>
              <button type="submit" className="btn btn-primary" disabled={add.isPending || picked.length === 0 || !email.trim()}>
                {add.isPending ? <Loader2 className="h-4 w-4 animate-spin" aria-hidden /> : <UserPlus className="h-4 w-4" aria-hidden />}{" "}
                {t("usersTab.addPerson")}
              </button>
            </>
          }
        >
          <div className="space-y-5">
            {sent && (
              <div role="status" className="space-y-2 rounded-xl border border-line-strong bg-row-active p-4 text-sm text-zinc-200">
                {sent.added || !sent.link ? (
                  <p className="flex items-center gap-2">
                    <Check className="h-4 w-4 text-good-text" aria-hidden /> {t("usersTab.hasAccess", { email: sent.email })}
                  </p>
                ) : (
                  <>
                    <p>
                      {sent.emailed
                        ? t("usersTab.invitedByEmail", { email: sent.email })
                        : t("usersTab.invitedNoAccount", { email: sent.email })}{" "}
                      {sent.emailed ? t("usersTab.youCanAlsoSendThem") : t("usersTab.sendThemThisLinkIt")}
                    </p>
                    <div className="flex flex-wrap items-center gap-2">
                      <code className="min-w-0 flex-1 break-all rounded bg-zinc-950 px-2 py-1.5 text-xs text-zinc-300">{sent.link}</code>
                      <CopyButton text={sent.link} label={t("usersTab.copyLink")} />
                    </div>
                  </>
                )}
              </div>
            )}
            <Field label={t("usersTab.theirEmail")}>
              {(id) => (
                <input
                  id={id}
                  className="input max-w-md"
                  type="email"
                  required
                  autoComplete="off"
                  placeholder={t("usersTab.friendExampleCom")}
                  value={email}
                  onChange={(e) => {
                    setEmail(e.target.value);
                    if (sent) setSent(null);
                  }}
                />
              )}
            </Field>
            <div>
              <div className="label">{t("usersTab.whatCanTheyDo")}</div>
              {perms.isPending ? <Spinner /> : <AccessChooser all={all} value={picked} onChange={setPicked} />}
            </div>
            <ErrorBox error={add.error ?? perms.error} />
          </div>
        </Section>
      </form>

      <ConfirmDialog
        open={!!toRemove}
        title={t("usersTab.confirmRemove", { email: toRemove?.email ?? "" })}
        body={t("usersTab.theyLoseAllAccessTo")}
        confirmLabel={t("usersTab.removeAccess")}
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
