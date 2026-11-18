import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Lock, RotateCcw } from "lucide-react";
import { api, call, type components } from "../api/client";
import { keys, startupQuery } from "../api/queries";
import { CopyButton, ErrorBox, Pill, Section, Spinner } from "../components/ui";
import { fillCommand, inputKind } from "../lib/startup";

type StartupView = components["schemas"]["StartupView"];
type StartupVar = components["schemas"]["StartupVar"];
type Change = { env?: Record<string, string>; image?: string; startup?: string; crash_restart?: boolean };

export function StartupTab({ uuid }: { uuid: string }) {
  const startup = useQuery(startupQuery(uuid));
  if (startup.isPending) return <Spinner />;
  if (startup.error) return <ErrorBox error={startup.error} />;
  return <StartupForms uuid={uuid} s={startup.data} />;
}

function useSave(uuid: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: Change) => call(api.PUT("/servers/{uuid}/startup", { params: { path: { uuid } }, body })),
    // The reply is the new state: no need to fetch it again.
    onSuccess: (data) => qc.setQueryData(keys.startup(uuid), data),
  });
}

function StartupForms({ uuid, s }: { uuid: string; s: StartupView }) {
  return (
    <div className="space-y-5">
      {s.pending && (
        <p role="status" className="rounded-xl border border-amber-500/30 bg-amber-500/10 px-4 py-3 text-sm text-amber-200">
          Your changes are saved. They take effect the next time the server starts, so restart it when it suits you.
        </p>
      )}
      <CommandSection uuid={uuid} s={s} />
      <ImageSection uuid={uuid} s={s} />
      <VariablesSection uuid={uuid} s={s} />
      <CrashSection uuid={uuid} s={s} />
    </div>
  );
}

function CrashSection({ uuid, s }: { uuid: string; s: StartupView }) {
  const save = useSave(uuid);
  return (
    <Section
      title="Crashes"
      desc="What happens when the server stops without anyone asking. A server that exits normally, for example after a stop command typed in the game, counts as stopped, not crashed."
      footer={<span role={save.isSuccess ? "status" : undefined}>{save.isPending ? "Saving..." : save.isSuccess ? "Saved. This applies right away." : "If it crashes again within a minute of a restart, it is left stopped."}</span>}
    >
      <label className="flex items-start gap-3 text-sm text-zinc-200">
        <input
          type="checkbox"
          className="mt-0.5 h-4 w-4 accent-indigo-500"
          checked={s.crash_restart}
          disabled={!s.can_edit_crash_restart || save.isPending}
          onChange={(e) => save.mutate({ crash_restart: e.target.checked })}
        />
        <span>
          Restart the server automatically when it crashes
          {!s.can_edit_crash_restart && <span className="block text-xs text-zinc-500">You do not have permission to change this.</span>}
        </span>
      </label>
      {save.error && <ErrorBox error={save.error} />}
    </Section>
  );
}

