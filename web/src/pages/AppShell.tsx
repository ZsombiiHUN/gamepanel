import { useEffect, useState } from "react";
import { Link, Outlet, useNavigate, useParams, useRouteContext, useRouterState, useSearch } from "@tanstack/react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  ClipboardList,
  Database as DatabaseIcon,
  Egg,
  HardDrive,
  LayoutGrid,
  LogOut,
  Menu,
  Plus,
  Search,
  ShieldCheck,
  X,
} from "lucide-react";
import { api, call } from "../api/client";
import { meQuery, serverQuery, serversQuery } from "../api/queries";
import { CommandPalette } from "../components/CommandPalette";
import { ThemeSwitch } from "../components/ThemeSwitch";
import { Empty, ServerIcon, StateMark } from "../components/ui";
import { isTypingTarget } from "../lib/palette";
import { shownState, sortServers } from "../lib/servers";
import { TAB_INFO } from "../lib/tabs";

const item =
  "group flex min-h-8 items-center gap-2.5 rounded-[7px] px-2 text-[13.5px] text-zinc-400 hover:bg-row-hover hover:text-zinc-100 focus-visible:outline-2 focus-visible:outline-indigo-500";
const itemOn = "bg-row-active font-medium !text-zinc-100 shadow-[inset_0_0_0_1px_var(--color-line)]";
const iconCls = "h-4 w-4 shrink-0 text-zinc-500 group-hover:text-zinc-300 group-aria-[current=page]:text-zinc-100";

const ADMIN = [
  ["/nodes", "Nodes", HardDrive],
  ["/eggs", "Eggs", Egg],
  ["/database-hosts", "Database hosts", DatabaseIcon],
  ["/audit", "Audit log", ClipboardList],
] as const;

