import i18n, { type ParseKeys } from "i18next";
import { initReactI18next } from "react-i18next";
import type en from "./locales/en/translation.json";

// English is the source and the fallback for every missing string. Other
// languages come from Crowdin as locales/<lang>/translation.json and are picked
// up here without any code change.
const files = import.meta.glob<{ default: typeof en }>("./locales/*/translation.json", { eager: true });
export const resources = Object.fromEntries(
  Object.entries(files).map(([path, mod]) => [path.split("/")[2] ?? "en", { translation: mod.default }]),
);

export const languages = Object.keys(resources);

function pickLanguage(): string {
  try {
    const saved = localStorage.getItem("lang");
    if (saved && languages.includes(saved)) return saved;
  } catch {
    // Storage can be blocked; the browser language still works.
  }
  const nav = typeof navigator === "undefined" ? [] : navigator.languages;
  for (const tag of nav) {
    const base = tag.toLowerCase().split("-")[0] ?? "";
    if (languages.includes(tag)) return tag;
    if (languages.includes(base)) return base;
  }
  return "en";
}

void i18n.use(initReactI18next).init({
  resources,
  lng: pickLanguage(),
  fallbackLng: "en",
  // React escapes output already.
  interpolation: { escapeValue: false },
  returnNull: false,
  react: { useSuspense: false },
});

if (typeof document !== "undefined") document.documentElement.lang = i18n.language;
i18n.on("languageChanged", (lng) => {
  if (typeof document !== "undefined") document.documentElement.lang = lng;
});

/**
 * A lookup table whose values come from the translation file, read on every
 * access so they follow the current language. Keys are checked at compile time.
 */
export function translated<K extends string>(keys: Record<K, ParseKeys>): Record<K, string> {
  const out = {} as Record<K, string>;
  for (const [k, key] of Object.entries(keys) as [K, ParseKeys][]) {
    Object.defineProperty(out, k, { enumerable: true, get: () => i18n.t(key) });
  }
  return out;
}

export default i18n;
