import { useRef, useState } from "react";
import { useNavigate, useSearch } from "@tanstack/react-router";
import { useMutation, useQuery } from "@tanstack/react-query";
import {
  ArchiveRestore,
  ArrowLeft,
  ChevronRight,
  Copy,
  Download,
  File as FileIcon,
  FileArchive,
  FilePlus,
  Folder,
  FolderInput,
  FolderPlus,
  Home,
  KeyRound,
  Link2,
  Pencil,
  Plug,
  Trash2,
  Upload,
  X,
} from "lucide-react";
import { api, call, type components } from "../api/client";
import { dirQuery, invalidatesAlways, keys, serverQuery } from "../api/queries";
import { SearchInput } from "../components/SearchInput";
import { ErrorBox, Spinner } from "../components/ui";
import { useDebouncedValue } from "../hooks/useDebouncedValue";
import { useFileSearch } from "../hooks/useFileSearch";
import { useSearchText } from "../hooks/useSearchText";
import { useUploads } from "../hooks/useUploads";
import { droppedFiles, isArchive, modeString, pickedFiles } from "../lib/files";
import { formatBytes, joinPath, parentPath } from "../lib/format";
import { FileEditor } from "./FileEditor";
import { FileDialogs, type FileAction, type FileItem } from "./files/FileDialogs";
import { RowMenu, type MenuItem } from "./files/RowMenu";
import { UploadPanel } from "./files/UploadPanel";
import { SftpDialog } from "./server/SftpConnect";

type FileInfo = components["schemas"]["FileInfo"];
type Row = FileInfo & { path: string; parent: string };

const downloadUrl = (uuid: string, path: string) => `/api/servers/${uuid}/files/download?path=${encodeURIComponent(path)}`;
const iconBtn =
  "inline-flex h-8 w-8 items-center justify-center rounded-md text-zinc-400 hover:bg-row-hover hover:text-zinc-100 focus-visible:outline-2 focus-visible:outline-indigo-500";

