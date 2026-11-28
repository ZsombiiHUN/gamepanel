export function formatBytes(n: number): string {
  if (!Number.isFinite(n) || n < 0) return "-";
  const units = ["B", "KiB", "MiB", "GiB", "TiB"];
  let i = 0;
  let v = n;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${v >= 100 || i === 0 ? v.toFixed(0) : v.toFixed(1)} ${units[i]}`;
}

/** "used / limit", naming the unit once when both share it: "1.8 / 2.0 GiB". */
export function formatUsage(used: number, limit: number): string {
  const u = formatBytes(used);
  const l = formatBytes(limit);
  const [uv, uu] = u.split(" ");
  const [lv, lu] = l.split(" ");
  return uu && uu === lu ? `${uv} / ${lv} ${lu}` : `${u} / ${l}`;
}

export function joinPath(dir: string, name: string): string {
  return dir === "/" || dir === "" ? `/${name}` : `${dir.replace(/\/+$/, "")}/${name}`;
}

export function parentPath(p: string): string {
  const parts = p.split("/").filter(Boolean);
  parts.pop();
  return "/" + parts.join("/");
}
