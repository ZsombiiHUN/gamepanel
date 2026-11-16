import { lazy, Suspense, useEffect, useRef } from "react";
import { useParams, useSearch } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { serverQuery } from "../api/queries";
import { useServerSocket } from "../api/ws";
import type { TerminalHandle } from "../components/Terminal";
import { ErrorBox, Spinner } from "../components/ui";
import { ActivityTab } from "./ActivityTab";
import { BackupsTab } from "./BackupsTab";
import { ConsolePanel } from "./server/ConsolePanel";
import { SchedulesTab } from "./SchedulesTab";
import { ServerHeader } from "./server/ServerHeader";
import { SettingsTab } from "./SettingsTab";
import { DatabasesTab } from "./DatabasesTab";
import { NetworkTab } from "./NetworkTab";
import { StartupTab } from "./StartupTab";
import { UsageRow } from "./server/UsageRow";
import { UsersTab } from "./UsersTab";

const FilesTab = lazy(() => import("./FilesTab").then((m) => ({ default: m.FilesTab })));

export type { Tab } from "../lib/tabs";

export function ServerDetailPage() {
  const { uuid } = useParams({ from: "/app/servers/$uuid" });
  const { tab } = useSearch({ from: "/app/servers/$uuid" });
  const server = useQuery(serverQuery(uuid));
  const term = useRef<TerminalHandle>(null);
  // Lines can arrive before the page (and so the terminal) has rendered; hold them until it has.
  const early = useRef<string[]>([]);
  const ws = useServerSocket(uuid, {
    onConsole: (l) => (term.current ? term.current.writeln(l) : early.current.push(l)),
    onReset: () => { early.current = []; term.current?.clear(); },
  });
  const ready = !server.isPending && !server.error;
  useEffect(() => {
    if (!ready || !term.current) return;
    for (const l of early.current) term.current.writeln(l);
    early.current = [];
  }, [ready]);

  if (server.isPending) return <Spinner />;
  if (server.error) return <ErrorBox error={server.error} />;
  const s = server.data;
  const state = ws.state ?? s.state;
  const stats = ws.stats ?? s.stats ?? null;

  const can = (need?: string) => !need || (need === "owner" ? s.owner : s.permissions.includes(need));
  return (
    <div className="space-y-6">
      <ServerHeader server={s} state={state} wsStatus={ws.status} can={can} />

      {/* The console comes first on small screens (it is what people came for) and below the charts on large ones. */}
      <div className={tab === "console" ? "flex flex-col gap-6" : undefined}>
        {tab === "console" && (
          <div className="order-2 lg:order-1">
            <UsageRow running={state === "running" ? stats : null} latest={stats} limits={{ memoryMb: s.memory_mb, diskMb: s.disk_mb, cpuPct: s.cpu_pct }} />
          </div>
        )}
        {/* Console stays mounted so the buffer survives tab switches */}
        <div hidden={tab !== "console"} className="order-1 lg:order-2">
          <ConsolePanel uuid={uuid} termRef={term} active={tab === "console"} canSend={can("console.send")} status={ws.status} />
        </div>
      </div>
      {tab === "files" && (
        <Suspense fallback={<Spinner />}>
          <FilesTab uuid={uuid} />
        </Suspense>
      )}
      {tab === "backups" && <BackupsTab uuid={uuid} />}
      {tab === "schedules" && <SchedulesTab uuid={uuid} />}
      {tab === "startup" && can("startup.read") && <StartupTab uuid={uuid} />}
      {tab === "network" && can("network.read") && <NetworkTab uuid={uuid} />}
      {tab === "databases" && can("databases.read") && <DatabasesTab uuid={uuid} />}
      {tab === "activity" && can("activity.read") && <ActivityTab uuid={uuid} />}
      {tab === "users" && s.owner && <UsersTab uuid={uuid} />}
      {tab === "settings" && <SettingsTab server={s} />}
    </div>
  );
}
