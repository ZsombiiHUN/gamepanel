import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Plus, Star, Trash2 } from "lucide-react";
import { api, call, type components } from "../api/client";
import { invalidates, keys, networkQuery } from "../api/queries";
import { ErrorBox, FormDialog, Field, Pill, Section, Spinner } from "../components/ui";

type NetworkView = components["schemas"]["NetworkView"];
type ServerPort = components["schemas"]["ServerPort"];

export function NetworkTab({ uuid }: { uuid: string }) {
  const net = useQuery(networkQuery(uuid));
  if (net.isPending) return <Spinner />;
  if (net.error) return <ErrorBox error={net.error} />;
  return <Ports uuid={uuid} n={net.data} />;
}

function Ports({ uuid, n }: { uuid: string; n: NetworkView }) {
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
        <p role="status" className="rounded-xl border border-amber-500/30 bg-amber-500/10 px-4 py-3 text-sm text-amber-200">
          Saved. The server picks up port changes the next time it starts, so restart it when it suits you.
        </p>
      )}
      <Section
        title="Ports"
        desc="Ports players and tools connect to. The primary port is the one the game listens on (SERVER_PORT); extra ones suit query, RCON, voice or map plugins. Each port is open for TCP and UDP."
        footer={
          n.can_manage ? (
            <>
              <span>
                {n.can_pick
                  ? `As an administrator you can add any free port from ${n.range_from} to ${n.range_to}.`
                  : `${extra} of ${n.limit} extra ${n.limit === 1 ? "port" : "ports"} used.`}
                {full && n.limit === 0 ? " Ask an administrator if you need more." : ""}
              </span>
              <button
                type="button"
                className="btn btn-primary"
                disabled={full || add.isPending}
                onClick={() => (n.can_pick ? setAdding(true) : add.mutate({}))}
              >
                <Plus className="h-4 w-4" aria-hidden /> Add port
              </button>
            </>
          ) : (
            <span>You can see these ports but not change them.</span>
          )
        }
      >
        <ul className="divide-y divide-zinc-800">
          {n.ports.map((p) => (
            <PortRow
              key={p.port}
              p={p}
              canManage={n.can_manage}
              pending={update.isPending || remove.isPending}
              onNotes={(notes) => update.mutate({ port: p.port, notes })}
              onPrimary={() => update.mutate({ port: p.port, primary: true })}
              onRemove={() => {
                if (window.confirm(`Remove port ${p.port}? Anything using it stops working.`)) remove.mutate(p.port);
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
  const [notes, setNotes] = useState<string | null>(null);
  const shown = notes ?? p.notes;
  return (
    <li className="flex flex-wrap items-center gap-x-4 gap-y-2 py-3 first:pt-0 last:pb-0">
      <span className="w-20 shrink-0 font-mono text-[15px] text-zinc-100">{p.port}</span>
      <span className="w-20 shrink-0">{p.primary ? <Pill tone="good">Primary</Pill> : <Pill>Extra</Pill>}</span>
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
            Notes for port {p.port}
          </label>
          <input
            id={`notes-${p.port}`}
            className="input !min-h-9"
            placeholder="What uses this port? (e.g. Dynmap)"
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
            <Star className="h-4 w-4" aria-hidden /> Make primary
          </button>
          <button
            type="button"
            className="inline-flex h-9 w-9 items-center justify-center rounded-md text-zinc-500 hover:bg-zinc-800 hover:text-red-300 focus-visible:outline-2 focus-visible:outline-indigo-500"
            disabled={pending}
            onClick={onRemove}
            aria-label={`Remove port ${p.port}`}
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
  const [port, setPort] = useState("");
  const [notes, setNotes] = useState("");
  const num = Number(port);
  const bad = port !== "" && (!Number.isInteger(num) || num < n.range_from || num > n.range_to);
  return (
    <FormDialog
      open
      title="Add a port"
      submitLabel="Add port"
      pending={pending}
      error={error}
      disabled={bad}
      onSubmit={() => onAdd({ port: port ? num : undefined, notes: notes.trim() || undefined })}
      onClose={onClose}
    >
      <Field label="Port" hint={bad ? `Choose a port from ${n.range_from} to ${n.range_to}.` : "Leave empty for the next free one."}>
        {(id) => (
          <input
            id={id}
            className="input font-mono"
            inputMode="numeric"
            value={port}
            onChange={(e) => setPort(e.target.value.replace(/\D/g, ""))}
            autoFocus
            placeholder={`${n.range_from}-${n.range_to}`}
          />
        )}
      </Field>
      <Field label="Notes (optional)">
        {(id) => (
          <input id={id} className="input" maxLength={255} value={notes} onChange={(e) => setNotes(e.target.value)} placeholder="Dynmap" />
        )}
      </Field>
    </FormDialog>
  );
}
