import { useQuery, useQueryClient } from "@tanstack/react-query";
import { dirQuery, keys } from "../api/queries";
import { searchFiles } from "../lib/fileSearch";

const FRESH_MS = 15_000;

/**
 * Names matching `term` in `dir` and every folder below it. The crawl is cached like any
 * query, shares folder listings with the file list, and TanStack aborts it when the term changes.
 */
export function useFileSearch(uuid: string, dir: string, term: string) {
  const qc = useQueryClient();
  return useQuery({
    queryKey: [...keys.files(uuid), "search", dir, term] as const,
    queryFn: ({ signal }) => searchFiles((d) => qc.fetchQuery({ ...dirQuery(uuid, d), staleTime: FRESH_MS }), dir, term, { signal }),
    enabled: term !== "",
    staleTime: FRESH_MS,
    retry: false,
  });
}
