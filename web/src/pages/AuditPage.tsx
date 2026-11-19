import { useNavigate, useSearch } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { ChevronLeft, ChevronRight, ScrollText } from "lucide-react";
import { AUDIT_PAGE_SIZE, auditQuery } from "../api/queries";
import { SearchInput } from "../components/SearchInput";
import { Empty, ErrorBox, PageHeader, Pill, Spinner } from "../components/ui";
import { useSearchText } from "../hooks/useSearchText";

export function AuditPage() {
  // The search term and page live in the address, so a view can be shared and Back works.
  // Changing the term and returning to page one is a single navigation, so paging is never undone by a timer.
  const { q: term = "", page = 0 } = useSearch({ from: "/app/admin/audit" });
  const navigate = useNavigate({ from: "/audit" });
  const [filter, setFilter] = useSearchText(term, (q) => void navigate({ search: { q: q || undefined }, replace: true }));
  const goTo = (p: number) => void navigate({ search: (prev) => ({ ...prev, page: p || undefined }) });

  const q = useQuery(auditQuery(term, page));
  const rows = q.data ?? [];

  return (
    <div className="mx-auto max-w-6xl">
      <PageHeader
        title="Audit log"
        sub="Security events across the panel, newest first. Times are in your browser's timezone."
        actions={
          <SearchInput
            label="Search the audit log"
            placeholder="Search event, user, target, IP…"
            value={filter}
            onChange={setFilter}
            className="w-72"
          />
        }
      />
      {q.isPending && <Spinner />}
      {q.error && <ErrorBox error={q.error} />}
      {q.data?.length === 0 && page === 0 && !term && (
        <Empty icon={<ScrollText className="h-5 w-5" aria-hidden />} title="No events yet">
          Sign-ins, power actions and settings changes show up here.
        </Empty>
      )}
      {q.data && (q.data.length > 0 || page > 0 || term) && (
        <div className="overflow-hidden rounded-2xl border border-zinc-800 bg-zinc-900">
          <div className="relative overflow-x-auto" tabIndex={0} role="region" aria-label="Audit events">
            <table className="w-full text-left text-sm">
              <caption className="sr-only">Security events, newest first</caption>
              <thead className="border-b border-zinc-800 text-xs uppercase tracking-wider text-zinc-500">
                <tr>
                  <th scope="col" className="px-4 py-3 font-medium">
                    Time
                  </th>
                  <th scope="col" className="px-4 py-3 font-medium">
                    Event
                  </th>
                  <th scope="col" className="px-4 py-3 font-medium">
                    User
                  </th>
                  <th scope="col" className="px-4 py-3 font-medium">
                    Target
                  </th>
                  <th scope="col" className="px-4 py-3 font-medium">
                    IP
                  </th>
                </tr>
              </thead>
              <tbody className="divide-y divide-zinc-800/70">
                {rows.map((e) => {
                  const bad = e.action.includes("fail") || e.action.includes("blocked");
                  return (
                    <tr key={e.id} className="hover:bg-white/[0.02]">
                      <td className="whitespace-nowrap px-4 py-2.5 text-zinc-400">{new Date(e.at * 1000).toLocaleString()}</td>
                      <td className="px-4 py-2.5">
                        {bad ? (
                          <Pill tone="warn">{e.action}</Pill>
                        ) : (
                          <span className="font-mono text-[13px] text-zinc-200">{e.action}</span>
                        )}
                      </td>
                      <td className="px-4 py-2.5 text-zinc-300">{e.actor_email}</td>
                      <td className="px-4 py-2.5 text-zinc-300">
                        {e.target}
                        {e.detail && <span className="text-zinc-500"> ({e.detail})</span>}
                      </td>
                      <td className="px-4 py-2.5 font-mono text-[13px] text-zinc-500">{e.ip}</td>
                    </tr>
                  );
                })}
                {rows.length === 0 && (
                  <tr>
                    <td colSpan={5} className="px-4 py-10 text-center text-zinc-500">
                      {term ? `No events match “${term}”.` : "No more events."}
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>
          <div className="flex items-center justify-between border-t border-zinc-800 bg-thead px-4 py-2.5 text-sm text-zinc-500">
            <span aria-live="polite">
              Page {page + 1}
              {q.data.length ? ` · ${q.data.length} events` : ""}
              {term ? ` matching “${term}”` : ""}
            </span>
            <div className="flex gap-2">
              <button type="button" className="btn" disabled={page === 0} onClick={() => goTo(page - 1)}>
                <ChevronLeft className="h-4 w-4" aria-hidden /> Newer
              </button>
              <button type="button" className="btn" disabled={q.data.length < AUDIT_PAGE_SIZE} onClick={() => goTo(page + 1)}>
                Older <ChevronRight className="h-4 w-4" aria-hidden />
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
