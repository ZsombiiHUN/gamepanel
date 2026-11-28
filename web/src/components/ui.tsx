import { useEffect, useId, useMemo, useRef, useState, type ReactNode } from "react";
import { AlertCircle, Check, Copy, Loader2 } from "lucide-react";
import { blockIcon } from "../lib/icon";
import { errorText } from "../lib/problem";
import { stateLabel } from "../lib/servers";
import { copyText } from "../lib/clipboard";
import { useTranslation } from "react-i18next";

export function Spinner({ label }: { label?: string }) {
  const { t } = useTranslation();
  return (
    <div role="status" className="flex items-center gap-2 text-sm text-zinc-400">
      <Loader2 className="h-4 w-4 animate-spin" aria-hidden /> {label ?? t("ui.loading")}...
    </div>
  );
}

export function ErrorBox({ error }: { error: unknown }) {
  if (!error) return null;
  const msg = errorText(error);
  return (
    <div role="alert" className="flex items-start gap-2 rounded-lg border border-bad/30 bg-bad-bg px-3 py-2.5 text-[13px] text-bad-text">
      <AlertCircle className="mt-0.5 h-4 w-4 shrink-0" aria-hidden />
      <span className="break-words">{msg}</span>
    </div>
  );
}

type Tone = "ok" | "warn" | "off" | "bad";
const stateTone: Record<string, Tone> = {
  running: "ok",
  starting: "warn",
  stopping: "warn",
  installing: "warn",
  install_failed: "bad",
  crashed: "bad",
  offline: "off",
  unknown: "off",
  suspended: "bad",
};
const toneCls: Record<Tone, string> = {
  ok: "border-good/25 bg-good-bg text-good-text",
  warn: "border-warn/25 bg-warn-bg text-warn-text",
  off: "border-line-strong bg-zinc-900 text-zinc-400",
  bad: "border-bad/30 bg-bad-bg text-bad-text",
};

/** Status mark. Shape carries the meaning as well as colour: filled dot, square, ring, triangle. */
export function StateMark({ state }: { state: string }) {
  const tone = stateTone[state] ?? "off";
  if (tone === "ok")
    return (
      <span className="inline-block h-[7px] w-[7px] shrink-0 rounded-full bg-good shadow-[0_0_0_3px_rgb(92_201_138/0.18)]" aria-hidden />
    );
  if (tone === "warn") return <span className="inline-block h-[7px] w-[7px] shrink-0 rounded-[2px] bg-warn" aria-hidden />;
  if (tone === "bad")
    return (
      <span className="inline-block h-0 w-0 shrink-0 border-x-[4.5px] border-b-[8px] border-x-transparent border-b-bad-text" aria-hidden />
    );
  return <span className="inline-block h-[7px] w-[7px] shrink-0 rounded-full border-[1.5px] border-zinc-500" aria-hidden />;
}

export function StateBadge({ state }: { state: string }) {
  const tone = stateTone[state] ?? "off";
  return (
    <span
      className={`inline-flex h-[22px] shrink-0 items-center gap-1.5 whitespace-nowrap rounded-full border px-2 text-xs font-medium ${toneCls[tone]}`}
    >
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
      style={{
        width: size,
        height: size,
        padding: size * 0.14,
        borderRadius: size * 0.24,
        boxSizing: "border-box",
        background: `hsl(${hue} 22% 13%)`,
        boxShadow: `inset 0 0 0 1px hsl(${hue} 40% 70% / .14)`,
      }}
    >
      <svg viewBox="0 0 38 38" width="100%" height="100%">
        {blocks.map((b) => (
          <rect key={b.key} x={b.x} y={b.y} width={6} height={6} rx={1.2} fill={b.fill} />
        ))}
      </svg>
    </span>
  );
}

export function OnlineDot({ online, label }: { online: boolean; label: string }) {
  const { t } = useTranslation();
  return (
    <span className="inline-flex items-center gap-1.5 text-xs text-zinc-300">
      <span className={`h-2 w-2 rounded-full ${online ? "bg-good" : "bg-bad-text"}`} aria-hidden />
      {label}: {online ? t("componentsUi.online") : t("componentsUi.offline")}
    </span>
  );
}

