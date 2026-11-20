import { useState, type SubmitEvent } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Loader2, Plus, ShieldCheck, Trash2, UserX } from "lucide-react";
import { api, call } from "../../api/client";
import { Avatar, ErrorBox, Pill, Section, Spinner } from "../../components/ui";
import {
  isIP,
  isPlayerName,
  LIST_FILES,
  listCommand,
  mcDate,
  parseList,
  parseProperties,
  setProperties,
  type BannedIP,
  type ListedPlayer,
  type ListKind,
} from "../../lib/minecraft";
import { refreshSoon, sendCommand, useServerFile, useWriteFile, type Can } from "./shared";

/** A row of any of the four lists: players have a name and UUID, IP bans an address. */
type Entry = Partial<ListedPlayer> & Partial<BannedIP>;

const TABS: { id: ListKind; label: string; desc: string }[] = [
  { id: "whitelist", label: "Whitelist", desc: "Players who may join while the whitelist is on." },
  { id: "ops", label: "Operators", desc: "Players who may use every command." },
  { id: "bans", label: "Banned players", desc: "Players who can never join." },
  { id: "ipbans", label: "Banned IPs", desc: "Addresses no one can join from." },
];

/**
 * Whitelist, operators and bans. While the server runs they change through its own commands (it would
 * overwrite edited files); while it is stopped the panel edits the JSON files, looking up UUIDs itself.
 */
