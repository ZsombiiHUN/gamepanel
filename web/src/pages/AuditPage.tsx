import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { api, call } from "../api/client";
import { AdminOnly } from "../components/AdminOnly";
import { ErrorBox, Spinner } from "../components/ui";

const PAGE = 100;

export function AuditPage() {
  return <AdminOnly><Audit /></AdminOnly>;
}

function Audit() {
  const [page, setPage] = useState(0);
  const q = useQuery({
    queryKey: ["audit", page],
    queryFn: async () => (await call(api.GET("/audit", { params: { query: { limit: PAGE, offset: page * PAGE } } }))) ?? [],
  });

  return (
    <div className="space-y-4">
      <h1 className="text-2xl font-semibold">Audit log</h1>
      {q.isPending && <Spinner />}
      {q.error && <ErrorBox error={q.error} />}
      {q.data && (
        <div className="overflow-x-auto rounded-lg border border-zinc-800">
          <table className="w-full text-left text-sm">
            <caption className="sr-only">Security events, newest first</caption>
            <thead className="bg-zinc-900 text-zinc-400">
              <tr>
                <th scope="col" className="px-3 py-2">Time</th>
                <th scope="col" className="px-3 py-2">Event</th>
                <th scope="col" className="px-3 py-2">User</th>
                <th scope="col" className="px-3 py-2">Target</th>
                <th scope="col" className="px-3 py-2">IP</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-zinc-800">
              {q.data.map((e) => (
                <tr key={e.id} className={e.action.includes("fail") || e.action.includes("blocked") ? "text-amber-300" : ""}>
                  <td className="whitespace-nowrap px-3 py-2">{new Date(e.at * 1000).toLocaleString()}</td>
                  <td className="px-3 py-2 font-mono">{e.action}</td>
                  <td className="px-3 py-2">{e.actor_email}</td>
                  <td className="px-3 py-2">{e.target}{e.detail && <span className="text-zinc-500"> ({e.detail})</span>}</td>
                  <td className="px-3 py-2 font-mono">{e.ip}</td>
                </tr>
              ))}
              {q.data.length === 0 && <tr><td colSpan={5} className="px-3 py-6 text-center text-zinc-500">No events yet.</td></tr>}
            </tbody>
          </table>
        </div>
      )}
      <div className="flex gap-2">
        <button type="button" className="btn" disabled={page === 0} onClick={() => setPage(page - 1)}>Newer</button>
        <button type="button" className="btn" disabled={!q.data || q.data.length < PAGE} onClick={() => setPage(page + 1)}>Older</button>
      </div>
    </div>
  );
}
