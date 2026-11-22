import { Link, useNavigate, useSearch } from "@tanstack/react-router";
import { useMutation, useQuery } from "@tanstack/react-query";
import { Loader2, Play, Plus, Search, Server, Square, SquareTerminal, X } from "lucide-react";
import { api, call, type components } from "../api/client";
import { invalidates, keys, meQuery, serversQuery } from "../api/queries";
import { SearchInput } from "../components/SearchInput";
import { useSearchText } from "../hooks/useSearchText";
import { Empty, ErrorBox, Meter, ServerIcon, Spinner, StateBadge } from "../components/ui";
import { formatBytes } from "../lib/format";
import { countGroups, GROUPS, groupLabel, groupOf, matchesServer, shownState, sortServers, summarize, type Group } from "../lib/servers";
import { useTranslation } from "react-i18next";

type ServerView = components["schemas"]["ServerView"];

function ServerCard({ s }: { s: ServerView }) {
  const { t } = useTranslation();
  const running = s.state === "running" || s.state === "starting";
  const busy = s.state === "installing" || s.state === "stopping" || s.state === "install_failed";
  const power = useMutation({
    mutationFn: () =>
      call(api.POST("/servers/{uuid}/power", { params: { path: { uuid: s.uuid } }, body: { action: running ? "stop" : "start" } })),
    meta: invalidates(keys.servers),
  });
  const live = s.stats && s.state === "running" ? s.stats : null;
  const memPct = live && s.memory_mb ? (live.mem_bytes / (s.memory_mb * 1024 * 1024)) * 100 : null;
  const diskPct = s.stats && s.disk_mb ? (s.stats.disk_bytes / (s.disk_mb * 1024 * 1024)) * 100 : null;
  const cpuPct = live ? (s.cpu_pct ? (live.cpu_pct / s.cpu_pct) * 100 : live.cpu_pct) : null;
  return (
    <article className="relative flex flex-col gap-3.5 rounded-xl border border-line bg-zinc-900 p-4 transition-colors hover:border-line-strong">
      <div className="flex items-center gap-3">
        <ServerIcon name={s.name} size={36} />
        <div className="min-w-0 flex-1">
          <Link
            to="/servers/$uuid"
            params={{ uuid: s.uuid }}
            search={{ tab: "console" }}
            className="block truncate text-[14.5px] font-semibold tracking-tight after:absolute after:inset-0 focus-visible:outline-2 focus-visible:outline-indigo-500"
          >
            {s.name}
          </Link>
          <div className="truncate text-[12.5px] text-zinc-500">
            {[s.egg_name, t("serversPage.port", { port: s.port }), s.node_online ? "" : t("serversPage.nodeOffline")]
              .filter(Boolean)
              .join(" · ")}
          </div>
        </div>
        <StateBadge state={s.state} />
      </div>
      <div className="grid grid-cols-2 gap-x-4 gap-y-3">
        {live && <Meter label={t("common.cpu")} text={`${live.cpu_pct.toFixed(0)}%`} pct={cpuPct} />}
        {live && (
          <Meter
            label={t("common.memory")}
            text={`${formatBytes(live.mem_bytes)} / ${formatBytes(s.memory_mb * 1024 * 1024)}`}
            pct={memPct}
          />
        )}
        {s.stats && (
          <div className={live ? "" : "col-span-2"}>
            <Meter
              label={t("common.disk")}
              text={`${formatBytes(s.stats.disk_bytes)} / ${s.disk_mb ? formatBytes(s.disk_mb * 1024 * 1024) : t("usage.noLimit")}`}
              pct={diskPct}
            />
          </div>
        )}
        {!s.stats && (
          <p className="col-span-2 py-0.5 text-[12.5px] text-zinc-500">
            {s.state === "installing"
              ? t("serversPage.settingUpFilesThisTakes")
              : s.state === "suspended"
                ? t("serversPage.suspendedContactYourHostTo")
                : s.state === "install_failed"
                  ? t("serversPage.theInstallFailedOpenThe")
                  : t("serversPage.notRunningStartItTo")}
          </p>
        )}
      </div>
      <div className="relative mt-auto flex gap-2 border-t border-line pt-3.5">
        <Link to="/servers/$uuid" params={{ uuid: s.uuid }} search={{ tab: "console" }} className="btn !min-h-8 flex-1">
          <SquareTerminal className="h-4 w-4" aria-hidden /> {t("serversPage.console")}
        </Link>
        <button
          type="button"
          className="btn !min-h-8 flex-1"
          disabled={power.isPending || busy || s.state === "suspended" || !s.node_online || !s.permissions.includes("power")}
          onClick={() => power.mutate()}
        >
          {power.isPending ? (
            <Loader2 className="h-4 w-4 animate-spin" aria-hidden />
          ) : running ? (
            <Square className="h-3.5 w-3.5 !text-bad" aria-hidden />
          ) : !busy && s.state !== "suspended" ? (
            <Play className="h-3.5 w-3.5 !text-good" aria-hidden />
          ) : null}
          {s.state === "installing"
            ? t("serversPage.installing")
            : s.state === "install_failed"
              ? t("serversPage.installFailed")
              : running
                ? t("common.stop")
                : t("common.start")}
        </button>
      </div>
      {power.error && <ErrorBox error={power.error} />}
    </article>
  );
}

