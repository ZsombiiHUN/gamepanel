import { useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { api, call } from "../../api/client";
import { invalidatesAlways, keys } from "../../api/queries";
import { Field, FormDialog } from "../../components/ui";
import { joinPath, parentPath } from "../../lib/format";
import { modeString, moveTarget, nameProblem, octal } from "../../lib/files";

export interface FileItem {
  name: string;
  path: string;
  dir: boolean;
  mode?: number;
}

export type FileAction =
  | { kind: "rename" | "chmod"; items: [FileItem] }
  | { kind: "move" | "compress" | "delete"; items: FileItem[] }
  | { kind: "newFolder" | "newFile" | "pull"; dir: string };

/** Runs one file request and refreshes the listing afterwards, whatever happened. */
function useFileMutation<T>(uuid: string, fn: (v: T) => Promise<unknown>, onDone: () => void) {
  return useMutation({ mutationFn: fn, onSuccess: onDone, meta: invalidatesAlways(keys.files(uuid)) });
}

const names = (items: FileItem[]) => (items.length === 1 ? `“${items[0]?.name ?? ""}”` : `${items.length} items`);

export function FileDialogs({
  uuid,
  action,
  onClose,
  onOpenFile,
}: {
  uuid: string;
  action: FileAction | null;
  onClose: () => void;
  onOpenFile: (path: string) => void;
}) {
  if (!action) return null;
  switch (action.kind) {
    case "rename":
      return <RenameDialog uuid={uuid} item={action.items[0]} onClose={onClose} />;
    case "move":
      return <MoveDialog uuid={uuid} items={action.items} onClose={onClose} />;
    case "chmod":
      return <ChmodDialog uuid={uuid} item={action.items[0]} onClose={onClose} />;
    case "compress":
      return <CompressDialog uuid={uuid} items={action.items} onClose={onClose} />;
    case "delete":
      return <DeleteDialog uuid={uuid} items={action.items} onClose={onClose} />;
    case "newFolder":
    case "newFile":
      return <NewDialog uuid={uuid} dir={action.dir} folder={action.kind === "newFolder"} onClose={onClose} onOpenFile={onOpenFile} />;
    case "pull":
      return <PullDialog uuid={uuid} dir={action.dir} onClose={onClose} />;
  }
}

function RenameDialog({ uuid, item, onClose }: { uuid: string; item: FileItem; onClose: () => void }) {
  const [name, setName] = useState(item.name);
  const problem = nameProblem(name);
  const m = useFileMutation(
    uuid,
    () =>
      call(
        api.POST("/servers/{uuid}/files/move", {
          params: { path: { uuid } },
          body: { moves: [{ from: item.path, to: joinPath(parentPath(item.path), name.trim()) }] },
        }),
      ),
    onClose,
  );
  return (
    <FormDialog
      open
      title={`Rename ${item.name}`}
      submitLabel="Rename"
      pending={m.isPending}
      error={m.error}
      disabled={!!problem || name.trim() === item.name}
      onSubmit={() => m.mutate(undefined)}
      onClose={onClose}
    >
      <Field label="New name" hint={name && problem ? problem : undefined}>
        {(id) => (
          <input
            id={id}
            className="input font-mono text-[13px]"
            value={name}
            onChange={(e) => setName(e.target.value)}
            autoFocus
            onFocus={(e) =>
              e.target.setSelectionRange(0, item.dir ? item.name.length : Math.max(item.name.lastIndexOf("."), 1) || item.name.length)
            }
            spellCheck={false}
          />
        )}
      </Field>
    </FormDialog>
  );
}

function MoveDialog({ uuid, items, onClose }: { uuid: string; items: FileItem[]; onClose: () => void }) {
  const [folder, setFolder] = useState(parentPath(items[0]?.path ?? "/"));
  const moves = items.map((i) => ({ from: i.path, to: moveTarget(i.path, folder) })).filter((m) => m.from !== m.to);
  const m = useFileMutation(
    uuid,
    () => call(api.POST("/servers/{uuid}/files/move", { params: { path: { uuid } }, body: { moves } })),
    onClose,
  );
  return (
    <FormDialog
      open
      title={`Move ${names(items)}`}
      submitLabel="Move"
      pending={m.isPending}
      error={m.error}
      disabled={moves.length === 0}
      onSubmit={() => m.mutate(undefined)}
      onClose={onClose}
    >
      <Field label="Into folder" hint="Missing folders are created. Nothing is overwritten.">
        {(id) => (
          <input
            id={id}
            className="input font-mono text-[13px]"
            value={folder}
            onChange={(e) => setFolder(e.target.value)}
            autoFocus
            spellCheck={false}
            placeholder="/plugins/old"
          />
        )}
      </Field>
    </FormDialog>
  );
}

const WHO = ["Owner", "Group", "Others"] as const;
const WHAT = [
  ["Read", 4],
  ["Write", 2],
  ["Run", 1],
] as const;

function ChmodDialog({ uuid, item, onClose }: { uuid: string; item: FileItem; onClose: () => void }) {
  const [mode, setMode] = useState(item.mode ?? 0o644);
  const m = useFileMutation(
    uuid,
    () => call(api.POST("/servers/{uuid}/files/chmod", { params: { path: { uuid } }, body: { paths: [item.path], mode: octal(mode) } })),
    onClose,
  );
  return (
    <FormDialog
      open
      title={`Permissions of ${item.name}`}
      submitLabel="Save permissions"
      pending={m.isPending}
      error={m.error}
      onSubmit={() => m.mutate(undefined)}
      onClose={onClose}
    >
      <table className="w-full text-sm">
        <caption className="sr-only">Who may read, write and run {item.name}</caption>
        <thead>
          <tr className="text-left text-xs text-zinc-500">
            <th scope="col" className="pb-2 font-medium" />
            {WHAT.map(([w]) => (
              <th key={w} scope="col" className="pb-2 font-medium">
                {w}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {WHO.map((who, wi) => (
            <tr key={who}>
              <th scope="row" className="py-1.5 pr-4 text-left font-medium text-zinc-300">
                {who}
              </th>
              {WHAT.map(([what, bit]) => {
                const b = bit << (3 * (2 - wi));
                return (
                  <td key={what} className="py-1.5">
                    <input
                      type="checkbox"
                      className="h-4 w-4 accent-indigo-500"
                      aria-label={`${who} can ${what.toLowerCase()}`}
                      checked={(mode & b) !== 0}
                      onChange={(e) => setMode(e.target.checked ? mode | b : mode & ~b)}
                    />
                  </td>
                );
              })}
            </tr>
          ))}
        </tbody>
      </table>
      <p className="font-mono text-[13px] text-zinc-400">
        {octal(mode)} · {modeString(mode)}
      </p>
      <p className="text-xs text-zinc-500">The panel always keeps read and write for the owner, so the server can still use the file.</p>
    </FormDialog>
  );
}

function CompressDialog({ uuid, items, onClose }: { uuid: string; items: FileItem[]; onClose: () => void }) {
  const [format, setFormat] = useState<"tar.gz" | "zip">("zip");
  const root = parentPath(items[0]?.path ?? "/");
  const m = useFileMutation(
    uuid,
    () =>
      call(
        api.POST("/servers/{uuid}/files/compress", { params: { path: { uuid } }, body: { root, paths: items.map((i) => i.path), format } }),
      ),
    onClose,
  );
  return (
    <FormDialog
      open
      title={`Compress ${names(items)}`}
      submitLabel="Create archive"
      pending={m.isPending}
      error={m.error}
      onSubmit={() => m.mutate(undefined)}
      onClose={onClose}
    >
      <fieldset>
        <legend className="label">Format</legend>
        <div className="flex gap-4">
          {(["zip", "tar.gz"] as const).map((f) => (
            <label key={f} className="inline-flex items-center gap-2">
              <input type="radio" name="format" className="accent-indigo-500" checked={format === f} onChange={() => setFormat(f)} /> .{f}
            </label>
          ))}
        </div>
      </fieldset>
      <p className="text-xs text-zinc-500">
        The archive is saved in {root} and counts toward the server's disk limit. {m.isPending && "Large folders can take a few minutes."}
      </p>
    </FormDialog>
  );
}

function DeleteDialog({ uuid, items, onClose }: { uuid: string; items: FileItem[]; onClose: () => void }) {
  const m = useFileMutation(
    uuid,
    () => call(api.POST("/servers/{uuid}/files/delete", { params: { path: { uuid } }, body: { paths: items.map((i) => i.path) } })),
    onClose,
  );
  const folders = items.filter((i) => i.dir).length;
  return (
    <FormDialog
      open
      danger
      title={`Delete ${names(items)}?`}
      submitLabel="Delete"
      pending={m.isPending}
      error={m.error}
      onSubmit={() => m.mutate(undefined)}
      onClose={onClose}
    >
      <p>
        {folders > 0 ? "Folders are deleted with everything in them. " : ""}This cannot be undone, so take a backup first if you are unsure.
      </p>
      {items.length > 1 && (
        <ul className="max-h-40 overflow-y-auto rounded-lg border border-line bg-zinc-950 p-2 font-mono text-xs text-zinc-400">
          {items.map((i) => (
            <li key={i.path}>{i.path}</li>
          ))}
        </ul>
      )}
    </FormDialog>
  );
}

function NewDialog({
  uuid,
  dir,
  folder,
  onClose,
  onOpenFile,
}: {
  uuid: string;
  dir: string;
  folder: boolean;
  onClose: () => void;
  onOpenFile: (p: string) => void;
}) {
  const [name, setName] = useState("");
  const problem = nameProblem(name);
  const path = joinPath(dir, name.trim());
  const m = useFileMutation(
    uuid,
    () =>
      folder
        ? call(api.POST("/servers/{uuid}/files/folder", { params: { path: { uuid } }, body: { path } }))
        : call(api.PUT("/servers/{uuid}/files/content", { params: { path: { uuid } }, body: { path, content: "" } })),
    () => {
      onClose();
      if (!folder) onOpenFile(path);
    },
  );
  return (
    <FormDialog
      open
      title={folder ? "New folder" : "New file"}
      submitLabel={folder ? "Create folder" : "Create and edit"}
      pending={m.isPending}
      error={m.error}
      disabled={!!problem}
      onSubmit={() => m.mutate(undefined)}
      onClose={onClose}
    >
      <Field label="Name" hint={name && problem ? problem : `Created in ${dir}`}>
        {(id) => (
          <input
            id={id}
            className="input font-mono text-[13px]"
            value={name}
            onChange={(e) => setName(e.target.value)}
            autoFocus
            spellCheck={false}
            placeholder={folder ? "plugins" : "server.properties"}
          />
        )}
      </Field>
    </FormDialog>
  );
}

function PullDialog({ uuid, dir, onClose }: { uuid: string; dir: string; onClose: () => void }) {
  const [url, setUrl] = useState("");
  const [name, setName] = useState("");
  const valid = /^https?:\/\/\S+$/i.test(url.trim());
  const target = name.trim() ? joinPath(dir, name.trim()) : dir.endsWith("/") ? dir : `${dir}/`;
  const m = useFileMutation(
    uuid,
    () => call(api.POST("/servers/{uuid}/files/pull", { params: { path: { uuid } }, body: { url: url.trim(), path: target } })),
    onClose,
  );
  return (
    <FormDialog
      open
      title="Download from a URL"
      submitLabel="Download"
      pending={m.isPending}
      error={m.error}
      disabled={!valid || (!!name && !!nameProblem(name))}
      onSubmit={() => m.mutate(undefined)}
      onClose={onClose}
    >
      <Field label="URL" hint="The node downloads it directly, which is quicker than downloading and uploading it yourself.">
        {(id) => (
          <input
            id={id}
            type="url"
            className="input font-mono text-[13px]"
            value={url}
            onChange={(e) => setUrl(e.target.value)}
            autoFocus
            placeholder="https://example.com/plugin.jar"
            spellCheck={false}
          />
        )}
      </Field>
      <Field label="Save as (optional)" hint={`Saved in ${dir}. Leave empty to keep the file's own name.`}>
        {(id) => (
          <input
            id={id}
            className="input font-mono text-[13px]"
            value={name}
            onChange={(e) => setName(e.target.value)}
            spellCheck={false}
          />
        )}
      </Field>
      {m.isPending && (
        <p role="status" className="text-xs text-zinc-400">
          Downloading. Large files can take a few minutes.
        </p>
      )}
    </FormDialog>
  );
}
