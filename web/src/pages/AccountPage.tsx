import { useState, type SubmitEvent } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { Loader2, ShieldAlert } from "lucide-react";
import { api, call } from "../api/client";
import { identitiesQuery, invalidates, keys, meQuery } from "../api/queries";
import { Avatar, ErrorBox, PageHeader, Pill, Section, SectionsWithNav } from "../components/ui";
import { PasswordField, MfaCodeField } from "../components/AuthFields";
import { ApiKeysSection } from "./ApiKeysSection";
import { PasskeysSection } from "./PasskeysSection";
import { SessionsSection } from "./SessionsSection";
import { FirstPassword, SignInMethods } from "./SignInMethods";
import { TotpSection } from "./TotpSection";

export function AccountPage() {
  const me = useQuery(meQuery);
  const ids = useQuery(identitiesQuery);
  const [current, setCurrent] = useState("");
  const [next, setNext] = useState("");
  const [again, setAgain] = useState("");
  const [code, setCode] = useState("");
  // With two-factor on, a password change needs a code as well.
  const mfa = me.data?.totp_enabled ?? false;
  const mismatch = again !== "" && next !== again;

  const change = useMutation({
    mutationFn: () =>
      call(api.POST("/auth/password", { body: { current_password: current, new_password: next, ...(mfa ? { code } : {}) } })),
    meta: invalidates(keys.sessions),
    onSuccess: () => {
      setCurrent("");
      setNext("");
      setAgain("");
      setCode("");
    },
  });

  const submit = (e: SubmitEvent) => {
    e.preventDefault();
    if (!mismatch) change.mutate();
  };

  return (
    <div className="mx-auto max-w-[1088px]">
      <PageHeader
        title="Account"
        sub={
          <span className="inline-flex items-center gap-2">
            <Avatar name={me.data?.email ?? "?"} size={22} /> {me.data?.email}
            {me.data?.is_admin && <Pill>Administrator</Pill>}
          </span>
        }
      />
      <SectionsWithNav>
        {me.data?.mfa_required && (
          <div role="alert" className="flex gap-3 rounded-xl border border-warn/30 bg-warn-bg p-4 text-sm text-warn-text">
            <ShieldAlert className="h-5 w-5 shrink-0" aria-hidden />
            <p>
              This panel requires two-factor authentication. Set up an authenticator app or add a passkey below to continue. Until then,
              only this page works.
            </p>
          </div>
        )}
        {ids.data && !ids.data.has_password ? (
          <FirstPassword />
        ) : (
          <form onSubmit={submit}>
            <Section
              title="Password"
              desc="12 to 256 characters. Other browsers are signed out when you change it."
              footer={
                <>
                  <span className={change.isSuccess ? "text-good-text" : ""} role={change.isSuccess ? "status" : undefined}>
                    {change.isSuccess ? "Password changed. Your other sessions were signed out." : ""}
                  </span>
                  <button type="submit" className="btn btn-primary" disabled={change.isPending || mismatch}>
                    {change.isPending && <Loader2 className="h-4 w-4 animate-spin" aria-hidden />} Change password
                  </button>
                </>
              }
            >
              <div className="grid max-w-md gap-4">
                <PasswordField label="Current password" value={current} onChange={setCurrent} />
                <PasswordField label="New password" kind="new" value={next} onChange={setNext} />
                <PasswordField label="Repeat new password" kind="new" value={again} onChange={setAgain} invalid={mismatch} />
                {mfa && <MfaCodeField value={code} onChange={setCode} />}
                {mismatch && (
                  <p role="alert" className="text-sm text-bad-text">
                    The passwords do not match.
                  </p>
                )}
                {change.error && <ErrorBox error={change.error} />}
              </div>
            </Section>
          </form>
        )}

        <PasskeysSection />
        <SignInMethods />
        <TotpSection />
        {/* Keys would outlive the two-factor restriction, so they wait until it is lifted. */}
        {!me.data?.mfa_required && <ApiKeysSection />}

        <SessionsSection />
      </SectionsWithNav>
    </div>
  );
}
