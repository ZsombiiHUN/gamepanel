import type { QueryClient } from "@tanstack/react-query";
import { createRootRouteWithContext, createRoute, createRouter, Link, Outlet, redirect, type ErrorComponentProps, type RouterHistory } from "@tanstack/react-router";
import { AlertTriangle, Compass, Lock } from "lucide-react";
import { ApiError } from "./api/client";
import { auditQuery, databaseHostsQuery, eggsQuery, meQuery, nodesQuery, serverQuery, serversQuery } from "./api/queries";
import { Empty } from "./components/ui";
import { errorText } from "./lib/problem";
import { GROUPS } from "./lib/servers";
import { oneOf, page, text } from "./lib/search";
import { TABS } from "./lib/tabs";
import { AccountPage } from "./pages/AccountPage";
import { AppShell } from "./pages/AppShell";
import { AuditPage } from "./pages/AuditPage";
import { CreateServerPage } from "./pages/CreateServerPage";
import { DatabaseHostsPage } from "./pages/DatabaseHostsPage";
import { EggsPage } from "./pages/EggsPage";
import { LoginPage } from "./pages/LoginPage";
import { NodesPage } from "./pages/NodesPage";
import { ServerDetailPage } from "./pages/ServerDetailPage";
import { ServersPage } from "./pages/ServersPage";

export interface RouterContext {
  queryClient: QueryClient;
}

function NotFound() {
  return (
    <div className="mx-auto max-w-md px-6 pt-24">
      <Empty icon={<Compass className="h-5 w-5" aria-hidden />} title="Page not found">
        That address does not lead anywhere. <Link to="/" className="link">Back to your servers</Link>
      </Empty>
    </div>
  );
}

/** Last stop for a page that failed to load or crashed: a plain message and a way out, never a stack trace. */
function AppError({ error }: ErrorComponentProps) {
  return (
    <div role="alert" className="mx-auto max-w-md px-6 pt-24">
      <Empty icon={<AlertTriangle className="h-5 w-5" aria-hidden />} title="This page could not be shown">
        {errorText(error)}{" "}
        <button type="button" className="link" onClick={() => window.location.reload()}>Reload</button>
      </Empty>
    </div>
  );
}

class Forbidden extends Error {}

function ForbiddenPage({ error }: ErrorComponentProps) {
  if (!(error instanceof Forbidden)) throw error;
  return (
    <div role="alert" className="mx-auto max-w-md pt-10">
      <Empty icon={<Lock className="h-5 w-5" aria-hidden />} title="Administrators only">You need administrator access to view this page.</Empty>
    </div>
  );
}

/** The signed-in user, or null when there is no session. Other failures are real errors. */
async function currentUser(queryClient: QueryClient) {
  try {
    return await queryClient.ensureQueryData(meQuery);
  } catch (err) {
    if (err instanceof ApiError && err.status === 401) return null;
    throw err;
  }
}

const rootRoute = createRootRouteWithContext<RouterContext>()({ component: Outlet, notFoundComponent: NotFound, errorComponent: AppError });

const loginRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/login",
  component: LoginPage,
  // Already signed in: skip the form.
  beforeLoad: async ({ context }) => {
    if (await currentUser(context.queryClient)) throw redirect({ to: "/" });
  },
});

/** Everything behind sign-in. `me` is resolved before any page renders, so pages never handle "not signed in". */
const appRoute = createRoute({
  getParentRoute: () => rootRoute,
  id: "app",
  component: AppShell,
  beforeLoad: async ({ context }) => {
    const me = await currentUser(context.queryClient);
    if (!me) throw redirect({ to: "/login" });
    return { me };
  },
});

/*
 * Loaders only start fetching early (and on link hover); pages still read their data with
 * useQuery, so loading and error states stay inline instead of replacing the whole page.
 */
const indexRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/",
  component: ServersPage,
  validateSearch: (s: Record<string, unknown>): { q?: string; status?: (typeof GROUPS)[number] } => ({
    q: text(s.q),
    status: oneOf(s.status, GROUPS),
  }),
  loader: ({ context }) => void context.queryClient.prefetchQuery(serversQuery),
});
const serverRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/servers/$uuid",
  component: ServerDetailPage,
  validateSearch: (s: Record<string, unknown>): { tab: (typeof TABS)[number]; dir?: string; q?: string } => ({
    tab: oneOf(s.tab, TABS) ?? "console",
    dir: text(s.dir),
    q: text(s.q),
  }),
  loader: ({ context, params }) => void context.queryClient.prefetchQuery(serverQuery(params.uuid)),
});

const accountRoute = createRoute({ getParentRoute: () => appRoute, path: "/account", component: AccountPage });

/** Pages only administrators may open. */
const adminRoute = createRoute({
  getParentRoute: () => appRoute,
  id: "admin",
  // Checked before any child loader runs, so a non-admin never triggers admin requests.
  beforeLoad: ({ context }) => {
    if (!context.me.is_admin) throw new Forbidden();
  },
  errorComponent: ForbiddenPage,
});
const newServerRoute = createRoute({
  getParentRoute: () => adminRoute,
  path: "/servers/new",
  component: CreateServerPage,
  loader: ({ context }) => {
    void context.queryClient.prefetchQuery(nodesQuery);
    void context.queryClient.prefetchQuery(eggsQuery);
  },
});
const nodesRoute = createRoute({
  getParentRoute: () => adminRoute,
  path: "/nodes",
  component: NodesPage,
  loader: ({ context }) => void context.queryClient.prefetchQuery(nodesQuery),
});
const eggsRoute = createRoute({
  getParentRoute: () => adminRoute,
  path: "/eggs",
  component: EggsPage,
  loader: ({ context }) => void context.queryClient.prefetchQuery(eggsQuery),
});
const databaseHostsRoute = createRoute({
  getParentRoute: () => adminRoute,
  path: "/database-hosts",
  component: DatabaseHostsPage,
  loader: ({ context }) => {
    void context.queryClient.prefetchQuery(databaseHostsQuery);
    void context.queryClient.prefetchQuery(nodesQuery);
  },
});
const auditRoute = createRoute({
  getParentRoute: () => adminRoute,
  path: "/audit",
  component: AuditPage,
  validateSearch: (s: Record<string, unknown>): { q?: string; page?: number } => ({ q: text(s.q), page: page(s.page) }),
  loaderDeps: ({ search }) => ({ q: search.q ?? "", page: search.page ?? 0 }),
  loader: ({ context, deps }) => void context.queryClient.prefetchQuery(auditQuery(deps.q, deps.page)),
});

const routeTree = rootRoute.addChildren([
  loginRoute,
  appRoute.addChildren([indexRoute, serverRoute, accountRoute, adminRoute.addChildren([newServerRoute, nodesRoute, eggsRoute, databaseHostsRoute, auditRoute])]),
]);

export function createAppRouter(queryClient: QueryClient, history?: RouterHistory) {
  return createRouter({ routeTree, history, context: { queryClient }, defaultPreload: "intent" });
}

declare module "@tanstack/react-router" {
  interface Register {
    router: ReturnType<typeof createAppRouter>;
  }
}
