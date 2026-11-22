import { useState, type SubmitEvent } from "react";
import { Link, useNavigate } from "@tanstack/react-router";
import { useMutation, useQuery } from "@tanstack/react-query";
import { CheckCircle2, Loader2 } from "lucide-react";
import { api, call } from "../api/client";
import { authOptionsQuery, inviteQuery, invalidates, keys } from "../api/queries";
import { AuthLayout, tokenFromHash } from "../components/AuthLayout";
import { ErrorBox, Field } from "../components/ui";
import { PasswordField } from "../components/AuthFields";
import { SSOButtons } from "./LoginPage";
import { useTranslation } from "react-i18next";

export function ForgotPage() {
  const { t } = useTranslation();
  const [email, setEmail] = useState("");
  const send = useMutation({ mutationFn: () => call(api.POST("/auth/forgot", { body: { email: email.trim() } })) });
  if (send.isSuccess) {
    return (
      <AuthLayout title={t("passwordPages.checkYourEmail")} sub="">
        <p role="status" className="flex gap-2 text-sm text-zinc-300">
          <CheckCircle2 className="h-5 w-5 shrink-0 text-good-text" aria-hidden />
          {t("passwordPages.linkSent", { email: email.trim() })}
        </p>
        <Link to="/login" className="link text-sm">
          {t("passwordPages.backToSignIn")}
        </Link>
      </AuthLayout>
    );
  }
  return (
    <AuthLayout title={t("passwordPages.forgotYourPassword")} sub={t("passwordPages.weWillEmailYouA")}>
      <form
        className="space-y-6"
        aria-labelledby="auth-title"
        onSubmit={(e: SubmitEvent) => {
          e.preventDefault();
          send.mutate();
        }}
      >
        <ErrorBox error={send.error} />
        <Field label={t("common.email")}>
          {(id) => (
            <input
              id={id}
              className="input"
              type="email"
              autoComplete="username"
              required
              autoFocus
              value={email}
              onChange={(e) => setEmail(e.target.value)}
            />
          )}
        </Field>
        <button type="submit" className="btn btn-primary w-full" disabled={send.isPending}>
          {send.isPending && <Loader2 className="h-4 w-4 animate-spin" aria-hidden />} {t("passwordPages.sendTheLink")}
        </button>
        <Link to="/login" className="link block text-center text-sm">
          {t("passwordPages.backToSignIn")}
        </Link>
      </form>
    </AuthLayout>
  );
}

/** Choose a password twice; the second must match before the form is sent. */
function NewPassword({
  password,
  confirm,
  onPassword,
  onConfirm,
}: {
  password: string;
  confirm: string;
  onPassword: (v: string) => void;
  onConfirm: (v: string) => void;
}) {
  const { t } = useTranslation();
  return (
    <>
      <PasswordField
        label={t("passwordPages.newPassword")}
        kind="new"
        value={password}
        onChange={onPassword}
        hint={t("passwordPages.atLeast12CharactersA")}
      />
      <PasswordField
        label={t("passwordPages.repeatIt")}
        kind="new"
        value={confirm}
        onChange={onConfirm}
        hint={confirm && confirm !== password ? t("passwordPages.theTwoDoNotMatch") : undefined}
      />
    </>
  );
}

