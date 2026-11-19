import { useEffect, useId, useRef, useState, type ReactNode } from "react";
import { MoreHorizontal } from "lucide-react";

export interface MenuItem {
  label: string;
  icon: ReactNode;
  onSelect?: () => void;
  /** Rendered as a link (downloads). */
  href?: string;
  danger?: boolean;
}

/** A small action menu: opens below its button, closes on choice, Esc or a click elsewhere. Arrow keys move. */
export function RowMenu({ label, items }: { label: string; items: MenuItem[] }) {
  const [open, setOpen] = useState(false);
  // Fixed to the viewport, so a scrolling table cannot clip it; flips up near the bottom.
  const [pos, setPos] = useState<{ top?: number; bottom?: number; right: number }>({ right: 0 });
  const box = useRef<HTMLDivElement>(null);
  const button = useRef<HTMLButtonElement>(null);
  const id = useId();

  useEffect(() => {
    if (!open) return;
    const onDown = (e: MouseEvent) => {
      if (!box.current?.contains(e.target as Node)) setOpen(false);
    };
    // A fixed menu would drift away from its row: close it when anything scrolls.
    const onScroll = () => setOpen(false);
    document.addEventListener("mousedown", onDown);
    window.addEventListener("scroll", onScroll, true);
    window.addEventListener("resize", onScroll);
    box.current?.querySelector<HTMLElement>("[role=menuitem]")?.focus({ preventScroll: true });
    return () => {
      document.removeEventListener("mousedown", onDown);
      window.removeEventListener("scroll", onScroll, true);
      window.removeEventListener("resize", onScroll);
    };
  }, [open]);

  const close = () => {
    setOpen(false);
    button.current?.focus();
  };
  const onKey = (e: React.KeyboardEvent) => {
    const all = [...(box.current?.querySelectorAll<HTMLElement>("[role=menuitem]") ?? [])];
    const i = all.indexOf(document.activeElement as HTMLElement);
    if (e.key === "Escape") {
      e.preventDefault();
      e.stopPropagation(); // do not also clear the file search
      close();
    } else if (e.key === "ArrowDown") {
      e.preventDefault();
      all[(i + 1) % all.length]?.focus();
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      all[(i - 1 + all.length) % all.length]?.focus();
    }
  };
  const cls = (danger?: boolean) => `menu-item w-full ${danger ? "!text-bad-text" : ""}`;

  return (
    // Arrow keys and Escape bubble up from the button and the menu items.
    // eslint-disable-next-line jsx-a11y/no-static-element-interactions -- see above
    <div ref={box} className="relative inline-block" onKeyDown={onKey}>
      <button
        ref={button}
        type="button"
        className="inline-flex h-8 w-8 items-center justify-center rounded-md text-zinc-400 hover:bg-row-hover hover:text-zinc-100 focus-visible:outline-2 focus-visible:outline-indigo-500"
        aria-label={label}
        aria-haspopup="menu"
        aria-expanded={open}
        aria-controls={open ? id : undefined}
        onClick={() => {
          const r = button.current?.getBoundingClientRect();
          if (!r) return;
          const up = window.innerHeight - r.bottom < 320 && r.top > window.innerHeight - r.bottom;
          setPos(
            up
              ? { bottom: window.innerHeight - r.top + 4, right: window.innerWidth - r.right }
              : { top: r.bottom + 4, right: window.innerWidth - r.right },
          );
          setOpen(!open);
        }}
      >
        <MoreHorizontal className="h-4 w-4" aria-hidden />
      </button>
      {open && (
        <div
          id={id}
          role="menu"
          aria-label={label}
          style={pos}
          className="fixed z-40 flex min-w-44 flex-col rounded-xl border border-zinc-700 bg-zinc-900 p-1 text-left shadow-xl"
        >
          {items.map((it) =>
            it.href ? (
              <a key={it.label} role="menuitem" tabIndex={-1} className={cls(it.danger)} href={it.href} download onClick={close}>
                {it.icon} {it.label}
              </a>
            ) : (
              <button
                key={it.label}
                type="button"
                role="menuitem"
                tabIndex={-1}
                className={cls(it.danger)}
                onClick={() => {
                  setOpen(false);
                  it.onSelect?.();
                }}
              >
                {it.icon} {it.label}
              </button>
            ),
          )}
        </div>
      )}
    </div>
  );
}
