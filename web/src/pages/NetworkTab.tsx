import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Plus, Star, Trash2 } from "lucide-react";
import { api, call, type components } from "../api/client";
import { invalidates, keys, networkQuery } from "../api/queries";
import { ErrorBox, FormDialog, Field, Pill, Section, Spinner } from "../components/ui";
import { useTranslation } from "react-i18next";

type NetworkView = components["schemas"]["NetworkView"];
type ServerPort = components["schemas"]["ServerPort"];

export function NetworkTab({ uuid }: { uuid: string }) {
  const net = useQuery(networkQuery(uuid));
  if (net.isPending) return <Spinner />;
  if (net.error) return <ErrorBox error={net.error} />;
  return <Ports uuid={uuid} n={net.data} />;
}

function Ports({ uuid, n }: { uuid: string; n: NetworkView }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  // Every change answers with the new list; the server's own port (in its header) may change too.
  const done = (data: NetworkView) => qc.setQueryData(keys.network(uuid), data);
  const add = useMutation({
    mutationFn: (body: { port?: number; notes?: string }) =>
      call(api.POST("/servers/{uuid}/network", { params: { path: { uuid } }, body })),
    onSuccess: done,
  });
  const update = useMutation({
    mutationFn: ({ port, ...body }: { port: number; notes?: string; primary?: boolean }) =>
      call(api.PUT("/servers/{uuid}/network/{port}", { params: { path: { uuid, port } }, body })),
    onSuccess: done,
    meta: invalidates(keys.server(uuid), keys.servers),
  });
  const remove = useMutation({
    mutationFn: (port: number) => call(api.DELETE("/servers/{uuid}/network/{port}", { params: { path: { uuid, port } } })),
    onSuccess: done,
  });
  const [adding, setAdding] = useState(false);
  const extra = n.ports.length - 1;
  const full = !n.can_pick && extra >= n.limit;
  const error = add.error ?? update.error ?? remove.error;

  return (
    <div className="space-y-5">
      {n.pending && (
        <p role="status" className="rounded-xl border border-warn/30 bg-warn-bg px-4 py-3 text-sm text-warn-text">
          {t("networkTab.savedTheServerPicksUp")}
        </p>
      )}
      <Section
        title={t("networkTab.ports")}
        desc={t("networkTab.portsPlayersAndToolsConnect")}
        footer={
          n.can_manage ? (
            <>
              <span>
                {n.can_pick
                  ? t("networkTab.asAnAdministratorYouCan", { range_from: n.range_from, range_to: n.range_to })
                  : t("networkTab.used", { used: extra, count: n.limit })}
                {full && n.limit === 0 ? ` ${t("networkTab.askAnAdministratorIfYou")}` : ""}
              </span>
              <button
                type="button"
                className="btn btn-primary"
                disabled={full || add.isPending}
                onClick={() => (n.can_pick ? setAdding(true) : add.mutate({}))}
              >
                <Plus className="h-4 w-4" aria-hidden /> {t("networkTab.addPort")}
              </button>
            </>
          ) : (
            <span>{t("networkTab.youCanSeeThesePorts")}</span>
          )
        }
      >
        <ul className="divide-y divide-line">
          {n.ports.map((p) => (
            <PortRow
              key={p.port}
              p={p}
              canManage={n.can_manage}
              pending={update.isPending || remove.isPending}
              onNotes={(notes) => update.mutate({ port: p.port, notes })}
              onPrimary={() => update.mutate({ port: p.port, primary: true })}
              onRemove={() => {
                if (window.confirm(t("networkTab.confirmRemove", { port: p.port }))) remove.mutate(p.port);
              }}
            />
          ))}
        </ul>
        {error ? (
          <div className="mt-3">
            <ErrorBox error={error} />
          </div>
        ) : null}
      </Section>
      {adding && (
        <AddPortDialog
          n={n}
          pending={add.isPending}
          error={add.error}
          onAdd={(body) => add.mutate(body, { onSuccess: () => setAdding(false) })}
          onClose={() => {
            setAdding(false);
            add.reset();
          }}
        />
      )}
    </div>
  );
}

