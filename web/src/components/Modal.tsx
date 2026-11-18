import { useEffect, useId, useRef, type ReactNode } from "react";
import { X } from "lucide-react";

/** A modal with a title and a close button, for content that is not a form. Esc closes it. */
export function Modal({ open, title, children, onClose, wide = false }: { open: boolean; title: string; children: ReactNode; onClose: () => void; wide?: boolean }) {
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
      className={`m-auto w-full ${wide ? "max-w-2xl" : "max-w-lg"} rounded-2xl border border-zinc-800 bg-zinc-900 p-6 text-zinc-100 shadow-2xl backdrop:bg-black/70`}
    >
      {open && (
        <>
          <div className="flex items-start justify-between gap-4">
            <h2 id={titleId} className="text-lg font-semibold">{title}</h2>
            <button type="button" className="btn btn-ghost -mr-2 -mt-1 !px-2" aria-label="Close" onClick={onClose}><X className="h-4 w-4" aria-hidden /></button>
          </div>
          <div className="mt-3">{children}</div>
        </>
      )}
    </dialog>
  );
}
