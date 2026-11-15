import { useEffect, useImperativeHandle, useRef, type Ref } from "react";
import { Terminal as XTerm } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";

export interface TerminalHandle {
  writeln: (line: string) => void;
  clear: () => void;
  fit: () => void;
}

export function ConsoleTerminal({ ref }: { ref: Ref<TerminalHandle> }) {
  const host = useRef<HTMLDivElement>(null);
  const term = useRef<XTerm | null>(null);
  const fitAddon = useRef<FitAddon | null>(null);

  const fit = () => {
    const el = host.current;
    if (!el || el.offsetWidth === 0 || el.offsetHeight === 0) return; // hidden tab
    try { fitAddon.current?.fit(); } catch { /* not ready */ }
  };

  useImperativeHandle(ref, () => ({
    writeln: (l) => term.current?.writeln(l),
    clear: () => term.current?.reset(),
    fit,
  }));

  useEffect(() => {
    const t = new XTerm({
      convertEol: true,
      disableStdin: true,
      cursorBlink: false,
      scrollback: 5000,
      fontSize: 13,
      fontFamily: "ui-monospace, SFMono-Regular, Menlo, Consolas, monospace",
      theme: { background: "#09090b", foreground: "#e4e4e7" },
    });
    const f = new FitAddon();
    t.loadAddon(f);
    t.open(host.current!);
    term.current = t;
    fitAddon.current = f;
    fit();
    const ro = new ResizeObserver(fit);
    ro.observe(host.current!);
    return () => {
      ro.disconnect();
      t.dispose();
      term.current = null;
      fitAddon.current = null;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  return (
    <div
      ref={host}
      role="log"
      aria-label="Server console output"
      className="h-[55vh] min-h-64 overflow-hidden rounded-md border border-zinc-800 bg-zinc-950 p-2"
    />
  );
}
