import { createRootRoute, createRoute, createRouter, Outlet } from "@tanstack/react-router";
import { AppShell } from "./pages/AppShell";
import { LoginPage } from "./pages/LoginPage";
import { ServersPage } from "./pages/ServersPage";
import { ServerDetailPage, type Tab } from "./pages/ServerDetailPage";
import { NodesPage } from "./pages/NodesPage";
import { EggsPage } from "./pages/EggsPage";
import { CreateServerPage } from "./pages/CreateServerPage";

const rootRoute = createRootRoute({ component: Outlet });

const loginRoute = createRoute({ getParentRoute: () => rootRoute, path: "/login", component: LoginPage });
const appRoute = createRoute({ getParentRoute: () => rootRoute, id: "app", component: AppShell });

const indexRoute = createRoute({ getParentRoute: () => appRoute, path: "/", component: ServersPage });
const newServerRoute = createRoute({ getParentRoute: () => appRoute, path: "/servers/new", component: CreateServerPage });
const serverRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/servers/$uuid",
  component: ServerDetailPage,
  validateSearch: (s: Record<string, unknown>): { tab: Tab } => ({
    tab: s.tab === "files" || s.tab === "settings" ? s.tab : "console",
  }),
});
const nodesRoute = createRoute({ getParentRoute: () => appRoute, path: "/nodes", component: NodesPage });
const eggsRoute = createRoute({ getParentRoute: () => appRoute, path: "/eggs", component: EggsPage });

const routeTree = rootRoute.addChildren([
  loginRoute,
  appRoute.addChildren([indexRoute, newServerRoute, serverRoute, nodesRoute, eggsRoute]),
]);

export const router = createRouter({ routeTree });

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}
