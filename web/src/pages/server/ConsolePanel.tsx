import { useEffect, useState, type FormEvent, type RefObject } from "react";
import { useMutation } from "@tanstack/react-query";
import { Check, Copy, Eraser, Expand, Loader2, Send, Shrink } from "lucide-react";
import { api, call } from "../../api/client";
import { ConsoleTerminal, type TerminalHandle } from "../../components/Terminal";
import { ErrorBox } from "../../components/ui";

export function ConsolePanel({
  uuid,
  termRef,
  active,
  canSend,
  status,
}: {
  uuid: string;
  termRef: RefObject<TerminalHandle | null>;
  active: boolean;
  canSend: boolean;
  status: string;
}) {
  const [cmd, setCmd] = useState("");
  const [expanded, setExpanded] = useState(false);
  const [copied, setCopied] = useState(false);
  const send = useMutation({
    mutationFn: (command: string) => call(api.POST("/servers/{uuid}/command", { params: { path: { uuid } }, body: { command } })),
    onSuccess: () => setCmd(""),
  });
  // Re-fit when the tab becomes visible or the panel resizes.
  useEffect(() => {
    if (active) termRef.current?.fit();
  }, [active, expanded, termRef]);

  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (cmd.trim()) send.mutate(cmd);
  };
  const tool =
    "inline-flex h-8 w-8 items-center justify-center rounded-md text-zinc-400 hover:bg-zinc-800 hover:text-zinc-100 focus-visible:outline-2 focus-visible:outline-indigo-500";

  return (
    <section aria-label="Console" className="overflow-clip rounded-2xl border border-zinc-800 bg-well">
      <div className="flex items-center gap-3 border-b border-row-hover py-1.5 pl-[18px] pr-2.5">
        <h2 className="flex items-center gap-2.5 text-[15px] font-semibold">
          <span
            className={`h-[7px] w-[7px] rounded-full ${status === "open" ? "bg-good" : status === "closed" ? "bg-bad-text" : "bg-warn"}`}
            title={
              status === "open"
                ? "Following live output"
                : status === "closed"
                  ? "Disconnected: you no longer have access to this console"
                  : "Reconnecting"
            }
            aria-hidden
          />
          Console
        </h2>
        <div className="ml-auto flex items-center gap-0.5">
          <button
            type="button"
            className={tool}
            aria-label={copied ? "Copied" : "Copy log"}
            title="Copy log"
            onClick={() => {
              void navigator.clipboard
                ?.writeText(termRef.current?.text() ?? "")
                .then(() => {
                  setCopied(true);
                  setTimeout(() => setCopied(false), 1800);
                })
                .catch(() => undefined);
            }}
          >
            {copied ? <Check className="h-4 w-4 text-good" aria-hidden /> : <Copy className="h-4 w-4" aria-hidden />}
          </button>
          <button type="button" className={tool} aria-label="Clear" title="Clear" onClick={() => termRef.current?.clear()}>
            <Eraser className="h-4 w-4" aria-hidden />
          </button>
          <button
            type="button"
            className={tool}
            aria-label={expanded ? "Shrink" : "Expand"}
            aria-pressed={expanded}
            title={expanded ? "Shrink" : "Expand"}
            onClick={() => setExpanded((v) => !v)}
          >
            {expanded ? <Shrink className="h-4 w-4" aria-hidden /> : <Expand className="h-4 w-4" aria-hidden />}
          </button>
        </div>
      </div>
      {status === "closed" && (
        <p role="status" className="border-b border-row-hover px-[18px] py-2 text-[13px] text-bad-text">
          The console was closed because you no longer have access to it.
        </p>
      )}
      <ConsoleTerminal ref={termRef} expanded={expanded} />
      {canSend && (
        <form
          onSubmit={submit}
          aria-label="Send a console command"
          className="sticky bottom-0 z-10 flex items-center gap-2.5 border-t border-row-hover bg-thead pl-[18px] pr-2.5"
        >
          <span className="font-mono text-zinc-600" aria-hidden>
            ›
          </span>
          <label htmlFor="cmd" className="sr-only">
            Console command
          </label>
          <input
            id="cmd"
            className="min-h-12 flex-1 bg-transparent font-mono text-sm outline-none placeholder:text-zinc-500"
            placeholder="Send a command"
            autoComplete="off"
            maxLength={2048}
            value={cmd}
            onChange={(e) => setCmd(e.target.value)}
          />
          <button type="submit" className="btn btn-ghost !min-h-9" disabled={send.isPending || !cmd.trim()}>
            {send.isPending ? <Loader2 className="h-4 w-4 animate-spin" aria-hidden /> : <Send className="h-4 w-4" aria-hidden />} Send
          </button>
        </form>
      )}
      {send.error ? (
        <div className="p-3">
          <ErrorBox error={send.error} />
        </div>
      ) : null}
    </section>
  );
}
