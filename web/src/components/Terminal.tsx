import { useEffect, useImperativeHandle, useRef, type Ref } from "react";
import { Terminal as XTerm } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";

export interface TerminalHandle {
  writeln: (line: string) => void;
  clear: () => void;
  fit: () => void;
  text: () => string;
}

export function ConsoleTerminal({ ref, expanded = false }: { ref: Ref<TerminalHandle>; expanded?: boolean }) {
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
    text: () => {
      const x = term.current;
      if (!x) return "";
      x.selectAll();
      const s = x.getSelection();
      x.clearSelection();
      return s;
    },
  }));

  useEffect(() => {
    const t = new XTerm({
      convertEol: true,
      disableStdin: true,
      cursorBlink: false,
      scrollback: 5000,
      fontSize: 13,
      fontFamily: "\"JetBrains Mono Variable\", ui-monospace, SFMono-Regular, Menlo, Consolas, monospace",
      lineHeight: 1.5,
      theme: { background: "#090b0d", foreground: "#d5d9dd" },
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
      className={`overflow-hidden bg-well px-3 py-3 ${expanded ? "h-[75vh]" : "h-[45vh]"} min-h-64`}
    />
  );
}
