import { useState, type SubmitEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Database, Loader2, Play, Plus, Power, Terminal, Trash2, X } from "lucide-react";
import { api, call } from "../api/client";
import { invalidates, keys, refreshLater, schedulesQuery } from "../api/queries";
import { ErrorBox, Field } from "../components/ui";
import { useTranslation } from "react-i18next";
import i18n from "../i18n";

type Action = "command" | "power" | "backup";
interface TaskDraft {
  action: Action;
  payload: string;
  offset_seconds: number;
  continue_on_failure: boolean;
}

const blankTask = (): TaskDraft => ({ action: "command", payload: "", offset_seconds: 0, continue_on_failure: false });
const when = (secs: number) =>
  secs ? new Date(secs * 1000).toLocaleString(undefined, { timeZone: "UTC", dateStyle: "short", timeStyle: "short" }) : "-";
const pad = (n: string) => n.padStart(2, "0");
/** Plain words for the common "every day at hh:mm" shapes; anything else stays as cron. */
function humanCron(c: string): string {
  const m = /^(\d{1,2}) (\d{1,2}) \* \* \*$/.exec(c.trim());
  if (m?.[1] && m[2]) return i18n.t("schedulesTab.everyDayAt", { time: `${pad(m[2])}:${pad(m[1])}` });
  const h = /^(\d{1,2}) \* \* \* \*$/.exec(c.trim());
  if (h?.[1]) return i18n.t("schedulesTab.everyHourAt", { minute: pad(h[1]) });
  return c;
}

