import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import CodeMirror from "@uiw/react-codemirror";
import { oneDark } from "@codemirror/theme-one-dark";
import { json } from "@codemirror/lang-json";
import { yaml } from "@codemirror/lang-yaml";
import { javascript } from "@codemirror/lang-javascript";
import type { Extension } from "@codemirror/state";
import { ChevronRight, File as FileIcon, Folder, Home, Loader2, Save, Search, Trash2, ArrowLeft } from "lucide-react";
import { api, ApiError, call, type components } from "../api/client";
import { ConfirmDialog, ErrorBox, Spinner } from "../components/ui";
import { formatBytes, joinPath, parentPath } from "../lib/format";

type FileInfo = components["schemas"]["FileInfo"];

function langFor(path: string): Extension[] {
  const ext = path.split(".").pop()?.toLowerCase();
  if (ext === "json") return [json()];
  if (ext === "yml" || ext === "yaml") return [yaml()];
  if (ext === "js" || ext === "mjs" || ext === "ts") return [javascript()];
  return [];
}

function friendly(err: unknown): unknown {
  if (err instanceof ApiError && (err.status === 413 || /too large/i.test(err.message))) {
    return new Error("This file is too large to edit in the browser. Use SFTP or another tool for large files.");
  }
  return err;
}

export function FilesTab({ uuid }: { uuid: string }) {
  const [dir, setDir] = useState("/");
  const [openFile, setOpenFile] = useState<string | null>(null);
  const [toDelete, setToDelete] = useState<FileInfo | null>(null);
  const [q, setQ] = useState("");
  const qc = useQueryClient();

  const list = useQuery({
    queryKey: ["files", uuid, dir],
    queryFn: async () =>
      (await call(api.GET("/servers/{uuid}/files", { params: { path: { uuid }, query: { path: dir } } }))) ?? [],
  });

  const del = useMutation({
    mutationFn: (p: string) =>
      call(api.DELETE("/servers/{uuid}/files", { params: { path: { uuid }, query: { path: p } } })),
    onSuccess: async () => {
      setToDelete(null);
      await qc.invalidateQueries({ queryKey: ["files", uuid] });
    },
  });

  const entries = [...(list.data ?? [])].sort((a, b) => Number(b.dir) - Number(a.dir) || a.name.localeCompare(b.name));
  const crumbs = dir.split("/").filter(Boolean);

  if (openFile) {
    return (
      <FileEditor
        uuid={uuid}
        path={openFile}
        onClose={() => setOpenFile(null)}
        onDeleted={() => {
          setOpenFile(null);
          void qc.invalidateQueries({ queryKey: ["files", uuid] });
        }}
      />
    );
  }

  const shown = entries.filter((f) => f.name.toLowerCase().includes(q.trim().toLowerCase()));
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
          <label className="flex min-h-9 items-center gap-2 rounded-lg border border-zinc-700 bg-[#101317] px-3 text-zinc-500">
            <Search className="h-4 w-4" aria-hidden />
            <span className="sr-only">Search files</span>
            <input className="w-36 bg-transparent text-sm text-zinc-100 outline-none placeholder:text-zinc-500" placeholder="Search files" value={q} onChange={(e) => setQ(e.target.value)} />
          </label>
        </div>

        {list.isPending && <div className="px-4 py-3"><Spinner /></div>}
        {list.error ? <div className="px-4 pb-3"><ErrorBox error={list.error} /></div> : null}

        {list.data && (
          <div className="overflow-x-auto">
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
                {dir !== "/" && (
                  <tr className="border-t border-[#1b2025]">
                    <td className="px-5 py-2.5" colSpan={4}>
                      <button type="button" className="link inline-flex items-center gap-2" onClick={() => setDir(parentPath(dir))}>
                        <ArrowLeft className="h-4 w-4" aria-hidden /> Parent directory
                      </button>
                    </td>
                  </tr>
                )}
                {shown.length === 0 && <tr className="border-t border-[#1b2025]"><td className="px-5 py-4 text-zinc-500" colSpan={4}>{q ? "No files match." : "Empty directory."}</td></tr>}
                {shown.map((f) => (
                  <tr key={f.name} className="group border-t border-[#1b2025] hover:bg-[#161a1f]">
                    <td className="px-5 py-2.5">
                      <button
                        type="button"
                        className="inline-flex items-center gap-3 rounded text-[14.5px] text-zinc-100 focus-visible:outline-2 focus-visible:outline-indigo-500"
                        onClick={() => (f.dir ? setDir(joinPath(dir, f.name)) : setOpenFile(joinPath(dir, f.name)))}
                      >
                        {f.dir ? <Folder className="h-[17px] w-[17px] text-indigo-400" aria-hidden /> : <FileIcon className="h-[17px] w-[17px] text-zinc-500" aria-hidden />}
                        {f.name}
                      </button>
                    </td>
                    <td className="px-3 py-2.5 font-mono text-[12.5px] text-zinc-400">{f.dir ? "-" : formatBytes(f.size)}</td>
                    <td className="px-3 py-2.5 text-[13px] text-zinc-400">{f.mod_unix ? new Date(f.mod_unix * 1000).toLocaleString() : "-"}</td>
                    <td className="px-3 py-2.5 text-right">
                      <button type="button" className="inline-flex h-8 w-8 items-center justify-center rounded-md text-zinc-500 opacity-60 hover:bg-zinc-800 hover:text-red-300 hover:opacity-100 focus-visible:opacity-100 focus-visible:outline-2 focus-visible:outline-indigo-500 group-hover:opacity-100" onClick={() => setToDelete(f)} aria-label={`Delete ${f.name}`}>
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
      <p className="text-[12.5px] text-zinc-500">{entries.length} items. Click a file to edit it. Use SFTP to upload.</p>

      <ConfirmDialog
        open={!!toDelete}
        title={`Delete ${toDelete?.name ?? ""}?`}
        body={toDelete?.dir ? "This deletes the directory and everything in it." : "This permanently deletes the file."}
        confirmLabel="Delete"
        pending={del.isPending}
        error={del.error}
        onConfirm={() => toDelete && del.mutate(joinPath(dir, toDelete.name))}
        onCancel={() => { setToDelete(null); del.reset(); }}
      />
    </div>
  );
}

function FileEditor({ uuid, path, onClose, onDeleted }: { uuid: string; path: string; onClose: () => void; onDeleted: () => void }) {
  const file = useQuery({
    queryKey: ["file", uuid, path],
    queryFn: () => call(api.GET("/servers/{uuid}/files/content", { params: { path: { uuid }, query: { path } } })),
    retry: false,
    gcTime: 0,
  });
  const [text, setText] = useState("");
  const [saved, setSaved] = useState("");
  const [confirm, setConfirm] = useState(false);

  useEffect(() => {
    if (file.data) {
      setText(file.data.content);
      setSaved(file.data.content);
    }
  }, [file.data]);

  const save = useMutation({
    mutationFn: () =>
      call(api.PUT("/servers/{uuid}/files/content", { params: { path: { uuid } }, body: { path, content: text } })),
    onSuccess: () => setSaved(text),
  });
  const del = useMutation({
    mutationFn: () => call(api.DELETE("/servers/{uuid}/files", { params: { path: { uuid }, query: { path } } })),
    onSuccess: onDeleted,
  });

  const dirty = text !== saved;
  const close = () => {
    if (dirty && !window.confirm("Discard unsaved changes?")) return;
    onClose();
  };

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-2">
        <button type="button" className="btn" onClick={close}><ArrowLeft className="h-4 w-4" aria-hidden /> Back</button>
        <h2 className="break-all font-mono text-sm">{path}{dirty && <span className="ml-2 text-amber-400" title="Unsaved changes">(modified)</span>}</h2>
        <div className="ml-auto flex gap-2">
          <button type="button" className="btn btn-primary" onClick={() => save.mutate()} disabled={!file.data || !dirty || save.isPending}>
            {save.isPending ? <Loader2 className="h-4 w-4 animate-spin" aria-hidden /> : <Save className="h-4 w-4" aria-hidden />} Save
          </button>
          <button type="button" className="btn btn-danger" onClick={() => setConfirm(true)}><Trash2 className="h-4 w-4" aria-hidden /> Delete</button>
        </div>
      </div>
      {file.isPending && <Spinner />}
      <ErrorBox error={friendly(file.error)} />
      <ErrorBox error={save.error} />
      {save.isSuccess && !dirty && <p role="status" className="text-sm text-emerald-400">Saved.</p>}
      {file.data && (
        <div role="group" aria-label={`Editor for ${path}`} className="overflow-hidden rounded-md border border-zinc-800">
          <CodeMirror value={text} onChange={setText} theme={oneDark} extensions={langFor(path)} height="60vh" />
        </div>
      )}
      <ConfirmDialog
        open={confirm}
        title="Delete this file?"
        body={<span className="break-all font-mono">{path}</span>}
        confirmLabel="Delete"
        pending={del.isPending}
        error={del.error}
        onConfirm={() => del.mutate()}
        onCancel={() => { setConfirm(false); del.reset(); }}
      />
    </div>
  );
}
