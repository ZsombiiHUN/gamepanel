import { useState, type FormEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Loader2, Play, Plus, Trash2, X } from "lucide-react";
import { api, call } from "../api/client";
import { ErrorBox, Field } from "../components/ui";

type Action = "command" | "power" | "backup";
type TaskDraft = { action: Action; payload: string; offset_seconds: number; continue_on_failure: boolean };

const blankTask = (): TaskDraft => ({ action: "command", payload: "", offset_seconds: 0, continue_on_failure: false });
const when = (t: number) => (t ? new Date(t * 1000).toLocaleString() : "-");

export function SchedulesTab({ uuid }: { uuid: string }) {
  const qc = useQueryClient();
  const key = ["schedules", uuid];
  const list = useQuery({ queryKey: key, queryFn: () => call(api.GET("/servers/{uuid}/schedules", { params: { path: { uuid } } })) });
  const refresh = () => qc.invalidateQueries({ queryKey: key });

  const [name, setName] = useState("");
  const [cron, setCron] = useState("0 4 * * *");
  const [onlyOnline, setOnlyOnline] = useState(false);
  const [tasks, setTasks] = useState<TaskDraft[]>([{ ...blankTask(), action: "backup" }]);
  const patch = (i: number, p: Partial<TaskDraft>) => setTasks((ts) => ts.map((t, j) => (j === i ? { ...t, ...p } : t)));

  const create = useMutation({
    mutationFn: () =>
      call(api.POST("/servers/{uuid}/schedules", {
        params: { path: { uuid } },
        body: {
          name, cron, enabled: true, only_when_online: onlyOnline,
          // Backup patterns are typed comma separated; the server wants one per line.
          tasks: tasks.map((t) => (t.action === "backup" ? { ...t, payload: t.payload.split(",").map((x) => x.trim()).filter(Boolean).join("\n") } : t)),
        },
      })),
    onSuccess: () => {
      setName("");
      return refresh();
    },
  });
  const remove = useMutation({
    mutationFn: (id: number) => call(api.DELETE("/servers/{uuid}/schedules/{id}", { params: { path: { uuid, id } } })),
    onSuccess: refresh,
  });
  const run = useMutation({
    mutationFn: (id: number) => call(api.POST("/servers/{uuid}/schedules/{id}/run", { params: { path: { uuid, id } } })),
    // Tasks run in the background; look again shortly for the result.
    onSuccess: () => setTimeout(() => void refresh(), 2500),
  });

  const submit = (e: FormEvent) => {
    e.preventDefault();
    create.mutate();
  };

  return (
    <div className="space-y-4">
      <div className="card space-y-3" aria-labelledby="sc-list">
        <h2 id="sc-list" className="text-lg font-medium">Schedules</h2>
        <ErrorBox error={list.error ?? remove.error ?? run.error} />
        {list.data?.length === 0 && <p className="text-sm text-zinc-400">No schedules yet.</p>}
        {list.data?.map((s) => (
          <div key={s.id} className="rounded-md border border-zinc-800 p-3 text-sm">
            <div className="flex flex-wrap items-center gap-2">
              <strong>{s.name}</strong>
              <code className="text-zinc-400">{s.cron} (UTC)</code>
              {!s.enabled && <span className="text-amber-400">disabled</span>}
              <span className="ml-auto flex gap-2">
                <button type="button" className="btn" onClick={() => run.mutate(s.id)} disabled={run.isPending} aria-label={`Run ${s.name} now`} title="Run now">
                  <Play className="h-4 w-4" aria-hidden />
                </button>
                <button type="button" className="btn btn-danger" onClick={() => window.confirm(`Delete schedule "${s.name}"?`) && remove.mutate(s.id)} aria-label={`Delete ${s.name}`}>
                  <Trash2 className="h-4 w-4" aria-hidden />
                </button>
              </span>
            </div>
            <ol className="mt-2 list-decimal pl-5 text-zinc-300">
              {s.tasks.map((t) => (
                <li key={t.id}>
                  {t.action}: <code>{t.action === "backup" ? (t.payload ? `leave out ${t.payload.split("\n").join(", ")}` : "(everything)") : t.payload}</code>
                  {t.offset_seconds > 0 && ` after ${t.offset_seconds}s`}
                  {t.continue_on_failure && " (continues on failure)"}
                </li>
              ))}
            </ol>
            <p className="mt-2 text-xs text-zinc-500">
              Next: {when(s.next_run)} · Last: {when(s.last_run)} {s.last_status && `· ${s.last_status}`}
            </p>
          </div>
        ))}
      </div>

      <form onSubmit={submit} className="card space-y-3" aria-labelledby="sc-new">
        <h2 id="sc-new" className="text-lg font-medium">New schedule</h2>
        <div className="flex flex-wrap items-end gap-3">
          <Field label="Name">
            {(id) => <input id={id} className="input" required maxLength={64} value={name} onChange={(e) => setName(e.target.value)} />}
          </Field>
          <Field label="Cron (UTC)" hint="minute hour day-of-month month day-of-week. Example: 0 4 * * * = every day at 04:00">
            {(id) => <input id={id} className="input font-mono" required value={cron} onChange={(e) => setCron(e.target.value)} />}
          </Field>
        </div>
        <label className="flex items-center gap-2 text-sm text-zinc-300">
          <input type="checkbox" checked={onlyOnline} onChange={(e) => setOnlyOnline(e.target.checked)} /> Only run while the server is running
        </label>
        <fieldset className="space-y-2">
          <legend className="text-sm font-medium">Tasks (run in order; a failure stops the schedule unless marked to continue)</legend>
          {tasks.map((t, i) => (
            <div key={i} className="flex flex-wrap items-end gap-2">
              <Field label="Action">
                {(id) => (
                  <select id={id} className="input" value={t.action} onChange={(e) => patch(i, { action: e.target.value as Action, payload: e.target.value === "power" ? "restart" : "" })}>
                    <option value="command">Send command</option>
                    <option value="power">Power</option>
                    <option value="backup">Backup</option>
                  </select>
                )}
              </Field>
              {t.action === "power" ? (
                <Field label="Power">
                  {(id) => (
                    <select id={id} className="input" value={t.payload} onChange={(e) => patch(i, { payload: e.target.value })}>
                      {["start", "stop", "restart", "kill"].map((a) => <option key={a}>{a}</option>)}
                    </select>
                  )}
                </Field>
              ) : (
                <Field label={t.action === "command" ? "Command" : "Leave out (comma separated, e.g. logs, *.tmp)"}>
                  {(id) => <input id={id} className="input" required={t.action === "command"} maxLength={1000} value={t.payload} onChange={(e) => patch(i, { payload: e.target.value })} />}
                </Field>
              )}
              <Field label="Wait (s)">
                {(id) => <input id={id} className="input w-24" type="number" min={0} max={900} value={t.offset_seconds} onChange={(e) => patch(i, { offset_seconds: Number(e.target.value) })} />}
              </Field>
              <label className="flex items-center gap-1 pb-2 text-sm text-zinc-300">
                <input type="checkbox" checked={t.continue_on_failure} onChange={(e) => patch(i, { continue_on_failure: e.target.checked })} /> continue on failure
              </label>
              <button type="button" className="btn mb-1" aria-label={`Remove task ${i + 1}`} onClick={() => setTasks((ts) => ts.filter((_, j) => j !== i))} disabled={tasks.length === 1}>
                <X className="h-4 w-4" aria-hidden />
              </button>
            </div>
          ))}
          <button type="button" className="btn" onClick={() => setTasks((ts) => [...ts, blankTask()])} disabled={tasks.length >= 20}>
            <Plus className="h-4 w-4" aria-hidden /> Add task
          </button>
        </fieldset>
        <ErrorBox error={create.error} />
        <button type="submit" className="btn btn-primary" disabled={create.isPending}>
          {create.isPending && <Loader2 className="h-4 w-4 animate-spin" aria-hidden />} Create schedule
        </button>
      </form>
    </div>
  );
}
