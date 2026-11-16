import { useEffect, useState } from "react";

export type ThemePref = "system" | "light" | "dark";
const KEY = "gp-theme";
const media = () => window.matchMedia("(prefers-color-scheme: light)");

export function readPref(): ThemePref {
  try {
    const v = localStorage.getItem(KEY);
    return v === "light" || v === "dark" ? v : "system";
  } catch {
    return "system"; // storage blocked: follow the system
  }
}

/** Apply a preference to the page (index.html does the same before the first paint). */
export function applyTheme(pref: ThemePref) {
  const light = pref === "light" || (pref === "system" && media().matches);
  document.documentElement.dataset.theme = light ? "light" : "dark";
}

/** The saved preference, a setter that saves and applies it, and following the system while on "system". */
export function useTheme() {
  const [pref, setPref] = useState<ThemePref>(readPref);
  useEffect(() => {
    applyTheme(pref);
    if (pref !== "system") return;
    const m = media();
    const follow = () => applyTheme("system");
    m.addEventListener("change", follow);
    return () => m.removeEventListener("change", follow);
  }, [pref]);
  const set = (p: ThemePref) => {
    try {
      if (p === "system") localStorage.removeItem(KEY);
      else localStorage.setItem(KEY, p);
    } catch {
      // still applies for this visit
    }
    setPref(p);
  };
  return [pref, set] as const;
}
