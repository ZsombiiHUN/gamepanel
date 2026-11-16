import { Check, Loader2, X, AlertCircle } from "lucide-react";
import type { Upload } from "../../hooks/useUploads";
import { formatBytes } from "../../lib/format";

/** Progress of this session's uploads. Finished ones stay until cleared, failures say why. */
export function UploadPanel({ items, onCancel, onClear }: { items: Upload[]; onCancel: (id: number) => void; onClear: () => void }) {
  if (items.length === 0) return null;
  const active = items.filter((u) => u.status === "uploading" || u.status === "queued");
  const total = items.reduce((n, u) => n + u.size, 0);
  const sent = items.reduce((n, u) => n + (u.status === "done" ? u.size : u.sent), 0);
  const failed = items.filter((u) => u.status === "failed").length;
  return (
    <section aria-labelledby="uploads-title" className="overflow-hidden rounded-2xl border border-zinc-800 bg-zinc-900">
      <div className="flex flex-wrap items-center justify-between gap-2 px-4 py-2.5">
        <h2 id="uploads-title" className="text-sm font-semibold">
          {active.length > 0 ? `Uploading ${active.length} ${active.length === 1 ? "file" : "files"}` : "Uploads finished"}
          <span className="ml-2 font-mono text-xs font-normal text-zinc-500">{formatBytes(sent)} of {formatBytes(total)}</span>
          {failed > 0 && <span className="ml-2 text-xs font-normal text-red-300">{failed} failed</span>}
        </h2>
        {active.length === 0 && <button type="button" className="btn btn-ghost !min-h-8 !px-2.5 text-xs" onClick={onClear}>Clear</button>}
      </div>
      <div className="h-1 bg-zinc-800" role="progressbar" aria-label="Upload progress" aria-valuemin={0} aria-valuemax={100} aria-valuenow={total ? Math.round((sent / total) * 100) : 100}>
        <div className="h-1 bg-indigo-500 transition-[width]" style={{ width: `${total ? (sent / total) * 100 : 100}%` }} />
      </div>
      <ul className="max-h-56 divide-y divide-zinc-800 overflow-y-auto text-[13px]">
        {items.map((u) => (
          <li key={u.id} className="flex items-center gap-3 px-4 py-2">
            {u.status === "done" ? <Check className="h-4 w-4 shrink-0 text-emerald-400" aria-label="Uploaded" />
              : u.status === "failed" ? <AlertCircle className="h-4 w-4 shrink-0 text-red-300" aria-label="Failed" />
              : u.status === "cancelled" ? <X className="h-4 w-4 shrink-0 text-zinc-500" aria-label="Cancelled" />
              : <Loader2 className={`h-4 w-4 shrink-0 text-zinc-400 ${u.status === "uploading" ? "animate-spin" : ""}`} aria-label={u.status === "uploading" ? "Uploading" : "Waiting"} />}
            <span className="min-w-0 flex-1">
              <span className="block truncate font-mono text-zinc-200" title={u.path}>{u.path}</span>
              {u.error && <span className="block text-xs text-red-300">{u.error}</span>}
            </span>
            <span className="shrink-0 font-mono text-xs text-zinc-500">
              {u.status === "uploading" ? `${Math.round((u.sent / Math.max(u.size, 1)) * 100)}%` : formatBytes(u.size)}
            </span>
            {(u.status === "uploading" || u.status === "queued") && (
              <button type="button" className="inline-flex h-7 w-7 items-center justify-center rounded-md text-zinc-500 hover:bg-zinc-800 hover:text-zinc-100" aria-label={`Cancel ${u.path}`} onClick={() => onCancel(u.id)}>
                <X className="h-4 w-4" aria-hidden />
              </button>
            )}
          </li>
        ))}
      </ul>
    </section>
  );
}
