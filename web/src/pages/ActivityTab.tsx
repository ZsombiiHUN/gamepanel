import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { History } from "lucide-react";
import { ACTIVITY_PAGE_SIZE as PAGE, activityQuery } from "../api/queries";
import { SearchInput } from "../components/SearchInput";
import { Avatar, Empty, ErrorBox, Spinner } from "../components/ui";
import { useDebouncedValue } from "../hooks/useDebouncedValue";
import { describeAction, relativeTime } from "../lib/activity";

export function ActivityTab({ uuid }: { uuid: string }) {
  const [filter, setFilter] = useState("");
  const [page, setPage] = useState(0);
  const term = useDebouncedValue(filter.trim(), 250);
  const q = useQuery(activityQuery(uuid, term, page));
  const rows = q.data ?? [];
  return (
    <section className="overflow-hidden rounded-2xl border border-zinc-800 bg-zinc-900" aria-labelledby="activity-title">
      <div className="flex flex-wrap items-center justify-between gap-3 px-5 py-3.5">
        <h2 id="activity-title" className="text-[15px] font-semibold">Activity</h2>
        <SearchInput label="Search activity" placeholder="Search action, person or file" value={filter} onChange={(v) => { setFilter(v); setPage(0); }} />
      </div>
      {q.isPending && <div className="px-5 pb-4"><Spinner /></div>}
      <div className="px-5"><ErrorBox error={q.error} /></div>
      {q.data && rows.length === 0 && (
        <div className="px-5 pb-5">
          <Empty icon={<History className="h-5 w-5" aria-hidden />} title={term ? "Nothing matches" : "Nothing yet"}>{term ? "Try other words." : "Power actions, file changes, backups and access changes show up here."}</Empty>
        </div>
      )}
      <ol className="divide-y divide-line border-t border-line">
        {rows.map((e) => (
          <li key={e.id} className="flex items-start gap-3 px-5 py-3">
            <Avatar name={e.actor_email || "?"} size={30} />
            <div className="min-w-0 flex-1">
              <p className="text-sm text-zinc-200">
                <span className="font-medium">{e.actor_email || "Someone"}</span> {describeAction(e.action, e.detail)}
              </p>
              {e.detail && e.action !== "server.power" && <p className="mt-0.5 break-all font-mono text-xs text-zinc-500">{e.detail}</p>}
            </div>
            <div className="shrink-0 text-right text-xs text-zinc-500">
              <time dateTime={new Date(e.at * 1000).toISOString()} title={new Date(e.at * 1000).toLocaleString()}>{relativeTime(e.at)}</time>
              {e.ip && <div className="font-mono">{e.ip}</div>}
            </div>
          </li>
        ))}
      </ol>
      {(page > 0 || rows.length === PAGE) && (
        <div className="flex justify-between border-t border-line px-5 py-3">
          <button type="button" className="btn btn-ghost" disabled={page === 0} onClick={() => setPage(page - 1)}>Newer</button>
          <button type="button" className="btn btn-ghost" disabled={rows.length < PAGE} onClick={() => setPage(page + 1)}>Older</button>
        </div>
      )}
    </section>
  );
}
