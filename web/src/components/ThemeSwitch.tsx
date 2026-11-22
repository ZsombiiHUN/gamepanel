import { Monitor, Moon, Sun } from "lucide-react";
import { useTheme, type ThemePref } from "../lib/theme";
import { useTranslation } from "react-i18next";

const OPTIONS: { value: ThemePref; Icon: typeof Sun }[] = [
  { value: "system", Icon: Monitor },
  { value: "light", Icon: Sun },
  { value: "dark", Icon: Moon },
];

/** Three-way switch: follow the system, or always light, or always dark. */
export function ThemeSwitch() {
  const { t } = useTranslation();
  const [pref, setPref] = useTheme();
  return (
    <div role="group" aria-label={t("themeSwitch.theme")} className="inline-flex rounded-lg border border-line p-0.5">
      {OPTIONS.map(({ value, Icon }) => {
        const label = t(`themeSwitch.${value}`);
        return (
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
        );
      })}
    </div>
  );
}
