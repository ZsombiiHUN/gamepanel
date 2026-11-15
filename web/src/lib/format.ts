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

export function joinPath(dir: string, name: string): string {
  return dir === "/" || dir === "" ? `/${name}` : `${dir.replace(/\/+$/, "")}/${name}`;
}

export function parentPath(p: string): string {
  const parts = p.split("/").filter(Boolean);
  parts.pop();
  return "/" + parts.join("/");
}
