import { useEffect, useRef, useState } from "react";

export type WsStatus = "connecting" | "open" | "reconnecting";
export interface LiveStats { cpu_pct: number; mem_bytes: number }

interface Handlers {
  onConsole: (line: string) => void;
  onReset: () => void;
}

/** Live websocket for one server with exponential-backoff reconnect. */
export function useServerSocket(uuid: string, handlers: Handlers) {
  const [status, setStatus] = useState<WsStatus>("connecting");
  const [state, setState] = useState<string | null>(null);
  const [stats, setStats] = useState<LiveStats | null>(null);
  const hRef = useRef(handlers);
  hRef.current = handlers;

  useEffect(() => {
    let ws: WebSocket | null = null;
    let timer: ReturnType<typeof setTimeout> | undefined;
    let attempt = 0;
    let stopped = false;
    setState(null);
    setStats(null);

    const connect = () => {
      const proto = location.protocol === "https:" ? "wss" : "ws";
      ws = new WebSocket(`${proto}://${location.host}/api/servers/${uuid}/ws`);
      ws.onopen = () => {
        attempt = 0;
        setStatus("open");
        hRef.current.onReset(); // server may replay backlog; avoid duplicates
      };
      ws.onmessage = (ev) => {
        if (typeof ev.data !== "string") return;
        let frame: { type?: string; data?: unknown };
        try {
          frame = JSON.parse(ev.data);
        } catch {
          return;
        }
        let data = frame.data;
        if (typeof data === "string") {
          try { data = JSON.parse(data); } catch { /* keep string */ }
        }
        if (!data || typeof data !== "object") return;
        const d = data as Record<string, unknown>;
        if (frame.type === "console" && typeof d.line === "string") hRef.current.onConsole(d.line);
        else if (frame.type === "state" && typeof d.state === "string") setState(d.state);
        else if (frame.type === "stats" && typeof d.cpu_pct === "number" && typeof d.mem_bytes === "number")
          setStats({ cpu_pct: d.cpu_pct, mem_bytes: d.mem_bytes });
      };
      ws.onclose = () => {
        if (stopped) return;
        setStatus("reconnecting");
        const delay = Math.min(15_000, 500 * 2 ** attempt++);
        timer = setTimeout(connect, delay);
      };
      ws.onerror = () => ws?.close();
    };
    connect();

    return () => {
      stopped = true;
      clearTimeout(timer);
      if (ws) {
        ws.onclose = null;
        ws.close();
      }
    };
  }, [uuid]);

  return { status, state, stats };
}
