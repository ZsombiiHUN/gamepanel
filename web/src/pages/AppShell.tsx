import { useEffect, useState } from "react";
import { Link, Navigate, Outlet, useNavigate, useParams, useRouterState, useSearch } from "@tanstack/react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Archive, ChevronDown, ClipboardList, Clock, FolderOpen, LayoutGrid, LogOut, Menu, Plus, Settings, SquareTerminal, Users, X, Egg, HardDrive } from "lucide-react";
import { api, ApiError, call } from "../api/client";
import { meQuery, serverQuery, serversQuery } from "../api/queries";
import { ErrorBox, Spinner, StateMark } from "../components/ui";
import type { Tab } from "./ServerDetailPage";

const item = "flex min-h-9 items-center gap-2.5 rounded-[7px] px-2 text-sm text-zinc-400 hover:bg-[#14181c] hover:text-zinc-100 focus-visible:outline-2 focus-visible:outline-indigo-500";
const itemOn = "bg-[#181c21] font-medium !text-zinc-100";

// need: a permission the viewer must hold, or "owner" for owner-only sections.
const sections: { id: Tab; label: string; icon: typeof SquareTerminal; need?: string }[] = [
  { id: "console", label: "Console", icon: SquareTerminal, need: "console.read" },
  { id: "files", label: "Files", icon: FolderOpen, need: "files.read" },
  { id: "backups", label: "Backups", icon: Archive, need: "backups.read" },
  { id: "schedules", label: "Schedules", icon: Clock, need: "schedules.manage" },
  { id: "users", label: "Access", icon: Users, need: "owner" },
  { id: "settings", label: "Settings", icon: Settings },
];

