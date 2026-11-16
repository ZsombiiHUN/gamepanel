import { MutationCache, QueryClient } from "@tanstack/react-query";
import { ApiError } from "./api/client";

export function createQueryClient() {
  const client: QueryClient = new QueryClient({
    // A mutation declares what it changes (`meta: invalidates(keys.x)`); this refreshes it once the call succeeds.
    mutationCache: new MutationCache({
      onSuccess: async (_data, _vars, _ctx, mutation) => {
        await Promise.all((mutation.meta?.invalidates ?? []).map((queryKey) => client.invalidateQueries({ queryKey })));
      },
      // Some requests change things even when they fail part-way (deleting ten files, the sixth refused).
      onError: async (_err, _vars, _ctx, mutation) => {
        if (!mutation.meta?.evenOnError) return;
        await Promise.all((mutation.meta.invalidates ?? []).map((queryKey) => client.invalidateQueries({ queryKey })));
      },
    }),
    defaultOptions: {
      queries: {
        retry: (n, err) => !(err instanceof ApiError && err.status >= 400 && err.status < 500) && n < 2,
        // A loader's prefetch and the page's own query are one request, not two.
        staleTime: 5_000,
        refetchOnWindowFocus: false,
        // Polling in a hidden tab only burns the daemon's time.
        refetchIntervalInBackground: false,
      },
    },
  });
  return client;
}

export const queryClient = createQueryClient();
