import { useState, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  Archive,
  CalendarClock,
  ChevronLeft,
  ChevronRight,
  Database,
  FileText,
  History,
  Power,
  Server,
  SquareTerminal,
  Users,
} from "lucide-react";
import { ACTIVITY_PAGE_SIZE as PAGE, activityQuery } from "../api/queries";
import type { components } from "../api/client";
import { SearchInput } from "../components/SearchInput";
import { Empty, ErrorBox, Spinner } from "../components/ui";
import { useDebouncedValue } from "../hooks/useDebouncedValue";
import { ACTIVITY_FILTERS, activityKind, describeAction, groupByDay, relativeTime, type ActivityKind } from "../lib/activity";

type Entry = components["schemas"]["AuditEntry"];

const ICONS: Record<ActivityKind, ReactNode> = {
  power: <Power className="h-4 w-4" aria-hidden />,
  console: <SquareTerminal className="h-4 w-4" aria-hidden />,
  files: <FileText className="h-4 w-4" aria-hidden />,
  backups: <Archive className="h-4 w-4" aria-hidden />,
  schedules: <CalendarClock className="h-4 w-4" aria-hidden />,
  access: <Users className="h-4 w-4" aria-hidden />,
  databases: <Database className="h-4 w-4" aria-hidden />,
  server: <Server className="h-4 w-4" aria-hidden />,
};

/** Removals and kills read red, power and access changes stand out; the rest stays quiet. */
function tone(e: Entry) {
  if (/\.(delete|remove)$/.test(e.action) || e.action === "server.crash" || (e.action === "server.power" && e.detail === "kill"))
    return "bg-bad-bg text-bad-text";
  const k = activityKind(e.action);
  if (k === "power") return "bg-warn-bg text-warn-text";
  if (k === "access") return "bg-indigo-500/15 text-indigo-300";
  if (e.action.endsWith(".create") || e.action === "backup.restore") return "bg-good-bg text-good-text";
  return "bg-chip text-zinc-400";
}

function Detail({ e }: { e: Entry }) {
  if (!e.detail || e.action === "server.power") return null;
  if (e.action === "server.command")
    return (
      <code className="mt-1 inline-block max-w-full break-all rounded-md bg-well px-2 py-1 font-mono text-xs text-zinc-300">
        <span className="select-none text-zinc-500">&gt; </span>
        {e.detail}
      </code>
    );
  return <p className="mt-1 break-all font-mono text-xs text-zinc-500">{e.detail}</p>;
}

const clock = (unix: number) => new Date(unix * 1000).toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" });

