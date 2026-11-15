import { queryOptions } from "@tanstack/react-query";
import { api, call } from "./client";

export const meQuery = queryOptions({
  queryKey: ["me"],
  queryFn: () => call(api.GET("/auth/me")),
  retry: false,
  staleTime: 60_000,
});

export const serversQuery = queryOptions({
  queryKey: ["servers"],
  queryFn: async () => (await call(api.GET("/servers"))) ?? [],
  refetchInterval: 5000,
});

export const serverQuery = (uuid: string) =>
  queryOptions({
    queryKey: ["server", uuid],
    queryFn: () => call(api.GET("/servers/{uuid}", { params: { path: { uuid } } })),
    refetchInterval: 5000,
  });

export const nodesQuery = queryOptions({
  queryKey: ["nodes"],
  queryFn: async () => (await call(api.GET("/nodes"))) ?? [],
  refetchInterval: 10_000,
});

export const eggsQuery = queryOptions({
  queryKey: ["eggs"],
  queryFn: async () => (await call(api.GET("/eggs"))) ?? [],
});
