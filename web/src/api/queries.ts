import { keepPreviousData, queryOptions, type QueryClient, type QueryKey } from "@tanstack/react-query";
import { api, call } from "./client";

/**
 * Every query key in the app. Pages never write a key by hand; they use these
 * so a rename or typo is a compile error instead of a cache that silently never refreshes.
 */
export const keys = {
  me: ["me"] as const,
  servers: ["servers"] as const,
  server: (uuid: string) => ["server", uuid] as const,
  nodes: ["nodes"] as const,
  eggs: ["eggs"] as const,
  permissions: ["permissions"] as const,
  apiKeys: ["apikeys"] as const,
  audit: ["audit"] as const,
  /** Everything about one server's files; invalidating this refreshes listings and open files. */
  files: (uuid: string) => ["files", uuid] as const,
  subusers: (uuid: string) => ["subusers", uuid] as const,
  backups: (uuid: string) => ["backups", uuid] as const,
  schedules: (uuid: string) => ["schedules", uuid] as const,
  startup: (uuid: string) => ["startup", uuid] as const,
  network: (uuid: string) => ["network", uuid] as const,
  databases: (uuid: string) => ["databases", uuid] as const,
  databaseHosts: ["database-hosts"] as const,
  activity: (uuid: string) => ["activity", uuid] as const,
  authOptions: ["auth-options"] as const,
  invite: ["invite"] as const,
  invites: (uuid: string) => ["invites", uuid] as const,
};

/** What a mutation refreshes on success. The query client applies it, so mutations do not repeat the plumbing. */
export function invalidates(...queryKeys: QueryKey[]) {
  return { invalidates: queryKeys };
}

/** Like invalidates, but also after a failure: for requests that may have changed something before failing. */
export function invalidatesAlways(...queryKeys: QueryKey[]) {
  return { invalidates: queryKeys, evenOnError: true };
}

/** Look again now, for changes made outside a mutation (uploads). */
export function refreshNow(qc: QueryClient, queryKey: QueryKey) {
  void qc.invalidateQueries({ queryKey });
}

/** Look again after `ms`, for work the server finishes in the background. */
export function refreshLater(qc: QueryClient, queryKey: QueryKey, ms: number) {
  setTimeout(() => void qc.invalidateQueries({ queryKey }), ms);
}

declare module "@tanstack/react-query" {
  interface Register {
    mutationMeta: { invalidates?: QueryKey[]; evenOnError?: boolean };
  }
}

export const meQuery = queryOptions({
  queryKey: keys.me,
  queryFn: () => call(api.GET("/auth/me")),
  retry: false,
  staleTime: 60_000,
});

/** How this panel lets people in (public; no session needed). */
export const authOptionsQuery = queryOptions({
  queryKey: keys.authOptions,
  queryFn: () => call(api.GET("/auth/options")),
  staleTime: 5 * 60_000,
});

/** What an invitation link is for (public). */
export const inviteQuery = (token: string) =>
  queryOptions({
    queryKey: [...keys.invite, token] as const,
    queryFn: () => call(api.POST("/auth/invite", { body: { token } })),
    retry: false,
  });

/** Invitations to a server that nobody has used yet. */
export const serverInvitesQuery = (uuid: string) =>
  queryOptions({
    queryKey: keys.invites(uuid),
    queryFn: async () => (await call(api.GET("/servers/{uuid}/invites", { params: { path: { uuid } } }))) ?? [],
  });

export const serversQuery = queryOptions({
  queryKey: keys.servers,
  queryFn: async () => (await call(api.GET("/servers"))) ?? [],
  refetchInterval: 5000,
});

export const serverQuery = (uuid: string) =>
  queryOptions({
    queryKey: keys.server(uuid),
    queryFn: () => call(api.GET("/servers/{uuid}", { params: { path: { uuid } } })),
    refetchInterval: 5000,
  });

export const nodesQuery = queryOptions({
  queryKey: keys.nodes,
  queryFn: async () => (await call(api.GET("/nodes"))) ?? [],
  refetchInterval: 10_000,
});

