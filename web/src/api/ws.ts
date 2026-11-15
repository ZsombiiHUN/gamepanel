import { useEffect, useEffectEvent, useState } from "react";

export type WsStatus = "connecting" | "open" | "reconnecting";
export interface LiveStats { cpu_pct: number; mem_bytes: number; disk_bytes: number }

interface Handlers {
  onConsole: (line: string) => void;
  onReset: () => void;
}

/** Live websocket for one server with exponential-backoff reconnect. */
export function useServerSocket(uuid: string, handlers: Handlers) {
  const [status, setStatus] = useState<WsStatus>("connecting");
  // Readings belong to one server; a reading kept from the previous page is never shown for this one.
  const [live, setLive] = useState<{ uuid: string; state: string | null; stats: LiveStats | null }>({ uuid, state: null, stats: null });
  const update = (patch: { state?: string; stats?: LiveStats }) =>
    setLive((l) => ({ ...(l.uuid === uuid ? l : { state: null, stats: null }), uuid, ...patch }));
  const onConsole = useEffectEvent(handlers.onConsole);
  const onReset = useEffectEvent(handlers.onReset);
  const onLive = useEffectEvent(update);

  useEffect(() => {
    let ws: WebSocket | null = null;
    let timer: ReturnType<typeof setTimeout> | undefined;
    let attempt = 0;
    let stopped = false;

    const connect = () => {
      const proto = location.protocol === "https:" ? "wss" : "ws";
      ws = new WebSocket(`${proto}://${location.host}/api/servers/${uuid}/ws`);
      ws.onopen = () => {
        attempt = 0;
        setStatus("open");
        onReset(); // server may replay backlog; avoid duplicates
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
        if (frame.type === "console" && typeof d.line === "string") onConsole(d.line);
        else if (frame.type === "state" && typeof d.state === "string") onLive({ state: d.state });
        else if (frame.type === "stats" && typeof d.cpu_pct === "number" && typeof d.mem_bytes === "number")
          onLive({ stats: { cpu_pct: d.cpu_pct, mem_bytes: d.mem_bytes, disk_bytes: typeof d.disk_bytes === "number" ? d.disk_bytes : 0 } });
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

  const own = live.uuid === uuid ? live : null;
  return { status, state: own?.state ?? null, stats: own?.stats ?? null };
}