export function PlayersSection({ uuid, running, can }: { uuid: string; running: boolean; can: Can }) {
  const [kind, setKind] = useState<ListKind>("whitelist");
  const qc = useQueryClient();
  const file = useServerFile(uuid, LIST_FILES[kind]);
  const propsFile = useServerFile(uuid, "/server.properties");
  const write = useWriteFile(uuid);
  const [who, setWho] = useState("");
  const [reason, setReason] = useState("");
  const viaCommand = running && can("console.send");
  const canEdit = viaCommand || (!running && can("files.write"));
  const props = parseProperties(propsFile.data ?? "");
  const onlineMode = props.get("online-mode") !== "false";
  const whitelistOn = props.get("white-list") === "true";

  const change = useMutation({
    mutationFn: async ({ action, target }: { action: "add" | "remove"; target: string }) => {
      if (viaCommand) {
        await sendCommand(uuid, listCommand(kind, action, target, reason));
        refreshSoon(qc, uuid);
        return;
      }
      const list = parseList<Entry>(file.data ?? "");
      let next: unknown[];
      if (action === "remove") {
        next = list.filter((e) => (kind === "ipbans" ? e.ip !== target : e.name?.toLowerCase() !== target.toLowerCase()));
      } else if (kind === "ipbans") {
        if (list.some((e) => e.ip === target)) return;
        next = [
          ...list,
          { ip: target, created: mcDate(), source: "GamePanel", expires: "forever", reason: reason.trim() || "Banned by an operator." },
        ];
      } else {
        if (list.some((e) => e.name?.toLowerCase() === target.toLowerCase())) return;
        const p = await call(
          api.GET("/servers/{uuid}/minecraft/players/{name}", {
            params: { path: { uuid, name: target }, query: { offline: !onlineMode } },
          }),
        );
        const base = { uuid: p.uuid, name: p.name };
        const entry =
          kind === "ops"
            ? { ...base, level: 4, bypassesPlayerLimit: false }
            : kind === "bans"
              ? { ...base, created: mcDate(), source: "GamePanel", expires: "forever", reason: reason.trim() || "Banned by an operator." }
              : base;
        next = [...list, entry];
      }
      await write.mutateAsync({ path: LIST_FILES[kind], content: JSON.stringify(next, null, 2) + "\n" });
    },
    onSuccess: (_, v) => {
      if (v.action === "add") {
        setWho("");
        setReason("");
      }
    },
  });

  const toggleWhitelist = useMutation({
    mutationFn: async (on: boolean) => {
      if (viaCommand) {
        await sendCommand(uuid, `whitelist ${on ? "on" : "off"}`);
        refreshSoon(qc, uuid);
        return;
      }
      await write.mutateAsync({ path: "/server.properties", content: setProperties(propsFile.data ?? "", { "white-list": String(on) }) });
    },
  });

  const valid = kind === "ipbans" ? isIP(who.trim()) : isPlayerName(who.trim());
  const submit = (e: SubmitEvent) => {
    e.preventDefault();
    if (valid) change.mutate({ action: "add", target: who.trim() });
  };
  const list = parseList<Entry>(file.data ?? "");
  const tab = TABS.find((t) => t.id === kind) ?? { id: kind, label: kind, desc: "" };

  return (
    <Section
      title="Players"
      desc={
        running
          ? "Changes go through the server's own commands and apply at once."
          : "The server is stopped, so the panel edits its lists directly. They load at the next start."
      }
    >
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div role="group" aria-label="List" className="seg">
          {TABS.map((t) => (
            <button key={t.id} type="button" aria-pressed={kind === t.id} onClick={() => setKind(t.id)}>
              {t.label}
            </button>
          ))}
        </div>
        {kind === "whitelist" && propsFile.data !== undefined && propsFile.data !== null && (
          <div className="flex items-center gap-2 text-[13px]">
            <Pill tone={whitelistOn ? "good" : "neutral"}>{whitelistOn ? "Whitelist on" : "Whitelist off"}</Pill>
            {canEdit && (
              <button
                type="button"
                className="btn"
                disabled={toggleWhitelist.isPending}
                onClick={() => toggleWhitelist.mutate(!whitelistOn)}
              >
                {toggleWhitelist.isPending && <Loader2 className="h-4 w-4 animate-spin" aria-hidden />}
                Turn {whitelistOn ? "off" : "on"}
              </button>
            )}
          </div>
        )}
      </div>
      <p className="mt-3 text-[13px] text-zinc-500">{tab.desc}</p>

      {canEdit && (
        <form onSubmit={submit} className="mt-3 flex flex-wrap items-end gap-2" aria-label={`Add to ${tab.label}`}>
          <div className="min-w-48 flex-1">
            <label htmlFor="mc-who" className="sr-only">
              {kind === "ipbans" ? "IP address" : "Player name"}
            </label>
            <input
              id="mc-who"
              className="input"
              placeholder={kind === "ipbans" ? "IP address, such as 203.0.113.7" : "Player name"}
              value={who}
              maxLength={kind === "ipbans" ? 45 : 16}
              autoComplete="off"
              spellCheck={false}
              onChange={(e) => setWho(e.target.value)}
            />
          </div>
          {(kind === "bans" || kind === "ipbans") && (
            <div className="min-w-48 flex-1">
              <label htmlFor="mc-reason" className="sr-only">
                Reason
              </label>
              <input
                id="mc-reason"
                className="input"
                placeholder="Reason (optional)"
                value={reason}
                maxLength={200}
                onChange={(e) => setReason(e.target.value)}
              />
            </div>
          )}
          <button type="submit" className="btn btn-primary" disabled={!valid || change.isPending}>
            {change.isPending && change.variables.action === "add" ? (
              <Loader2 className="h-4 w-4 animate-spin" aria-hidden />
            ) : (
              <Plus className="h-4 w-4" aria-hidden />
            )}
            {kind === "bans" || kind === "ipbans" ? "Ban" : "Add"}
          </button>
        </form>
      )}
      {!canEdit && (
        <p className="mt-3 text-[13px] text-zinc-500">
          {running
            ? "Changing lists while the server runs needs permission to send console commands."
            : "Changing lists needs permission to edit files."}
        </p>
      )}
      {kind !== "ipbans" && !onlineMode && !viaCommand && canEdit && (
        <p className="mt-2 text-[12.5px] text-zinc-500">
          This server runs in offline mode, so players get the UUID it derives from their name.
        </p>
      )}

      <div className="mt-4">
        {file.isPending ? (
          <Spinner />
        ) : file.error ? (
          <ErrorBox error={file.error} />
        ) : list.length === 0 ? (
          <p className="rounded-lg border border-dashed border-line-strong px-4 py-6 text-center text-[13px] text-zinc-500">
            {file.data === null ? "The server creates this list on its first start." : "No one yet."}
          </p>
        ) : (
          <ul className="divide-y divide-line overflow-hidden rounded-lg border border-line" aria-label={tab.label}>
            {list.map((e) => {
              const id = kind === "ipbans" ? e.ip : e.name;
              return (
                <li key={`${id ?? ""}${e.uuid ?? ""}`} className="flex items-center gap-3 px-3 py-2">
                  {kind === "ipbans" ? (
                    <span className="grid h-7 w-7 place-content-center rounded-[7px] bg-row-active text-zinc-400" aria-hidden>
                      <UserX className="h-4 w-4" />
                    </span>
                  ) : (
                    <Avatar name={e.name ?? "?"} size={28} />
                  )}
                  <div className="min-w-0 flex-1">
                    <p className="truncate text-[13.5px] font-medium text-zinc-100">
                      {id}
                      {kind === "ops" && e.level !== undefined && (
                        <span className="ml-2 inline-flex items-center gap-1 text-xs font-normal text-zinc-500">
                          <ShieldCheck className="h-3.5 w-3.5" aria-hidden /> level {e.level}
                        </span>
                      )}
                    </p>
                    <p className="truncate font-mono text-[11.5px] text-zinc-500">
                      {kind === "bans" || kind === "ipbans"
                        ? [e.reason, e.expires && e.expires !== "forever" ? `until ${e.expires}` : ""].filter(Boolean).join(" · ")
                        : e.uuid}
                    </p>
                  </div>
                  {canEdit && id && (
                    <button
                      type="button"
                      className="btn btn-ghost !min-h-8 !px-2"
                      aria-label={`Remove ${id}`}
                      title={kind === "bans" || kind === "ipbans" ? "Unban" : "Remove"}
                      disabled={change.isPending}
                      onClick={() => change.mutate({ action: "remove", target: id })}
                    >
                      {change.isPending && change.variables.target === id ? (
                        <Loader2 className="h-4 w-4 animate-spin" aria-hidden />
                      ) : (
                        <Trash2 className="h-4 w-4" aria-hidden />
                      )}
                    </button>
                  )}
                </li>
              );
            })}
          </ul>
        )}
      </div>
      {change.error || toggleWhitelist.error ? (
        <div className="mt-3">
          <ErrorBox error={change.error ?? toggleWhitelist.error} />
        </div>
      ) : null}
    </Section>
  );
}
