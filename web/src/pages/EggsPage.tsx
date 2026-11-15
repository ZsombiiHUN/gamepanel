import { useState, type ChangeEvent, type FormEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Loader2, Upload } from "lucide-react";
import { api, call } from "../api/client";
import { eggsQuery } from "../api/queries";
import { AdminOnly } from "../components/AdminOnly";
import { ErrorBox, Field, Spinner } from "../components/ui";

export function EggsPage() {
  return <AdminOnly><Eggs /></AdminOnly>;
}

function Eggs() {
  const qc = useQueryClient();
  const eggs = useQuery(eggsQuery);
  const [text, setText] = useState("");
  const [parseError, setParseError] = useState<string | null>(null);

  const imp = useMutation({
    mutationFn: (body: Record<string, unknown>) => call(// Spec types the JSON body as a loose object; the generated union is awkward.
      api.POST("/eggs/import", { body: body as never })),
    onSuccess: async () => {
      setText("");
      await qc.invalidateQueries({ queryKey: ["eggs"] });
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

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-semibold">Eggs</h1>

      <form onSubmit={submit} className="card space-y-3" aria-labelledby="import-h">
        <h2 id="import-h" className="text-lg font-medium">Import Pterodactyl egg</h2>
        <Field label="Egg file (.json)">
          {(id) => <input id={id} type="file" accept="application/json,.json" onChange={onFile} className="input file:mr-3 file:rounded file:border-0 file:bg-zinc-700 file:px-2 file:py-1 file:text-zinc-100" />}
        </Field>
        <Field label="Or paste egg JSON">
          {(id) => <textarea id={id} className="input h-40 font-mono" spellCheck={false} value={text} onChange={(e) => setText(e.target.value)} required />}
        </Field>
        <ErrorBox error={parseError} />
        <ErrorBox error={imp.error} />
        {imp.isSuccess && <p role="status" className="text-sm text-emerald-400">Imported egg "{imp.data.name}".</p>}
        <button type="submit" className="btn btn-primary" disabled={imp.isPending || !text.trim()}>
          {imp.isPending ? <Loader2 className="h-4 w-4 animate-spin" aria-hidden /> : <Upload className="h-4 w-4" aria-hidden />} Import
        </button>
      </form>

      {eggs.isPending && <Spinner />}
      <ErrorBox error={eggs.error} />
      <ul className="grid gap-4 sm:grid-cols-2">
        {eggs.data?.length === 0 && <li className="text-sm text-zinc-500">No eggs imported yet.</li>}
        {eggs.data?.map((egg) => (
          <li key={egg.id} className="card">
            <h3 className="font-medium">{egg.name} <span className="text-zinc-500">#{egg.id}</span></h3>
            {egg.description && <p className="mt-1 text-sm text-zinc-400">{egg.description}</p>}
            <dl className="mt-3 space-y-1 text-xs">
              <div><dt className="inline text-zinc-500">Image: </dt><dd className="inline break-all font-mono">{egg.docker_image}</dd></div>
              <div><dt className="inline text-zinc-500">Startup: </dt><dd className="inline break-all font-mono">{egg.startup}</dd></div>
              <div><dt className="inline text-zinc-500">Variables: </dt><dd className="inline">{(egg.variables ?? []).length}</dd></div>
            </dl>
          </li>
        ))}
      </ul>
    </div>
  );
}