export function Field({ label, children, hint }: { label: string; children: (id: string) => ReactNode; hint?: string }) {
  const id = useId();
  return (
    <div>
      <label htmlFor={id} className="label">
        {label}
      </label>
      {children(id)}
      {hint && <p className="mt-1 text-xs text-zinc-500">{hint}</p>}
    </div>
  );
}

export function CopyButton({ text, label }: { text: string; label?: string }) {
  const { t } = useTranslation();
  label ??= t("common.copy");
  const [done, setDone] = useState(false);
  const [err, setErr] = useState(false);
  return (
    <button
      type="button"
      className="btn"
      onClick={() => {
        void (async () => {
          try {
            await copyText(text);
            setDone(true);
            setErr(false);
            setTimeout(() => setDone(false), 2000);
          } catch {
            setErr(true);
          }
        })();
      }}
    >
      {done ? <Check className="h-4 w-4" aria-hidden /> : <Copy className="h-4 w-4" aria-hidden />}
      {done ? t("common.copied") : err ? t("componentsUi.copyFailedSelectManually") : label}
    </button>
  );
}

/**
 * Opens and closes a <dialog> as a modal to follow `open`. The element gives
 * focus trapping, Esc and the backdrop for free.
 */
export function useModalDialog(open: boolean) {
  const ref = useRef<HTMLDialogElement>(null);
  useEffect(() => {
    const d = ref.current;
    if (!d) return;
    if (open && !d.open) d.showModal();
    if (!open && d.open) d.close();
  }, [open]);
  return ref;
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
  const { t } = useTranslation();
  const ref = useModalDialog(open);
  const titleId = useId();
  return (
    <dialog
      ref={ref}
      onClose={onCancel}
      aria-labelledby={titleId}
      className="m-auto w-full max-w-md rounded-xl border border-line-strong bg-zinc-900 p-6 text-zinc-100 shadow-2xl backdrop:bg-black/60 backdrop:backdrop-blur-[2px]"
    >
      <h2 id={titleId} className="text-lg font-semibold">
        {title}
      </h2>
      <div className="mt-2 text-sm text-zinc-300">{body}</div>
      {error ? (
        <div className="mt-3">
          <ErrorBox error={error} />
        </div>
      ) : null}
      <div className="mt-5 flex justify-end gap-2">
        <button type="button" className="btn" onClick={onCancel} autoFocus>
          {t("common.cancel")}
        </button>
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
  open,
  title,
  children,
  submitLabel,
  pending,
  error,
  disabled,
  danger,
  onSubmit,
  onClose,
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
  const { t } = useTranslation();
  const ref = useModalDialog(open);
  const titleId = useId();
  return (
    <dialog
      ref={ref}
      onClose={onClose}
      aria-labelledby={titleId}
      className="m-auto w-full max-w-lg rounded-xl border border-line-strong bg-zinc-900 p-6 text-zinc-100 shadow-2xl backdrop:bg-black/60 backdrop:backdrop-blur-[2px]"
    >
      {open && (
        <form
          onSubmit={(e) => {
            e.preventDefault();
            if (!disabled && !pending) onSubmit();
          }}
        >
          <h2 id={titleId} className="text-lg font-semibold">
            {title}
          </h2>
          <div className="mt-3 space-y-3 text-sm text-zinc-300">{children}</div>
          {error ? (
            <div className="mt-3">
              <ErrorBox error={error} />
            </div>
          ) : null}
          <div className="mt-5 flex justify-end gap-2">
            <button type="button" className="btn" onClick={onClose}>
              {t("common.cancel")}
            </button>
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
      {/* A value too long for the line wraps under the label instead of being cut off. */}
      <div className="flex flex-wrap justify-between gap-x-2 text-[12.5px]">
        <span className="text-zinc-500">{label}</span>
        <span className={`ml-auto max-w-full truncate font-mono text-xs ${warn ? "text-warn-text" : "text-zinc-200"}`}>{text}</span>
      </div>
      <div className="h-1.5 overflow-hidden rounded-[3px] bg-line" role="presentation">
        <div
          className={`h-full rounded-[3px] bg-gradient-to-r ${warn ? "from-[#c98a30] to-warn" : "from-zinc-300 to-zinc-100"}`}
          style={{ width: `${w}%` }}
        />
      </div>
    </div>
  );
}

/** Page title with an optional line under it and actions on the right. */
export function PageHeader({ title, sub, actions }: { title: string; sub?: ReactNode; actions?: ReactNode }) {
  return (
    <header className="mb-6 flex flex-wrap items-end justify-between gap-4">
      <div className="min-w-0">
        <h1 className="h-display text-[22px] leading-tight text-zinc-100">{title}</h1>
        {sub && <p className="mt-1 max-w-2xl text-[13.5px] text-zinc-400">{sub}</p>}
      </div>
      {actions && <div className="flex flex-wrap items-center gap-2">{actions}</div>}
    </header>
  );
}

/**
 * A titled block: heading and description on top, content, then an optional footer
 * (a hint on the left, the save button on the right). `danger` tints the border.
 */
export function Section({
  title,
  desc,
  children,
  footer,
  danger = false,
  id,
}: {
  title: string;
  desc?: ReactNode;
  children?: ReactNode;
  footer?: ReactNode;
  danger?: boolean;
  id?: string;
}) {
  const hid = id ?? `s-${title.toLowerCase().replace(/[^a-z0-9]+/g, "-")}`;
  return (
    <section
      aria-labelledby={hid}
      className={`scroll-mt-20 overflow-hidden rounded-xl border bg-zinc-900 ${danger ? "border-bad/30" : "border-line"}`}
    >
      <div className="p-5">
        <h2 id={hid} className={`text-[15px] font-semibold tracking-tight ${danger ? "text-bad-text" : "text-zinc-100"}`}>
          {title}
        </h2>
        {desc && <p className="mt-1 max-w-2xl text-[13.5px] text-zinc-400">{desc}</p>}
        {children && <div className="mt-4">{children}</div>}
      </div>
      {footer && (
        <div className="flex flex-wrap items-center justify-between gap-3 border-t border-line bg-thead px-5 py-2.5 text-[13px] text-zinc-500">
          {footer}
        </div>
      )}
    </section>
  );
}

/** Label / value list with hairline dividers. Values are monospace unless `plain`. */
export function InfoList({ rows }: { rows: { label: string; value: ReactNode; plain?: boolean }[] }) {
  return (
    <dl className="divide-y divide-line text-[13px]">
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
  const c = {
    neutral: "border-line-strong bg-zinc-900 text-zinc-300",
    good: "border-good/25 bg-good-bg text-good-text",
    warn: "border-warn/25 bg-warn-bg text-warn-text",
    bad: "border-bad/30 bg-bad-bg text-bad-text",
  }[tone];
  return (
    <span className={`inline-flex h-[22px] shrink-0 items-center gap-1.5 rounded-full border px-2 text-xs font-medium ${c}`}>
      {children}
    </span>
  );
}

/** Empty state for lists. */
export function Empty({ icon, title, children }: { icon: ReactNode; title: string; children?: ReactNode }) {
  return (
    <div className="flex flex-col items-center rounded-xl border border-dashed border-line-strong px-6 py-12 text-center">
      <div className="mb-3 grid h-10 w-10 place-content-center rounded-[10px] border border-line-strong bg-zinc-900 text-zinc-400 shadow-btn">
        {icon}
      </div>
      <p className="font-medium text-zinc-200">{title}</p>
      {children && <p className="mt-1 max-w-sm text-sm text-zinc-500">{children}</p>}
    </div>
  );
}

export function Avatar({ name, size = 32 }: { name: string; size?: number }) {
  return (
    <span
      aria-hidden
      className="grid shrink-0 place-content-center rounded-[7px] bg-gradient-to-br from-zinc-600 to-zinc-800 text-xs font-semibold uppercase text-zinc-100"
      style={{ width: size, height: size }}
    >
      {name.slice(0, 1)}
    </span>
  );
}

/**
 * A column of <Section>s with an "On this page" list beside it on wide screens. The list is read from
 * the sections themselves, so it follows whatever the page shows (sections can depend on permissions).
 */
export function SectionsWithNav({ children, label }: { children: ReactNode; label?: string }) {
  const { t } = useTranslation();
  label ??= t("ui.onThisPage");
  const ref = useRef<HTMLDivElement>(null);
  const [items, setItems] = useState<{ id: string; title: string }[]>([]);
  const [active, setActive] = useState<string | null>(null);
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const read = () => {
      // Only the outermost sections; a section inside another one is part of it.
      const secs = [...el.querySelectorAll<HTMLElement>("section[aria-labelledby]")].filter((s) => {
        const outer = s.parentElement?.closest("section");
        return !outer || !el.contains(outer);
      });
      const next = secs.flatMap((s) => {
        const id = s.getAttribute("aria-labelledby") ?? "";
        const title = document.getElementById(id)?.textContent.trim() ?? "";
        return id && title ? [{ id, title }] : [];
      });
      setItems((prev) => (JSON.stringify(prev) === JSON.stringify(next) ? prev : next));
    };
    read();
    const mo = new MutationObserver(read);
    mo.observe(el, { childList: true, subtree: true, characterData: true });
    return () => mo.disconnect();
  }, []);
  useEffect(() => {
    if (typeof IntersectionObserver === "undefined" || items.length === 0) return;
    const io = new IntersectionObserver(
      (entries) => {
        const top = entries.filter((e) => e.isIntersecting).sort((a, b) => a.boundingClientRect.top - b.boundingClientRect.top)[0];
        if (top) setActive(top.target.getAttribute("aria-labelledby"));
      },
      { rootMargin: "-80px 0px -55% 0px" },
    );
    for (const it of items) {
      const sec = document.getElementById(it.id)?.closest("section");
      if (sec) io.observe(sec);
    }
    return () => io.disconnect();
  }, [items]);
  const showNav = items.length >= 3;
  return (
    <div className={showNav ? "xl:grid xl:grid-cols-[176px_minmax(0,1fr)] xl:gap-8" : undefined}>
      {showNav && (
        <nav aria-label={label} className="hidden xl:block">
          <ul className="sticky top-[76px] space-y-0.5">
            {items.map((it) => (
              <li key={it.id}>
                <a
                  href={`#${it.id}`}
                  aria-current={active === it.id ? "location" : undefined}
                  onClick={(e) => {
                    e.preventDefault();
                    document.getElementById(it.id)?.closest("section")?.scrollIntoView({ behavior: "smooth", block: "start" });
                    setActive(it.id);
                  }}
                  className="flex min-h-8 items-center rounded-md px-2.5 text-[13px] text-zinc-400 hover:bg-row-hover hover:text-zinc-100 focus-visible:outline-2 focus-visible:outline-indigo-500 aria-[current=location]:bg-row-active aria-[current=location]:font-medium aria-[current=location]:text-zinc-100"
                >
                  {it.title}
                </a>
              </li>
            ))}
          </ul>
        </nav>
      )}
      <div ref={ref} className="min-w-0 max-w-[880px] space-y-5">
        {children}
      </div>
    </div>
  );
}

/** A labelled figure in a small card; tiles sit in a row (see StatRow). */
export function Stat({
  label,
  value,
  unit,
  hint,
  children,
  tone,
}: {
  label: string;
  value: ReactNode;
  unit?: ReactNode;
  hint?: ReactNode;
  children?: ReactNode;
  tone?: "warn" | "bad";
}) {
  const toneClass = tone === "warn" ? "text-warn-text" : tone === "bad" ? "text-bad-text" : "";
  return (
    <div className="min-w-0 rounded-xl border border-line bg-zinc-900 px-4 py-3.5">
      <dt className="text-[13px] text-zinc-400">{label}</dt>
      <dd className="mt-1.5">
        <span className={`text-[22px] font-semibold leading-none tracking-tight ${toneClass}`}>{value}</span>
        {unit && <span className="ml-1.5 text-[13px] text-zinc-500">{unit}</span>}
        {children && <div className="mt-3">{children}</div>}
        {hint && <p className={`mt-2 truncate text-[12.5px] ${toneClass || "text-zinc-500"}`}>{hint}</p>}
      </dd>
    </div>
  );
}

export function StatRow({ children, label }: { children: ReactNode; label: string }) {
  return (
    <dl aria-label={label} className="grid gap-3 [grid-template-columns:repeat(auto-fit,minmax(min(100%,200px),1fr))]">
      {children}
    </dl>
  );
}
