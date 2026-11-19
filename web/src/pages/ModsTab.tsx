import { useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { Check, Download, Loader2, Package } from "lucide-react";
import { api, call, type components } from "../api/client";
import { invalidates, keys, modSearchQuery, modVersionsQuery, serverQuery } from "../api/queries";
import { SearchInput } from "../components/SearchInput";
import { Empty, ErrorBox, Spinner } from "../components/ui";
import { useDebouncedValue } from "../hooks/useDebouncedValue";
import { compactNumber, guessFromEgg, LOADERS, type ModType } from "../lib/mods";

type Hit = components["schemas"]["Hit"];

export function ModsTab({ uuid }: { uuid: string }) {
  const server = useQuery(serverQuery(uuid));
  const [guess] = useState(() => guessFromEgg(server.data?.egg_name ?? ""));
  const [type, setType] = useState<ModType>(guess.type);
  const [loader, setLoader] = useState(guess.loader);
  const [version, setVersion] = useState("");
  const [q, setQ] = useState("");
  const query = useDebouncedValue(q.trim(), 300);
  const v = useDebouncedValue(version.trim(), 400);
  const results = useQuery(modSearchQuery(uuid, { q: query, type, loader, version: v }));

  return (
    <div className="space-y-4">
      <section className="space-y-3 rounded-2xl border border-zinc-800 bg-zinc-900 p-4" aria-label="Find mods">
        <div className="flex flex-wrap items-end gap-3">
          <div>
            <label htmlFor="mod-type" className="label">
              Kind
            </label>
            <select
              id="mod-type"
              className="input"
              value={type}
              onChange={(e) => {
                const t = e.target.value as ModType;
                setType(t);
                if (!LOADERS[t].includes(loader)) setLoader("");
              }}
            >
              <option value="mod">Mods</option>
              <option value="plugin">Plugins</option>
              <option value="datapack">Datapacks</option>
            </select>
          </div>
          {LOADERS[type].length > 0 && (
            <div>
              <label htmlFor="mod-loader" className="label">
                Loader
              </label>
              <select id="mod-loader" className="input" value={loader} onChange={(e) => setLoader(e.target.value)}>
                <option value="">Any</option>
                {LOADERS[type].map((l) => (
                  <option key={l} value={l}>
                    {l[0]!.toUpperCase() + l.slice(1)}
                  </option>
                ))}
              </select>
            </div>
          )}
          <div className="w-32">
            <label htmlFor="mod-version" className="label">
              Game version
            </label>
            <input
              id="mod-version"
              className="input font-mono"
              placeholder="1.21.1"
              value={version}
              onChange={(e) => setVersion(e.target.value)}
            />
          </div>
          <div className="min-w-56 flex-1">
            <SearchInput label="Search Modrinth" placeholder="Search Modrinth" value={q} onChange={setQ} className="w-full" />
          </div>
        </div>
        <p className="text-xs text-zinc-500">
          From{" "}
          <a className="link underline" href="https://modrinth.com" target="_blank" rel="noreferrer">
            Modrinth
          </a>
          . Every download is checked against Modrinth's checksum before it reaches the server.
        </p>
      </section>
      <ErrorBox error={results.error} />
      {results.isPending && <Spinner label="Searching Modrinth" />}
      {results.data?.hits.length === 0 && (
        <Empty icon={<Package className="h-5 w-5" aria-hidden />} title="Nothing found">
          Try other words, or loosen the loader and version.
        </Empty>
      )}
      <ul className="space-y-2.5">
        {results.data?.hits.map((h) => (
          <ModRow key={h.project_id} uuid={uuid} hit={h} type={type} loader={loader} version={v} />
        ))}
      </ul>
    </div>
  );
}

function ModRow({ uuid, hit, type, loader, version }: { uuid: string; hit: Hit; type: ModType; loader: string; version: string }) {
  const [open, setOpen] = useState(false);
  const versions = useQuery({ ...modVersionsQuery(uuid, hit.project_id, loader, version), enabled: open });
  const [picked, setPicked] = useState("");
  const install = useMutation({
    mutationFn: (version_id: string) => call(api.POST("/servers/{uuid}/mods", { params: { path: { uuid } }, body: { version_id, type } })),
    meta: invalidates(keys.files(uuid)),
  });
  const list = versions.data ?? [];
  const chosen = picked || list[0]?.id || "";
  return (
    <li className="rounded-xl border border-zinc-800 bg-zinc-900 p-4">
      <div className="flex flex-wrap items-start gap-3">
        {hit.icon_url ? (
          <img
            src={hit.icon_url}
            alt=""
            width={44}
            height={44}
            className="h-11 w-11 shrink-0 rounded-lg bg-chip"
            loading="lazy"
            referrerPolicy="no-referrer"
          />
        ) : (
          <span className="grid h-11 w-11 shrink-0 place-content-center rounded-lg bg-chip">
            <Package className="h-5 w-5 text-zinc-500" aria-hidden />
          </span>
        )}
        <div className="min-w-0 flex-1 basis-60">
          <h3 className="font-medium text-zinc-100">
            {hit.title} <span className="text-sm font-normal text-zinc-500">by {hit.author}</span>
          </h3>
          <p className="mt-0.5 text-sm text-zinc-400">{hit.description}</p>
          <p className="mt-1 text-xs text-zinc-500">{compactNumber(hit.downloads)} downloads</p>
        </div>
        {!open ? (
          <button type="button" className="btn" onClick={() => setOpen(true)}>
            <Download className="h-4 w-4" aria-hidden /> Install
          </button>
        ) : (
          <div className="flex flex-wrap items-center gap-2">
            {versions.isPending && <Loader2 className="h-4 w-4 animate-spin text-zinc-400" aria-label="Loading versions" />}
            {versions.data && list.length === 0 && (
              <span className="text-sm text-zinc-400">No version matches this loader and game version.</span>
            )}
            {list.length > 0 && (
              <>
                <label htmlFor={`v-${hit.project_id}`} className="sr-only">
                  Version of {hit.title}
                </label>
                <select id={`v-${hit.project_id}`} className="input !w-56" value={chosen} onChange={(e) => setPicked(e.target.value)}>
                  {list.slice(0, 50).map((x) => (
                    <option key={x.id} value={x.id}>
                      {x.version_number}
                      {x.version_type !== "release" ? ` (${x.version_type})` : ""} · {(x.game_versions ?? []).slice(-2).join(", ")}
                    </option>
                  ))}
                </select>
                <button
                  type="button"
                  className="btn btn-primary"
                  disabled={!chosen || install.isPending}
                  onClick={() => install.mutate(chosen)}
                >
                  {install.isPending ? (
                    <Loader2 className="h-4 w-4 animate-spin" aria-hidden />
                  ) : (
                    <Download className="h-4 w-4" aria-hidden />
                  )}{" "}
                  Install
                </button>
              </>
            )}
          </div>
        )}
      </div>
      {install.isSuccess && (
        <p role="status" className="mt-2 flex items-center gap-1.5 text-sm text-good-text">
          <Check className="h-4 w-4" aria-hidden /> Installed to <code className="font-mono">{install.data.path}</code>. Restart the server
          to load it.
        </p>
      )}
      <div className="mt-2">
        <ErrorBox error={install.error ?? versions.error} />
      </div>
    </li>
  );
}
