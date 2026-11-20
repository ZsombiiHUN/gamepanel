import { useState, type SubmitEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Box, CloudSun, Crown, Gavel, Loader2, LogOut, Megaphone, RefreshCw, Save, Sun, Users } from "lucide-react";
import { api, call, type components } from "../api/client";
import { invalidates, keys, minecraftStatusQuery, minecraftVersionsQuery, refreshLater } from "../api/queries";
import { ConfirmDialog, ErrorBox, FormDialog, InfoList, Pill, Section, SectionsWithNav, Spinner, Avatar } from "../components/ui";
import { IconSection, WorldSection } from "./minecraft/WorldSection";
import { PlayersSection } from "./minecraft/PlayersSection";
import { PropertiesSection } from "./minecraft/PropertiesSection";
import { Motd, sendCommand, type Can } from "./minecraft/shared";

type ServerView = components["schemas"]["ServerView"];

/** Minecraft-specific tools: who is online, lists, settings, version, world and icon. */
export function MinecraftTab({ server, state, can, kind }: { server: ServerView; state: string; can: Can; kind: "server" | "proxy" }) {
  const uuid = server.uuid;
  const running = state === "running";
  const stopped = state === "offline" || state === "crashed";
  return (
    <SectionsWithNav>
      <StatusSection uuid={uuid} state={state} can={can} kind={kind} />
      {kind === "server" && <PlayersSection uuid={uuid} running={running} can={can} />}
      {kind === "server" && <PropertiesSection uuid={uuid} running={running} can={can} />}
      {can("startup.read") && <VersionSection uuid={uuid} stopped={stopped} can={can} />}
      {kind === "server" && can("files.write") && <WorldSection uuid={uuid} running={running} stopped={stopped} can={can} />}
      {can("files.write") && <IconSection uuid={uuid} running={running} />}
    </SectionsWithNav>
  );
}

// ---- Status --------------------------------------------------------------------------------

function StatusSection({ uuid, state, can, kind }: { uuid: string; state: string; can: Can; kind: "server" | "proxy" }) {
  const running = state === "running";
  const status = useQuery(minecraftStatusQuery(uuid, running && can("console.read")));
  const qc = useQueryClient();
  const [ban, setBan] = useState<string | null>(null);
  const act = useMutation({
    mutationFn: (command: string) => sendCommand(uuid, command),
    // The player list changes once the server has acted on it.
    onSuccess: () => refreshLater(qc, keys.minecraft(uuid), 1200),
  });
  const s = status.data;
  const moderate = kind === "server" && can("console.send");

  return (
    <Section
      title="Status"
      desc="What players see in their server list, checked every 10 seconds while the server runs."
      footer={
        running && s ? (
          <>
            <span>
              {s.version} · answered in {s.latency_ms} ms
            </span>
            <button type="button" className="btn btn-ghost !min-h-8" onClick={() => void status.refetch()} disabled={status.isFetching}>
              <RefreshCw className={`h-4 w-4 ${status.isFetching ? "animate-spin" : ""}`} aria-hidden /> Refresh
            </button>
          </>
        ) : undefined
      }
    >
      {!running ? (
        <p className="text-[13.5px] text-zinc-400">
          {state === "starting"
            ? "The server is still starting. Its status appears once it is ready."
            : "Start the server to see who is online."}
        </p>
      ) : !can("console.read") ? (
        <p className="text-[13.5px] text-zinc-400">Seeing who is online needs console access.</p>
      ) : status.isPending ? (
        <Spinner />
      ) : status.error ? (
        <ErrorBox error={status.error} />
      ) : s ? (
        <div className="space-y-4">
          <div className="flex items-start gap-4 rounded-lg border border-line bg-[#1b1b1b] p-3">
            {s.favicon ? (
              <img
                src={s.favicon}
                alt="Server icon"
                width={64}
                height={64}
                className="h-16 w-16 shrink-0 rounded-[4px] [image-rendering:pixelated]"
              />
            ) : (
              <span className="grid h-16 w-16 shrink-0 place-content-center rounded-[4px] bg-zinc-800 text-zinc-500" aria-hidden>
                <Box className="h-7 w-7" />
              </span>
            )}
            <div className="min-w-0 flex-1">
              <Motd text={s.motd || "A Minecraft Server"} />
            </div>
            <div className="shrink-0 text-right font-mono text-[13px] text-zinc-400">
              <span className="text-zinc-100">{s.online}</span>/{s.max}
            </div>
          </div>

          <div>
            <h3 className="mb-2 flex items-center gap-2 text-[13px] font-medium text-zinc-300">
              <Users className="h-4 w-4 text-zinc-500" aria-hidden /> Online now
              <Pill tone={s.online > 0 ? "good" : "neutral"}>{s.online}</Pill>
            </h3>
            {s.online === 0 ? (
              <p className="text-[13px] text-zinc-500">No one is online.</p>
            ) : s.players.length === 0 ? (
              <p className="text-[13px] text-zinc-500">The server does not share who is online (hide-online-players).</p>
            ) : (
              <ul className="divide-y divide-line overflow-hidden rounded-lg border border-line" aria-label="Players online">
                {s.players.map((p) => (
                  <li key={p.id || p.name} className="flex items-center gap-3 px-3 py-2">
                    <Avatar name={p.name} size={28} />
                    <span className="min-w-0 flex-1 truncate text-[13.5px] font-medium">{p.name}</span>
                    {moderate && (
                      <div className="flex gap-1">
                        <button
                          type="button"
                          className="btn btn-ghost !min-h-8 !px-2"
                          title="Make operator"
                          aria-label={`Make ${p.name} an operator`}
                          onClick={() => act.mutate(`op ${p.name}`)}
                        >
                          <Crown className="h-4 w-4" aria-hidden />
                        </button>
                        <button
                          type="button"
                          className="btn btn-ghost !min-h-8 !px-2"
                          title="Kick"
                          aria-label={`Kick ${p.name}`}
                          onClick={() => act.mutate(`kick ${p.name}`)}
                        >
                          <LogOut className="h-4 w-4" aria-hidden />
                        </button>
                        <button
                          type="button"
                          className="btn btn-ghost !min-h-8 !px-2"
                          title="Ban"
                          aria-label={`Ban ${p.name}`}
                          onClick={() => setBan(p.name)}
                        >
                          <Gavel className="h-4 w-4" aria-hidden />
                        </button>
                      </div>
                    )}
                  </li>
                ))}
              </ul>
            )}
            {s.online > s.players.length && s.players.length > 0 && (
              <p className="mt-2 text-[12px] text-zinc-500">The server lists {s.players.length} of them.</p>
            )}
          </div>

          {moderate && <QuickActions uuid={uuid} />}
          {act.error ? <ErrorBox error={act.error} /> : null}
        </div>
      ) : null}
      <ConfirmDialog
        open={ban !== null}
        title={`Ban ${ban ?? ""}?`}
        body="They are kicked and cannot join again until you unban them under Players."
        confirmLabel="Ban"
        pending={act.isPending}
        onCancel={() => setBan(null)}
        onConfirm={() => {
          if (ban) act.mutate(`ban ${ban}`, { onSettled: () => setBan(null) });
        }}
      />
    </Section>
  );
}

