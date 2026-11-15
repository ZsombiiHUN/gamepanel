import { createRootRoute, createRoute, createRouter, Link, Outlet } from "@tanstack/react-router";
import { AppShell } from "./pages/AppShell";
import { LoginPage } from "./pages/LoginPage";
import { ServersPage } from "./pages/ServersPage";
import { ServerDetailPage, type Tab } from "./pages/ServerDetailPage";
import { NodesPage } from "./pages/NodesPage";
import { EggsPage } from "./pages/EggsPage";
import { AccountPage } from "./pages/AccountPage";
import { AuditPage } from "./pages/AuditPage";
import { CreateServerPage } from "./pages/CreateServerPage";
import { Empty } from "./components/ui";
import { Compass } from "lucide-react";

function NotFound() {
  return (
    <div className="mx-auto max-w-md px-6 pt-24">
      <Empty icon={<Compass className="h-5 w-5" aria-hidden />} title="Page not found">
        That address does not lead anywhere. <Link to="/" className="link">Back to your servers</Link>
      </Empty>
    </div>
  );
}

const rootRoute = createRootRoute({ component: Outlet, notFoundComponent: NotFound });

const loginRoute = createRoute({ getParentRoute: () => rootRoute, path: "/login", component: LoginPage });
const appRoute = createRoute({ getParentRoute: () => rootRoute, id: "app", component: AppShell });

const indexRoute = createRoute({ getParentRoute: () => appRoute, path: "/", component: ServersPage });
const newServerRoute = createRoute({ getParentRoute: () => appRoute, path: "/servers/new", component: CreateServerPage });
const serverRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/servers/$uuid",
  component: ServerDetailPage,
  validateSearch: (s: Record<string, unknown>): { tab: Tab } => ({
    tab: s.tab === "files" || s.tab === "backups" || s.tab === "schedules" || s.tab === "users" || s.tab === "settings" ? s.tab : "console",
  }),
});
const nodesRoute = createRoute({ getParentRoute: () => appRoute, path: "/nodes", component: NodesPage });
const eggsRoute = createRoute({ getParentRoute: () => appRoute, path: "/eggs", component: EggsPage });

const accountRoute = createRoute({ getParentRoute: () => appRoute, path: "/account", component: AccountPage });
const auditRoute = createRoute({ getParentRoute: () => appRoute, path: "/audit", component: AuditPage });

const routeTree = rootRoute.addChildren([
  loginRoute,
  appRoute.addChildren([indexRoute, newServerRoute, serverRoute, nodesRoute, eggsRoute, accountRoute, auditRoute]),
]);

export const router = createRouter({ routeTree });

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}
