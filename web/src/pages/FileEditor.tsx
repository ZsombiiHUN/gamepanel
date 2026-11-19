import { useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import CodeMirror from "@uiw/react-codemirror";
import { oneDark } from "@codemirror/theme-one-dark";
import { json } from "@codemirror/lang-json";
import { yaml } from "@codemirror/lang-yaml";
import { javascript } from "@codemirror/lang-javascript";
import type { Extension } from "@codemirror/state";
import { ArrowLeft, Loader2, Save, Trash2 } from "lucide-react";
import { api, ApiError, call } from "../api/client";
import { fileQuery, invalidates, keys } from "../api/queries";
import { ConfirmDialog, ErrorBox, Spinner } from "../components/ui";

function langFor(path: string): Extension[] {
  const ext = path.split(".").pop()?.toLowerCase();
  if (ext === "json") return [json()];
  if (ext === "yml" || ext === "yaml") return [yaml()];
  if (ext === "js" || ext === "mjs" || ext === "ts") return [javascript()];
  return [];
}

function friendly(err: unknown): unknown {
  if (err instanceof ApiError && err.status === 413) {
    return new Error("This file is too large to edit in the browser. Use SFTP or another tool for large files.");
  }
  return err;
}

export function FileEditor({ uuid, path, onClose, onDeleted }: { uuid: string; path: string; onClose: () => void; onDeleted: () => void }) {
  const file = useQuery(fileQuery(uuid, path));
  // What the user typed (null until they type) and what was last saved. Neither copies server data into state.
  const [edited, setEdited] = useState<string | null>(null);
  const [saved, setSaved] = useState<string | null>(null);
  const [confirm, setConfirm] = useState(false);

  const base = saved ?? file.data?.content ?? "";
  const text = edited ?? base;
  const dirty = text !== base;

  const save = useMutation({
    mutationFn: () => call(api.PUT("/servers/{uuid}/files/content", { params: { path: { uuid } }, body: { path, content: text } })),
    onSuccess: () => setSaved(text),
  });
  const del = useMutation({
    mutationFn: () => call(api.DELETE("/servers/{uuid}/files", { params: { path: { uuid }, query: { path } } })),
    meta: invalidates(keys.files(uuid)),
    onSuccess: onDeleted,
  });

  const close = () => {
    if (dirty && !window.confirm("Discard unsaved changes?")) return;
    onClose();
  };

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-2">
        <button type="button" className="btn" onClick={close}>
          <ArrowLeft className="h-4 w-4" aria-hidden /> Back
        </button>
        <h2 className="break-all font-mono text-sm">
          {path}
          {dirty && (
            <span className="ml-2 text-amber-400" title="Unsaved changes">
              (modified)
            </span>
          )}
        </h2>
        <div className="ml-auto flex gap-2">
          <button type="button" className="btn btn-primary" onClick={() => save.mutate()} disabled={!file.data || !dirty || save.isPending}>
            {save.isPending ? <Loader2 className="h-4 w-4 animate-spin" aria-hidden /> : <Save className="h-4 w-4" aria-hidden />} Save
          </button>
          <button type="button" className="btn btn-danger" onClick={() => setConfirm(true)}>
            <Trash2 className="h-4 w-4" aria-hidden /> Delete
          </button>
        </div>
      </div>
      {file.isPending && <Spinner />}
      <ErrorBox error={friendly(file.error)} />
      <ErrorBox error={save.error} />
      {save.isSuccess && !dirty && (
        <p role="status" className="text-sm text-emerald-400">
          Saved.
        </p>
      )}
      {file.data && (
        <div role="group" aria-label={`Editor for ${path}`} className="overflow-hidden rounded-md border border-zinc-800">
          <CodeMirror value={text} onChange={setEdited} theme={oneDark} extensions={langFor(path)} height="60vh" />
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
        onCancel={() => {
          setConfirm(false);
          del.reset();
        }}
      />
    </div>
  );
}