function QuickActions({ uuid }: { uuid: string }) {
  const [msg, setMsg] = useState("");
  const [done, setDone] = useState<string | null>(null);
  const run = useMutation({
    mutationFn: ({ command }: { command: string; label: string }) => sendCommand(uuid, command),
    onSuccess: (_, v) => {
      setDone(v.label);
      setTimeout(() => setDone(null), 2500);
    },
  });
  const actions = [
    { label: "Save the world", command: "save-all", icon: Save },
    { label: "Make it day", command: "time set day", icon: Sun },
    { label: "Clear the weather", command: "weather clear", icon: CloudSun },
  ];
  const say = (e: SubmitEvent) => {
    e.preventDefault();
    if (msg.trim()) run.mutate({ command: `say ${msg.trim()}`, label: "Message sent" }, { onSuccess: () => setMsg("") });
  };
  return (
    <div>
      <h3 className="mb-2 text-[13px] font-medium text-zinc-300">Quick actions</h3>
      <div className="flex flex-wrap gap-2">
        {actions.map((a) => (
          <button key={a.command} type="button" className="btn" disabled={run.isPending} onClick={() => run.mutate(a)}>
            <a.icon className="h-4 w-4" aria-hidden /> {a.label}
          </button>
        ))}
      </div>
      <form onSubmit={say} className="mt-2 flex gap-2" aria-label="Message everyone">
        <label htmlFor="mc-say" className="sr-only">
          Message to everyone online
        </label>
        <input
          id="mc-say"
          className="input"
          placeholder="Message everyone online"
          maxLength={200}
          value={msg}
          onChange={(e) => setMsg(e.target.value)}
        />
        <button type="submit" className="btn" disabled={!msg.trim() || run.isPending}>
          <Megaphone className="h-4 w-4" aria-hidden /> Send
        </button>
      </form>
      <p role="status" className="mt-1 h-4 text-[12px] text-good-text">
        {done}
      </p>
      {run.error ? <ErrorBox error={run.error} /> : null}
    </div>
  );
}

