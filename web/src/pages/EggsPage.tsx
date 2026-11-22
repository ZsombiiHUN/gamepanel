import { useState, type ChangeEvent, type SubmitEvent } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { Egg, Loader2, Plus, Upload, X } from "lucide-react";
import { api, call } from "../api/client";
import { eggsQuery, invalidates, keys } from "../api/queries";
import { Empty, ErrorBox, Field, PageHeader, Pill, Section, ServerIcon, Spinner } from "../components/ui";
import { useTranslation } from "react-i18next";

export function EggsPage() {
  const { t } = useTranslation();
  const eggs = useQuery(eggsQuery);
  const [text, setText] = useState("");
  const [open, setOpen] = useState(false);
  const [parseError, setParseError] = useState<string | null>(null);

  const imp = useMutation({
    mutationFn: (body: Record<string, unknown>) =>
      call(
        // Spec types the JSON body as a loose object; the generated union is awkward.
        api.POST("/eggs/import", { body: body as never }),
      ),
    meta: invalidates(keys.eggs),
    onSuccess: () => {
      setText("");
      setOpen(false);
    },
  });

  const onFile = async (e: ChangeEvent<HTMLInputElement>) => {
    const f = e.target.files?.[0];
    if (f) setText(await f.text());
    e.target.value = "";
  };

  const submit = (e: SubmitEvent) => {
    e.preventDefault();
    setParseError(null);
    imp.reset();
    try {
      const parsed: unknown = JSON.parse(text);
      if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) throw new Error(t("eggsPage.notObject"));
      imp.mutate(parsed as Record<string, unknown>);
    } catch {
      setParseError(t("eggsPage.invalidJson"));
    }
  };

  const showImport = open || eggs.data?.length === 0;

  return (
    <div>
      <PageHeader
        title={t("eggsPage.eggs")}
        sub={t("eggsPage.templatesThatSayHowTo")}
        actions={
          eggs.data?.length ? (
            <button type="button" className="btn btn-primary" aria-expanded={open} onClick={() => setOpen(!open)}>
              {open ? <X className="h-4 w-4" aria-hidden /> : <Plus className="h-4 w-4" aria-hidden />}{" "}
              {open ? t("common.cancel") : t("eggsPage.importEgg")}
            </button>
          ) : null
        }
      />

      <div className="space-y-5">
        {imp.isSuccess && !showImport && (
          <p role="status" className="rounded-xl border border-good/25 bg-good-bg px-4 py-2.5 text-sm text-good-text">
            {t("eggsPage.imported", { name: imp.data.name })}
          </p>
        )}
        {showImport && (
          <form onSubmit={submit}>
            <Section
              title={t("eggsPage.importAnEgg")}
              desc={t("eggsPage.chooseAnEggSJson")}
              footer={
                <>
                  <span>{t("eggsPage.installScriptsAreImportedToo")}</span>
                  <button type="submit" className="btn btn-primary" disabled={imp.isPending || !text.trim()}>
                    {imp.isPending ? <Loader2 className="h-4 w-4 animate-spin" aria-hidden /> : <Upload className="h-4 w-4" aria-hidden />}{" "}
                    {t("eggsPage.import")}
                  </button>
                </>
              }
            >
              <div className="space-y-4">
                <Field label={t("eggsPage.eggFileJson")}>
                  {(id) => (
                    <input
                      id={id}
                      type="file"
                      accept="application/json,.json"
                      onChange={(e) => void onFile(e)}
                      className="input file:mr-3 file:rounded-md file:border-0 file:bg-zinc-800 file:px-2.5 file:py-1 file:text-zinc-100"
                    />
                  )}
                </Field>
                <Field label={t("eggsPage.orPasteEggJson")}>
                  {(id) => (
                    <textarea
                      id={id}
                      className="input h-40 font-mono text-[13px]"
                      spellCheck={false}
                      value={text}
                      onChange={(e) => setText(e.target.value)}
                      required
                    />
                  )}
                </Field>
                <ErrorBox error={parseError} />
                <ErrorBox error={imp.error} />
                {imp.isSuccess && (
                  <p role="status" className="text-sm text-good-text">
                    {t("eggsPage.imported", { name: imp.data.name })}
                  </p>
                )}
              </div>
            </Section>
          </form>
        )}

        {eggs.isPending && <Spinner />}
        <ErrorBox error={eggs.error} />
        {eggs.data?.length === 0 && (
          <Empty icon={<Egg className="h-5 w-5" aria-hidden />} title={t("eggsPage.noEggsImportedYet")}>
            {t("eggsPage.importOneAboveToBe")}
          </Empty>
        )}
        <ul className="grid gap-4 sm:grid-cols-2">
          {eggs.data?.map((egg) => (
            <li key={egg.id} className="rounded-xl border border-line bg-zinc-900 p-5">
              <div className="flex items-start gap-3.5">
                <ServerIcon name={egg.name} size={44} />
                <div className="min-w-0 flex-1">
                  <h2 className="truncate text-[15px] font-semibold text-zinc-100">
                    {egg.name} <span className="font-normal text-zinc-500">#{egg.id}</span>
                  </h2>
                  {egg.description && <p className="mt-0.5 line-clamp-2 text-sm text-zinc-400">{egg.description}</p>}
                </div>
                <Pill>{t("eggsPage.variables", { count: (egg.variables ?? []).length })}</Pill>
              </div>
              <dl className="mt-4 space-y-2 text-[13px]">
                <div>
                  <dt className="text-[11px] font-semibold uppercase tracking-wider text-zinc-500">{t("eggsPage.image")}</dt>
                  <dd className="mt-0.5 break-all font-mono text-zinc-300">{egg.docker_image}</dd>
                </div>
                <div>
                  <dt className="text-[11px] font-semibold uppercase tracking-wider text-zinc-500">{t("eggsPage.startup")}</dt>
                  <dd className="mt-0.5 break-all rounded-lg bg-well px-2.5 py-1.5 font-mono text-zinc-300">{egg.startup}</dd>
                </div>
              </dl>
            </li>
          ))}
        </ul>
      </div>
    </div>
  );
}
