import { useEffect, useState } from "react";
import { Link, Outlet, useNavigate, useParams, useRouteContext, useRouterState, useSearch } from "@tanstack/react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Archive,
  ChevronDown,
  History,
  Database as DatabaseIcon,
  ClipboardList,
  Clock,
  FolderOpen,
  LayoutGrid,
  LogOut,
  Menu,
  Network,
  Package,
  Plus,
  Rocket,
  Search,
  Settings,
  ShieldCheck,
  SquareTerminal,
  Users,
  X,
  Egg,
  HardDrive,
} from "lucide-react";
import { api, call } from "../api/client";
import { meQuery, serverQuery, serversQuery } from "../api/queries";
import { CommandPalette } from "../components/CommandPalette";
import { ThemeSwitch } from "../components/ThemeSwitch";
import { Empty, StateMark } from "../components/ui";
import { isTypingTarget } from "../lib/palette";
import { sortServers } from "../lib/servers";
import { canSee, TAB_INFO, type Tab } from "../lib/tabs";

const item =
  "flex min-h-9 items-center gap-2.5 rounded-[7px] px-2 text-sm text-zinc-400 hover:bg-row-hover hover:text-zinc-100 focus-visible:outline-2 focus-visible:outline-indigo-500";
const itemOn = "bg-row-active font-medium !text-zinc-100";

const icons: Record<Tab, typeof SquareTerminal> = {
  console: SquareTerminal,
  files: FolderOpen,
  mods: Package,
  backups: Archive,
  schedules: Clock,
  startup: Rocket,
  network: Network,
  databases: DatabaseIcon,
  activity: History,
  users: Users,
  settings: Settings,
};
const sections = TAB_INFO.map((t) => ({ ...t, icon: icons[t.id] }));