// ---- Version -------------------------------------------------------------------------------

function VersionSection({ uuid, stopped, can }: { uuid: string; stopped: boolean; can: Can }) {
  const v = useQuery(minecraftVersionsQuery(uuid));
  const [pick, setPick] = useState<string | null>(null);
  const [confirm, setConfirm] = useState(false);
  const allowed = can("startup.update") && can("settings.reinstall");
  const sw = useMutation({
    mutationFn: async ({ variable, version, reset }: { variable: string; version: string; reset: Record<string, string> }) => {
      await call(api.PUT("/servers/{uuid}/startup", { params: { path: { uuid } }, body: { env: { ...reset, [variable]: version } } }));
      await call(api.POST("/servers/{uuid}/reinstall", { params: { path: { uuid } } }));
    },
    meta: invalidates(keys.server(uuid), keys.startup(uuid), keys.minecraft(uuid)),
    onSuccess: () => {
      setConfirm(false);
      setPick(null);
    },
  });
  const reinstall = useMutation({
    mutationFn: () => call(api.POST("/servers/{uuid}/reinstall", { params: { path: { uuid } } })),
    meta: invalidates(keys.server(uuid)),
  });

  if (v.isPending)
    return (
      <Section title="Version">
        <Spinner />
      </Section>
    );
  if (v.error)
    return (
      <Section title="Version">
        <ErrorBox error={v.error} />
      </Section>
    );
  const d = v.data;
  if (d.flavor === "bungeecord" || (d.variable && d.versions.length === 0 && d.flavor))
    return (
      <Section
        title="Version"
        desc="This server always installs the newest build. Reinstall it to update."
        footer={
          <>
            <span>{stopped ? "Reinstalling keeps your files and configs." : "Stop the server to reinstall it."}</span>
            <button
              type="button"
              className="btn"
              disabled={!stopped || !can("settings.reinstall") || reinstall.isPending}
              onClick={() => reinstall.mutate()}
            >
              {reinstall.isPending && <Loader2 className="h-4 w-4 animate-spin" aria-hidden />} Update to the newest build
            </button>
          </>
        }
      >
        {reinstall.error ? <ErrorBox error={reinstall.error} /> : null}
      </Section>
    );
  if (!d.variable)
    return <Section title="Version" desc="This egg does not say which version it installs. Look for a version setting under Startup." />;

  const choice = pick ?? d.current;
  const isLatest = d.current === "latest" || d.current === "";
  const options = ["latest", ...d.versions];
  return (
    <Section
      title="Version"
      desc={
        d.flavor === "velocity"
          ? "The Velocity version to install. Switching reinstalls the proxy; your configs stay."
          : "The Minecraft version to install. Switching reinstalls the server software; your worlds, plugins and configs stay."
      }
      footer={
        <>
          <span>
            {!stopped
              ? "Stop the server to switch versions."
              : !allowed
                ? "Switching needs the startup and reinstall permissions."
                : "Make a backup first: a world opened in a newer version cannot go back."}
          </span>
          <button
            type="button"
            className="btn btn-primary"
            disabled={!stopped || !allowed || choice === d.current}
            onClick={() => setConfirm(true)}
          >
            Switch version
          </button>
        </>
      }
    >
      <InfoList rows={[{ label: "Installed", value: isLatest ? "latest (newest release at install time)" : d.current }]} />
      <div className="mt-4 max-w-sm">
        <label htmlFor="mc-version" className="label">
          Switch to
        </label>
        <select id="mc-version" className="input" value={choice} onChange={(e) => setPick(e.target.value)} disabled={!allowed}>
          {!options.includes(d.current) && <option value={d.current}>{d.current}</option>}
          {options.map((o) => (
            <option key={o} value={o}>
              {o === "latest" ? "Latest release" : o}
            </option>
          ))}
        </select>
        {d.versions.length > 0 && <p className="mt-1 text-[12px] text-zinc-500">{d.versions.length} releases available.</p>}
      </div>
      <FormDialog
        open={confirm}
        title={`Switch to ${choice === "latest" ? "the latest release" : choice}?`}
        submitLabel="Switch and reinstall"
        pending={sw.isPending}
        error={sw.error}
        onClose={() => setConfirm(false)}
        onSubmit={() => sw.mutate({ variable: d.variable, version: choice, reset: d.reset })}
      >
        <p>The server software is downloaded again for this version. Worlds, plugins, mods and configs are kept.</p>
        <p className="text-warn-text">
          Going to an older version can corrupt a world, and plugins or mods may not support the new one. Make a backup first if you are not
          sure.
        </p>
      </FormDialog>
    </Section>
  );
}

// ---- World ---------------------------------------------------------------------------------
