import { useEffect, useRef, useState, type ReactNode } from "react";
import { AlertCircle, Check, Copy, Loader2 } from "lucide-react";

export function Spinner({ label = "Loading" }: { label?: string }) {
  return (
    <div role="status" className="flex items-center gap-2 text-sm text-zinc-400">
      <Loader2 className="h-4 w-4 animate-spin" aria-hidden /> {label}...
    </div>
  );
}

export function ErrorBox({ error }: { error: unknown }) {
  if (!error) return null;
  const msg = error instanceof Error ? error.message : String(error);
  return (
    <div role="alert" className="flex items-start gap-2 rounded-md border border-red-800 bg-red-950/60 p-3 text-sm text-red-200">
      <AlertCircle className="mt-0.5 h-4 w-4 shrink-0" aria-hidden />
      <span className="break-words">{msg}</span>
    </div>
  );
}

const stateStyles: Record<string, string> = {
  running: "bg-emerald-900/60 text-emerald-300 border-emerald-700",
  starting: "bg-amber-900/60 text-amber-300 border-amber-700",
  stopping: "bg-amber-900/60 text-amber-300 border-amber-700",
  offline: "bg-zinc-800 text-zinc-300 border-zinc-700",
  unknown: "bg-zinc-800 text-zinc-400 border-zinc-700",
};

export function StateBadge({ state }: { state: string }) {
  return (
    <span className={`inline-block rounded-full border px-2 py-0.5 text-xs font-medium ${stateStyles[state] ?? stateStyles.unknown}`}>
      {state}
    </span>
  );
}

export function OnlineDot({ online, label }: { online: boolean; label: string }) {
  return (
    <span className="inline-flex items-center gap-1.5 text-xs text-zinc-300">
      <span className={`h-2 w-2 rounded-full ${online ? "bg-emerald-400" : "bg-red-500"}`} aria-hidden />
      {label}: {online ? "online" : "offline"}
    </span>
  );
}

export function Field({ label, children, hint }: { label: string; children: (id: string) => ReactNode; hint?: string }) {
  const id = useRef(`f-${Math.random().toString(36).slice(2)}`).current;
  return (
    <div>
      <label htmlFor={id} className="label">{label}</label>
      {children(id)}
      {hint && <p className="mt-1 text-xs text-zinc-500">{hint}</p>}
    </div>
  );
}

export function CopyButton({ text, label = "Copy" }: { text: string; label?: string }) {
  const [done, setDone] = useState(false);
  const [err, setErr] = useState(false);
  return (
    <button
      type="button"
      className="btn"
      onClick={async () => {
        try {
          await navigator.clipboard.writeText(text);
          setDone(true);
          setErr(false);
          setTimeout(() => setDone(false), 2000);
        } catch {
          setErr(true);
        }
      }}
    >
      {done ? <Check className="h-4 w-4" aria-hidden /> : <Copy className="h-4 w-4" aria-hidden />}
      {done ? "Copied" : err ? "Copy failed - select manually" : label}
    </button>
  );
}

export function ConfirmDialog({
  open,
  title,
  body,
  confirmLabel,
  pending,
  error,
  onConfirm,
  onCancel,
}: {
  open: boolean;
  title: string;
  body: ReactNode;
  confirmLabel: string;
  pending?: boolean;
  error?: unknown;
  onConfirm: () => void;
  onCancel: () => void;
}) {
  const ref = useRef<HTMLDialogElement>(null);
  useEffect(() => {
    const d = ref.current;
    if (!d) return;
    if (open && !d.open) d.showModal();
    if (!open && d.open) d.close();
  }, [open]);
  return (
    <dialog
      ref={ref}
      onClose={onCancel}
      aria-labelledby="confirm-title"
      className="m-auto w-full max-w-md rounded-lg border border-zinc-700 bg-zinc-900 p-5 text-zinc-100 backdrop:bg-black/60"
    >
      <h2 id="confirm-title" className="text-lg font-semibold">{title}</h2>
      <div className="mt-2 text-sm text-zinc-300">{body}</div>
      {error ? <div className="mt-3"><ErrorBox error={error} /></div> : null}
      <div className="mt-5 flex justify-end gap-2">
        <button type="button" className="btn" onClick={onCancel} autoFocus>Cancel</button>
        <button type="button" className="btn btn-danger" onClick={onConfirm} disabled={pending}>
          {pending && <Loader2 className="h-4 w-4 animate-spin" aria-hidden />}
          {confirmLabel}
        </button>
      </div>
    </dialog>
  );
}
