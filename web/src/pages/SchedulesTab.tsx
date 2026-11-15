import { useState, type FormEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Database, Loader2, Play, Plus, Power, Terminal, Trash2, X } from "lucide-react";
import { api, call } from "../api/client";
import { ErrorBox, Field } from "../components/ui";

type Action = "command" | "power" | "backup";
type TaskDraft = { action: Action; payload: string; offset_seconds: number; continue_on_failure: boolean };

const blankTask = (): TaskDraft => ({ action: "command", payload: "", offset_seconds: 0, continue_on_failure: false });
const when = (t: number) => (t ? new Date(t * 1000).toLocaleString(undefined, { timeZone: "UTC", dateStyle: "short", timeStyle: "short" }) : "-");
const pad = (n: string) => n.padStart(2, "0");
/** Plain words for the common "every day at hh:mm" shapes; anything else stays as cron. */
function humanCron(c: string): string {
  const m = c.trim().match(/^(\d{1,2}) (\d{1,2}) \* \* \*$/);
  if (m) return `Every day at ${pad(m[2]!)}:${pad(m[1]!)}`;
  const h = c.trim().match(/^(\d{1,2}) \* \* \* \*$/);
  if (h) return `Every hour at :${pad(h[1]!)}`;
  return c;
}

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

  const toggle = useMutation({
    mutationFn: (s: NonNullable<typeof list.data>[number]) =>
      call(api.PUT("/servers/{uuid}/schedules/{id}", {
        params: { path: { uuid, id: s.id } },
        body: {
          name: s.name, cron: s.cron, enabled: !s.enabled, only_when_online: s.only_when_online,
          tasks: s.tasks.map((t) => ({ action: t.action, payload: t.payload, offset_seconds: t.offset_seconds, continue_on_failure: t.continue_on_failure })),
        },
      })),
    onSuccess: refresh,
  });
  const ghost = "inline-flex h-9 w-9 items-center justify-center rounded-lg text-zinc-400 hover:bg-zinc-800 focus-visible:outline-2 focus-visible:outline-indigo-500";

  return (
    <div className="space-y-4">
      <section className="overflow-hidden rounded-2xl border border-zinc-800 bg-zinc-900" aria-labelledby="sc-list">
        <div className="flex items-center justify-between px-5 py-4">
          <h2 id="sc-list" className="text-[15px] font-semibold">{list.data ? `${list.data.length} ${list.data.length === 1 ? "schedule" : "schedules"}` : "Schedules"}</h2>
          <span className="text-[13px] text-zinc-500">All times are UTC</span>
        </div>
        <div className="px-5 empty:hidden"><ErrorBox error={list.error ?? remove.error ?? run.error ?? toggle.error} /></div>
        {list.data?.length === 0 && <p className="border-t border-[#1b2025] px-5 py-5 text-sm text-zinc-400">No schedules yet. Add one below to back up or restart on a timer.</p>}
        <ul>
          {list.data?.map((s) => (
            <li key={s.id} className="flex flex-wrap items-center gap-x-5 gap-y-3 border-t border-[#1b2025] px-5 py-4 hover:bg-[#161a1f]">
              <button
                type="button"
                role="switch"
                aria-checked={s.enabled}
                aria-label={`${s.name} enabled`}
                disabled={toggle.isPending}
                onClick={() => toggle.mutate(s)}
                className={`relative h-[22px] w-[38px] shrink-0 rounded-full transition-colors focus-visible:outline-2 focus-visible:outline-indigo-500 ${s.enabled ? "bg-[#2f7d4f]" : "bg-zinc-700"}`}
              >
                <span className={`absolute top-[3px] h-4 w-4 rounded-full bg-white transition-all ${s.enabled ? "right-[3px]" : "left-[3px]"}`} />
              </button>
              <div className="min-w-0 flex-1 basis-60">
                <div className="text-[15px] font-medium">{s.name}</div>
                <div className="mt-0.5 text-[13px] text-zinc-400">{humanCron(s.cron)}{s.only_when_online && " · only while running"}</div>
              </div>
              <ol className="flex flex-wrap items-center gap-1.5" aria-label="Tasks">
                {s.tasks.map((t) => (
                  <li key={t.id} className="inline-flex min-h-7 items-center gap-1.5 rounded-md border border-[#22272d] bg-[#101317] px-2.5 text-[12.5px] text-zinc-200">
                    {t.action === "backup" ? <Database className="h-3 w-3" aria-hidden /> : t.action === "power" ? <Power className="h-3 w-3" aria-hidden /> : <Terminal className="h-3 w-3" aria-hidden />}
                    {t.action === "backup" ? "Back up" : t.action === "power" ? `Power: ${t.payload}` : `Command: ${t.payload}`}
                    {t.offset_seconds > 0 && <span className="text-zinc-500">after {t.offset_seconds}s</span>}
                  </li>
                ))}
              </ol>
              <div className="min-w-32 text-right text-[12.5px] text-zinc-500">
                <div>Next run</div>
                <div className="mt-0.5 font-mono text-[13px] text-zinc-100">{s.enabled ? when(s.next_run) : "paused"}</div>
                {s.last_run > 0 && <div className="mt-0.5">Last: {when(s.last_run)}{s.last_status && ` · ${s.last_status}`}</div>}
              </div>
              <div className="flex items-center gap-1">
                <button type="button" className="btn !min-h-9" onClick={() => run.mutate(s.id)} disabled={run.isPending} aria-label={`Run ${s.name} now`}>
                  <Play className="h-4 w-4" aria-hidden /> Run now
                </button>
                <button type="button" className={`${ghost} hover:!text-[#ff8a7a]`} onClick={() => window.confirm(`Delete schedule "${s.name}"?`) && remove.mutate(s.id)} aria-label={`Delete ${s.name}`} title="Delete">
                  <Trash2 className="h-4 w-4" aria-hidden />
                </button>
              </div>
            </li>
          ))}
        </ul>
      </section>
      <p className="text-[12.5px] text-zinc-500">A schedule runs its tasks in order. Use the switch to pause one without deleting it.</p>

      <details className="group rounded-2xl border border-zinc-800 bg-zinc-900 open:p-0">
        <summary className="flex cursor-pointer list-none items-center gap-2 px-5 py-4 text-[15px] font-semibold focus-visible:outline-2 focus-visible:outline-indigo-500 [&::-webkit-details-marker]:hidden"><Plus className="h-4 w-4" aria-hidden /> New schedule</summary>
      <form onSubmit={submit} className="space-y-3 border-t border-[#1b2025] p-5" aria-label="New schedule">
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
      </details>
    </div>
  );
}
