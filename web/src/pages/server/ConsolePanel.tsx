import { useEffect, useState, type SubmitEvent, type RefObject } from "react";
import { useMutation } from "@tanstack/react-query";
import { Check, Copy, Eraser, Expand, Loader2, Send, Shrink } from "lucide-react";
import { api, call } from "../../api/client";
import { ConsoleTerminal, type TerminalHandle } from "../../components/Terminal";
import { ErrorBox } from "../../components/ui";
import { copyText } from "../../lib/clipboard";
import { useTranslation } from "react-i18next";

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
  const { t } = useTranslation();
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

  const submit = (e: SubmitEvent) => {
    e.preventDefault();
    if (cmd.trim()) send.mutate(cmd);
  };
  const tool =
    "inline-flex h-8 w-8 items-center justify-center rounded-md text-zinc-400 hover:bg-row-hover hover:text-zinc-100 focus-visible:outline-2 focus-visible:outline-indigo-500";

  return (
    <section aria-label={t("consolePanel.console")} className="overflow-clip rounded-xl border border-line bg-well shadow-panel">
      <div className="flex items-center gap-3 border-b border-line bg-thead py-1 pl-4 pr-2">
        <h2 className="flex items-center gap-2.5 text-[13.5px] font-medium">
          <span
            className={`h-[7px] w-[7px] rounded-full ${status === "open" ? "bg-good" : status === "closed" ? "bg-bad-text" : "bg-warn"}`}
            title={
              status === "open"
                ? t("consolePanel.followingLiveOutput")
                : status === "closed"
                  ? t("consolePanel.disconnectedYouNoLongerHave")
                  : t("consolePanel.reconnecting")
            }
            aria-hidden
          />
          {t("tabs.console")}
        </h2>
        <div className="ml-auto flex items-center gap-0.5">
          <button
            type="button"
            className={tool}
            aria-label={copied ? t("common.copied") : t("consolePanel.copyLog")}
            title={t("consolePanel.copyLog")}
            onClick={() => {
              void copyText(termRef.current?.text() ?? "")
                .then(() => {
                  setCopied(true);
                  setTimeout(() => setCopied(false), 1800);
                })
                .catch(() => undefined);
            }}
          >
            {copied ? <Check className="h-4 w-4 text-good" aria-hidden /> : <Copy className="h-4 w-4" aria-hidden />}
          </button>
          <button
            type="button"
            className={tool}
            aria-label={t("consolePanel.clear")}
            title={t("consolePanel.clear")}
            onClick={() => termRef.current?.clear()}
          >
            <Eraser className="h-4 w-4" aria-hidden />
          </button>
          <button
            type="button"
            className={tool}
            aria-label={expanded ? t("consolePanel.shrink") : t("consolePanel.expand")}
            aria-pressed={expanded}
            title={expanded ? t("consolePanel.shrink") : t("consolePanel.expand")}
            onClick={() => setExpanded((v) => !v)}
          >
            {expanded ? <Shrink className="h-4 w-4" aria-hidden /> : <Expand className="h-4 w-4" aria-hidden />}
          </button>
        </div>
      </div>
      {status === "closed" && (
        <p role="status" className="border-b border-row-hover px-[18px] py-2 text-[13px] text-bad-text">
          {t("consolePanel.theConsoleWasClosedBecause")}
        </p>
      )}
      <ConsoleTerminal ref={termRef} expanded={expanded} />
      {canSend && (
        <form
          onSubmit={submit}
          aria-label={t("consolePanel.sendAConsoleCommand")}
          className="sticky bottom-0 z-10 flex items-center gap-2.5 border-t border-line bg-thead pl-4 pr-2"
        >
          <span className="font-mono text-zinc-600" aria-hidden>
            ›
          </span>
          <label htmlFor="cmd" className="sr-only">
            {t("consolePanel.consoleCommand")}
          </label>
          <input
            id="cmd"
            className="min-h-12 flex-1 bg-transparent font-mono text-sm outline-none placeholder:text-zinc-500"
            placeholder={t("consolePanel.sendACommand")}
            autoComplete="off"
            maxLength={2048}
            value={cmd}
            onChange={(e) => setCmd(e.target.value)}
          />
          <button type="submit" className="btn btn-ghost !min-h-9" disabled={send.isPending || !cmd.trim()}>
            {send.isPending ? <Loader2 className="h-4 w-4 animate-spin" aria-hidden /> : <Send className="h-4 w-4" aria-hidden />}{" "}
            {t("consolePanel.send")}
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
