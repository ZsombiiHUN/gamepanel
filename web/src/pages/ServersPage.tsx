import { Link } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { Cpu, MemoryStick, Network } from "lucide-react";
import { meQuery, serversQuery } from "../api/queries";
import { ErrorBox, OnlineDot, Spinner, StateBadge } from "../components/ui";
import { formatBytes } from "../lib/format";

export function ServersPage() {
  const servers = useQuery(serversQuery);
  const me = useQuery(meQuery);

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-semibold">Servers</h1>
        {me.data?.is_admin && <Link to="/servers/new" className="btn btn-primary">Create server</Link>}
      </div>
      {servers.isPending && <Spinner />}
      <ErrorBox error={servers.error} />
      {servers.data && servers.data.length === 0 && (
        <p className="card text-sm text-zinc-400">No servers yet.{me.data?.is_admin ? " Create one to get started." : " Ask an admin to create one for you."}</p>
      )}
      <ul className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
        {servers.data?.map((s) => (
          <li key={s.uuid}>
            <Link to="/servers/$uuid" params={{ uuid: s.uuid }} search={{ tab: "console" }} className="card block h-full hover:border-indigo-500 focus-visible:outline-2 focus-visible:outline-indigo-400">
              <div className="flex items-start justify-between gap-2">
                <h2 className="truncate text-lg font-medium">{s.name}</h2>
                <StateBadge state={s.state} />
              </div>
              <div className="mt-1"><OnlineDot online={s.node_online} label="Node" /></div>
              <dl className="mt-3 space-y-1 text-sm text-zinc-300">
                <div className="flex items-center gap-2"><Network className="h-4 w-4 text-zinc-500" aria-hidden /><dt className="sr-only">Port</dt><dd>Port {s.port}</dd></div>
                <div className="flex items-center gap-2"><MemoryStick className="h-4 w-4 text-zinc-500" aria-hidden /><dt className="sr-only">Memory</dt>
                  <dd>{s.stats ? `${formatBytes(s.stats.mem_bytes)} / ` : ""}{s.memory_mb} MB</dd></div>
                {s.stats && (
                  <div className="flex items-center gap-2"><Cpu className="h-4 w-4 text-zinc-500" aria-hidden /><dt className="sr-only">CPU</dt><dd>{s.stats.cpu_pct.toFixed(1)}% CPU</dd></div>
                )}
              </dl>
            </Link>
          </li>
        ))}
      </ul>
    </div>
  );
}
