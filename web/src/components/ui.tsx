import { useEffect, useId, useMemo, useRef, useState, type ReactNode } from "react";
import { AlertCircle, Check, Copy, Loader2 } from "lucide-react";
import { blockIcon } from "../lib/icon";
import { errorText } from "../lib/problem";
import { stateLabel } from "../lib/servers";

export function Spinner({ label = "Loading" }: { label?: string }) {
  return (
    <div role="status" className="flex items-center gap-2 text-sm text-zinc-400">
      <Loader2 className="h-4 w-4 animate-spin" aria-hidden /> {label}...
    </div>
  );
}

export function ErrorBox({ error }: { error: unknown }) {
  if (!error) return null;
  const msg = errorText(error);
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
  const { hue, blocks } = useMemo(() => blockIcon(name), [name]);
  return (
    <span
      aria-hidden
      className="inline-block shrink-0"
      style={{ width: size, height: size, padding: size * 0.14, borderRadius: size * 0.24, boxSizing: "border-box", background: `hsl(${hue} 38% 15%)`, boxShadow: `inset 0 0 0 1px hsl(${hue} 50% 60% / .22)` }}
    >
      <svg viewBox="0 0 38 38" width="100%" height="100%">
        {blocks.map((b) => <rect key={b.key} x={b.x} y={b.y} width={6} height={6} rx={1.2} fill={b.fill} />)}
      </svg>
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
  const id = useId();
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
      className="m-auto w-full max-w-md rounded-2xl border border-zinc-800 bg-zinc-900 p-6 text-zinc-100 shadow-2xl backdrop:bg-black/70"
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

/**
 * A modal form: title, fields, then Cancel and a submit button. Enter submits;
 * Esc and Cancel close. The dialog element gives focus trapping for free.
 */
export function FormDialog({
  open, title, children, submitLabel, pending, error, disabled, danger, onSubmit, onClose,
}: {
  open: boolean;
  title: string;
  children: ReactNode;
  submitLabel: string;
  pending?: boolean;
  error?: unknown;
  disabled?: boolean;
  danger?: boolean;
  onSubmit: () => void;
  onClose: () => void;
}) {
  const ref = useRef<HTMLDialogElement>(null);
  const titleId = useId();
  useEffect(() => {
    const d = ref.current;
    if (!d) return;
    if (open && !d.open) d.showModal();
    if (!open && d.open) d.close();
  }, [open]);
  return (
    <dialog
      ref={ref}
      onClose={onClose}
      aria-labelledby={titleId}
      className="m-auto w-full max-w-lg rounded-2xl border border-zinc-800 bg-zinc-900 p-6 text-zinc-100 shadow-2xl backdrop:bg-black/70"
    >
      {open && (
        <form
          onSubmit={(e) => {
            e.preventDefault();
            if (!disabled && !pending) onSubmit();
          }}
        >
          <h2 id={titleId} className="text-lg font-semibold">{title}</h2>
          <div className="mt-3 space-y-3 text-sm text-zinc-300">{children}</div>
          {error ? <div className="mt-3"><ErrorBox error={error} /></div> : null}
          <div className="mt-5 flex justify-end gap-2">
            <button type="button" className="btn" onClick={onClose}>Cancel</button>
            <button type="submit" className={`btn ${danger ? "btn-danger" : "btn-primary"}`} disabled={pending || disabled}>
              {pending && <Loader2 className="h-4 w-4 animate-spin" aria-hidden />}
              {submitLabel}
            </button>
          </div>
        </form>
      )}
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

/** Page title with an optional line under it and actions on the right. */
export function PageHeader({ title, sub, actions }: { title: string; sub?: ReactNode; actions?: ReactNode }) {
  return (
    <header className="mb-6 flex flex-wrap items-end justify-between gap-4">
      <div className="min-w-0">
        <h1 className="h-display text-[28px] leading-tight text-zinc-100">{title}</h1>
        {sub && <p className="mt-1 max-w-2xl text-sm text-zinc-400">{sub}</p>}
      </div>
      {actions && <div className="flex flex-wrap items-center gap-2">{actions}</div>}
    </header>
  );
}

/**
 * A titled block: heading and description on top, content, then an optional footer
 * (a hint on the left, the save button on the right). `danger` tints the border.
 */
export function Section({ title, desc, children, footer, danger = false, id }: {
  title: string; desc?: ReactNode; children?: ReactNode; footer?: ReactNode; danger?: boolean; id?: string;
}) {
  const hid = id ?? `s-${title.toLowerCase().replace(/[^a-z0-9]+/g, "-")}`;
  return (
    <section aria-labelledby={hid} className={`overflow-hidden rounded-2xl border bg-zinc-900 ${danger ? "border-red-900/70" : "border-zinc-800"}`}>
      <div className="p-5">
        <h2 id={hid} className={`text-[15px] font-semibold ${danger ? "text-red-300" : "text-zinc-100"}`}>{title}</h2>
        {desc && <p className="mt-1 max-w-2xl text-sm text-zinc-400">{desc}</p>}
        {children && <div className="mt-4">{children}</div>}
      </div>
      {footer && <div className="flex flex-wrap items-center justify-between gap-3 border-t border-zinc-800 bg-black/15 px-5 py-3 text-sm text-zinc-500">{footer}</div>}
    </section>
  );
}

/** Label / value list with hairline dividers. Values are monospace unless `plain`. */
export function InfoList({ rows }: { rows: { label: string; value: ReactNode; plain?: boolean }[] }) {
  return (
    <dl className="divide-y divide-zinc-800 text-sm">
      {rows.map((r) => (
        <div key={r.label} className="flex flex-wrap items-baseline justify-between gap-x-6 gap-y-0.5 py-2.5 first:pt-0 last:pb-0">
          <dt className="text-zinc-500">{r.label}</dt>
          <dd className={`min-w-0 break-all text-right text-zinc-200 ${r.plain ? "" : "font-mono text-[13px]"}`}>{r.value}</dd>
        </div>
      ))}
    </dl>
  );
}

/** Small rounded tag for roles, counts and states that have no dot. */
export function Pill({ children, tone = "neutral" }: { children: ReactNode; tone?: "neutral" | "good" | "warn" | "bad" }) {
  const c = { neutral: "bg-zinc-800 text-zinc-300", good: "bg-emerald-500/10 text-emerald-300", warn: "bg-amber-500/10 text-amber-300", bad: "bg-red-500/10 text-red-300" }[tone];
  return <span className={`inline-flex items-center rounded-full px-2 py-0.5 text-xs font-medium ${c}`}>{children}</span>;
}

/** Empty state for lists. */
export function Empty({ icon, title, children }: { icon: ReactNode; title: string; children?: ReactNode }) {
  return (
    <div className="flex flex-col items-center rounded-2xl border border-dashed border-zinc-800 px-6 py-12 text-center">
      <div className="mb-3 grid h-11 w-11 place-content-center rounded-xl bg-zinc-800 text-zinc-400">{icon}</div>
      <p className="font-medium text-zinc-200">{title}</p>
      {children && <p className="mt-1 max-w-sm text-sm text-zinc-500">{children}</p>}
    </div>
  );
}

export function Avatar({ name, size = 32 }: { name: string; size?: number }) {
  return (
    <span aria-hidden className="grid shrink-0 place-content-center rounded-full bg-zinc-800 text-xs font-semibold uppercase text-zinc-300" style={{ width: size, height: size }}>
      {name.slice(0, 1)}
    </span>
  );
}