export function ResetPage() {
  const { t } = useTranslation();
  const [token] = useState(() => tokenFromHash());
  const [password, setPassword] = useState("");
  const [confirm, setConfirm] = useState("");
  const reset = useMutation({ mutationFn: () => call(api.POST("/auth/reset", { body: { token, password } })) });
  if (!token) {
    return (
      <AuthLayout title={t("passwordPages.thisLinkIsIncomplete")} sub={t("passwordPages.openTheLinkFromThe")}>
        <Link to="/forgot" className="link text-sm">
          {t("passwordPages.askForANewLink")}
        </Link>
      </AuthLayout>
    );
  }
  if (reset.isSuccess) {
    return (
      <AuthLayout title={t("passwordPages.passwordChanged")} sub={t("passwordPages.youWereSignedOutEverywhere")}>
        <Link to="/login" className="btn btn-primary w-full">
          {t("common.signIn")}
        </Link>
      </AuthLayout>
    );
  }
  return (
    <AuthLayout title={t("passwordPages.chooseANewPassword")} sub={t("passwordPages.everyDeviceSignedInTo")}>
      <form
        className="space-y-6"
        aria-labelledby="auth-title"
        onSubmit={(e: SubmitEvent) => {
          e.preventDefault();
          if (password === confirm) reset.mutate();
        }}
      >
        <ErrorBox error={reset.error} />
        <NewPassword password={password} confirm={confirm} onPassword={setPassword} onConfirm={setConfirm} />
        <button type="submit" className="btn btn-primary w-full" disabled={reset.isPending || password !== confirm}>
          {reset.isPending && <Loader2 className="h-4 w-4 animate-spin" aria-hidden />} {t("passwordPages.saveNewPassword")}
        </button>
      </form>
    </AuthLayout>
  );
}

export function RegisterPage() {
  const { t } = useTranslation();
  const [token] = useState(() => tokenFromHash());
  const invite = useQuery({ ...inviteQuery(token), enabled: !!token });
  const options = useQuery(authOptionsQuery);
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [confirm, setConfirm] = useState("");
  const navigate = useNavigate();
  const register = useMutation({
    mutationFn: () => call(api.POST("/auth/register", { body: token ? { token, password } : { email: email.trim(), password } })),
    meta: invalidates(keys.me),
    onSuccess: () => void navigate({ to: "/" }),
  });
  const inv = invite.data;
  const sub = inv
    ? inv.server_name
      ? t("passwordPages.invitedToServer", { server: inv.server_name })
      : t("passwordPages.invitedToAccount")
    : t("passwordPages.registerSub");
  return (
    <AuthLayout title={inv ? t("passwordPages.youAreInvited") : t("passwordPages.createAnAccount")} sub={sub}>
      {token && invite.isPending && (
        <Loader2 className="h-5 w-5 animate-spin text-zinc-400" aria-label={t("passwordPages.checkingTheInvitation")} />
      )}
      {token && invite.error ? (
        <>
          <ErrorBox error={invite.error} />
          <Link to="/login" className="link text-sm">
            {t("passwordPages.backToSignIn")}
          </Link>
        </>
      ) : (
        (!token || inv) && (
          <form
            className="space-y-6"
            aria-labelledby="auth-title"
            onSubmit={(e: SubmitEvent) => {
              e.preventDefault();
              if (password === confirm) register.mutate();
            }}
          >
            <ErrorBox error={register.error} />
            <Field label={t("common.email")} hint={inv ? t("passwordPages.theInvitationIsForThis") : undefined}>
              {(id) => (
                <input
                  id={id}
                  className="input"
                  type="email"
                  autoComplete="username"
                  required
                  readOnly={!!inv}
                  value={inv?.email ?? email}
                  onChange={(e) => setEmail(e.target.value)}
                />
              )}
            </Field>
            <NewPassword password={password} confirm={confirm} onPassword={setPassword} onConfirm={setConfirm} />
            <button type="submit" className="btn btn-primary w-full" disabled={register.isPending || password !== confirm}>
              {register.isPending && <Loader2 className="h-4 w-4 animate-spin" aria-hidden />} {t("passwordPages.createAccount")}
            </button>
            {(options.data?.sso.length ?? 0) > 0 && (
              <>
                <div className="flex items-center gap-3 text-xs text-zinc-500" aria-hidden>
                  <span className="h-px flex-1 bg-zinc-800" />
                  {t("passwordPages.orWithoutPassword")}
                  <span className="h-px flex-1 bg-zinc-800" />
                </div>
                <SSOButtons providers={options.data?.sso ?? []} query={token ? { invite: token } : {}} />
              </>
            )}
            <p className="text-center text-sm text-zinc-400">
              {t("passwordPages.alreadyHaveOne")}{" "}
              <Link to="/login" className="link">
                {t("common.signIn")}
              </Link>
            </p>
          </form>
        )
      )}
    </AuthLayout>
  );
}
