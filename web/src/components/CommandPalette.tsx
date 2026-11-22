import { useEffect, useId, useMemo, useRef, useState } from "react";
import { useNavigate } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { CornerDownLeft, Search } from "lucide-react";
import { meQuery, serversQuery } from "../api/queries";
import { rankItems, type PaletteItem } from "../lib/palette";
import { sortServers, stateLabel } from "../lib/servers";
import { TAB_INFO, tabShown } from "../lib/tabs";
import { StateMark } from "./ui";
import { useTranslation } from "react-i18next";

type Item = PaletteItem & { go: () => void; state?: string };

// The list scrolls, so this only bounds the DOM for very large installs.
const MAX_SHOWN = 100;

/** Quick switcher: jump to any server, a tab of the current server, or an admin page. Opened with Ctrl/Cmd+K. */
export function CommandPalette({ open, onClose, currentServer }: { open: boolean; onClose: () => void; currentServer?: string }) {
  const { t } = useTranslation();
  const dialog = useRef<HTMLDialogElement>(null);
  const input = useRef<HTMLInputElement>(null);
  const [q, setQ] = useState("");
  const [active, setActive] = useState(0);
  const navigate = useNavigate();
  const servers = useQuery(serversQuery);
  const me = useQuery(meQuery);
  const listId = useId();

  useEffect(() => {
    const d = dialog.current;
    if (!d) return;
    if (open && !d.open) {
      setQ("");
      setActive(0);
      d.showModal();
      input.current?.focus();
    }
    if (!open && d.open) d.close();
  }, [open]);

  const items = useMemo<Item[]>(() => {
    const out: Item[] = [];
    const cur = (servers.data ?? []).find((s) => s.uuid === currentServer);
    if (cur) {
      for (const info of TAB_INFO) {
        const { id: tab, label, keywords } = info;
        if (!tabShown(info, cur)) continue;
        out.push({
          id: `tab:${tab}`,
          group: t("commandPalette.thisServer", { name: cur.name }),
          label,
          keywords: `${cur.name} tab ${keywords ?? ""}`,
          go: () => void navigate({ to: "/servers/$uuid", params: { uuid: cur.uuid }, search: { tab } }),
        });
      }
    }
    for (const s of sortServers(servers.data ?? [])) {
      out.push({
        id: `server:${s.uuid}`,
        group: t("nav.servers"),
        label: s.name,
        keywords: [s.egg_name, s.node_name, String(s.port), s.state, stateLabel[s.state]].filter(Boolean).join(" "),
        state: s.state,
        go: () => void navigate({ to: "/servers/$uuid", params: { uuid: s.uuid }, search: { tab: "console" } }),
      });
    }
    const pages: [string, string, string][] = [
      ["/", t("commandPalette.allServers"), ""],
      ["/account", t("nav.account"), "profile password two-factor 2fa api keys sessions"],
    ];
    if (me.data?.is_admin)
      pages.push(
        ["/servers/new", t("createServerPage.newServer"), "create add"],
        ["/nodes", t("nav.nodes"), "machines daemon"],
        ["/eggs", t("nav.eggs"), "templates import games"],
        ["/database-hosts", t("nav.databaseHosts"), "mysql mariadb"],
        ["/audit", t("nav.audit"), "events security history"],
      );
    for (const [to, label, keywords] of pages)
      out.push({ id: `page:${to}`, group: t("commandPalette.pages"), label, keywords, go: () => void navigate({ to }) });
    return out;
  }, [servers.data, me.data, currentServer, navigate, t]);

  const shown = useMemo(() => rankItems(items, q).slice(0, MAX_SHOWN), [items, q]);
  const idx = Math.min(active, Math.max(0, shown.length - 1));

  const choose = (it: Item | undefined) => {
    if (!it) return;
    onClose();
    it.go();
  };

  return (
    // Clicking the backdrop closes the switcher; Escape already does that for keyboards.
    // eslint-disable-next-line jsx-a11y/click-events-have-key-events, jsx-a11y/no-noninteractive-element-interactions -- see above
    <dialog
      ref={dialog}
      aria-label={t("commandPalette.quickSwitcher")}
      onClose={onClose}
      onClick={(e) => {
        if (e.target === dialog.current) onClose();
      }}
      className="m-auto mt-[12vh] w-full max-w-xl rounded-xl border border-zinc-700 bg-zinc-900 p-0 text-zinc-100 shadow-2xl backdrop:bg-black/70"
    >
      <div className="flex items-center gap-3 border-b border-line px-4">
        <Search className="h-4 w-4 text-zinc-500" aria-hidden />
        <input
          ref={input}
          role="combobox"
          aria-expanded="true"
          aria-controls={listId}
          aria-activedescendant={shown[idx] ? `${listId}-${idx}` : undefined}
          aria-label={t("commandPalette.searchServersAndPages")}
          autoComplete="off"
          spellCheck={false}
          className="min-h-12 flex-1 bg-transparent text-[15px] outline-none placeholder:text-zinc-500"
          placeholder={t("commandPalette.jumpToAServerOr")}
          value={q}
          onChange={(e) => {
            setQ(e.target.value);
            setActive(0);
          }}
          onKeyDown={(e) => {
            if (e.key === "ArrowDown") {
              e.preventDefault();
              setActive(Math.min(idx + 1, shown.length - 1));
            } else if (e.key === "ArrowUp") {
              e.preventDefault();
              setActive(Math.max(idx - 1, 0));
            } else if (e.key === "Home") {
              e.preventDefault();
              setActive(0);
            } else if (e.key === "End") {
              e.preventDefault();
              setActive(shown.length - 1);
            } else if (e.key === "Enter") {
              e.preventDefault();
              choose(shown[idx]);
            }
          }}
        />
        <kbd className="rounded border border-zinc-700 px-1.5 font-mono text-[11px] text-zinc-500" aria-hidden>
          {t("commandPalette.esc")}
        </kbd>
      </div>
      <ul id={listId} role="listbox" aria-label={t("commandPalette.results")} className="max-h-[50vh] overflow-y-auto p-1.5">
        {shown.length === 0 && (
          <li role="presentation" className="px-3 py-6 text-center text-sm text-zinc-500">
            {t("commandPalette.nothingMatches", { q: q.trim() })}
          </li>
        )}
        {shown.map((it, i) => (
          <li key={it.id} role="presentation">
            {(i === 0 || shown[i - 1]?.group !== it.group) && <div className="px-3 pb-1 pt-2.5 text-xs text-zinc-500">{it.group}</div>}
            {/* Options are reached with the arrow keys from the search box (aria-activedescendant), so they take no focus. */}
            {/* eslint-disable-next-line jsx-a11y/click-events-have-key-events, jsx-a11y/interactive-supports-focus -- see above */}
            <div
              id={`${listId}-${i}`}
              role="option"
              aria-selected={i === idx}
              onMouseMove={() => setActive(i)}
              onClick={() => choose(it)}
              className={`flex min-h-10 cursor-pointer items-center gap-2.5 rounded-lg px-3 text-sm ${i === idx ? "bg-chip text-zinc-100" : "text-zinc-300"}`}
            >
              {it.state && <StateMark state={it.state} />}
              <span className="flex-1 truncate">{it.label}</span>
              {it.state && it.keywords && (
                <span className="hidden truncate text-xs text-zinc-500 sm:inline">{it.keywords.split(" ").slice(0, 2).join(" · ")}</span>
              )}
              {i === idx && <CornerDownLeft className="h-3.5 w-3.5 text-zinc-500" aria-hidden />}
            </div>
          </li>
        ))}
      </ul>
    </dialog>
  );
}
