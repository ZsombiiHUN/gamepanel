import { useMutation, useQuery } from "@tanstack/react-query";
import { Loader2, LogOut, Monitor, Smartphone } from "lucide-react";
import { api, call } from "../api/client";
import { invalidates, keys, sessionsQuery } from "../api/queries";
import { ErrorBox, Pill, Section } from "../components/ui";
import { relativeTime } from "../lib/activity";
import { describeSignIn, describeUserAgent } from "../lib/useragent";
import { Trans, useTranslation } from "react-i18next";

const mobile = (ua: string) => /iPhone|iPad|iPod|Android|Mobile/.test(ua);

/** Every signed-in browser or device on the account, each of which can be signed out (ASVS v5.0.0-7.5.2). */
export function SessionsSection() {
  const { t } = useTranslation();
  const list = useQuery({ ...sessionsQuery, refetchInterval: 60_000 });
  const revoke = useMutation({
    mutationFn: (id: string) => call(api.DELETE("/auth/sessions/{id}", { params: { path: { id } } })),
    meta: invalidates(keys.sessions),
  });
  const revokeOthers = useMutation({
    mutationFn: () => call(api.POST("/auth/sessions/revoke-others")),
    meta: invalidates(keys.sessions),
  });
  const others = (list.data ?? []).filter((s) => !s.current).length;

  return (
    <Section
      title={t("sessionsSection.sessions")}
      id="sessions-h"
      desc={t("sessionsSection.browsersAndDevicesSignedIn")}
      footer={
        <>
          <span className={revokeOthers.isSuccess ? "text-good-text" : ""} role={revokeOthers.isSuccess ? "status" : undefined}>
            {revokeOthers.isSuccess ? t("sessionsSection.otherSessionsSignedOut") : ""}
          </span>
          <button type="button" className="btn" onClick={() => revokeOthers.mutate()} disabled={revokeOthers.isPending || others === 0}>
            {revokeOthers.isPending && <Loader2 className="h-4 w-4 animate-spin" aria-hidden />}{" "}
            {t("sessionsSection.signOutAllOtherSessions")}
          </button>
        </>
      }
    >
      {list.isPending && <Loader2 className="h-4 w-4 animate-spin text-zinc-500" aria-label={t("sessionsSection.loadingSessions")} />}
      {list.data && (
        <ul className="divide-y divide-line rounded-xl border border-line" aria-label={t("sessionsSection.signedInSessions")}>
          {list.data.map((s) => {
            const Icon = mobile(s.user_agent) ? Smartphone : Monitor;
            const name = describeUserAgent(s.user_agent);
            const where = s.last_ip || s.ip;
            return (
              <li key={s.id} className="flex items-center gap-3 px-3.5 py-3">
                <Icon className="h-5 w-5 shrink-0 text-zinc-500" aria-hidden />
                <div className="min-w-0 flex-1">
                  <div className="flex flex-wrap items-center gap-2 text-sm text-zinc-200">
                    <span title={s.user_agent || undefined}>{name}</span>
                    {s.current && <Pill tone="good">{t("sessionsSection.thisDevice")}</Pill>}
                  </div>
                  <div className="mt-0.5 text-xs text-zinc-500">
                    {[
                      where && (
                        <span key="ip" className="font-mono">
                          {where}
                        </span>
                      ),
                      s.ip && s.last_ip && s.ip !== s.last_ip && (
                        <span key="from">
                          <Trans
                            i18nKey="sessionsSection.signedInFrom"
                            values={{ ip: s.ip }}
                            components={{ ip: <span className="font-mono" /> }}
                          />
                        </span>
                      ),
                      s.current ? t("sessionsSection.activeNow") : t("sessionsSection.active", { when: relativeTime(s.last_seen_at) }),
                      s.created_at > 0 && (
                        <span key="at" title={new Date(s.created_at * 1000).toLocaleString()}>
                          {t("sessionsSection.signedIn", { when: relativeTime(s.created_at) })}
                        </span>
                      ),
                      s.method && describeSignIn(s.method),
                    ]
                      .filter(Boolean)
                      .map((part, i) => (
                        <span key={i}>
                          {i > 0 && " · "}
                          {part}
                        </span>
                      ))}
                  </div>
                </div>
                {!s.current && (
                  <button
                    type="button"
                    className="btn btn-ghost !px-2.5 text-zinc-500 hover:text-bad-text"
                    aria-label={where ? t("sessionsSection.signOutAt", { name, where }) : t("sessionsSection.signOut", { name })}
                    title={t("common.signOut")}
                    disabled={revoke.isPending && revoke.variables === s.id}
                    onClick={() => revoke.mutate(s.id)}
                  >
                    {revoke.isPending && revoke.variables === s.id ? (
                      <Loader2 className="h-4 w-4 animate-spin" aria-hidden />
                    ) : (
                      <LogOut className="h-4 w-4" aria-hidden />
                    )}
                  </button>
                )}
              </li>
            );
          })}
        </ul>
      )}
      <ErrorBox error={list.error ?? revoke.error ?? revokeOthers.error} />
    </Section>
  );
}
