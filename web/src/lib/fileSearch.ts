import { joinPath } from "./format";

export interface Entry {
  name: string;
  dir: boolean;
  size: number;
  mod_unix: number;
  mode?: number;
}
export interface Hit extends Entry {
  /** Absolute path of the entry, e.g. /plugins/Essentials/config.yml */
  path: string;
}
export interface SearchResult {
  hits: Hit[];
  /** Directories that were listed. */
  scanned: number;
  /** True when a limit stopped the search early, so there may be more matches. */
  truncated: boolean;
}
export interface SearchOptions {
  maxDirs?: number;
  maxHits?: number;
  /** Directories listed at the same time. */
  concurrency?: number;
  signal?: AbortSignal;
}

/**
 * Breadth-first search for entries whose name contains `query`, below `root`.
 * `list` fetches one directory. A directory that fails to list is skipped, so one
 * unreadable folder does not hide matches elsewhere. Results are shallow-first.
 */
export async function searchFiles(
  list: (dir: string) => Promise<Entry[]>,
  root: string,
  query: string,
  opts: SearchOptions = {},
): Promise<SearchResult> {
  const { maxDirs = 200, maxHits = 200, concurrency = 4, signal } = opts;
  const needle = query.trim().toLowerCase();
  const result: SearchResult = { hits: [], scanned: 0, truncated: false };
  if (!needle) return result;

  let level = [root];
  while (level.length > 0) {
    const next: string[] = [];
    for (let i = 0; i < level.length; i += concurrency) {
      if (signal?.aborted) return result;
      const batch = level.slice(i, i + concurrency);
      if (result.scanned + batch.length > maxDirs) {
        batch.length = Math.max(0, maxDirs - result.scanned);
        result.truncated = true;
        if (batch.length === 0) return result;
      }
      const listed = await Promise.all(batch.map((d) => list(d).catch(() => null)));
      if (signal?.aborted) return result;
      result.scanned += batch.length;
      batch.forEach((dir, k) => {
        const entries = listed[k];
        if (!entries) return;
        for (const e of [...entries].sort((a, b) => Number(b.dir) - Number(a.dir) || a.name.localeCompare(b.name))) {
          const path = joinPath(dir, e.name);
          if (e.name.toLowerCase().includes(needle)) {
            if (result.hits.length >= maxHits) {
              result.truncated = true;
              continue;
            }
            result.hits.push({ ...e, path });
          }
          if (e.dir) next.push(path);
        }
      });
      if (result.truncated && result.scanned >= maxDirs) return result;
    }
    level = next;
  }
  return result;
}