function PortRow({
  p,
  canManage,
  pending,
  onNotes,
  onPrimary,
  onRemove,
}: {
  p: ServerPort;
  canManage: boolean;
  pending: boolean;
  onNotes: (n: string) => void;
  onPrimary: () => void;
  onRemove: () => void;
}) {
  const { t } = useTranslation();
  const [notes, setNotes] = useState<string | null>(null);
  const shown = notes ?? p.notes;
  return (
    <li className="flex flex-wrap items-center gap-x-4 gap-y-2 py-3 first:pt-0 last:pb-0">
      <span className="w-20 shrink-0 font-mono text-[15px] text-zinc-100">{p.port}</span>
      <span className="w-20 shrink-0">
        {p.primary ? <Pill tone="good">{t("networkTab.primary")}</Pill> : <Pill>{t("networkTab.extra")}</Pill>}
      </span>
      {canManage ? (
        <form
          className="min-w-40 flex-1"
          onSubmit={(e) => {
            e.preventDefault();
            onNotes(shown.trim());
            setNotes(null);
          }}
        >
          <label htmlFor={`notes-${p.port}`} className="sr-only">
            {t("networkTab.notesFor", { port: p.port })}
          </label>
          <input
            id={`notes-${p.port}`}
            className="input !min-h-9"
            placeholder={t("networkTab.whatUsesThisPortE")}
            maxLength={255}
            value={shown}
            onChange={(e) => setNotes(e.target.value)}
            onBlur={() => {
              if (notes !== null && notes.trim() !== p.notes) onNotes(notes.trim());
              setNotes(null);
            }}
          />
        </form>
      ) : (
        <span className="min-w-40 flex-1 text-sm text-zinc-400">{p.notes || "-"}</span>
      )}
      {canManage && !p.primary && (
        <span className="flex gap-1">
          <button type="button" className="btn btn-ghost !min-h-9 !px-3" disabled={pending} onClick={onPrimary}>
            <Star className="h-4 w-4" aria-hidden /> {t("networkTab.makePrimary")}
          </button>
          <button
            type="button"
            className="inline-flex h-9 w-9 items-center justify-center rounded-md text-zinc-500 hover:bg-row-hover hover:text-bad-text focus-visible:outline-2 focus-visible:outline-indigo-500"
            disabled={pending}
            onClick={onRemove}
            aria-label={t("networkTab.removePortPort", { port: p.port })}
          >
            <Trash2 className="h-4 w-4" aria-hidden />
          </button>
        </span>
      )}
    </li>
  );
}

function AddPortDialog({
  n,
  pending,
  error,
  onAdd,
  onClose,
}: {
  n: NetworkView;
  pending: boolean;
  error: unknown;
  onAdd: (b: { port?: number; notes?: string }) => void;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const [port, setPort] = useState("");
  const [notes, setNotes] = useState("");
  const num = Number(port);
  const bad = port !== "" && (!Number.isInteger(num) || num < n.range_from || num > n.range_to);
  return (
    <FormDialog
      open
      title={t("networkTab.addAPort")}
      submitLabel={t("networkTab.addPort")}
      pending={pending}
      error={error}
      disabled={bad}
      onSubmit={() => onAdd({ port: port ? num : undefined, notes: notes.trim() || undefined })}
      onClose={onClose}
    >
      <Field
        label={t("common.port")}
        hint={
          bad
            ? t("networkTab.chooseAPortFromRange", { range_from: n.range_from, range_to: n.range_to })
            : t("networkTab.leaveEmptyForTheNext")
        }
      >
        {(id) => (
          <input
            id={id}
            className="input font-mono"
            inputMode="numeric"
            value={port}
            onChange={(e) => setPort(e.target.value.replace(/\D/g, ""))}
            autoFocus
            placeholder={t("networkTab.rangeFromRangeTo", { range_from: n.range_from, range_to: n.range_to })}
          />
        )}
      </Field>
      <Field label={t("networkTab.notesOptional")}>
        {(id) => (
          <input
            id={id}
            className="input"
            maxLength={255}
            value={notes}
            onChange={(e) => setNotes(e.target.value)}
            placeholder={t("networkTab.dynmap")}
          />
        )}
      </Field>
    </FormDialog>
  );
}
