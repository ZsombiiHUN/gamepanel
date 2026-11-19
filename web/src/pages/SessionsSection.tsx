import { useMutation, useQuery } from "@tanstack/react-query";
import { Loader2, LogOut, Monitor, Smartphone } from "lucide-react";
import { api, call } from "../api/client";
import { invalidates, keys, sessionsQuery } from "../api/queries";
import { ErrorBox, Pill, Section } from "../components/ui";
import { relativeTime } from "../lib/activity";
import { describeSignIn, describeUserAgent } from "../lib/useragent";

const mobile = (ua: string) => /iPhone|iPad|iPod|Android|Mobile/.test(ua);

/** Every signed-in browser or device on the account, each of which can be signed out (ASVS v5.0.0-7.5.2). */
export function SessionsSection() {
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
      title="Sessions"
      id="sessions-h"
      desc="Browsers and devices signed in to your account. If you do not recognise one, sign it out and change your password."
      footer={
        <>
          <span className={revokeOthers.isSuccess ? "text-emerald-400" : ""} role={revokeOthers.isSuccess ? "status" : undefined}>
            {revokeOthers.isSuccess ? "Other sessions signed out." : ""}
          </span>
          <button type="button" className="btn" onClick={() => revokeOthers.mutate()} disabled={revokeOthers.isPending || others === 0}>
            {revokeOthers.isPending && <Loader2 className="h-4 w-4 animate-spin" aria-hidden />} Sign out all other sessions
          </button>
        </>
      }
    >
      {list.isPending && <Loader2 className="h-4 w-4 animate-spin text-zinc-500" aria-label="Loading sessions" />}
      {list.data && (
        <ul className="divide-y divide-zinc-800 rounded-xl border border-zinc-800" aria-label="Signed-in sessions">
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
                    {s.current && <Pill tone="good">This device</Pill>}
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
                          signed in from <span className="font-mono">{s.ip}</span>
                        </span>
                      ),
                      s.current ? "active now" : `active ${relativeTime(s.last_seen_at)}`,
                      s.created_at > 0 && (
                        <span key="at" title={new Date(s.created_at * 1000).toLocaleString()}>
                          signed in {relativeTime(s.created_at)}
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
                    className="btn btn-ghost !px-2.5 text-zinc-500 hover:text-red-300"
                    aria-label={`Sign out ${name}${where ? ` at ${where}` : ""}`}
                    title="Sign out"
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
