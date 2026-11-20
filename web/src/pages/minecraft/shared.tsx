import { useMutation, useQuery, type QueryClient } from "@tanstack/react-query";
import { api, call } from "../../api/client";
import { invalidates, keys, refreshLater, serverFileQuery } from "../../api/queries";
import { motdLines } from "../../lib/minecraft";

/** One file of the server, as text; a file that does not exist reads as null. */
export const useServerFile = (uuid: string, path: string, enabled = true) => useQuery(serverFileQuery(uuid, path, enabled));

/** Writes a text file and refreshes everything that shows the server's files. */
export function useWriteFile(uuid: string) {
  return useMutation({
    mutationFn: ({ path, content }: { path: string; content: string }) =>
      call(api.PUT("/servers/{uuid}/files/content", { params: { path: { uuid } }, body: { path, content } })),
    meta: invalidates(keys.files(uuid)),
  });
}

export function sendCommand(uuid: string, command: string) {
  return call(api.POST("/servers/{uuid}/command", { params: { path: { uuid } }, body: { command } }));
}

/** The server rewrites its files a moment after a command; read them again then. */
export function refreshSoon(qc: QueryClient, uuid: string, ms = 1500) {
  refreshLater(qc, keys.files(uuid), ms);
}

/** A MOTD as the game draws it: § colours and styles on a dark background. */
export function Motd({ text, className = "" }: { text: string; className?: string }) {
  return (
    <div className={`font-mono text-[13px] leading-5 text-[#AAAAAA] ${className}`}>
      {motdLines(text).map((line, i) => (
        <div key={i} className="min-h-5 whitespace-pre-wrap break-words">
          {line.map((s, j) => (
            <span
              key={j}
              style={{
                color: s.color,
                fontWeight: s.bold ? 700 : undefined,
                fontStyle: s.italic ? "italic" : undefined,
                textDecoration: [s.underline && "underline", s.strike && "line-through"].filter(Boolean).join(" ") || undefined,
              }}
              className={s.obfuscated ? "blur-[2px]" : undefined}
            >
              {s.text}
            </span>
          ))}
        </div>
      ))}
    </div>
  );
}

export type Can = (need?: string) => boolean;