export function FilesTab({ uuid }: { uuid: string }) {
  // The folder and the search live in the address, so a folder can be linked to and Back goes up.
  const { dir = "/", q: urlQuery } = useSearch({ from: "/app/servers/$uuid" });
  const navigate = useNavigate({ from: "/servers/$uuid" });
  const setDir = (d: string) => {
    setSelected(new Set());
    void navigate({ search: (prev) => ({ ...prev, dir: d === "/" ? undefined : d, q: undefined }) });
  };
  const [q, setQ] = useSearchText(
    urlQuery,
    (text) => void navigate({ search: (prev) => ({ ...prev, q: text || undefined }), replace: true }),
    250,
  );
  const server = useQuery(serverQuery(uuid));
  const canWrite = !!server.data?.permissions.includes("files.write");

  const [openFile, setOpenFile] = useState<string | null>(null);
  const [action, setAction] = useState<FileAction | null>(null);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [dragging, setDragging] = useState(false);
  const [sftpOpen, setSftpOpen] = useState(false);
  const uploads = useUploads(uuid);
  const fileInput = useRef<HTMLInputElement>(null);
  const folderInput = useRef<HTMLInputElement>(null);

  const list = useQuery(dirQuery(uuid, dir));
  // Searching looks through every folder below the current one, not just this folder.
  const needle = q.trim();
  const settled = useDebouncedValue(needle, 250);
  const search = useFileSearch(uuid, dir, settled);
  const found = needle !== "" && needle === settled ? search.data : undefined;
  const searching = needle !== "" && !found && !search.isError;

  const quick = useMutation({ mutationFn: (fn: () => Promise<unknown>) => fn(), meta: invalidatesAlways(keys.files(uuid)) });

  const entries = [...(list.data ?? [])].sort((a, b) => Number(b.dir) - Number(a.dir) || a.name.localeCompare(b.name));
  const crumbs = dir.split("/").filter(Boolean);

  if (openFile) {
    return <FileEditor uuid={uuid} path={openFile} onClose={() => setOpenFile(null)} onDeleted={() => setOpenFile(null)} />;
  }

  const rows: Row[] = needle
    ? (found?.hits ?? []).map((h) => ({ ...h, mode: h.mode ?? 0, parent: parentPath(h.path) }))
    : entries.map((f) => ({ ...f, path: joinPath(dir, f.name), parent: "" }));
  const open = (f: { dir: boolean; path: string }) => {
    if (f.dir) {
      setQ("");
      setDir(f.path);
    } else setOpenFile(f.path);
  };
  const item = (r: Row): FileItem => ({ name: r.name, path: r.path, dir: r.dir, mode: r.mode });
  const chosen = rows.filter((r) => selected.has(r.path));
  const toggle = (p: string) =>
    setSelected((s) => {
      const n = new Set(s);
      if (n.has(p)) n.delete(p);
      else n.add(p);
      return n;
    });
  const allChosen = rows.length > 0 && chosen.length === rows.length;

  const menu = (r: Row): MenuItem[] => {
    const out: MenuItem[] = [];
    if (!r.dir) out.push({ label: "Download", icon: <Download className="h-4 w-4" aria-hidden />, href: downloadUrl(uuid, r.path) });
    if (!canWrite) return out;
    out.push(
      {
        label: "Rename",
        icon: <Pencil className="h-4 w-4" aria-hidden />,
        onSelect: () => setAction({ kind: "rename", items: [item(r)] }),
      },
      {
        label: "Move",
        icon: <FolderInput className="h-4 w-4" aria-hidden />,
        onSelect: () => setAction({ kind: "move", items: [item(r)] }),
      },
      {
        label: "Make a copy",
        icon: <Copy className="h-4 w-4" aria-hidden />,
        onSelect: () =>
          quick.mutate(() => call(api.POST("/servers/{uuid}/files/copy", { params: { path: { uuid } }, body: { path: r.path } }))),
      },
      {
        label: "Permissions",
        icon: <KeyRound className="h-4 w-4" aria-hidden />,
        onSelect: () => setAction({ kind: "chmod", items: [item(r)] }),
      },
    );
    if (!r.dir && isArchive(r.name)) {
      out.push({
        label: "Extract here",
        icon: <ArchiveRestore className="h-4 w-4" aria-hidden />,
        onSelect: () =>
          quick.mutate(() => call(api.POST("/servers/{uuid}/files/decompress", { params: { path: { uuid } }, body: { path: r.path } }))),
      });
    } else {
      out.push({
        label: "Compress",
        icon: <FileArchive className="h-4 w-4" aria-hidden />,
        onSelect: () => setAction({ kind: "compress", items: [item(r)] }),
      });
    }
    out.push({
      label: "Delete",
      icon: <Trash2 className="h-4 w-4" aria-hidden />,
      danger: true,
      onSelect: () => setAction({ kind: "delete", items: [item(r)] }),
    });
    return out;
  };

  const onDrop = async (e: React.DragEvent) => {
    e.preventDefault();
    setDragging(false);
    if (!canWrite) return;
    const files = await droppedFiles(e.dataTransfer);
    if (files.length) uploads.add(dir, files);
  };
  const hasFiles = (e: React.DragEvent) => [...e.dataTransfer.types].includes("Files");

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-2">
        {canWrite && (
          <>
            <button type="button" className="btn btn-primary" onClick={() => fileInput.current?.click()}>
              <Upload className="h-4 w-4" aria-hidden /> Upload files
            </button>
            {/* Secondary actions keep their names for screen readers but show only icons on phones. */}
            <button
              type="button"
              className="btn !px-3"
              aria-label="Upload folder"
              title="Upload folder"
              onClick={() => folderInput.current?.click()}
            >
              <FolderInput className="h-4 w-4" aria-hidden />
              <span className="hidden sm:inline">Upload folder</span>
            </button>
            <button
              type="button"
              className="btn !px-3"
              aria-label="New folder"
              title="New folder"
              onClick={() => setAction({ kind: "newFolder", dir })}
            >
              <FolderPlus className="h-4 w-4" aria-hidden />
              <span className="hidden sm:inline">New folder</span>
            </button>
            <button
              type="button"
              className="btn !px-3"
              aria-label="New file"
              title="New file"
              onClick={() => setAction({ kind: "newFile", dir })}
            >
              <FilePlus className="h-4 w-4" aria-hidden />
              <span className="hidden sm:inline">New file</span>
            </button>
            <button
              type="button"
              className="btn !px-3"
              aria-label="From URL"
              title="Download from a URL"
              onClick={() => setAction({ kind: "pull", dir })}
            >
              <Link2 className="h-4 w-4" aria-hidden />
              <span className="hidden sm:inline">From URL</span>
            </button>
            <input
              ref={fileInput}
              type="file"
              multiple
              hidden
              onChange={(e) => {
                if (e.target.files) uploads.add(dir, pickedFiles(e.target.files));
                e.target.value = "";
              }}
            />
            {/* webkitdirectory is not in React's types; every current browser supports it. */}
            <input
              ref={folderInput}
              type="file"
              multiple
              hidden
              {...{ webkitdirectory: "" }}
              onChange={(e) => {
                if (e.target.files) uploads.add(dir, pickedFiles(e.target.files));
                e.target.value = "";
              }}
            />
          </>
        )}
        <button
          type="button"
          className="btn btn-ghost !px-3 sm:ml-auto sm:!px-4"
          aria-label="Connect with SFTP"
          title="Open these files in an SFTP client"
          onClick={() => setSftpOpen(true)}
        >
          <Plug className="h-4 w-4" aria-hidden />
          <span className="hidden sm:inline">SFTP</span>
        </button>
      </div>
      <SftpDialog open={sftpOpen} uuid={uuid} serverName={server.data?.name ?? ""} onClose={() => setSftpOpen(false)} />

      <UploadPanel items={uploads.items} onCancel={uploads.cancel} onClear={uploads.clear} />

      <section
        className={`relative overflow-hidden rounded-xl border bg-zinc-900 ${dragging ? "border-indigo-500" : "border-line"}`}
        onDragOver={(e) => {
          if (canWrite && hasFiles(e)) {
            e.preventDefault();
            setDragging(true);
          }
        }}
        onDragLeave={(e) => {
          if (!e.currentTarget.contains(e.relatedTarget as Node)) setDragging(false);
        }}
        onDrop={(e) => void onDrop(e)}
      >
        {dragging && (
          <div className="pointer-events-none absolute inset-0 z-20 grid place-content-center bg-indigo-500/10 text-sm font-medium text-zinc-100">
            Drop files or folders to upload them to {dir}
          </div>
        )}
        <div className="flex flex-wrap items-center justify-between gap-3 px-3.5 py-2.5">
          <nav aria-label="Breadcrumb" className="flex flex-wrap items-center gap-1 text-sm">
            <button
              type="button"
              className="inline-flex h-8 w-8 items-center justify-center rounded-md text-zinc-200 hover:bg-row-hover focus-visible:outline-2 focus-visible:outline-indigo-500"
              onClick={() => setDir("/")}
              aria-label="Root folder"
            >
              <Home className="h-4 w-4" aria-hidden />
            </button>
            {crumbs.map((c, i) => {
              const p = "/" + crumbs.slice(0, i + 1).join("/");
              return (
                <span key={p} className="flex items-center gap-1">
                  <ChevronRight className="h-3 w-3 text-zinc-600" aria-hidden />
                  {i === crumbs.length - 1 ? (
                    <span aria-current="page" className="font-medium">
                      {c}
                    </span>
                  ) : (
                    <button type="button" className="link" onClick={() => setDir(p)}>
                      {c}
                    </button>
                  )}
                </span>
              );
            })}
          </nav>
          <SearchInput
            label="Search files"
            placeholder={dir === "/" ? "Search all files" : "Search in this folder"}
            value={q}
            onChange={setQ}
          />
        </div>

        {chosen.length > 0 && (
          <div
            role="toolbar"
            aria-label="Selected items"
            className="flex flex-wrap items-center gap-2 border-t border-line bg-indigo-500/5 px-3.5 py-2 text-sm"
          >
            <span className="mr-1 font-medium">{chosen.length} selected</span>
            {canWrite && (
              <button type="button" className="btn !min-h-8 !px-3" onClick={() => setAction({ kind: "move", items: chosen.map(item) })}>
                <FolderInput className="h-4 w-4" aria-hidden /> Move
              </button>
            )}
            {canWrite && (
              <button type="button" className="btn !min-h-8 !px-3" onClick={() => setAction({ kind: "compress", items: chosen.map(item) })}>
                <FileArchive className="h-4 w-4" aria-hidden /> Compress
              </button>
            )}
            {canWrite && (
              <button
                type="button"
                className="btn btn-danger !min-h-8 !px-3"
                onClick={() => setAction({ kind: "delete", items: chosen.map(item) })}
              >
                <Trash2 className="h-4 w-4" aria-hidden /> Delete
              </button>
            )}
            <button type="button" className="btn btn-ghost !min-h-8 !px-2.5" onClick={() => setSelected(new Set())}>
              <X className="h-4 w-4" aria-hidden /> Clear selection
            </button>
          </div>
        )}

        {list.isPending && (
          <div className="px-4 py-3">
            <Spinner />
          </div>
        )}
        {needle && searching && (
          <div className="px-4 py-3" role="status">
            <Spinner label={`Searching ${dir === "/" ? "all folders" : dir}`} />
          </div>
        )}
        {list.error ? (
          <div className="px-4 pb-3">
            <ErrorBox error={list.error} />
          </div>
        ) : null}
        {quick.error ? (
          <div className="px-4 pb-3">
            <ErrorBox error={quick.error} />
          </div>
        ) : null}

        {list.data && (
          <div className="relative overflow-x-auto" tabIndex={0} role="region" aria-label="Files">
            <table className="w-full text-sm sm:min-w-[620px]">
              <caption className="sr-only">Files in {dir}</caption>
              <thead className="bg-thead text-left text-[12.5px] text-zinc-500">
                <tr>
                  <th scope="col" className="w-10 py-2.5 pl-4">
                    <input
                      type="checkbox"
                      className="h-4 w-4 accent-indigo-500"
                      aria-label="Select all"
                      checked={allChosen}
                      ref={(el) => {
                        if (el) el.indeterminate = chosen.length > 0 && !allChosen;
                      }}
                      onChange={() => setSelected(allChosen ? new Set() : new Set(rows.map((r) => r.path)))}
                      disabled={rows.length === 0}
                    />
                  </th>
                  <th scope="col" className="px-2 py-2.5 font-medium">
                    Name
                  </th>
                  <th scope="col" className="px-3 py-2.5 font-medium">
                    Size
                  </th>
                  <th scope="col" className="hidden px-3 py-2.5 font-medium md:table-cell">
                    Permissions
                  </th>
                  <th scope="col" className="hidden px-3 py-2.5 font-medium sm:table-cell">
                    Modified
                  </th>
                  <th scope="col" className="px-3 py-2.5">
                    <span className="sr-only">Actions</span>
                  </th>
                </tr>
              </thead>
              <tbody>
                {dir !== "/" && !needle && (
                  <tr className="border-t border-line">
                    <td />
                    <td className="px-2 py-2.5" colSpan={5}>
                      <button type="button" className="link inline-flex items-center gap-2" onClick={() => setDir(parentPath(dir))}>
                        <ArrowLeft className="h-4 w-4" aria-hidden /> Parent directory
                      </button>
                    </td>
                  </tr>
                )}
                {rows.length === 0 && !(needle && searching) && (
                  <tr className="border-t border-line">
                    <td />
                    <td className="px-2 py-4 text-zinc-500" colSpan={5}>
                      {needle
                        ? `No files or folders named “${needle}” in ${dir === "/" ? "any folder" : dir + " or its subfolders"}.`
                        : canWrite
                          ? "Empty directory. Drop files here to upload them."
                          : "Empty directory."}
                    </td>
                  </tr>
                )}
                {rows.map((f) => (
                  <tr
                    key={f.path}
                    className={`group border-t border-line hover:bg-row-hover ${selected.has(f.path) ? "bg-indigo-500/5" : ""}`}
                  >
                    <td className="py-2.5 pl-4">
                      <input
                        type="checkbox"
                        className="h-4 w-4 accent-indigo-500"
                        aria-label={`Select ${f.path.slice(1)}`}
                        checked={selected.has(f.path)}
                        onChange={() => toggle(f.path)}
                      />
                    </td>
                    <td className="px-2 py-2.5">
                      <button
                        type="button"
                        className="inline-flex items-center gap-3 rounded text-left text-[14.5px] text-zinc-100 focus-visible:outline-2 focus-visible:outline-indigo-500"
                        onClick={() => open(f)}
                      >
                        {f.dir ? (
                          <Folder className="h-[17px] w-[17px] shrink-0 text-indigo-400" aria-hidden />
                        ) : isArchive(f.name) ? (
                          <FileArchive className="h-[17px] w-[17px] shrink-0 text-warn-text" aria-hidden />
                        ) : (
                          <FileIcon className="h-[17px] w-[17px] shrink-0 text-zinc-500" aria-hidden />
                        )}
                        <span className="break-all">
                          {f.name}
                          {f.parent && (
                            <span className="ml-2 font-mono text-xs text-zinc-500">
                              <span className="sr-only">, in </span>
                              {f.parent}
                            </span>
                          )}
                        </span>
                      </button>
                    </td>
                    <td className="px-3 py-2.5 font-mono text-[12.5px] text-zinc-400">{f.dir ? "-" : formatBytes(f.size)}</td>
                    <td className="hidden px-3 py-2.5 font-mono text-[12.5px] text-zinc-500 md:table-cell">
                      {f.mode ? modeString(f.mode) : "-"}
                    </td>
                    <td className="hidden px-3 py-2.5 text-[13px] text-zinc-400 sm:table-cell">
                      {f.mod_unix ? new Date(f.mod_unix * 1000).toLocaleString() : "-"}
                    </td>
                    <td className="px-3 py-2.5 text-right">
                      <span className="inline-flex items-center gap-0.5">
                        {!f.dir && (
                          <a
                            href={downloadUrl(uuid, f.path)}
                            download
                            className={`${iconBtn} opacity-60 hover:opacity-100 focus-visible:opacity-100 group-hover:opacity-100`}
                            aria-label={`Download ${f.path.slice(1)}`}
                          >
                            <Download className="h-4 w-4" aria-hidden />
                          </a>
                        )}
                        <RowMenu label={`Actions for ${f.path.slice(1)}`} items={menu(f)} />
                      </span>
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
          : `${entries.length} items. Click a file to edit it${canWrite ? ", or drop files and folders anywhere on the list to upload them" : ""}. Tip: type to search inside every subfolder.`}
      </p>

      <FileDialogs
        uuid={uuid}
        action={action}
        onClose={() => {
          setAction(null);
          setSelected(new Set());
        }}
        onOpenFile={setOpenFile}
      />
    </div>
  );
}
