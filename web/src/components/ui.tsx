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

type Tone = "ok" | "warn" | "off" | "bad";
const stateTone: Record<string, Tone> = {
  running: "ok", starting: "warn", stopping: "warn", installing: "warn", install_failed: "bad", offline: "off", unknown: "off",
};
const toneCls: Record<Tone, string> = {
  ok: "bg-[#12261a] text-[#7bdb9c]",
  warn: "bg-[#2a2010] text-[#f0b35a]",
  off: "bg-[#1b1f24] text-[#9ba2aa]",
  bad: "bg-[#2a1411] text-[#ff8a7a]",
};
const stateLabel: Record<string, string> = {
  running: "Running", starting: "Starting", stopping: "Stopping", installing: "Installing", install_failed: "Install failed", offline: "Stopped", unknown: "Unknown",
};

/** Status mark. Shape carries the meaning as well as colour: filled dot, square, ring, triangle. */
export function StateMark({ state }: { state: string }) {
  const tone = stateTone[state] ?? "off";
  if (tone === "ok") return <span className="inline-block h-2 w-2 shrink-0 rounded-full bg-[#4cc27a]" aria-hidden />;
  if (tone === "warn") return <span className="inline-block h-2 w-2 shrink-0 rounded-[2px] bg-[#e8a33d]" aria-hidden />;
  if (tone === "bad") return <span className="inline-block h-0 w-0 shrink-0 border-x-[4.5px] border-b-[8px] border-x-transparent border-b-[#ff8a7a]" aria-hidden />;
  return <span className="inline-block h-2 w-2 shrink-0 rounded-full border-[1.5px] border-zinc-500" aria-hidden />;
}

export function StateBadge({ state }: { state: string }) {
  const tone = stateTone[state] ?? "off";
  return (
    <span className={`inline-flex min-h-[26px] items-center gap-1.5 rounded-full px-2.5 text-[12.5px] font-medium ${toneCls[tone]}`}>
      <StateMark state={state} />
      {stateLabel[state] ?? state.replace("_", " ")}
    </span>
  );
}

/** Deterministic blocky icon, unique per server name. Never a game logo (trademarks). */
export function ServerIcon({ name, size = 44 }: { name: string; size?: number }) {
  let h = 2166136261;
  for (let i = 0; i < name.length; i++) h = Math.imul(h ^ name.charCodeAt(i), 16777619) >>> 0;
  const next = () => { h = Math.imul(h ^ (h >>> 15), 2246822507) >>> 0; h = Math.imul(h ^ (h >>> 13), 3266489909) >>> 0; return (h ^= h >>> 16) >>> 0; };
  const hue = next() % 360;
  const cells: React.ReactNode[] = [];
  for (let r = 0; r < 5; r++)
    for (let c = 0; c < 3; c++) {
      const v = next();
      if (v % 100 >= 56) continue;
      const fill = v % 2 ? `hsl(${hue} 70% 72%)` : `hsl(${(hue + 28) % 360} 62% 55%)`;
      for (const cc of new Set([c, 4 - c]))
        cells.push(<rect key={`${r}-${cc}`} x={3 + cc * 7} y={3 + r * 7} width={6} height={6} rx={1.2} fill={fill} />);
    }
  return (
    <span
      aria-hidden
      className="inline-block shrink-0"
      style={{ width: size, height: size, padding: size * 0.14, borderRadius: size * 0.24, boxSizing: "border-box", background: `hsl(${hue} 38% 15%)`, boxShadow: `inset 0 0 0 1px hsl(${hue} 50% 60% / .22)` }}
    >
      <svg viewBox="0 0 38 38" width="100%" height="100%">{cells}</svg>
    </span>
  );
}

export function OnlineDot({ online, label }: { online: boolean; label: string }) {
  return (
    <span className="inline-flex items-center gap-1.5 text-xs text-zinc-300">
      <span className={`h-2 w-2 rounded-full ${online ? "bg-[#4cc27a]" : "bg-[#ff8a7a]"}`} aria-hidden />
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

/** Labelled thin bar. Turns amber when `pct` is at or above `warnAt`. */
export function Meter({ label, text, pct, warnAt = 85 }: { label: string; text: string; pct: number | null; warnAt?: number }) {
  const warn = pct !== null && pct >= warnAt;
  const w = pct === null ? 0 : Math.max(0, Math.min(100, pct));
  return (
    <div className="flex flex-col gap-1.5">
      <div className="flex justify-between text-[12.5px]">
        <span className="text-zinc-500">{label}</span>
        <span className={`font-mono ${warn ? "text-[#f0b35a]" : "text-zinc-200"}`}>{text}</span>
      </div>
      <div className="h-1 rounded-full bg-[#23282e]" role="presentation">
        <div className={`h-full rounded-full ${warn ? "bg-[#e8a33d]" : "bg-indigo-400"}`} style={{ width: `${w}%` }} />
      </div>
    </div>
  );
}