export function ServersPage() {
  const { t } = useTranslation();
  const servers = useQuery(serversQuery);
  const me = useQuery(meQuery);
  // The filter lives in the address: a link to "only the stopped servers" works, and Back undoes a filter.
  const { q: urlQuery, status } = useSearch({ from: "/app/" });
  const group = status ?? "all";
  const navigate = useNavigate({ from: "/" });
  const setGroup = (g: Group | "all") =>
    void navigate({ search: (prev) => ({ ...prev, status: g === "all" ? undefined : g }), replace: true });
  const [q, setQ] = useSearchText(
    urlQuery,
    (text) => void navigate({ search: (prev) => ({ ...prev, q: text || undefined }), replace: true }),
    400,
  );
  // A suspended server shows (and filters and counts) as suspended, whatever its node says.
  const all = (servers.data ?? []).map((s) => ({ ...s, state: shownState(s) }));
  const groups = countGroups(all);
  const filtering = q.trim() !== "" || group !== "all";
  const list = sortServers(all).filter((s) => (group === "all" || groupOf(s.state) === group) && matchesServer(s, q));
  const summary = summarize(all);
  const clear = () => {
    setQ("");
    void navigate({ search: {}, replace: true });
  };
  const chips: (Group | "all")[] = ["all", ...GROUPS.filter((g) => groups[g] > 0)];

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div>
          <h1 className="h-display text-[22px] leading-tight">{t("common.servers")}</h1>
          {summary && <p className="mt-1 text-[13.5px] text-zinc-400">{summary}</p>}
        </div>
        <div className="flex gap-2">
          <SearchInput
            label={t("serversPage.filterServers")}
            placeholder={t("serversPage.filterByNameGamePort")}
            value={q}
            onChange={setQ}
            hint={
              <kbd className="kbd hidden sm:inline-flex" aria-hidden>
                /
              </kbd>
            }
          />
          {me.data?.is_admin && (
            <Link to="/servers/new" className="btn btn-primary">
              <Plus className="h-4 w-4" aria-hidden /> {t("serversPage.newServer")}
            </Link>
          )}
        </div>
      </div>
      {chips.length > 2 && (
        <div
          role="group"
          aria-label={t("serversPage.filterByStatus")}
          className="inline-flex max-w-full flex-wrap gap-0.5 rounded-[10px] border border-line bg-well p-1"
        >
          {chips.map((g) => (
            <button
              key={g}
              type="button"
              aria-pressed={group === g}
              onClick={() => setGroup(g)}
              className={`inline-flex h-7 items-center gap-2 rounded-[7px] px-2.5 text-[13px] font-medium focus-visible:outline-2 focus-visible:outline-indigo-500 ${group === g ? "bg-zinc-900 text-zinc-100 shadow-[inset_0_0_0_1px_var(--color-line-strong),0_1px_2px_rgb(0_0_0/0.3)]" : "text-zinc-500 hover:text-zinc-100"}`}
            >
              {g === "all" ? t("serversPage.all") : groupLabel[g]}
              <span className="font-mono text-xs text-zinc-500">{g === "all" ? all.length : groups[g]}</span>
            </button>
          ))}
        </div>
      )}
      {servers.isPending && <Spinner />}
      <ErrorBox error={servers.error} />
      {all.length === 0 && servers.data && (
        <Empty icon={<Server className="h-5 w-5" aria-hidden />} title={t("serversPage.noServersYet")}>
          {me.data?.is_admin ? t("serversPage.createOneToGetStarted") : t("serversPage.askAnAdminToCreate")}
        </Empty>
      )}
      {all.length > 0 && list.length === 0 && (
        <Empty icon={<Search className="h-5 w-5" aria-hidden />} title={t("serversPage.noServersMatch")}>
          {q.trim() ? t("serversPage.nothingMatchesQuery", { q: q.trim() }) : t("serversPage.nothingMatchesStatus")}{" "}
          <button type="button" className="link inline-flex items-center gap-1" onClick={clear}>
            <X className="h-3.5 w-3.5" aria-hidden /> {t("serversPage.clearFilter")}
          </button>
        </Empty>
      )}
      <ul className="grid gap-3 [grid-template-columns:repeat(auto-fill,minmax(min(100%,320px),1fr))]">
        {list.map((s) => (
          <li key={s.uuid} className="flex min-w-0">
            <div className="min-w-0 flex-1 [&>article]:h-full">
              <ServerCard s={s} />
            </div>
          </li>
        ))}
        {me.data?.is_admin && !filtering && all.length > 0 && (
          <li>
            <Link
              to="/servers/new"
              className="flex h-full min-h-40 items-center justify-center gap-2 rounded-xl border border-dashed border-line-strong text-[13px] text-zinc-500 hover:bg-row-hover hover:text-zinc-200 focus-visible:outline-2 focus-visible:outline-indigo-500"
            >
              <Plus className="h-4 w-4" aria-hidden />
              {t("serversPage.newServer")}
            </Link>
          </li>
        )}
      </ul>
    </div>
  );
}
