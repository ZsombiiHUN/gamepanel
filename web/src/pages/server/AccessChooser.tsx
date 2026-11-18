import { useEffect, useRef, useState } from "react";
import { Check, SlidersHorizontal } from "lucide-react";
import { ROLES, areaSummary, groupPerms, permLabel, requires, roleOf, setArea, togglePerm, type Perm } from "../../lib/access";

/** A checkbox that can show "some ticked". */
function AreaBox({ checked, mixed, onChange, label }: { checked: boolean; mixed: boolean; onChange: (on: boolean) => void; label: string }) {
  const ref = useRef<HTMLInputElement>(null);
  useEffect(() => {
    if (ref.current) ref.current.indeterminate = mixed;
  }, [mixed]);
  return (
    <input
      ref={ref}
      type="checkbox"
      aria-label={label}
      className="h-4 w-4 cursor-pointer accent-indigo-400"
      checked={checked}
      onChange={(e) => onChange(e.target.checked)}
    />
  );
}

/** One card per area (console, files, backups ...) with a tick-all box and a row per permission. */
function PermPicker({ all, value, onChange }: { all: Perm[]; value: string[]; onChange: (v: string[]) => void }) {
  const known = all.map((p) => p.id);
  return (
    <fieldset className="grid gap-3 sm:grid-cols-2">
      <legend className="sr-only">Permissions</legend>
      {groupPerms(all).map((g) => {
        const on = g.perms.filter((p) => value.includes(p.id)).length;
        return (
          <div key={g.area} className={`rounded-xl border p-3 transition-colors ${on ? "border-indigo-500/30 bg-indigo-500/[0.04]" : "border-zinc-800"}`}>
            <div className="mb-2 flex items-center gap-2.5">
              {g.perms.length > 1 ? (
                <AreaBox label={`All ${g.label} permissions`} checked={on === g.perms.length} mixed={on > 0 && on < g.perms.length} onChange={(v) => onChange(setArea(value, g.area, v, known))} />
              ) : (
                <span className="w-4" aria-hidden />
              )}
              <span className="text-sm font-semibold text-zinc-100">{g.label}</span>
              <span className="ml-auto font-mono text-xs text-zinc-500">{on}/{g.perms.length}</span>
            </div>
            <div className="space-y-0.5">
              {g.perms.map((p) => {
                const needs = requires(p.id).filter((r) => known.includes(r));
                return (
                  <label key={p.id} className="flex cursor-pointer items-start gap-2.5 rounded-lg px-1 py-1.5 hover:bg-row-hover">
                    <input type="checkbox" className="mt-0.5 h-4 w-4 cursor-pointer accent-indigo-400" checked={value.includes(p.id)} onChange={() => onChange(togglePerm(value, p.id, known))} />
                    <span className="min-w-0">
                      <span className="block text-sm text-zinc-200">{permLabel(p.id)}</span>
                      <span className="block text-xs text-zinc-500">
                        {p.description}
                        {needs.length > 0 && <> · includes {needs.map(permLabel).join(", ").toLowerCase()}</>}
                      </span>
                    </span>
                  </label>
                );
              })}
            </div>
          </div>
        );
      })}
    </fieldset>
  );
}

/** Role cards plus an optional full permission list. */
export function AccessChooser({ all, value, onChange }: { all: Perm[]; value: string[]; onChange: (v: string[]) => void }) {
  const known = all.map((p) => p.id);
  const role = roleOf(value, known);
  const [custom, setCustom] = useState(!role && value.length > 0);
  return (
    <div className="space-y-4">
      <div role="radiogroup" aria-label="Role" className="grid gap-2 sm:grid-cols-2 lg:grid-cols-4">
        {ROLES.map((r) => {
          const active = role?.id === r.id;
          return (
            <button
              key={r.id}
              type="button"
              role="radio"
              aria-checked={active}
              onClick={() => onChange(r.perms(known))}
              className={`relative rounded-xl border p-3 text-left transition-colors focus-visible:outline-2 focus-visible:outline-indigo-500 ${
                active ? "border-indigo-400/70 bg-indigo-500/10" : "border-zinc-800 hover:border-zinc-700 hover:bg-row-hover"
              }`}
            >
              <span className="flex items-center justify-between gap-2">
                <span className="text-sm font-semibold text-zinc-100">{r.label}</span>
                {active && <Check className="h-4 w-4 text-indigo-400" aria-hidden />}
              </span>
              <span className="mt-1 block text-xs leading-snug text-zinc-500">{r.desc}</span>
            </button>
          );
        })}
      </div>
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-sm">
        <button type="button" className="link inline-flex items-center gap-1.5" aria-expanded={custom} onClick={() => setCustom(!custom)}>
          <SlidersHorizontal className="h-3.5 w-3.5" aria-hidden />
          {custom ? "Hide individual permissions" : "Pick individual permissions"}
        </button>
        <span className="text-zinc-500">
          {value.length === 0 ? "Nothing selected yet." : <>{role ? role.label : "Custom"}: {value.length} of {known.length || value.length} · {areaSummary(value)}</>}
        </span>
      </div>
      {custom && <PermPicker all={all} value={value} onChange={onChange} />}
    </div>
  );
}