export function SchedulesTab({ uuid }: { uuid: string }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const list = useQuery(schedulesQuery(uuid));
  const meta = invalidates(keys.schedules(uuid));

  const [name, setName] = useState("");
  const [cron, setCron] = useState("0 4 * * *");
  const [onlyOnline, setOnlyOnline] = useState(false);
  const [tasks, setTasks] = useState<TaskDraft[]>([{ ...blankTask(), action: "backup" }]);
  const patch = (i: number, p: Partial<TaskDraft>) => setTasks((ts) => ts.map((task, j) => (j === i ? { ...task, ...p } : task)));

  const create = useMutation({
    mutationFn: () =>
      call(
        api.POST("/servers/{uuid}/schedules", {
          params: { path: { uuid } },
          body: {
            name,
            cron,
            enabled: true,
            only_when_online: onlyOnline,
            // Backup patterns are typed comma separated; the server wants one per line.
            tasks: tasks.map((task) =>
              task.action === "backup"
                ? {
                    ...task,
                    payload: task.payload
                      .split(",")
                      .map((x) => x.trim())
                      .filter(Boolean)
                      .join("\n"),
                  }
                : task,
            ),
          },
        }),
      ),
    meta,
    onSuccess: () => setName(""),
  });
  const remove = useMutation({
    mutationFn: (id: number) => call(api.DELETE("/servers/{uuid}/schedules/{id}", { params: { path: { uuid, id } } })),
    meta,
  });
  const run = useMutation({
    mutationFn: (id: number) => call(api.POST("/servers/{uuid}/schedules/{id}/run", { params: { path: { uuid, id } } })),
    // Tasks run in the background; look again shortly for the result.
    onSuccess: () => refreshLater(qc, keys.schedules(uuid), 2500),
  });

  const submit = (e: SubmitEvent) => {
    e.preventDefault();
    create.mutate();
  };

  const toggle = useMutation({
    mutationFn: (s: NonNullable<typeof list.data>[number]) =>
      call(
        api.PUT("/servers/{uuid}/schedules/{id}", {
          params: { path: { uuid, id: s.id } },
          body: {
            name: s.name,
            cron: s.cron,
            enabled: !s.enabled,
            only_when_online: s.only_when_online,
            tasks: s.tasks.map((task) => ({
              action: task.action,
              payload: task.payload,
              offset_seconds: task.offset_seconds,
              continue_on_failure: task.continue_on_failure,
            })),
          },
        }),
      ),
    meta,
  });
  const ghost =
    "inline-flex h-9 w-9 items-center justify-center rounded-lg text-zinc-400 hover:bg-row-hover focus-visible:outline-2 focus-visible:outline-indigo-500";

  return (
    <div className="space-y-4">
      <section className="overflow-hidden rounded-xl border border-line bg-zinc-900" aria-labelledby="sc-list">
        <div className="flex items-center justify-between px-5 py-4">
          <h2 id="sc-list" className="text-[15px] font-semibold">
            {list.data ? t("schedulesTab.count", { count: list.data.length }) : t("schedulesTab.schedules")}
          </h2>
          <span className="text-[13px] text-zinc-500">{t("schedulesTab.allTimesAreUtc")}</span>
        </div>
        <div className="px-5 empty:hidden">
          <ErrorBox error={list.error ?? remove.error ?? run.error ?? toggle.error} />
        </div>
        {list.data?.length === 0 && (
          <p className="border-t border-line px-5 py-5 text-sm text-zinc-400">{t("schedulesTab.noSchedulesYetAddOne")}</p>
        )}
        <ul>
          {list.data?.map((s) => (
            <li key={s.id} className="flex flex-wrap items-center gap-x-5 gap-y-3 border-t border-line px-5 py-4 hover:bg-row-hover">
              <button
                type="button"
                role="switch"
                aria-checked={s.enabled}
                aria-label={t("schedulesTab.nameEnabled", { name: s.name })}
                disabled={toggle.isPending}
                onClick={() => toggle.mutate(s)}
                className={`relative h-[22px] w-[38px] shrink-0 rounded-full transition-colors focus-visible:outline-2 focus-visible:outline-indigo-500 ${s.enabled ? "bg-good-dim" : "bg-zinc-700"}`}
              >
                <span
                  className={`absolute top-[3px] h-4 w-4 rounded-full bg-white transition-all ${s.enabled ? "right-[3px]" : "left-[3px]"}`}
                />
              </button>
              <div className="min-w-0 flex-1 basis-60">
                <div className="text-[15px] font-medium">{s.name}</div>
                <div className="mt-0.5 text-[13px] text-zinc-400">
                  {humanCron(s.cron)}
                  {s.only_when_online && ` · ${t("schedulesTab.onlyWhileRunning")}`}
                </div>
              </div>
              <ol className="flex flex-wrap items-center gap-1.5" aria-label={t("schedulesTab.tasks")}>
                {s.tasks.map((task) => (
                  <li
                    key={task.id}
                    className="inline-flex min-h-7 items-center gap-1.5 rounded-md border border-line-strong bg-thead px-2.5 text-[12.5px] text-zinc-200"
                  >
                    {task.action === "backup" ? (
                      <Database className="h-3 w-3" aria-hidden />
                    ) : task.action === "power" ? (
                      <Power className="h-3 w-3" aria-hidden />
                    ) : (
                      <Terminal className="h-3 w-3" aria-hidden />
                    )}
                    {task.action === "backup"
                      ? t("schedulesTab.taskBackup")
                      : task.action === "power"
                        ? t("schedulesTab.taskPower", { action: task.payload })
                        : t("schedulesTab.taskCommand", { command: task.payload })}
                    {task.offset_seconds > 0 && (
                      <span className="text-zinc-500">{t("schedulesTab.afterSeconds", { seconds: task.offset_seconds })}</span>
                    )}
                  </li>
                ))}
              </ol>
              <div className="min-w-32 text-right text-[12.5px] text-zinc-500">
                <div>{t("schedulesTab.nextRun")}</div>
                <div className="mt-0.5 font-mono text-[13px] text-zinc-100">{s.enabled ? when(s.next_run) : t("schedulesTab.paused")}</div>
                {s.last_run > 0 && (
                  <div className="mt-0.5">
                    {t("schedulesTab.lastRun", { when: when(s.last_run) })}
                    {s.last_status && ` · ${s.last_status}`}
                  </div>
                )}
              </div>
              <div className="flex items-center gap-1">
                <button
                  type="button"
                  className="btn !min-h-9"
                  onClick={() => run.mutate(s.id)}
                  disabled={run.isPending}
                  aria-label={t("schedulesTab.runNameNow", { name: s.name })}
                >
                  <Play className="h-4 w-4" aria-hidden /> {t("schedulesTab.runNow")}
                </button>
                <button
                  type="button"
                  className={`${ghost} hover:!text-bad-text`}
                  onClick={() => window.confirm(t("schedulesTab.confirmDelete", { name: s.name })) && remove.mutate(s.id)}
                  aria-label={t("schedulesTab.deleteName", { name: s.name })}
                  title={t("common.delete")}
                >
                  <Trash2 className="h-4 w-4" aria-hidden />
                </button>
              </div>
            </li>
          ))}
        </ul>
      </section>
      <p className="text-[12.5px] text-zinc-500">{t("schedulesTab.aScheduleRunsItsTasks")}</p>

      <details className="group rounded-xl border border-line bg-zinc-900 open:p-0">
        <summary className="flex cursor-pointer list-none items-center gap-2 px-5 py-4 text-[15px] font-semibold focus-visible:outline-2 focus-visible:outline-indigo-500 [&::-webkit-details-marker]:hidden">
          <Plus className="h-4 w-4" aria-hidden /> {t("schedulesTab.newSchedule")}
        </summary>
        <form onSubmit={submit} className="space-y-3 border-t border-line p-5" aria-label={t("schedulesTab.newSchedule")}>
          <div className="flex flex-wrap items-end gap-3">
            <Field label={t("common.name")}>
              {(id) => <input id={id} className="input" required maxLength={64} value={name} onChange={(e) => setName(e.target.value)} />}
            </Field>
            <Field label={t("schedulesTab.cronUtc")} hint={t("schedulesTab.minuteHourDayOfMonth")}>
              {(id) => <input id={id} className="input font-mono" required value={cron} onChange={(e) => setCron(e.target.value)} />}
            </Field>
          </div>
          <label className="flex items-center gap-2 text-sm text-zinc-300">
            <input type="checkbox" checked={onlyOnline} onChange={(e) => setOnlyOnline(e.target.checked)} />{" "}
            {t("schedulesTab.onlyRunWhileTheServer")}
          </label>
          <fieldset className="space-y-2">
            <legend className="text-sm font-medium">{t("schedulesTab.tasksRunInOrderA")}</legend>
            {tasks.map((task, i) => (
              <div key={i} className="flex flex-wrap items-end gap-2">
                <Field label={t("schedulesTab.action")}>
                  {(id) => (
                    <select
                      id={id}
                      className="input"
                      value={task.action}
                      onChange={(e) => patch(i, { action: e.target.value as Action, payload: e.target.value === "power" ? "restart" : "" })}
                    >
                      <option value="command">{t("schedulesTab.actionCommand")}</option>
                      <option value="power">{t("schedulesTab.actionPower")}</option>
                      <option value="backup">{t("schedulesTab.actionBackup")}</option>
                    </select>
                  )}
                </Field>
                {task.action === "power" ? (
                  <Field label={t("schedulesTab.actionPower")}>
                    {(id) => (
                      <select id={id} className="input" value={task.payload} onChange={(e) => patch(i, { payload: e.target.value })}>
                        {(["start", "stop", "restart", "kill"] as const).map((a) => (
                          <option key={a} value={a}>
                            {t(`schedulesTab.power.${a}`)}
                          </option>
                        ))}
                      </select>
                    )}
                  </Field>
                ) : (
                  <Field label={task.action === "command" ? t("schedulesTab.command") : t("schedulesTab.leaveOut")}>
                    {(id) => (
                      <input
                        id={id}
                        className="input"
                        required={task.action === "command"}
                        maxLength={1000}
                        value={task.payload}
                        onChange={(e) => patch(i, { payload: e.target.value })}
                      />
                    )}
                  </Field>
                )}
                <Field label={t("schedulesTab.waitSeconds")}>
                  {(id) => (
                    <input
                      id={id}
                      className="input w-24"
                      type="number"
                      min={0}
                      max={900}
                      value={task.offset_seconds}
                      onChange={(e) => patch(i, { offset_seconds: Number(e.target.value) })}
                    />
                  )}
                </Field>
                <label className="flex items-center gap-1 pb-2 text-sm text-zinc-300">
                  <input
                    type="checkbox"
                    checked={task.continue_on_failure}
                    onChange={(e) => patch(i, { continue_on_failure: e.target.checked })}
                  />{" "}
                  {t("schedulesTab.continueOnFailure")}
                </label>
                <button
                  type="button"
                  className="btn mb-1"
                  aria-label={t("schedulesTab.removeTask", { n: i + 1 })}
                  onClick={() => setTasks((ts) => ts.filter((_, j) => j !== i))}
                  disabled={tasks.length === 1}
                >
                  <X className="h-4 w-4" aria-hidden />
                </button>
              </div>
            ))}
            <button type="button" className="btn" onClick={() => setTasks((ts) => [...ts, blankTask()])} disabled={tasks.length >= 20}>
              <Plus className="h-4 w-4" aria-hidden /> {t("schedulesTab.addTask")}
            </button>
          </fieldset>
          <ErrorBox error={create.error} />
          <button type="submit" className="btn btn-primary" disabled={create.isPending}>
            {create.isPending && <Loader2 className="h-4 w-4 animate-spin" aria-hidden />} {t("schedulesTab.createSchedule")}
          </button>
        </form>
      </details>
    </div>
  );
}