function Sidebar({ isAdmin, email, onSignOut, signingOut }: { isAdmin: boolean; email: string; onSignOut: () => void; signingOut: boolean }) {
  const servers = useQuery(serversQuery);
  const params = useParams({ strict: false }) as { uuid?: string };
  const search = useSearch({ strict: false }) as { tab?: Tab };
  const current = params.uuid;
  const cur = useQuery({ ...serverQuery(current ?? ""), enabled: !!current });
  const path = useRouterState({ select: (s) => s.location.pathname });
  const can = (need?: string) => !need || !cur.data || (need === "owner" ? cur.data.owner : cur.data.permissions.includes(need));

  return (
    <div className="flex h-full flex-col gap-3.5 px-2.5 pb-2.5 pt-3">
      <Link to="/" className="flex items-center gap-2.5 px-2 py-1 font-display text-lg font-bold tracking-tight focus-visible:outline-2 focus-visible:outline-indigo-500">
        <span className="h-5 w-5 rounded-md bg-indigo-500" aria-hidden /> GamePanel
      </Link>
      <nav aria-label="Servers" className="flex flex-col gap-px">
        <Link to="/" activeOptions={{ exact: true }} className={item} activeProps={{ className: `${item} ${itemOn}`, "aria-current": "page" }}>
          <LayoutGrid className="h-4 w-4" aria-hidden /> <span className="flex-1">All servers</span>
          {servers.data && <span className="font-mono text-xs text-zinc-500">{servers.data.length}</span>}
        </Link>
        <div className="px-2 pb-1.5 pt-3.5 text-xs text-zinc-600">Servers</div>
        {servers.data?.map((s) =>
          s.uuid === current ? (
            <div key={s.uuid}>
              <Link to="/servers/$uuid" params={{ uuid: s.uuid }} search={{ tab: "console" }} className={`${item} !text-zinc-100 font-medium`}>
                <StateMark state={s.state} /> <span className="flex-1 truncate">{s.name}</span>
                <ChevronDown className="h-3.5 w-3.5 text-zinc-500" aria-hidden />
              </Link>
              <ul className="ml-[15px] mt-0.5 mb-1.5 flex flex-col gap-px border-l border-[#22272d] pl-2">
                {sections.filter((x) => can(x.need)).map((x) => {
                  const on = path.startsWith("/servers/") && (search.tab ?? "console") === x.id;
                  return (
                    <li key={x.id}>
                      <Link to="/servers/$uuid" params={{ uuid: s.uuid }} search={{ tab: x.id }} aria-current={on ? "page" : undefined} className={`${item} min-h-[34px] ${on ? itemOn : ""}`}>
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
        <nav aria-label="Admin" className="mt-auto flex flex-col gap-px">
          <div className="px-2 pb-1.5 pt-2 text-xs text-zinc-600">Admin</div>
          {([["/nodes", "Nodes", HardDrive], ["/eggs", "Eggs", Egg], ["/audit", "Audit log", ClipboardList]] as const).map(([to, label, Icon]) => (
            <Link key={to} to={to} className={item} activeProps={{ className: `${item} ${itemOn}`, "aria-current": "page" }}>
              <Icon className="h-4 w-4" aria-hidden /> {label}
            </Link>
          ))}
        </nav>
      )}
      <div className={`flex items-center gap-1 border-t border-[#1b1f24] pt-2.5 ${isAdmin ? "" : "mt-auto"}`}>
        <Link to="/account" className="flex min-w-0 flex-1 items-center gap-2.5 rounded-[7px] px-2 py-1.5 text-[13.5px] text-zinc-400 hover:text-zinc-100 focus-visible:outline-2 focus-visible:outline-indigo-500">
          <span className="flex h-[26px] w-[26px] shrink-0 items-center justify-center rounded-full bg-[#23282e] text-xs font-semibold text-zinc-100" aria-hidden>{email.slice(0, 1).toUpperCase()}</span>
          <span className="truncate">{email}</span>
        </Link>
        <button type="button" className="btn btn-ghost !min-h-8 !px-2" aria-label="Sign out" title="Sign out" onClick={onSignOut} disabled={signingOut}>
          <LogOut className="h-4 w-4" aria-hidden />
        </button>
      </div>
    </div>
  );
}

export function AppShell() {
  const me = useQuery(meQuery);
  const qc = useQueryClient();
  const navigate = useNavigate();
  const [open, setOpen] = useState(false);
  const loc = useRouterState({ select: (s) => s.location.href });
  useEffect(() => setOpen(false), [loc]);
  const logout = useMutation({
    mutationFn: () => call(api.POST("/auth/logout")),
    onSettled: () => {
      qc.clear();
      void navigate({ to: "/login" });
    },
  });

  if (me.isPending) return <div className="p-8"><Spinner /></div>;
  if (me.error instanceof ApiError && me.error.status === 401) return <Navigate to="/login" />;
  if (me.error) return <div className="p-8"><ErrorBox error={me.error} /></div>;
  const user = me.data;

  return (
    <div className="min-h-screen lg:flex">
      <header className="flex items-center gap-2 border-b border-[#1b1f24] bg-side px-3 py-2 lg:hidden">
        <button type="button" className="btn btn-ghost !px-2" aria-label="Open menu" aria-expanded={open} onClick={() => setOpen(true)}><Menu className="h-5 w-5" aria-hidden /></button>
        <span className="font-display text-lg font-bold tracking-tight">GamePanel</span>
      </header>
      {open && <div className="fixed inset-0 z-30 bg-black/60 lg:hidden" onClick={() => setOpen(false)} aria-hidden />}
      <aside
        aria-label="Navigation"
        className={`fixed inset-y-0 left-0 z-40 w-64 overflow-y-auto border-r border-[#1b1f24] bg-side transition-transform lg:sticky lg:top-0 lg:z-auto lg:h-screen lg:w-[248px] lg:shrink-0 lg:translate-x-0 ${open ? "translate-x-0" : "-translate-x-full"}`}
      >
        <button type="button" className="btn btn-ghost absolute right-2 top-2 !px-2 lg:hidden" aria-label="Close menu" onClick={() => setOpen(false)}><X className="h-5 w-5" aria-hidden /></button>
        <Sidebar isAdmin={user.is_admin} email={user.email} onSignOut={() => logout.mutate()} signingOut={logout.isPending} />
      </aside>
      <main className="min-w-0 flex-1 px-5 py-6 sm:px-8 lg:px-10 lg:py-8">
        <Outlet />
      </main>
    </div>
  );
}