function Sidebar({
  isAdmin,
  email,
  onSignOut,
  signingOut,
  onSearch,
}: {
  isAdmin: boolean;
  email: string;
  onSignOut: () => void;
  signingOut: boolean;
  onSearch: () => void;
}) {
  const servers = useQuery(serversQuery);
  const current = useParams({ from: "/app/servers/$uuid", shouldThrow: false })?.uuid;
  const tab = useSearch({ from: "/app/servers/$uuid", shouldThrow: false })?.tab ?? "console";
  const cur = useQuery({ ...serverQuery(current ?? ""), enabled: !!current });
  const path = useRouterState({ select: (s) => s.location.pathname });
  const can = (need?: string) => !cur.data || canSee(need, cur.data);

  return (
    <div className="flex h-full flex-col gap-3 px-2.5 pb-2.5 pt-3">
      <Link
        to="/"
        className="flex shrink-0 items-center gap-2.5 px-2 py-1 font-display text-lg font-bold tracking-tight focus-visible:outline-2 focus-visible:outline-indigo-500"
      >
        <span className="h-5 w-5 rounded-md bg-indigo-500" aria-hidden /> GamePanel
      </Link>
      <button
        type="button"
        onClick={onSearch}
        aria-keyshortcuts="Control+K Meta+K"
        className="flex min-h-9 shrink-0 items-center gap-2 rounded-lg border border-zinc-800 bg-thead px-2.5 text-[13px] text-zinc-400 hover:text-zinc-200 focus-visible:outline-2 focus-visible:outline-indigo-500"
      >
        <Search className="h-4 w-4" aria-hidden />{" "}
        <span className="flex-1 text-left">
          Search <span className="sr-only">servers and pages</span>
        </span>
        <kbd className="rounded border border-zinc-700 px-1.5 font-mono text-[11px]" aria-hidden>
          Ctrl K
        </kbd>
      </button>
      {/* Only the server list scrolls; the admin links and account stay in view however many servers there are. */}
      <nav aria-label="Servers" className="-mx-1 flex min-h-0 flex-1 flex-col gap-px overflow-y-auto px-1">
        <Link
          to="/"
          activeOptions={{ exact: true }}
          className={item}
          activeProps={{ className: `${item} ${itemOn}`, "aria-current": "page" }}
        >
          <LayoutGrid className="h-4 w-4" aria-hidden /> <span className="flex-1">All servers</span>
          {servers.data && <span className="font-mono text-xs text-zinc-500">{servers.data.length}</span>}
        </Link>
        <div className="px-2 pb-1.5 pt-3.5 text-xs text-zinc-500">Servers</div>
        {sortServers(servers.data ?? []).map((s) =>
          s.uuid === current ? (
            <div key={s.uuid}>
              <Link
                to="/servers/$uuid"
                params={{ uuid: s.uuid }}
                search={{ tab: "console" }}
                className={`${item} !text-zinc-100 font-medium`}
              >
                <StateMark state={s.state} /> <span className="flex-1 truncate">{s.name}</span>
                <ChevronDown className="h-3.5 w-3.5 text-zinc-500" aria-hidden />
              </Link>
              <ul className="ml-[15px] mt-0.5 mb-1.5 flex flex-col gap-px border-l border-line-strong pl-2">
                {sections
                  .filter((x) => can(x.need))
                  .map((x) => {
                    const on = path.startsWith("/servers/") && tab === x.id;
                    return (
                      <li key={x.id}>
                        <Link
                          to="/servers/$uuid"
                          params={{ uuid: s.uuid }}
                          search={{ tab: x.id }}
                          aria-current={on ? "page" : undefined}
                          className={`${item} min-h-[34px] ${on ? itemOn : ""}`}
                        >
                          <x.icon className="h-[15px] w-[15px]" aria-hidden /> {x.label}
                        </Link>
                      </li>
                    );
                  })}
              </ul>
            </div>
          ) : (
            <Link key={s.uuid} to="/servers/$uuid" params={{ uuid: s.uuid }} search={{ tab: "console" }} className={item}>
              <StateMark state={s.state} /> <span className="truncate">{s.name}</span>
            </Link>
          ),
        )}
        {isAdmin && (
          <Link to="/servers/new" className={`${item} mt-0.5 text-zinc-500`} activeProps={{ className: `${item} ${itemOn}` }}>
            <Plus className="h-4 w-4" aria-hidden /> New server
          </Link>
        )}
      </nav>
      {isAdmin && (
        <nav aria-label="Admin" className="flex shrink-0 flex-col gap-px">
          <div className="px-2 pb-1.5 pt-2 text-xs text-zinc-500">Admin</div>
          {(
            [
              ["/nodes", "Nodes", HardDrive],
              ["/eggs", "Eggs", Egg],
              ["/database-hosts", "Database hosts", DatabaseIcon],
              ["/audit", "Audit log", ClipboardList],
            ] as const
          ).map(([to, label, Icon]) => (
            <Link key={to} to={to} className={item} activeProps={{ className: `${item} ${itemOn}`, "aria-current": "page" }}>
              <Icon className="h-4 w-4" aria-hidden /> {label}
            </Link>
          ))}
        </nav>
      )}
      <div className="flex shrink-0 items-center justify-between px-2">
        <span className="text-xs text-zinc-500">Theme</span>
        <ThemeSwitch />
      </div>
      <div className="flex shrink-0 items-center gap-1 border-t border-line pt-2.5">
        <Link
          to="/account"
          className="flex min-w-0 flex-1 items-center gap-2.5 rounded-[7px] px-2 py-1.5 text-[13.5px] text-zinc-400 hover:text-zinc-100 focus-visible:outline-2 focus-visible:outline-indigo-500"
        >
          <span
            className="flex h-[26px] w-[26px] shrink-0 items-center justify-center rounded-full bg-line-strong text-xs font-semibold text-zinc-100"
            aria-hidden
          >
            {email.slice(0, 1).toUpperCase()}
          </span>
          <span className="truncate">{email}</span>
        </Link>
        <button
          type="button"
          className="btn btn-ghost !min-h-8 !px-2"
          aria-label="Sign out"
          title="Sign out"
          onClick={onSignOut}
          disabled={signingOut}
        >
          <LogOut className="h-4 w-4" aria-hidden />
        </button>
      </div>
    </div>
  );
}

/**
 * The sidebar while the panel holds this session to setting up two-factor:
 * no server list or search (they would be refused), just a way out.
 */
function RestrictedSidebar({ email, onSignOut, signingOut }: { email: string; onSignOut: () => void; signingOut: boolean }) {
  return (
    <div className="flex h-full flex-col gap-3 px-2.5 pb-2.5 pt-3">
      <span className="flex shrink-0 items-center gap-2.5 px-2 py-1 font-display text-lg font-bold tracking-tight">
        <span className="h-5 w-5 rounded-md bg-indigo-500" aria-hidden /> GamePanel
      </span>
      <p className="flex-1 px-2 text-[13px] text-zinc-500">Set up two-factor authentication to use the rest of the panel.</p>
      <div className="truncate px-2 text-[13px] text-zinc-400">{email}</div>
      <button type="button" className={item} onClick={onSignOut} disabled={signingOut}>
        <LogOut className="h-4 w-4" aria-hidden /> Sign out
      </button>
    </div>
  );
}

