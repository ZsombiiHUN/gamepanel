export interface PaletteItem {
  id: string;
  label: string;
  /** Extra words that should match but are not the title, e.g. game, node, port. */
  keywords?: string;
  /** Section heading, e.g. "Servers". */
  group: string;
}

/**
 * Rank items for a typed query. Every word must match somewhere; label matches
 * beat keyword matches, and matches at the start of the label or of a word beat
 * matches in the middle. Equal scores keep their original order.
 */
export function rankItems<T extends PaletteItem>(items: readonly T[], query: string): T[] {
  const words = query.toLowerCase().split(/\s+/).filter(Boolean);
  if (words.length === 0) return [...items];
  const scored: { item: T; score: number; i: number }[] = [];
  items.forEach((item, i) => {
    const label = item.label.toLowerCase();
    const extra = (item.keywords ?? "").toLowerCase();
    let total = 0;
    for (const w of words) {
      const at = label.indexOf(w);
      let s: number;
      if (at === 0) s = 100;
      else if (at > 0 && /[\s\-_./]/.test(label.charAt(at - 1))) s = 80;
      else if (at > 0) s = 60;
      else if (extra.includes(w)) s = 30;
      else return;
      total += s;
    }
    scored.push({ item, score: total, i });
  });
  return scored.sort((a, b) => b.score - a.score || a.i - b.i).map((x) => x.item);
}

/** True when a keypress is going into something the user is typing in, so single-key shortcuts must not fire. */
export function isTypingTarget(el: EventTarget | null): boolean {
  if (!el || !(el instanceof HTMLElement)) return false;
  if (el.isContentEditable) return true;
  const tag = el.tagName;
  return tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT" || !!el.closest(".cm-editor, .xterm");
}
