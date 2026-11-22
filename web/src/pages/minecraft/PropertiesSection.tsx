import { useMemo, useState } from "react";
import { ChevronRight, Loader2, Lock } from "lucide-react";
import { ErrorBox, Section, Spinner } from "../../components/ui";
import { checkField, MANAGED_KEYS, parseProperties, PROPERTY_GROUPS, setProperties, type PropField } from "../../lib/minecraft";
import { Motd, useServerFile, useWriteFile, type Can } from "./shared";
import { useTranslation } from "react-i18next";

const GROUPED = new Set(PROPERTY_GROUPS.flatMap((g) => g.fields.map((f) => f.key)));

/** server.properties as a form: the common settings by group, every other key under "All settings". */
export function PropertiesSection({ uuid, running, can }: { uuid: string; running: boolean; can: Can }) {
  const { t } = useTranslation();
  const file = useServerFile(uuid, "/server.properties");
  const write = useWriteFile(uuid);
  const [draft, setDraft] = useState<Record<string, string>>({});
  const [saved, setSaved] = useState(false);
  const current = useMemo(() => parseProperties(file.data ?? ""), [file.data]);
  const editable = can("files.write");

  const value = (k: string, def = "") => draft[k] ?? current.get(k) ?? def;
  const set = (k: string, v: string) => {
    setSaved(false);
    // A value set back to what the file has is no longer a change.
    setDraft((d) =>
      Object.fromEntries(Object.entries({ ...d, [k]: v }).filter(([key, val]) => key !== k || (current.get(k) ?? null) !== val)),
    );
  };
  const fields = PROPERTY_GROUPS.flatMap((g) => g.fields);
  const errors: Record<string, string | null> = Object.fromEntries(
    fields.map((f) => [f.key, draft[f.key] !== undefined ? checkField(f, draft[f.key] ?? "") : null]),
  );
  const dirty = Object.keys(draft).length > 0;
  const bad = Object.values(errors).some(Boolean);
  const others = [...current.keys()].filter((k) => !GROUPED.has(k)).sort();

  const save = () =>
    write.mutate(
      { path: "/server.properties", content: setProperties(file.data ?? "", draft) },
      {
        onSuccess: () => {
          setDraft({});
          setSaved(true);
        },
      },
    );

  if (file.isPending)
    return (
      <Section title={t("propertiesSection.serverSettings")}>
        <Spinner />
      </Section>
    );
  if (file.error)
    return (
      <Section title={t("propertiesSection.serverSettings")}>
        <ErrorBox error={file.error} />
      </Section>
    );
  if (file.data === null)
    return <Section title={t("propertiesSection.serverSettings")} desc={t("propertiesSection.theServerWritesServerProperties")} />;

  return (
    <Section
      title={t("propertiesSection.serverSettings")}
      desc={t("propertiesSection.theCommonSettingsFromServer")}
      footer={
        <>
          <span role="status">
            {saved
              ? running
                ? t("propertiesSection.savedRestartTheServerTo")
                : t("propertiesSection.savedTheyApplyWhenThe")
              : dirty
                ? t("propertiesSection.unsaved", { count: Object.keys(draft).length })
                : t("propertiesSection.thePortIsSetBy")}
          </span>
          <div className="flex gap-2">
            {dirty && (
              <button type="button" className="btn" onClick={() => setDraft({})}>
                {t("propertiesSection.discard")}
              </button>
            )}
            <button type="button" className="btn btn-primary" disabled={!editable || !dirty || bad || write.isPending} onClick={save}>
              {write.isPending && <Loader2 className="h-4 w-4 animate-spin" aria-hidden />}
              {t("common.save")}
            </button>
          </div>
        </>
      }
    >
      <div className="space-y-6">
        {PROPERTY_GROUPS.map((g) => (
          <fieldset key={g.title} disabled={!editable}>
            <legend className="mb-3 text-[12px] font-medium uppercase tracking-wide text-zinc-500">{g.title}</legend>
            <div className="grid gap-x-5 gap-y-4 sm:grid-cols-2">
              {g.fields.map((f) => (
                <PropInput
                  key={f.key}
                  f={f}
                  value={value(f.key, f.def)}
                  error={errors[f.key] ?? null}
                  changed={f.key in draft}
                  onChange={(v) => set(f.key, v)}
                />
              ))}
            </div>
            {g.id === "general" && (
              <div
                role="figure"
                className="mt-4 rounded-lg border border-line bg-[#1b1b1b] px-3 py-2.5"
                aria-label={t("propertiesSection.howTheMessageOfThe")}
              >
                <p className="mb-1 text-[11px] font-medium uppercase tracking-wide text-zinc-500">{t("propertiesSection.preview")}</p>
                <Motd text={value("motd", "A Minecraft Server")} />
              </div>
            )}
          </fieldset>
        ))}

        {others.length > 0 && (
          <details className="group rounded-lg border border-line">
            <summary className="flex cursor-pointer list-none items-center gap-2 px-3 py-2.5 text-[13px] font-medium text-zinc-300 hover:text-zinc-100">
              <ChevronRight className="h-4 w-4 transition-transform group-open:rotate-90" aria-hidden />
              {t("propertiesSection.allSettings")}{" "}
              <span className="text-zinc-500">{t("propertiesSection.more", { count: others.length })}</span>
            </summary>
            <div className="grid gap-x-5 gap-y-3 border-t border-line p-3 sm:grid-cols-2">
              {others.map((k) => {
                const managed = MANAGED_KEYS.has(k);
                const id = `prop-${k}`;
                return (
                  <div key={k}>
                    <label htmlFor={id} className="mb-1 flex items-center gap-1.5 font-mono text-[12px] text-zinc-400">
                      {k}
                      {managed && <Lock className="h-3 w-3" aria-label={t("propertiesSection.setByThePanel")} />}
                    </label>
                    <input
                      id={id}
                      className={`input font-mono !text-[12.5px] ${k in draft ? "!border-zinc-400" : ""}`}
                      value={value(k)}
                      disabled={!editable || managed}
                      title={managed ? t("propertiesSection.thePanelWritesThisOn") : undefined}
                      onChange={(e) => set(k, e.target.value)}
                    />
                  </div>
                );
              })}
            </div>
          </details>
        )}
      </div>
      {write.error ? (
        <div className="mt-3">
          <ErrorBox error={write.error} />
        </div>
      ) : null}
    </Section>
  );
}

