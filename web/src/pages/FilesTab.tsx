import { useState } from "react";
import { useNavigate, useSearch } from "@tanstack/react-router";
import { useMutation, useQuery } from "@tanstack/react-query";
import { ChevronRight, File as FileIcon, Folder, Home, ArrowLeft, Trash2 } from "lucide-react";
import { api, call, type components } from "../api/client";
import { dirQuery, invalidates, keys } from "../api/queries";
import { SearchInput } from "../components/SearchInput";
import { ConfirmDialog, ErrorBox, Spinner } from "../components/ui";
import { useDebouncedValue } from "../hooks/useDebouncedValue";
import { useFileSearch } from "../hooks/useFileSearch";
import { useSearchText } from "../hooks/useSearchText";
import { formatBytes, joinPath, parentPath } from "../lib/format";
import { FileEditor } from "./FileEditor";

type FileInfo = components["schemas"]["FileInfo"];

export function FilesTab({ uuid }: { uuid: string }) {
  // The folder and the search live in the address, so a folder can be linked to and Back goes up.
  const { dir = "/", q: urlQuery } = useSearch({ from: "/app/servers/$uuid" });
  const navigate = useNavigate({ from: "/servers/$uuid" });
  const setDir = (d: string) => void navigate({ search: (prev) => ({ ...prev, dir: d === "/" ? undefined : d, q: undefined }) });
  const [q, setQ] = useSearchText(urlQuery, (text) => void navigate({ search: (prev) => ({ ...prev, q: text || undefined }), replace: true }), 250);

  const [openFile, setOpenFile] = useState<string | null>(null);
  const [toDelete, setToDelete] = useState<(FileInfo & { path: string }) | null>(null);

  const list = useQuery(dirQuery(uuid, dir));
  // Searching looks through every folder below the current one, not just this folder.
  const needle = q.trim();
  const settled = useDebouncedValue(needle, 250);
  const search = useFileSearch(uuid, dir, settled);
  const found = needle !== "" && needle === settled ? search.data : undefined;
  const searching = needle !== "" && !found && !search.isError;

  const del = useMutation({
    mutationFn: (p: string) => call(api.DELETE("/servers/{uuid}/files", { params: { path: { uuid }, query: { path: p } } })),
    meta: invalidates(keys.files(uuid)),
    onSuccess: () => setToDelete(null),
  });

  const entries = [...(list.data ?? [])].sort((a, b) => Number(b.dir) - Number(a.dir) || a.name.localeCompare(b.name));
  const crumbs = dir.split("/").filter(Boolean);

  if (openFile) {
    return (
      <FileEditor
        uuid={uuid}
        path={openFile}
        onClose={() => setOpenFile(null)}
        onDeleted={() => setOpenFile(null)}
      />
    );
  }

  const rows = needle
    ? (found?.hits ?? []).map((h) => ({ ...h, parent: parentPath(h.path) }))
    : entries.map((f) => ({ ...f, path: joinPath(dir, f.name), parent: "" }));
  const open = (f: { dir: boolean; path: string }) => {
    if (f.dir) {
      setQ("");
      setDir(f.path);
    } else setOpenFile(f.path);
  };
  return (
    <div className="space-y-3">
      <section className="overflow-hidden rounded-2xl border border-zinc-800 bg-zinc-900">
        <div className="flex flex-wrap items-center justify-between gap-3 px-3.5 py-2.5">
          <nav aria-label="Breadcrumb" className="flex flex-wrap items-center gap-1 text-sm">
            <button type="button" className="inline-flex h-8 w-8 items-center justify-center rounded-md text-zinc-200 hover:bg-zinc-800 focus-visible:outline-2 focus-visible:outline-indigo-500" onClick={() => setDir("/")} aria-label="Root folder"><Home className="h-4 w-4" aria-hidden /></button>
            {crumbs.map((c, i) => {
              const p = "/" + crumbs.slice(0, i + 1).join("/");
              return (
                <span key={p} className="flex items-center gap-1">
                  <ChevronRight className="h-3 w-3 text-zinc-600" aria-hidden />
                  {i === crumbs.length - 1 ? <span aria-current="page" className="font-medium">{c}</span> : <button type="button" className="link" onClick={() => setDir(p)}>{c}</button>}
                </span>
              );
            })}
          </nav>
          <SearchInput label="Search files" placeholder={dir === "/" ? "Search all files" : "Search in this folder"} value={q} onChange={setQ} />
        </div>

        {list.isPending && <div className="px-4 py-3"><Spinner /></div>}
        {needle && searching && <div className="px-4 py-3" role="status"><Spinner label={`Searching ${dir === "/" ? "all folders" : dir}`} /></div>}
        {list.error ? <div className="px-4 pb-3"><ErrorBox error={list.error} /></div> : null}

        {list.data && (
          <div className="relative overflow-x-auto" tabIndex={0} role="region" aria-label="Files">
            <table className="w-full min-w-[560px] text-sm">
              <caption className="sr-only">Files in {dir}</caption>
              <thead className="bg-[#101317] text-left text-[12.5px] text-zinc-500">
                <tr>
                  <th scope="col" className="px-5 py-2.5 font-medium">Name</th>
                  <th scope="col" className="px-3 py-2.5 font-medium">Size</th>
                  <th scope="col" className="px-3 py-2.5 font-medium">Modified</th>
                  <th scope="col" className="px-3 py-2.5"><span className="sr-only">Actions</span></th>
                </tr>
              </thead>
              <tbody>
                {dir !== "/" && !needle && (
                  <tr className="border-t border-[#1b2025]">
                    <td className="px-5 py-2.5" colSpan={4}>
                      <button type="button" className="link inline-flex items-center gap-2" onClick={() => setDir(parentPath(dir))}>
                        <ArrowLeft className="h-4 w-4" aria-hidden /> Parent directory
                      </button>
                    </td>
                  </tr>
                )}
                {rows.length === 0 && !(needle && searching) && <tr className="border-t border-[#1b2025]"><td className="px-5 py-4 text-zinc-500" colSpan={4}>{needle ? `No files or folders named “${needle}” in ${dir === "/" ? "any folder" : dir + " or its subfolders"}.` : "Empty directory."}</td></tr>}
                {rows.map((f) => (
                  <tr key={f.path} className="group border-t border-[#1b2025] hover:bg-[#161a1f]">
                    <td className="px-5 py-2.5">
                      <button
                        type="button"
                        className="inline-flex items-center gap-3 rounded text-[14.5px] text-zinc-100 focus-visible:outline-2 focus-visible:outline-indigo-500"
                        onClick={() => open(f)}
                      >
                        {f.dir ? <Folder className="h-[17px] w-[17px] shrink-0 text-indigo-400" aria-hidden /> : <FileIcon className="h-[17px] w-[17px] shrink-0 text-zinc-500" aria-hidden />}
                        <span>{f.name}{f.parent && <span className="ml-2 font-mono text-xs text-zinc-500"><span className="sr-only">, in </span>{f.parent}</span>}</span>
                      </button>
                    </td>
                    <td className="px-3 py-2.5 font-mono text-[12.5px] text-zinc-400">{f.dir ? "-" : formatBytes(f.size)}</td>
                    <td className="px-3 py-2.5 text-[13px] text-zinc-400">{f.mod_unix ? new Date(f.mod_unix * 1000).toLocaleString() : "-"}</td>
                    <td className="px-3 py-2.5 text-right">
                      <button type="button" className="inline-flex h-8 w-8 items-center justify-center rounded-md text-zinc-500 opacity-60 hover:bg-zinc-800 hover:text-red-300 hover:opacity-100 focus-visible:opacity-100 focus-visible:outline-2 focus-visible:outline-indigo-500 group-hover:opacity-100" onClick={() => setToDelete(f)} aria-label={`Delete ${f.path.slice(1)}`}>
                        <Trash2 className="h-4 w-4" aria-hidden />
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>
      <p className="text-[12.5px] text-zinc-500" aria-live="polite">
        {needle
          ? `${found ? `${found.hits.length} ${found.hits.length === 1 ? "match" : "matches"}` : "Searching"}${found?.truncated ? ` (stopped early after ${found.scanned} folders or too many matches. Narrow your search or open a subfolder.)` : ""}. Press Esc to clear.`
          : `${entries.length} items. Click a file to edit it. Use SFTP to upload. Tip: type to search inside every subfolder.`}
      </p>

      <ConfirmDialog
        open={!!toDelete}
        title={`Delete ${toDelete?.name ?? ""}?`}
        body={toDelete?.dir ? "This deletes the directory and everything in it." : "This permanently deletes the file."}
        confirmLabel="Delete"
        pending={del.isPending}
        error={del.error}
        onConfirm={() => toDelete && del.mutate(toDelete.path)}
        onCancel={() => { setToDelete(null); del.reset(); }}
      />
    </div>
  );
}

