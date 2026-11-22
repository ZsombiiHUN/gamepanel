import { useState, type SubmitEvent } from "react";
import { useSearch } from "@tanstack/react-router";
import { useMutation, useQuery } from "@tanstack/react-query";
import { Link2, Loader2 } from "lucide-react";
import { api, call } from "../api/client";
import { identitiesQuery, invalidates, keys } from "../api/queries";
import { ErrorBox, Pill, Section } from "../components/ui";
import { PasswordField } from "../components/AuthFields";
import { ssoError, ssoStart } from "../lib/sso";
import { useTranslation } from "react-i18next";

/** Linked single sign-on providers, with link and unlink. */
export function SignInMethods() {
  const { t } = useTranslation();
  const q = useQuery(identitiesQuery);
  const search = useSearch({ from: "/app/account" });
  const unlink = useMutation({
    mutationFn: (provider: string) => call(api.DELETE("/auth/identities/{provider}", { params: { path: { provider } } })),
    meta: invalidates(keys.identities),
  });
  const providers = q.data?.providers ?? [];
  if (!q.data || (providers.length === 0 && q.data.identities.length === 0)) return null;
  const linked = new Map(q.data.identities.map((i) => [i.provider, i]));
  const msg = ssoError(search.sso_error);
  const justLinked = search.sso_linked ? providers.find((p) => p.id === search.sso_linked)?.name : undefined;

  return (
    <Section title={t("signInMethods.signInMethods")} desc={t("signInMethods.signInWithAnAccount")}>
      {msg && (
        <p role="alert" className="mb-3 rounded-lg border border-warn/30 bg-warn-bg px-3 py-2.5 text-sm text-warn-text">
          {msg}
        </p>
      )}
      {justLinked && (
        <p role="status" className="mb-3 text-sm text-good-text">
          {t("signInMethods.justLinked", { name: justLinked })}
        </p>
      )}
      <ul className="divide-y divide-line rounded-xl border border-line">
        {providers.map((p) => {
          const id = linked.get(p.id);
          return (
            <li key={p.id} className="flex flex-wrap items-center gap-3 p-3.5">
              <div className="min-w-0 flex-1">
                <div className="text-sm font-medium text-zinc-100">{p.name}</div>
                <div className="text-xs text-zinc-500">
                  {id
                    ? id.email
                      ? t("signInMethods.linkedAs", { email: id.email })
                      : t("signInMethods.linked")
                    : t("signInMethods.notLinked")}
                </div>
              </div>
              {id ? (
                <button type="button" className="btn btn-ghost" disabled={unlink.isPending} onClick={() => unlink.mutate(p.id)}>
                  {t("signInMethods.unlink")}
                </button>
              ) : (
                <a className="btn" href={ssoStart(p.id, { mode: "link" })}>
                  <Link2 className="h-4 w-4" aria-hidden /> {t("signInMethods.link")}
                </a>
              )}
            </li>
          );
        })}
      </ul>
      {!q.data.has_password && (
        <p className="mt-3">
          <Pill tone="warn">{t("signInMethods.noPassword")}</Pill>{" "}
          <span className="text-sm text-zinc-400">{t("signInMethods.setOneBelowSoYou")}</span>
        </p>
      )}
      <div className="mt-3">
        <ErrorBox error={unlink.error} />
      </div>
    </Section>
  );
}

/** For accounts made through single sign-on: choose a first password. */
export function FirstPassword() {
  const { t } = useTranslation();
  const [pw, setPw] = useState("");
  const [again, setAgain] = useState("");
  const set = useMutation({
    mutationFn: () => call(api.POST("/auth/password/first", { body: { password: pw } })),
    meta: invalidates(keys.identities),
  });
  return (
    <form
      onSubmit={(e: SubmitEvent) => {
        e.preventDefault();
        if (pw === again) set.mutate();
      }}
    >
      <Section
        title={t("common.password")}
        desc={t("signInMethods.yourAccountWasMadeThrough")}
        footer={
          <>
            <span />
            <button type="submit" className="btn btn-primary" disabled={set.isPending || pw !== again}>
              {set.isPending && <Loader2 className="h-4 w-4 animate-spin" aria-hidden />} {t("signInMethods.setPassword")}
            </button>
          </>
        }
      >
        <div className="grid max-w-md gap-4">
          <PasswordField label={t("signInMethods.newPassword")} kind="new" value={pw} onChange={setPw} />
          <PasswordField
            label={t("signInMethods.repeatNewPassword")}
            kind="new"
            value={again}
            onChange={setAgain}
            hint={again && pw !== again ? t("signInMethods.theTwoDoNotMatch") : undefined}
          />
          <ErrorBox error={set.error} />
        </div>
      </Section>
    </form>
  );
}
