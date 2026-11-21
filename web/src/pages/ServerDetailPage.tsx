import { lazy, Suspense, useEffect, useRef, useState } from "react";
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
import { ServerTabs } from "./server/ServerTabs";
import { SettingsTab } from "./SettingsTab";
import { DatabasesTab } from "./DatabasesTab";
import { ModsTab } from "./ModsTab";
import { NetworkTab } from "./NetworkTab";
import { StartupTab } from "./StartupTab";
import { UsageRow } from "./server/UsageRow";
import { UsersTab } from "./UsersTab";
import { useMediaQuery } from "../hooks/useMediaQuery";
import { detectProblem, minecraftKind, startsRun, type Problem } from "../lib/minecraft";
import { MinecraftNotice } from "./server/MinecraftNotice";
import { MinecraftTab } from "./MinecraftTab";

const FilesTab = lazy(() => import("./FilesTab").then((m) => ({ default: m.FilesTab })));

export type { Tab } from "../lib/tabs";

export function ServerDetailPage() {
  const { uuid } = useParams({ from: "/app/servers/$uuid" });
  const { tab } = useSearch({ from: "/app/servers/$uuid" });
  const server = useQuery(serverQuery(uuid));
  const term = useRef<TerminalHandle>(null);
  // Lines can arrive before the page (and so the terminal) has rendered; hold them until it has.
  const early = useRef<string[]>([]);
  // A console line saying why a Minecraft server could not start (the EULA, its Java), offered as a fix.
  const [problem, setProblem] = useState<Problem | null>(null);
  const mc = minecraftKind(server.data?.features);
  const ws = useServerSocket(uuid, {
    onConsole: (l) => {
      if (term.current) term.current.writeln(l);
      else early.current.push(l);
      if (mc) {
        const p = detectProblem(l);
        if (p) setProblem(p);
        else if (startsRun(l)) setProblem(null);
      }
    },
    onReset: () => {
      early.current = [];
      term.current?.clear();
      setProblem(null);
    },
  });
  const ready = !server.isPending && !server.error;
  // On wide screens the charts sit in a narrow column beside the console.
  const wide = useMediaQuery("(min-width: 1280px)");
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
    <div className="space-y-5">
      <div className="space-y-5">
        <ServerHeader server={s} state={state} wsStatus={ws.status} can={can} />
        <ServerTabs uuid={uuid} tab={tab} can={can} features={s.features} />
      </div>

      {/* The console comes first on small screens (it is what people came for), below the charts on
          medium ones, and beside a narrow column of charts on wide ones. */}
      <div className={tab === "console" ? "flex flex-col gap-5 xl:grid xl:grid-cols-[minmax(0,1fr)_300px] xl:items-start" : undefined}>
        {tab === "console" && (
          <div className="order-2 lg:order-1 xl:order-2">
            <UsageRow
              uuid={uuid}
              running={state === "running" ? stats : null}
              latest={stats}
              limits={{ memoryMb: s.memory_mb, diskMb: s.disk_mb, cpuPct: s.cpu_pct }}
              rail={wide}
            />
          </div>
        )}
        {/* Console stays mounted so the buffer survives tab switches */}
        <div hidden={tab !== "console"} className="order-1 min-w-0 space-y-3 lg:order-2 xl:order-1">
          {/* Only while it is stopped: the backlog of an earlier failed run must not nag a server that now runs. */}
          {problem && (state === "offline" || state === "crashed") && (
            <MinecraftNotice uuid={uuid} problem={problem} can={can} onDone={() => setProblem(null)} />
          )}
          <ConsolePanel uuid={uuid} termRef={term} active={tab === "console"} canSend={can("console.send")} status={ws.status} />
        </div>
      </div>
      {tab === "minecraft" && mc && can("files.read") && <MinecraftTab server={s} state={state} can={can} kind={mc} />}
      {tab === "files" && (
        <Suspense fallback={<Spinner />}>
          <FilesTab uuid={uuid} />
        </Suspense>
      )}
      {tab === "mods" && can("files.write") && <ModsTab uuid={uuid} />}
      {tab === "backups" && <BackupsTab uuid={uuid} />}
      {tab === "schedules" && <SchedulesTab uuid={uuid} />}
      {tab === "startup" && can("startup.read") && <StartupTab uuid={uuid} />}
      {tab === "network" && can("network.read") && <NetworkTab uuid={uuid} />}
      {tab === "databases" && can("databases.read") && <DatabasesTab uuid={uuid} />}
      {tab === "activity" && can("activity.read") && <ActivityTab uuid={uuid} />}
      {tab === "users" && s.owner && <UsersTab uuid={uuid} ownerEmail={s.owner_email} />}
      {tab === "settings" && <SettingsTab server={s} />}
    </div>
  );
}