export function ActivityTab({ uuid }: { uuid: string }) {
  const [filter, setFilter] = useState("");
  const [kind, setKind] = useState<ActivityKind | null>(null);
  const [page, setPage] = useState(0);
  const typed = useDebouncedValue(filter.trim(), 250);
  const chip = ACTIVITY_FILTERS.find((f) => f.id === kind);
  const term = [typed, chip?.q].filter(Boolean).join(" ");
  const q = useQuery(activityQuery(uuid, term, page));
  const raw = q.data ?? [];
  // The server search also matches details, so a chip can let through other kinds; drop them here.
  const rows = kind ? raw.filter((e) => activityKind(e.action) === kind) : raw;
  const more = raw.length === PAGE;
  const narrowed = !!typed || !!kind;

  const choose = (k: ActivityKind | null) => {
    setKind(k);
    setPage(0);
  };
  const chipCls = (on: boolean) =>
    `inline-flex min-h-8 shrink-0 items-center rounded-full border px-3 text-[13px] font-medium transition-colors focus-visible:outline-2 focus-visible:outline-indigo-500 ${
      on ? "border-zinc-100 bg-zinc-100 text-zinc-950" : "border-zinc-800 text-zinc-400 hover:border-zinc-700 hover:text-zinc-200"
    }`;

  return (
    <section className="overflow-hidden rounded-2xl border border-zinc-800 bg-zinc-900" aria-labelledby="activity-title">
      <div className="space-y-3 px-5 pb-3 pt-4">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div>
            <h2 id="activity-title" className="text-[15px] font-semibold">
              Activity
            </h2>
            <p className="mt-0.5 text-sm text-zinc-400">Everything people did on this server, newest first.</p>
          </div>
          <SearchInput
            label="Search activity"
            placeholder="Search action, person or file"
            value={filter}
            onChange={(v) => {
              setFilter(v);
              setPage(0);
            }}
          />
        </div>
        <div role="group" aria-label="Show only" className="-mx-1 flex gap-1.5 overflow-x-auto px-1 pb-1">
          <button type="button" className={chipCls(kind === null)} aria-pressed={kind === null} onClick={() => choose(null)}>
            All
          </button>
          {ACTIVITY_FILTERS.map((f) => (
            <button
              key={f.id}
              type="button"
              className={chipCls(kind === f.id)}
              aria-pressed={kind === f.id}
              onClick={() => choose(kind === f.id ? null : f.id)}
            >
              {f.label}
            </button>
          ))}
        </div>
      </div>

      {q.isPending && (
        <div className="px-5 pb-5">
          <Spinner />
        </div>
      )}
      {q.error ? (
        <div className="px-5 pb-4">
          <ErrorBox error={q.error} />
        </div>
      ) : null}
      {q.data && rows.length === 0 && (
        <div className="px-5 pb-5">
          <Empty icon={<History className="h-5 w-5" aria-hidden />} title={narrowed ? "Nothing matches" : "Nothing yet"}>
            {narrowed ? (
              <>
                Try other words or another filter.{" "}
                <button
                  type="button"
                  className="link"
                  onClick={() => {
                    setFilter("");
                    choose(null);
                  }}
                >
                  Show everything
                </button>
              </>
            ) : (
              "Power actions, console commands, file changes, backups and access changes show up here."
            )}
          </Empty>
        </div>
      )}

      <div className={q.isPlaceholderData ? "opacity-60 transition-opacity" : undefined}>
        {groupByDay(rows).map((g) => (
          <div key={g.label + g.rows[0]?.id}>
            <h3 className="border-y border-line bg-thead px-5 py-1.5 text-[11px] font-semibold uppercase tracking-wider text-zinc-500">
              {g.label}
            </h3>
            <ol>
              {g.rows.map((e) => (
                <li key={e.id} className="flex items-start gap-3 border-b border-line px-5 py-3 last:border-b-0 hover:bg-row-hover">
                  <span className={`mt-0.5 grid h-8 w-8 shrink-0 place-content-center rounded-lg ${tone(e)}`}>
                    {ICONS[activityKind(e.action)]}
                  </span>
                  <div className="min-w-0 flex-1">
                    <p className="text-sm text-zinc-300">
                      {e.actor_email ? (
                        <button
                          type="button"
                          className="font-medium text-zinc-100 hover:underline focus-visible:outline-2 focus-visible:outline-indigo-500"
                          title={`Only show ${e.actor_email}`}
                          onClick={() => {
                            setFilter(e.actor_email);
                            setPage(0);
                          }}
                        >
                          {e.actor_email}
                        </button>
                      ) : (
                        <span className="font-medium text-zinc-100">{e.action === "server.crash" ? "The server" : "Someone"}</span>
                      )}{" "}
                      {describeAction(e.action, e.detail)}
                    </p>
                    <Detail e={e} />
                  </div>
                  <div className="shrink-0 text-right text-xs text-zinc-500">
                    <time
                      className="font-mono text-zinc-400"
                      dateTime={new Date(e.at * 1000).toISOString()}
                      title={new Date(e.at * 1000).toLocaleString()}
                    >
                      {clock(e.at)}
                    </time>
                    <div className="hidden sm:block">{relativeTime(e.at)}</div>
                    {e.ip && (
                      <div className="hidden font-mono sm:block" title="IP address">
                        {e.ip}
                      </div>
                    )}
                  </div>
                </li>
              ))}
            </ol>
          </div>
        ))}
      </div>

      {(page > 0 || more) && (
        <nav aria-label="Pages" className="flex items-center justify-between gap-3 border-t border-line px-5 py-3 text-sm text-zinc-500">
          <button type="button" className="btn btn-ghost" disabled={page === 0} onClick={() => setPage(page - 1)}>
            <ChevronLeft className="h-4 w-4" aria-hidden /> Newer
          </button>
          <span>Page {page + 1}</span>
          <button type="button" className="btn btn-ghost" disabled={!more} onClick={() => setPage(page + 1)}>
            Older <ChevronRight className="h-4 w-4" aria-hidden />
          </button>
        </nav>
      )}
    </section>
  );
}