export const eggsQuery = queryOptions({
  queryKey: keys.eggs,
  queryFn: async () => (await call(api.GET("/eggs"))) ?? [],
});

export const permissionsQuery = queryOptions({
  queryKey: keys.permissions,
  queryFn: () => call(api.GET("/permissions")),
  staleTime: Infinity,
});

export const apiKeysQuery = queryOptions({
  queryKey: keys.apiKeys,
  queryFn: () => call(api.GET("/auth/keys")),
});

export const subusersQuery = (uuid: string) =>
  queryOptions({
    queryKey: keys.subusers(uuid),
    queryFn: () => call(api.GET("/servers/{uuid}/users", { params: { path: { uuid } } })),
  });

export const schedulesQuery = (uuid: string) =>
  queryOptions({
    queryKey: keys.schedules(uuid),
    queryFn: () => call(api.GET("/servers/{uuid}/schedules", { params: { path: { uuid } } })),
  });

export const startupQuery = (uuid: string) =>
  queryOptions({
    queryKey: keys.startup(uuid),
    queryFn: () => call(api.GET("/servers/{uuid}/startup", { params: { path: { uuid } } })),
    // While a change waits for the next start, notice when it has been applied.
    refetchInterval: (q) => (q.state.data?.pending ? 5000 : false),
  });

export const networkQuery = (uuid: string) =>
  queryOptions({
    queryKey: keys.network(uuid),
    queryFn: () => call(api.GET("/servers/{uuid}/network", { params: { path: { uuid } } })),
  });

export const databasesQuery = (uuid: string) =>
  queryOptions({
    queryKey: keys.databases(uuid),
    queryFn: () => call(api.GET("/servers/{uuid}/databases", { params: { path: { uuid } } })),
  });

export const databaseHostsQuery = queryOptions({
  queryKey: keys.databaseHosts,
  queryFn: async () => (await call(api.GET("/database-hosts"))) ?? [],
});

export const backupsQuery = (uuid: string) =>
  queryOptions({
    queryKey: keys.backups(uuid),
    queryFn: () => call(api.GET("/servers/{uuid}/backups", { params: { path: { uuid } } })),
    // Poll only while a backup or a restore is running.
    refetchInterval: (q) => (q.state.data?.some((b) => b.status === "running" || b.restore_status === "running") ? 2000 : false),
  });

/** One folder. Shared by the file list and the recursive search, so each folder is fetched once. */
export const dirQuery = (uuid: string, dir: string) =>
  queryOptions({
    queryKey: [...keys.files(uuid), "dir", dir] as const,
    queryFn: async () => (await call(api.GET("/servers/{uuid}/files", { params: { path: { uuid }, query: { path: dir } } }))) ?? [],
  });

export const fileQuery = (uuid: string, path: string) =>
  queryOptions({
    queryKey: [...keys.files(uuid), "file", path] as const,
    queryFn: () => call(api.GET("/servers/{uuid}/files/content", { params: { path: { uuid }, query: { path } } })),
    retry: false,
    gcTime: 0,
  });

export const ACTIVITY_PAGE_SIZE = 50;

export const activityQuery = (uuid: string, term: string, page: number) =>
  queryOptions({
    queryKey: [...keys.activity(uuid), term, page] as const,
    queryFn: async () =>
      (await call(api.GET("/servers/{uuid}/activity", { params: { path: { uuid }, query: { limit: ACTIVITY_PAGE_SIZE, offset: page * ACTIVITY_PAGE_SIZE, ...(term ? { q: term } : {}) } } }))) ?? [],
    placeholderData: keepPreviousData,
  });

export const AUDIT_PAGE_SIZE = 100;

export const auditQuery = (term: string, page: number) =>
  queryOptions({
    queryKey: [...keys.audit, term, page] as const,
    queryFn: async () =>
      (await call(api.GET("/audit", { params: { query: { limit: AUDIT_PAGE_SIZE, offset: page * AUDIT_PAGE_SIZE, ...(term ? { q: term } : {}) } } }))) ?? [],
    placeholderData: keepPreviousData,
  });
