/** Archives the panel can extract. */
const ARCHIVE = /\.(zip|tar|tar\.gz|tgz|tar\.bz2|tbz2|gz)$/i;
export const isArchive = (name: string) => ARCHIVE.test(name);

/** "rwxr-x---" for 0o750. */
export function modeString(mode: number) {
  const bits = "rwxrwxrwx";
  return Array.from(bits, (c, i) => (mode & (1 << (8 - i)) ? c : "-")).join("");
}

/** "750" for 0o750. */
export const octal = (mode: number) => (mode & 0o777).toString(8).padStart(3, "0");

/** A name for a new file or folder: one segment, no slashes, not "." or "..". */
export function nameProblem(name: string): string | null {
  const n = name.trim();
  if (!n) return "Enter a name.";
  if (n === "." || n === "..") return "That name is reserved.";
  if (/[\\/\0]/.test(n)) return "Names cannot contain / or \\.";
  if (n.length > 255) return "Names can be at most 255 characters.";
  return null;
}

/** Where a move lands: a target folder plus the item's name. */
export function moveTarget(item: string, folder: string) {
  const name = item.split("/").filter(Boolean).pop() ?? "";
  const dir = "/" + folder.split("/").filter(Boolean).join("/");
  return dir === "/" ? `/${name}` : `${dir}/${name}`;
}

export interface DroppedFile {
  file: File;
  /** Path below the drop target, e.g. "world/region/r.0.0.mca". */
  rel: string;
}

/* Minimal types for the File and Directory Entries API (drag and drop of folders). */
interface FsEntry {
  isFile: boolean;
  isDirectory: boolean;
  name: string;
  file?: (ok: (f: File) => void, err: (e: unknown) => void) => void;
  createReader?: () => { readEntries: (ok: (e: FsEntry[]) => void, err: (e: unknown) => void) => void };
}

async function readEntry(entry: FsEntry, prefix: string, out: DroppedFile[]) {
  if (entry.isFile && entry.file) {
    const read = entry.file.bind(entry);
    const file = await new Promise<File>((ok, err) => {
      read(ok, err);
    });
    out.push({ file, rel: prefix + entry.name });
  } else if (entry.isDirectory && entry.createReader) {
    const reader = entry.createReader();
    // readEntries returns at most ~100 entries per call: keep asking until it returns none.
    for (;;) {
      const batch = await new Promise<FsEntry[]>((ok, err) => reader.readEntries(ok, err));
      if (batch.length === 0) break;
      for (const e of batch) await readEntry(e, `${prefix}${entry.name}/`, out);
    }
  }
}

/** Every file in a drop, including those inside dropped folders. */
export async function droppedFiles(dt: DataTransfer): Promise<DroppedFile[]> {
  const out: DroppedFile[] = [];
  const entries: FsEntry[] = [];
  for (const i of dt.items) {
    const e = i.kind === "file" ? (i.webkitGetAsEntry() as unknown as FsEntry | null) : null;
    if (e) entries.push(e);
  }
  if (entries.length === 0) {
    // No entries API: plain files only.
    for (const file of dt.files) out.push({ file, rel: file.name });
    return out;
  }
  for (const e of entries) await readEntry(e, "", out);
  return out;
}

/** Files from an <input type="file">, keeping folder structure when a folder was picked. */
export function pickedFiles(list: FileList): DroppedFile[] {
  return [...list].map((file) => ({ file, rel: (file as File & { webkitRelativePath?: string }).webkitRelativePath || file.name }));
}