function CommandSection({ uuid, s }: { uuid: string; s: StartupView }) {
  const values = Object.fromEntries([...Object.entries(s.builtins ?? {}), ...s.variables.map((v) => [v.env, v.value] as const)]);
  const preview = fillCommand(s.command, values);
  const [raw, setRaw] = useState(false);
  const [draft, setDraft] = useState<string | null>(null);
  const save = useSave(uuid);
  const editing = draft !== null;

  return (
    <Section
      title="Startup command"
      desc={s.custom_command ? "An administrator replaced the egg's command for this server." : "The command the server runs, from its egg."}
      footer={
        <>
          <span>{raw ? "Placeholders like {{SERVER_PORT}} are filled from the variables below." : "Shown with the current values filled in."}</span>
          <div className="flex flex-wrap gap-2">
            {!editing && <button type="button" className="btn btn-ghost" onClick={() => setRaw(!raw)} aria-pressed={raw}>{raw ? "Show values" : "Show placeholders"}</button>}
            {!editing && <CopyButton text={raw ? s.command : preview} label="Copy" />}
            {s.can_edit_command && !editing && <button type="button" className="btn" onClick={() => { save.reset(); setDraft(s.command); }}>Edit</button>}
          </div>
        </>
      }
    >
      {editing ? (
        <form
          onSubmit={(e) => {
            e.preventDefault();
            save.mutate({ startup: draft }, { onSuccess: () => setDraft(null) });
          }}
          className="space-y-3"
        >
          <label htmlFor="startup-cmd" className="label">Command</label>
          <textarea
            id="startup-cmd"
            className="input min-h-24 font-mono text-[13px]"
            value={draft}
            onChange={(e) => setDraft(e.target.value.replace(/[\r\n]+/g, " "))}
            spellCheck={false}
          />
          <p className="text-xs text-zinc-500">Use {"{{VARIABLE}}"} for values. They reach the server as environment variables, so they can never inject shell commands.</p>
          <ErrorBox error={save.error} />
          <div className="flex flex-wrap justify-end gap-2">
            {s.custom_command && (
              <button type="button" className="btn btn-ghost" disabled={save.isPending} onClick={() => save.mutate({ startup: "" }, { onSuccess: () => setDraft(null) })}>
                <RotateCcw className="h-4 w-4" aria-hidden /> Use the egg's command
              </button>
            )}
            <button type="button" className="btn" onClick={() => setDraft(null)}>Cancel</button>
            <button type="submit" className="btn btn-primary" disabled={save.isPending || !draft?.trim()}>Save command</button>
          </div>
        </form>
      ) : (
        <pre className="overflow-x-auto whitespace-pre-wrap break-all rounded-lg border border-zinc-800 bg-zinc-950 px-3 py-2.5 font-mono text-[13px] text-zinc-200">{raw ? s.command : preview}</pre>
      )}
    </Section>
  );
}

function ImageSection({ uuid, s }: { uuid: string; s: StartupView }) {
  const save = useSave(uuid);
  const listed = s.images.some((i) => i.image === s.image);
  const [custom, setCustom] = useState<string | null>(null);
  const label = s.images.find((i) => i.image === s.image)?.label;

  if (!s.can_change_image) {
    return (
      <Section
        title="Docker image"
        desc={s.custom_image ? "An administrator chose this image. Ask them if you need a different one." : "The runtime the server runs in."}
      >
        <p className="flex flex-wrap items-center gap-2 font-mono text-[13px] text-zinc-200">
          {label && <span className="font-sans text-sm font-medium">{label}</span>}
          {s.image}
          <Lock className="h-3.5 w-3.5 text-zinc-500" aria-label="Locked" />
        </p>
      </Section>
    );
  }
  const selectValue = custom !== null || !listed ? "__custom" : s.image;
  return (
    <Section
      title="Docker image"
      desc="The runtime the server runs in, such as a Java version. Pick a newer one if the game asks for it."
      footer={<span role={save.isSuccess ? "status" : undefined}>{save.isPending ? "Saving..." : save.isSuccess ? "Image saved." : "Changing the image does not touch your files."}</span>}
    >
      <label htmlFor="startup-image" className="label">Image</label>
      <select
        id="startup-image"
        className="input"
        value={selectValue}
        disabled={save.isPending}
        onChange={(e) => {
          if (e.target.value === "__custom") return setCustom(listed ? "" : s.image);
          setCustom(null);
          save.mutate({ image: e.target.value });
        }}
      >
        {s.images.map((i) => (
          <option key={i.image} value={i.image}>{i.label === i.image ? i.image : `${i.label} (${i.image})`}</option>
        ))}
        {/* Admins may run any image; others only pick from the egg's list. */}
        {(s.can_edit_command || !listed) && <option value="__custom">{listed ? "Custom image..." : `Custom: ${s.image}`}</option>}
      </select>
      {s.can_edit_command && selectValue === "__custom" && (
        <form
          className="mt-3 flex flex-wrap gap-2"
          onSubmit={(e) => {
            e.preventDefault();
            save.mutate({ image: (custom ?? s.image).trim() }, { onSuccess: () => setCustom(null) });
          }}
        >
          <label htmlFor="startup-custom-image" className="sr-only">Custom image</label>
          <input
            id="startup-custom-image"
            className="input flex-1 basis-64 font-mono text-[13px]"
            placeholder="ghcr.io/owner/image:tag"
            value={custom ?? s.image}
            onChange={(e) => setCustom(e.target.value)}
            spellCheck={false}
          />
          <button type="submit" className="btn btn-primary" disabled={save.isPending || !(custom ?? s.image).trim()}>Use image</button>
        </form>
      )}
      <div className="mt-3"><ErrorBox error={save.error} /></div>
    </Section>
  );
}

