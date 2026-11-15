import { useState, type ChangeEvent, type FormEvent } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { Egg, Loader2, Plus, Upload, X } from "lucide-react";
import { api, call } from "../api/client";
import { eggsQuery, invalidates, keys } from "../api/queries";
import { Empty, ErrorBox, Field, PageHeader, Pill, Section, ServerIcon, Spinner } from "../components/ui";

export function EggsPage() {
  const eggs = useQuery(eggsQuery);
  const [text, setText] = useState("");
  const [open, setOpen] = useState(false);
  const [parseError, setParseError] = useState<string | null>(null);

  const imp = useMutation({
    mutationFn: (body: Record<string, unknown>) => call(// Spec types the JSON body as a loose object; the generated union is awkward.
      api.POST("/eggs/import", { body: body as never })),
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

  const submit = (e: FormEvent) => {
    e.preventDefault();
    setParseError(null);
    imp.reset();
    try {
      const parsed: unknown = JSON.parse(text);
      if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) throw new Error("Egg must be a JSON object");
      imp.mutate(parsed as Record<string, unknown>);
    } catch (err) {
      setParseError(`Invalid JSON: ${err instanceof Error ? err.message : String(err)}`);
    }
  };

  const showImport = open || eggs.data?.length === 0;

  return (
    <div className="mx-auto max-w-5xl">
      <PageHeader
        title="Eggs"
        sub="Templates that say how to install and start a kind of game server. Pterodactyl and Pelican eggs import unchanged."
        actions={
          eggs.data?.length ? (
            <button type="button" className="btn btn-primary" aria-expanded={open} onClick={() => setOpen(!open)}>
              {open ? <X className="h-4 w-4" aria-hidden /> : <Plus className="h-4 w-4" aria-hidden />} {open ? "Cancel" : "Import egg"}
            </button>
          ) : null
        }
      />

      <div className="space-y-5">
        {imp.isSuccess && !showImport && <p role="status" className="rounded-xl border border-emerald-900 bg-emerald-950/30 px-4 py-2.5 text-sm text-emerald-300">Imported egg &quot;{imp.data.name}&quot;.</p>}
        {showImport && (
          <form onSubmit={submit}>
            <Section
              title="Import an egg"
              desc="Choose an egg's .json file, or paste its contents."
              footer={
                <>
                  <span>Install scripts are imported too. Values reach the server only as environment variables.</span>
                  <button type="submit" className="btn btn-primary" disabled={imp.isPending || !text.trim()}>
                    {imp.isPending ? <Loader2 className="h-4 w-4 animate-spin" aria-hidden /> : <Upload className="h-4 w-4" aria-hidden />} Import
                  </button>
                </>
              }
            >
              <div className="space-y-4">
                <Field label="Egg file (.json)">
                  {(id) => <input id={id} type="file" accept="application/json,.json" onChange={onFile} className="input file:mr-3 file:rounded-md file:border-0 file:bg-zinc-800 file:px-2.5 file:py-1 file:text-zinc-100" />}
                </Field>
                <Field label="Or paste egg JSON">
                  {(id) => <textarea id={id} className="input h-40 font-mono text-[13px]" spellCheck={false} value={text} onChange={(e) => setText(e.target.value)} required />}
                </Field>
                <ErrorBox error={parseError} />
                <ErrorBox error={imp.error} />
                {imp.isSuccess && <p role="status" className="text-sm text-emerald-400">Imported egg &quot;{imp.data.name}&quot;.</p>}
              </div>
            </Section>
          </form>
        )}

        {eggs.isPending && <Spinner />}
        <ErrorBox error={eggs.error} />
        {eggs.data?.length === 0 && <Empty icon={<Egg className="h-5 w-5" aria-hidden />} title="No eggs imported yet">Import one above to be able to create servers.</Empty>}
        <ul className="grid gap-4 sm:grid-cols-2">
          {eggs.data?.map((egg) => (
            <li key={egg.id} className="rounded-2xl border border-zinc-800 bg-zinc-900 p-5">
              <div className="flex items-start gap-3.5">
                <ServerIcon name={egg.name} size={44} />
                <div className="min-w-0 flex-1">
                  <h2 className="truncate text-[15px] font-semibold text-zinc-100">{egg.name} <span className="font-normal text-zinc-500">#{egg.id}</span></h2>
                  {egg.description && <p className="mt-0.5 line-clamp-2 text-sm text-zinc-400">{egg.description}</p>}
                </div>
                <Pill>{(egg.variables ?? []).length} variable{(egg.variables ?? []).length === 1 ? "" : "s"}</Pill>
              </div>
              <dl className="mt-4 space-y-2 text-[13px]">
                <div><dt className="text-[11px] font-semibold uppercase tracking-wider text-zinc-500">Image</dt><dd className="mt-0.5 break-all font-mono text-zinc-300">{egg.docker_image}</dd></div>
                <div><dt className="text-[11px] font-semibold uppercase tracking-wider text-zinc-500">Startup</dt><dd className="mt-0.5 break-all rounded-lg bg-well px-2.5 py-1.5 font-mono text-zinc-300">{egg.startup}</dd></div>
              </dl>
            </li>
          ))}
        </ul>
      </div>
    </div>
  );
}
