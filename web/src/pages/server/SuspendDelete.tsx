import { useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { Ban } from "lucide-react";
import { useTranslation } from "react-i18next";
import { api, call, type components } from "../../api/client";
import { invalidates, keys } from "../../api/queries";
import { ErrorBox, Field, FormDialog, Section } from "../../components/ui";

type ServerView = components["schemas"]["ServerView"];

/** Suspending stops the server and locks its owner out until it is lifted; nothing is deleted. */
export function SuspendSection({ server }: { server: ServerView }) {
  const { t } = useTranslation();
  const [confirm, setConfirm] = useState(false);
  const change = useMutation({
    mutationFn: () =>
      call(
        server.suspended
          ? api.POST("/servers/{uuid}/unsuspend", { params: { path: { uuid: server.uuid } } })
          : api.POST("/servers/{uuid}/suspend", { params: { path: { uuid: server.uuid } } }),
      ),
    meta: invalidates(keys.server(server.uuid), keys.servers),
    onSuccess: () => setConfirm(false),
  });
  return (
    <>
      <Section
        title={t("settingsTab.suspension")}
        desc={server.suspended ? t("settingsTab.thisServerIsSuspendedLifting") : t("settingsTab.stopsTheServerAndKeeps")}
        footer={
          <>
            <span>{server.suspended ? t("settingsTab.suspended") : t("settingsTab.billingSystemsDoThisThrough")}</span>
            <button
              type="button"
              className={server.suspended ? "btn btn-primary" : "btn"}
              disabled={change.isPending}
              onClick={() => (server.suspended ? change.mutate() : setConfirm(true))}
            >
              <Ban className="h-4 w-4" aria-hidden /> {server.suspended ? t("settingsTab.liftSuspension") : t("settingsTab.suspendServer")}
            </button>
          </>
        }
      >
        {server.suspended && change.error ? <ErrorBox error={change.error} /> : null}
      </Section>
      <FormDialog
        open={confirm}
        title={t("settingsTab.suspendName", { name: server.name })}
        submitLabel={t("settingsTab.suspendServer")}
        pending={change.isPending}
        error={change.error}
        onSubmit={() => change.mutate()}
        onClose={() => {
          setConfirm(false);
          change.reset();
        }}
      >
        <p>{t("settingsTab.theServerIsStoppedNow")}</p>
      </FormDialog>
    </>
  );
}

/** Deleting asks for the server's name, so it cannot happen by a stray click. */
export function DeleteDialog({
  open,
  name,
  offline,
  pending,
  error,
  onConfirm,
  onCancel,
}: {
  open: boolean;
  name: string;
  offline: boolean;
  pending: boolean;
  error: unknown;
  onConfirm: () => void;
  onCancel: () => void;
}) {
  const { t } = useTranslation();
  const [typed, setTyped] = useState("");
  const close = () => {
    setTyped("");
    onCancel();
  };
  return (
    <FormDialog
      open={open}
      danger
      title={t("settingsTab.deleteName", { name })}
      submitLabel={t("settingsTab.deleteServer")}
      pending={pending}
      error={error}
      disabled={typed.trim() !== name.trim()}
      onSubmit={onConfirm}
      onClose={close}
    >
      <p>{t("settingsTab.thisPermanentlyDeletesTheServer")}</p>
      {offline && (
        <p className="rounded-lg border border-warn/30 bg-warn-bg px-3 py-2 text-sm text-warn-text">
          {t("settingsTab.itsNodeIsOfflineThe")}
        </p>
      )}
      <Field label={t("settingsTab.typeNameToConfirm", { name })}>
        {(id) => (
          <input
            id={id}
            className="input"
            autoComplete="off"
            spellCheck={false}
            value={typed}
            onChange={(e) => setTyped(e.target.value)}
            autoFocus
          />
        )}
      </Field>
    </FormDialog>
  );
}