/** Shown instead of any page when the account has a second factor this sign-in did not use. */
function SignInAgain() {
  return (
    <div className="mx-auto max-w-lg">
      <Empty icon={<ShieldCheck className="h-5 w-5" aria-hidden />} title="Sign in with your second factor">
        This panel requires two-factor authentication. Your account has a passkey or authenticator app, but this sign-in did not use it.
        Sign out, then sign in again with your passkey, or with your password and authenticator code.
      </Empty>
    </div>
  );
}

export function AppShell() {
  const routeUser = useRouteContext({ from: "/app", select: (c) => c.me });
  // Follow the live answer, so setting up a second factor lifts the restriction at once.
  const user = useQuery(meQuery).data ?? routeUser;
  const qc = useQueryClient();
  const navigate = useNavigate();
  // The drawer remembers where it was opened, so any navigation closes it without an effect.
  const [openedAt, setOpenedAt] = useState<string | null>(null);
  const [palette, setPalette] = useState(false);
  const currentServer = useParams({ from: "/app/servers/$uuid", shouldThrow: false })?.uuid;
  const loc = useRouterState({ select: (s) => s.location.href });
  const open = openedAt === loc;
  const setOpen = (v: boolean) => setOpenedAt(v ? loc : null);
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && !e.altKey && !e.shiftKey && e.key.toLowerCase() === "k") {
        e.preventDefault();
        setPalette((v) => !v);
      } else if (e.key === "/" && !e.ctrlKey && !e.metaKey && !e.altKey && !isTypingTarget(e.target)) {
        const box = document.querySelector<HTMLInputElement>("[data-page-search]");
        if (box) {
          e.preventDefault();
          box.focus();
          box.select();
        }
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);
  const logout = useMutation({
    mutationFn: () => call(api.POST("/auth/logout")),
    onSettled: () => {
      qc.clear();
      void navigate({ to: "/login" });
    },
  });

  // While restricted the layout keeps its shape (only the sidebar's contents
  // change), so the page does not remount when the restriction lifts: the
  // account page must keep showing the recovery codes it was just given.
  const restricted = user.mfa_required;

  return (
    <div className="min-h-screen lg:flex">
      <header className="flex items-center gap-2 border-b border-line bg-side px-3 py-2 lg:hidden">
        <button type="button" className="btn btn-ghost !px-2" aria-label="Open menu" aria-expanded={open} onClick={() => setOpen(true)}>
          <Menu className="h-5 w-5" aria-hidden />
        </button>
        <span className="font-display text-lg font-bold tracking-tight">GamePanel</span>
      </header>
      {open && <div className="fixed inset-0 z-30 bg-black/60 lg:hidden" onClick={() => setOpen(false)} aria-hidden />}
      <aside
        aria-label="Navigation"
        className={`fixed left-0 top-0 z-40 h-dvh w-64 overflow-hidden border-r border-line bg-side transition-transform lg:sticky lg:top-0 lg:z-auto lg:h-screen lg:w-[248px] lg:shrink-0 lg:translate-x-0 ${open ? "translate-x-0" : "-translate-x-full"}`}
      >
        <button
          type="button"
          className="btn btn-ghost absolute right-2 top-2 !px-2 lg:hidden"
          aria-label="Close menu"
          onClick={() => setOpen(false)}
        >
          <X className="h-5 w-5" aria-hidden />
        </button>
        {restricted ? (
          <RestrictedSidebar email={user.email} onSignOut={() => logout.mutate()} signingOut={logout.isPending} />
        ) : (
          <Sidebar
            isAdmin={user.is_admin}
            email={user.email}
            onSignOut={() => logout.mutate()}
            signingOut={logout.isPending}
            onSearch={() => {
              setOpen(false);
              setPalette(true);
            }}
          />
        )}
      </aside>
      <main className="min-w-0 flex-1 px-5 py-6 sm:px-8 lg:px-10 lg:py-8">{user.mfa_sign_in_again ? <SignInAgain /> : <Outlet />}</main>
      {!restricted && <CommandPalette open={palette} onClose={() => setPalette(false)} currentServer={currentServer} />}
    </div>
  );
}
