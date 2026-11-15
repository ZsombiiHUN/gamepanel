import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ChevronLeft, ChevronRight, ScrollText, Search } from "lucide-react";
import { api, call } from "../api/client";
import { AdminOnly } from "../components/AdminOnly";
import { Empty, ErrorBox, PageHeader, Pill, Spinner } from "../components/ui";

const PAGE = 100;

export function AuditPage() {
  return <AdminOnly><Audit /></AdminOnly>;
}

function Audit() {
  const [page, setPage] = useState(0);
  const [filter, setFilter] = useState("");
  const q = useQuery({
    queryKey: ["audit", page],
    queryFn: async () => (await call(api.GET("/audit", { params: { query: { limit: PAGE, offset: page * PAGE } } }))) ?? [],
  });
  const f = filter.trim().toLowerCase();
  const rows = (q.data ?? []).filter((e) => !f || [e.action, e.actor_email, e.target, e.detail, e.ip].some((v) => String(v ?? "").toLowerCase().includes(f)));

  return (
    <div className="mx-auto max-w-6xl">
      <PageHeader
        title="Audit log"
        sub="Security events across the panel, newest first. Times are in your browser's timezone."
        actions={
          <label className="relative">
            <span className="sr-only">Filter this page</span>
            <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-zinc-500" aria-hidden />
            <input className="input w-60 !pl-9" placeholder="Filter this page" value={filter} onChange={(e) => setFilter(e.target.value)} />
          </label>
        }
      />
      {q.isPending && <Spinner />}
      {q.error && <ErrorBox error={q.error} />}
      {q.data && q.data.length === 0 && page === 0 && <Empty icon={<ScrollText className="h-5 w-5" aria-hidden />} title="No events yet">Sign-ins, power actions and settings changes show up here.</Empty>}
      {q.data && (q.data.length > 0 || page > 0) && (
        <div className="overflow-hidden rounded-2xl border border-zinc-800 bg-zinc-900">
          <div className="overflow-x-auto">
            <table className="w-full text-left text-sm">
              <caption className="sr-only">Security events, newest first</caption>
              <thead className="border-b border-zinc-800 text-xs uppercase tracking-wider text-zinc-500">
                <tr>
                  <th scope="col" className="px-4 py-3 font-medium">Time</th>
                  <th scope="col" className="px-4 py-3 font-medium">Event</th>
                  <th scope="col" className="px-4 py-3 font-medium">User</th>
                  <th scope="col" className="px-4 py-3 font-medium">Target</th>
                  <th scope="col" className="px-4 py-3 font-medium">IP</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-zinc-800/70">
                {rows.map((e) => {
                  const bad = e.action.includes("fail") || e.action.includes("blocked");
                  return (
                    <tr key={e.id} className="hover:bg-white/[0.02]">
                      <td className="whitespace-nowrap px-4 py-2.5 text-zinc-400">{new Date(e.at * 1000).toLocaleString()}</td>
                      <td className="px-4 py-2.5">{bad ? <Pill tone="warn">{e.action}</Pill> : <span className="font-mono text-[13px] text-zinc-200">{e.action}</span>}</td>
                      <td className="px-4 py-2.5 text-zinc-300">{e.actor_email}</td>
                      <td className="px-4 py-2.5 text-zinc-300">{e.target}{e.detail && <span className="text-zinc-500"> ({e.detail})</span>}</td>
                      <td className="px-4 py-2.5 font-mono text-[13px] text-zinc-500">{e.ip}</td>
                    </tr>
                  );
                })}
                {rows.length === 0 && <tr><td colSpan={5} className="px-4 py-10 text-center text-zinc-500">{f ? "Nothing on this page matches." : "No more events."}</td></tr>}
              </tbody>
            </table>
          </div>
          <div className="flex items-center justify-between border-t border-zinc-800 bg-black/15 px-4 py-2.5 text-sm text-zinc-500">
            <span>Page {page + 1}{q.data.length ? ` · ${q.data.length} events` : ""}</span>
            <div className="flex gap-2">
              <button type="button" className="btn" disabled={page === 0} onClick={() => setPage(page - 1)}><ChevronLeft className="h-4 w-4" aria-hidden /> Newer</button>
              <button type="button" className="btn" disabled={q.data.length < PAGE} onClick={() => setPage(page + 1)}>Older <ChevronRight className="h-4 w-4" aria-hidden /></button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
