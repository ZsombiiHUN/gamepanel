import { Link } from "@tanstack/react-router";
import { Archive, Clock, Database, FolderOpen, History, Network, Package, Rocket, Settings, SquareTerminal, Users } from "lucide-react";
import { TAB_INFO, type Tab } from "../../lib/tabs";

const icons: Record<Tab, typeof SquareTerminal> = {
  console: SquareTerminal,
  files: FolderOpen,
  mods: Package,
  backups: Archive,
  schedules: Clock,
  startup: Rocket,
  network: Network,
  databases: Database,
  activity: History,
  users: Users,
  settings: Settings,
};

/** The server's sections as a row of pill tabs. Scrolls sideways on narrow screens rather than wrapping. */
export function ServerTabs({ uuid, tab, can }: { uuid: string; tab: Tab; can: (need?: string) => boolean }) {
  return (
    <nav aria-label="Server sections" className="-mx-1 overflow-x-auto px-1 pb-1">
      <ul className="inline-flex gap-0.5 rounded-[10px] border border-line bg-well p-1">
        {TAB_INFO.filter((t) => can(t.need)).map((t) => {
          const Icon = icons[t.id];
          const on = t.id === tab;
          return (
            <li key={t.id}>
              <Link
                to="/servers/$uuid"
                params={{ uuid }}
                search={{ tab: t.id }}
                aria-current={on ? "page" : undefined}
                className={`flex h-8 items-center gap-1.5 whitespace-nowrap rounded-[7px] px-2.5 text-[13px] font-medium focus-visible:outline-2 focus-visible:outline-indigo-500 ${
                  on
                    ? "bg-zinc-900 text-zinc-100 shadow-[inset_0_0_0_1px_var(--color-line-strong),0_1px_2px_rgb(0_0_0/0.3)]"
                    : "text-zinc-500 hover:text-zinc-100"
                }`}
              >
                <Icon className="h-[15px] w-[15px]" aria-hidden /> {t.label}
              </Link>
            </li>
          );
        })}
      </ul>
    </nav>
  );
}