function PropInput({
  f,
  value,
  error,
  changed,
  onChange,
}: {
  f: PropField;
  value: string;
  error: string | null;
  changed: boolean;
  onChange: (v: string) => void;
}) {
  const id = `prop-${f.key}`;
  const ring = changed ? "!border-zinc-400" : "";
  if (f.type === "bool") {
    const on = value === "true";
    return (
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <label htmlFor={id} className="text-[13px] font-medium text-zinc-200">
            {f.label}
          </label>
          {f.hint && <p className="mt-0.5 text-[12px] text-zinc-500">{f.hint}</p>}
        </div>
        <button
          id={id}
          type="button"
          role="switch"
          aria-checked={on}
          onClick={() => onChange(on ? "false" : "true")}
          className={`relative mt-0.5 inline-flex h-5 w-9 shrink-0 items-center rounded-full border transition-colors focus-visible:outline-2 focus-visible:outline-indigo-500 disabled:opacity-50 ${
            on ? "border-zinc-100 bg-zinc-100" : "border-zinc-700 bg-well"
          } ${changed ? "ring-2 ring-zinc-500/40" : ""}`}
        >
          <span
            className={`inline-block h-3.5 w-3.5 rounded-full transition-transform ${on ? "translate-x-[18px] bg-zinc-900" : "translate-x-[2px] bg-zinc-500"}`}
          />
        </button>
      </div>
    );
  }
  return (
    <div>
      <label htmlFor={id} className="label">
        {f.label}
      </label>
      {f.type === "select" ? (
        <select id={id} className={`input ${ring}`} value={value} onChange={(e) => onChange(e.target.value)}>
          {!f.options?.includes(value) && <option value={value}>{value}</option>}
          {f.options?.map((o) => (
            <option key={o} value={o}>
              {o.charAt(0).toUpperCase() + o.slice(1)}
            </option>
          ))}
        </select>
      ) : f.lines ? (
        <textarea
          id={id}
          className={`input resize-none font-mono ${ring}`}
          rows={f.lines}
          maxLength={1024}
          value={value}
          aria-invalid={error ? true : undefined}
          aria-describedby={error || f.hint ? `${id}-hint` : undefined}
          onChange={(e) => onChange(e.target.value)}
        />
      ) : (
        <input
          id={id}
          className={`input ${ring}`}
          type={f.type === "number" ? "number" : "text"}
          inputMode={f.type === "number" ? "numeric" : undefined}
          min={f.min}
          max={f.max}
          maxLength={f.type === "text" ? 1024 : undefined}
          value={value}
          aria-invalid={error ? true : undefined}
          aria-describedby={error || f.hint ? `${id}-hint` : undefined}
          onChange={(e) => onChange(e.target.value)}
        />
      )}
      {(error || f.hint) && (
        <p id={`${id}-hint`} className={`mt-1 text-[12px] ${error ? "text-bad-text" : "text-zinc-500"}`}>
          {error ?? f.hint}
        </p>
      )}
    </div>
  );
}
