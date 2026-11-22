import { useEffect, useImperativeHandle, useRef, type Ref } from "react";
import { Terminal as XTerm } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import { useTranslation } from "react-i18next";

export interface TerminalHandle {
  writeln: (line: string) => void;
  clear: () => void;
  fit: () => void;
  text: () => string;
}

export function ConsoleTerminal({ ref, expanded = false }: { ref: Ref<TerminalHandle>; expanded?: boolean }) {
  const { t } = useTranslation();
  const host = useRef<HTMLDivElement>(null);
  const term = useRef<XTerm | null>(null);
  const fitAddon = useRef<FitAddon | null>(null);

  const fit = () => {
    const el = host.current;
    if (!el || el.offsetWidth === 0 || el.offsetHeight === 0) return; // hidden tab
    try {
      fitAddon.current?.fit();
    } catch {
      /* not ready */
    }
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
    const el = host.current;
    if (!el) return;
    const xterm = new XTerm({
      convertEol: true,
      disableStdin: true,
      cursorBlink: false,
      scrollback: 5000,
      fontSize: 13,
      fontFamily: '"Geist Mono Variable", ui-monospace, SFMono-Regular, Menlo, Consolas, monospace',
      lineHeight: 1.5,
      theme: { background: "#0d0d0c", foreground: "#dddbd4" },
    });
    const f = new FitAddon();
    xterm.loadAddon(f);
    xterm.open(el);
    term.current = xterm;
    fitAddon.current = f;
    fit();
    const ro = new ResizeObserver(fit);
    ro.observe(el);
    return () => {
      ro.disconnect();
      xterm.dispose();
      term.current = null;
      fitAddon.current = null;
    };
  }, []);

  return (
    <div
      ref={host}
      role="log"
      aria-label={t("terminal.serverConsoleOutput")}
      className={`overflow-hidden bg-console px-3 py-3 ${expanded ? "h-[75vh]" : "h-[45vh] xl:h-[max(360px,calc(100vh-380px))]"} min-h-64`}
    />
  );
}
