import { useEffect, useEffectEvent, useRef, useState } from "react";

/**
 * A text box whose value also lives in the URL. The box is local so typing is never
 * dropped while the router updates; the address follows once typing pauses.
 * `fromUrl` seeds the box; `toUrl` is called with the settled text.
 */
export function useSearchText(fromUrl: string | undefined, toUrl: (text: string) => void, delay = 300) {
  const [text, setText] = useState(fromUrl ?? "");
  const commit = useEffectEvent(toUrl);
  const last = useRef(fromUrl ?? "");
  useEffect(() => {
    const settled = text.trim();
    if (settled === last.current) return;
    const t = setTimeout(() => {
      last.current = settled;
      commit(settled);
    }, delay);
    return () => clearTimeout(t);
  }, [text, delay]);
  return [text, setText] as const;
}