function VariablesSection({ uuid, s }: { uuid: string; s: StartupView }) {
  const save = useSave(uuid);
  // Only what the person typed; everything else shows the saved value.
  const [edits, setEdits] = useState<Record<string, string>>({});
  const changed = Object.fromEntries(Object.entries(edits).filter(([k, v]) => s.variables.find((x) => x.env === k)?.value !== v));
  const dirty = Object.keys(changed).length > 0;

  if (s.variables.length === 0) {
    return <Section title="Variables" desc="This egg has no settings you can see." />;
  }
  return (
    <form
      onSubmit={(e) => {
        e.preventDefault();
        save.mutate({ env: changed }, { onSuccess: () => setEdits({}) });
      }}
    >
      <Section
        title="Variables"
        desc="Settings the egg passes to the server, such as the version or the jar to run."
        footer={
          s.can_edit_variables ? (
            <>
              <span role={save.isSuccess && !dirty ? "status" : undefined}>
                {dirty ? `${Object.keys(changed).length} unsaved ${Object.keys(changed).length === 1 ? "change" : "changes"}.` : save.isSuccess ? "Variables saved." : "Locked variables can only be changed by an administrator."}
              </span>
              <div className="flex gap-2">
                {dirty && <button type="button" className="btn btn-ghost" onClick={() => { setEdits({}); save.reset(); }}>Discard</button>}
                <button type="submit" className="btn btn-primary" disabled={!dirty || save.isPending}>Save variables</button>
              </div>
            </>
          ) : (
            <span>You can see these settings but not change them.</span>
          )
        }
      >
        <div className="grid gap-x-5 gap-y-5 md:grid-cols-2">
          {s.variables.map((v) => (
            <VariableField key={v.env} v={v} value={edits[v.env] ?? v.value} onChange={(val) => setEdits({ ...edits, [v.env]: val })} />
          ))}
        </div>
        <div className="mt-4"><ErrorBox error={save.error} /></div>
      </Section>
    </form>
  );
}

function VariableField({ v, value, onChange }: { v: StartupVar; value: string; onChange: (v: string) => void }) {
  const id = `var-${v.env}`;
  const kind = inputKind(v.rules, v.default);
  const hint = [v.description, v.rules_summary && `(${v.rules_summary})`].filter(Boolean).join(" ");
  return (
    <div>
      <div className="mb-1.5 flex flex-wrap items-center gap-2">
        <label htmlFor={id} className="text-sm font-medium text-zinc-300">{v.name}</label>
        <code className="rounded bg-zinc-800 px-1.5 py-0.5 text-[11px] text-zinc-400">{v.env}</code>
        {!v.editable && <Lock className="h-3.5 w-3.5 text-zinc-500" aria-label="Read only" />}
        {v.admin_only && <Pill tone="warn">Hidden from users</Pill>}
      </div>
      {kind.type === "select" ? (
        <select id={id} className="input" value={value} disabled={!v.editable} onChange={(e) => onChange(e.target.value)} aria-describedby={hint ? `${id}-hint` : undefined}>
          {!kind.options.includes(value) && <option value={value}>{value || "(empty)"}</option>}
          {kind.options.map((o) => <option key={o} value={o}>{kind.labels?.[o] ?? o}</option>)}
        </select>
      ) : (
        <input
          id={id}
          className="input font-mono text-[13px]"
          inputMode={kind.type === "number" ? "decimal" : undefined}
          value={value}
          readOnly={!v.editable}
          onChange={(e) => onChange(e.target.value)}
          aria-describedby={hint ? `${id}-hint` : undefined}
          spellCheck={false}
        />
      )}
      {hint && <p id={`${id}-hint`} className="mt-1 text-xs text-zinc-500">{hint}</p>}
    </div>
  );
}
