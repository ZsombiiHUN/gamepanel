import { Monitor, Moon, Sun } from "lucide-react";
import { useTheme, type ThemePref } from "../lib/theme";

const OPTIONS: { value: ThemePref; label: string; Icon: typeof Sun }[] = [
  { value: "system", label: "Match the system", Icon: Monitor },
  { value: "light", label: "Light", Icon: Sun },
  { value: "dark", label: "Dark", Icon: Moon },
];

/** Three-way switch: follow the system, or always light, or always dark. */
export function ThemeSwitch() {
  const [pref, setPref] = useTheme();
  return (
    <div role="group" aria-label="Theme" className="inline-flex rounded-lg border border-zinc-800 p-0.5">
      {OPTIONS.map(({ value, label, Icon }) => (
        <button
          key={value}
          type="button"
          aria-pressed={pref === value}
          aria-label={label}
          title={label}
          onClick={() => setPref(value)}
          className={`inline-flex h-7 w-7 items-center justify-center rounded-md focus-visible:outline-2 focus-visible:outline-indigo-500 ${pref === value ? "bg-zinc-800 text-zinc-100" : "text-zinc-500 hover:text-zinc-200"}`}
        >
          <Icon className="h-3.5 w-3.5" aria-hidden />
        </button>
      ))}
    </div>
  );
}