/** The product mark: a light tile with two blocks, the same in both themes. */
function Mark() {
  return (
    <span
      aria-hidden
      className="relative grid h-[30px] w-[30px] shrink-0 place-content-center rounded-lg bg-gradient-to-b from-[#f6f5f1] to-[#d9d7d0] shadow-[inset_0_-1px_0_rgb(0_0_0/0.2)]"
    >
      <span className="relative block h-[11px] w-[11px] rounded-[2px] bg-[#121211] after:absolute after:-bottom-[3px] after:-right-[3px] after:h-[5px] after:w-[5px] after:rounded-[1px] after:bg-[#121211] after:content-['']" />
    </span>
  );
}

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
  const n = servers.data?.length;

  return (
    <div className="flex h-full flex-col gap-2 px-2.5 pb-2.5 pt-2.5">
      <Link
        to="/"
        className="flex shrink-0 items-center gap-2.5 rounded-lg px-1.5 py-1.5 focus-visible:outline-2 focus-visible:outline-indigo-500"
      >
        <Mark />
        <span className="min-w-0 leading-tight">
          <span className="block text-sm font-semibold tracking-tight text-zinc-100">GamePanel</span>
          <span className="block text-xs text-zinc-500">{n === undefined ? "\u00a0" : `${n} server${n === 1 ? "" : "s"}`}</span>
        </span>
      </Link>
      <button
        type="button"
        onClick={onSearch}
        aria-keyshortcuts="Control+K Meta+K"
        className="mb-1 flex min-h-8 shrink-0 items-center gap-2 rounded-lg border border-line bg-zinc-900 pl-2.5 pr-2 text-[13px] text-zinc-500 hover:text-zinc-200 focus-visible:outline-2 focus-visible:outline-indigo-500"
      >
        <Search className="h-4 w-4" aria-hidden />{" "}
        <span className="flex-1 text-left">
          Search <span className="sr-only">servers and pages</span>
        </span>
        <kbd className="kbd" aria-hidden>
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
          <LayoutGrid className={iconCls} aria-hidden /> <span className="flex-1">All servers</span>
          {n !== undefined && <span className="font-mono text-xs text-zinc-500">{n}</span>}
        </Link>
        <div className="flex items-center justify-between px-2 pb-1 pt-3 text-[11.5px] font-medium text-zinc-500">
          Servers
          {isAdmin && (
            <Link
              to="/servers/new"
              aria-label="New server"
              title="New server"
              className="-mr-1 grid h-6 w-6 place-content-center rounded-md text-zinc-500 hover:bg-row-hover hover:text-zinc-100 focus-visible:outline-2 focus-visible:outline-indigo-500"
            >
              <Plus className="h-3.5 w-3.5" aria-hidden />
            </Link>
          )}
        </div>
        {sortServers(servers.data ?? []).map((s) => (
          <Link
            key={s.uuid}
            to="/servers/$uuid"
            params={{ uuid: s.uuid }}
            search={{ tab: "console" }}
            aria-current={s.uuid === current ? "page" : undefined}
            className={`${item} ${s.uuid === current ? itemOn : ""}`}
          >
            <ServerIcon name={s.name} size={20} /> <span className="flex-1 truncate">{s.name}</span>
            <StateMark state={shownState(s)} />
          </Link>
        ))}
      </nav>
      {isAdmin && (
        <nav aria-label="Admin" className="flex shrink-0 flex-col gap-px">
          <div className="px-2 pb-1 pt-2 text-[11.5px] font-medium text-zinc-500">Admin</div>
          {ADMIN.map(([to, label, Icon]) => (
            <Link key={to} to={to} className={item} activeProps={{ className: `${item} ${itemOn}`, "aria-current": "page" }}>
              <Icon className={iconCls} aria-hidden /> {label}
            </Link>
          ))}
        </nav>
      )}
      <div className="mt-1 flex shrink-0 items-center gap-1 rounded-[10px] border border-line bg-zinc-900 p-1.5">
        <Link
          to="/account"
          className="flex min-w-0 flex-1 items-center gap-2.5 rounded-[7px] px-1 py-0.5 text-[13px] text-zinc-300 hover:text-zinc-100 focus-visible:outline-2 focus-visible:outline-indigo-500"
        >
          <span
            className="grid h-7 w-7 shrink-0 place-content-center rounded-[7px] bg-gradient-to-br from-zinc-600 to-zinc-800 text-[11.5px] font-semibold text-zinc-100"
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

/** Where you are, as a trail: Servers / name / tab, or the admin page. */
function Crumbs() {
  const path = useRouterState({ select: (s) => s.location.pathname });
  const uuid = useParams({ from: "/app/servers/$uuid", shouldThrow: false })?.uuid;
  const tab = useSearch({ from: "/app/servers/$uuid", shouldThrow: false })?.tab ?? "console";
  const server = useQuery({ ...serverQuery(uuid ?? ""), enabled: !!uuid });
  const admin = ADMIN.find(([to]) => path === to);
  let trail: { label: string; to?: "/" }[];
  if (uuid)
    trail = [
      { label: "Servers", to: "/" },
      { label: server.data?.name ?? "…" },
      { label: TAB_INFO.find((t) => t.id === tab)?.label ?? "" },
    ];
  else if (path === "/servers/new") trail = [{ label: "Servers", to: "/" }, { label: "New server" }];
  else if (admin) trail = [{ label: "Admin" }, { label: admin[1] }];
  else if (path === "/account") trail = [{ label: "Account" }];
  else trail = [{ label: "Servers" }];
  return (
    <nav aria-label="Location" className="min-w-0">
      <ol className="flex min-w-0 items-center gap-2 text-[13.5px] text-zinc-500">
        {trail.map((c, i) => {
          const last = i === trail.length - 1;
          return (
            <li key={i} className={`flex min-w-0 items-center gap-2 ${last ? "" : "shrink-0"}`}>
              {i > 0 && (
                <span aria-hidden className="text-zinc-600">
                  /
                </span>
              )}
              {c.to && !last ? (
                <Link to={c.to} className="rounded hover:text-zinc-100 focus-visible:outline-2 focus-visible:outline-indigo-500">
                  {c.label}
                </Link>
              ) : (
                <span className={`truncate ${last ? "font-medium text-zinc-100" : ""}`} aria-current={last ? "page" : undefined}>
                  {c.label}
                </span>
              )}
            </li>
          );
        })}
      </ol>
    </nav>
  );
}

/**
 * The sidebar while the panel holds this session to setting up two-factor:
 * no server list or search (they would be refused), just a way out.
 */
function RestrictedSidebar({ email, onSignOut, signingOut }: { email: string; onSignOut: () => void; signingOut: boolean }) {
  return (
    <div className="flex h-full flex-col gap-3 px-2.5 pb-2.5 pt-3">
      <span className="flex shrink-0 items-center gap-2.5 px-1.5 py-1.5 text-sm font-semibold tracking-tight">
        <Mark /> GamePanel
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
    <div className="min-h-screen bg-side lg:flex">
      <header className="flex items-center gap-2 border-b border-line bg-side px-3 py-2 lg:hidden">
        <button type="button" className="btn btn-ghost !px-2" aria-label="Open menu" aria-expanded={open} onClick={() => setOpen(true)}>
          <Menu className="h-5 w-5" aria-hidden />
        </button>
        <span className="text-[15px] font-semibold tracking-tight">GamePanel</span>
      </header>
      {open && <div className="fixed inset-0 z-30 bg-black/60 lg:hidden" onClick={() => setOpen(false)} aria-hidden />}
      <aside
        aria-label="Navigation"
        className={`fixed left-0 top-0 z-40 h-dvh w-64 overflow-hidden border-r border-line bg-side transition-transform lg:sticky lg:top-0 lg:z-auto lg:h-screen lg:w-[244px] lg:border-r-0 lg:shrink-0 lg:translate-x-0 ${open ? "translate-x-0" : "-translate-x-full"}`}
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
      {/* The page sits in an inset panel beside the sidebar, with the trail and the theme switch above it. */}
      <div className="min-w-0 flex-1 bg-panel lg:my-2 lg:mr-2 lg:min-h-[calc(100vh-16px)] lg:rounded-[14px] lg:border lg:border-line lg:shadow-panel">
        <div className="sticky top-0 z-20 hidden h-[52px] items-center gap-3 border-b border-line bg-panel/90 px-5 backdrop-blur lg:flex lg:rounded-t-[14px]">
          {!restricted && <Crumbs />}
          <div className="ml-auto">
            <ThemeSwitch />
          </div>
        </div>
        <main className="mx-auto max-w-[1280px] px-5 py-6 sm:px-8 lg:px-8 lg:py-7">
          {user.mfa_sign_in_again ? <SignInAgain /> : <Outlet />}
        </main>
      </div>
      {!restricted && <CommandPalette open={palette} onClose={() => setPalette(false)} currentServer={currentServer} />}
    </div>
  );
}
