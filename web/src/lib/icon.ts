export interface Block {
  key: string;
  x: number;
  y: number;
  fill: string;
}
export interface BlockIcon {
  hue: number;
  blocks: Block[];
}

/** The same name always gives the same icon: a hash seeds a small generator that fills a mirrored 5x5 grid. */
export function blockIcon(name: string): BlockIcon {
  let h = 2166136261;
  for (let i = 0; i < name.length; i++) h = Math.imul(h ^ name.charCodeAt(i), 16777619) >>> 0;
  const next = () => {
    h = Math.imul(h ^ (h >>> 15), 2246822507) >>> 0;
    h = Math.imul(h ^ (h >>> 13), 3266489909) >>> 0;
    h = (h ^ (h >>> 16)) >>> 0;
    return h;
  };
  const hue = next() % 360;
  const blocks: Block[] = [];
  for (let r = 0; r < 5; r++) {
    for (let c = 0; c < 3; c++) {
      const v = next();
      if (v % 100 >= 56) continue;
      const fill = v % 2 ? `hsl(${hue} 70% 72%)` : `hsl(${(hue + 28) % 360} 62% 55%)`;
      for (const cc of new Set([c, 4 - c])) blocks.push({ key: `${r}-${cc}`, x: 3 + cc * 7, y: 3 + r * 7, fill });
    }
  }
  return { hue, blocks };
}
