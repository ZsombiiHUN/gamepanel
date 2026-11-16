export type ModType = "mod" | "plugin" | "datapack";

export const LOADERS: Record<ModType, string[]> = {
  mod: ["fabric", "forge", "neoforge", "quilt"],
  plugin: ["paper", "purpur", "spigot", "bukkit", "folia", "velocity", "bungeecord"],
  datapack: [],
};

/** A sensible starting filter from the egg's name ("Paper", "Fabric", "Forge" ...). */
export function guessFromEgg(egg: string): { type: ModType; loader: string } {
  const e = egg.toLowerCase();
  for (const l of LOADERS.plugin) if (e.includes(l)) return { type: "plugin", loader: l };
  if (e.includes("neoforge")) return { type: "mod", loader: "neoforge" };
  for (const l of LOADERS.mod) if (e.includes(l)) return { type: "mod", loader: l };
  if (e.includes("vanilla")) return { type: "datapack", loader: "" };
  return { type: "mod", loader: "" };
}

export const compactNumber = (n: number) => new Intl.NumberFormat(undefined, { notation: "compact", maximumFractionDigits: 1 }).format(n);
