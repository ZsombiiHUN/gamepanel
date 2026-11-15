import { Link, Navigate, Outlet, useNavigate } from "@tanstack/react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Egg, Gamepad2, LogOut, Server } from "lucide-react";
import { api, ApiError, call } from "../api/client";
import { meQuery } from "../api/queries";
import { ErrorBox, Spinner } from "../components/ui";

const navCls = "rounded-md px-3 py-1.5 text-sm text-zinc-300 hover:bg-zinc-800 focus-visible:outline-2 focus-visible:outline-indigo-400";

export function AppShell() {
  const me = useQuery(meQuery);
  const qc = useQueryClient();
  const navigate = useNavigate();
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
    <div className="min-h-screen">
      <header className="border-b border-zinc-800 bg-zinc-900/60">
        <div className="mx-auto flex max-w-6xl flex-wrap items-center gap-2 px-4 py-3">
          <Link to="/" className="mr-4 flex items-center gap-2 font-semibold text-zinc-100 focus-visible:outline-2 focus-visible:outline-indigo-400">
            <Gamepad2 className="h-5 w-5 text-indigo-400" aria-hidden /> GamePanel
          </Link>
          <nav aria-label="Main" className="flex flex-wrap items-center gap-1">
            <Link to="/" className={navCls} activeOptions={{ exact: true }} activeProps={{ className: `${navCls} bg-zinc-800 text-white`, "aria-current": "page" }}>
              Servers
            </Link>
            {user.is_admin && (
              <>
                <Link to="/servers/new" className={navCls} activeProps={{ className: `${navCls} bg-zinc-800 text-white`, "aria-current": "page" }}>
                  <Server className="mr-1 inline h-4 w-4" aria-hidden />Create server
                </Link>
                <Link to="/nodes" className={navCls} activeProps={{ className: `${navCls} bg-zinc-800 text-white`, "aria-current": "page" }}>Nodes</Link>
                <Link to="/eggs" className={navCls} activeProps={{ className: `${navCls} bg-zinc-800 text-white`, "aria-current": "page" }}>
                  <Egg className="mr-1 inline h-4 w-4" aria-hidden />Eggs
                </Link>
                <Link to="/audit" className={navCls} activeProps={{ className: `${navCls} bg-zinc-800 text-white`, "aria-current": "page" }}>Audit</Link>
              </>
            )}
          </nav>
          <div className="ml-auto flex items-center gap-3 text-sm text-zinc-400">
            <Link to="/account" className="hover:text-white focus-visible:outline-2 focus-visible:outline-indigo-400">{user.email}{user.is_admin && " (admin)"}</Link>
            <button type="button" className="btn btn-ghost" onClick={() => logout.mutate()} disabled={logout.isPending}>
              <LogOut className="h-4 w-4" aria-hidden /> Sign out
            </button>
          </div>
        </div>
      </header>
      <main className="mx-auto max-w-6xl px-4 py-6">
        <Outlet />
      </main>
    </div>
  );
}
