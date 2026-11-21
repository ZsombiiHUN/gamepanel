/** What the panel knows about Minecraft servers: console problems, server.properties, MOTD codes, player lists. */

/** "server" for a Minecraft server, "proxy" for Velocity/BungeeCord, null for anything else. */
export function minecraftKind(features: readonly string[] | undefined): "server" | "proxy" | null {
  if (!features) return null;
  if (features.includes("minecraft_proxy")) return "proxy";
  // Community (Pterodactyl) Minecraft eggs carry "eula" but not our own "minecraft" flag.
  if (features.includes("minecraft") || features.includes("eula")) return "server";
  return null;
}

// ---- Console problems -------------------------------------------------------

export type Problem = { kind: "eula" } | { kind: "java"; need: number; have?: number };

// eslint-disable-next-line no-control-regex
const ANSI = /\x1b\[[0-9;?]*[A-Za-z]/g;

/**
 * Whether a console line shows a newer run getting going: gp-java's launch plan, the game's
 * first line, or its "Done" line. The console replays earlier runs, so a problem only stands
 * while no such line follows it.
 */
export function startsRun(line: string): boolean {
  const l = line.replace(ANSI, "");
  return /\[gamepanel\] Java \d+ \||Starting minecraft server version|Done \(\d[\d.,]*s\)!/.test(l);
}

/**
 * A console line that means the server cannot start until someone acts: the EULA is not accepted, or
 * the image's Java is too old. Covers GamePanel's gp-java and the messages of the stock server jars.
 */
export function detectProblem(line: string): Problem | null {
  const l = line.replace(ANSI, "");
  if (/accept the Minecraft EULA|You need to agree to the EULA/i.test(l)) return { kind: "eula" };
  let m = /needs Java (\d+) or newer but the image has Java (\d+)/.exec(l);
  if (m) return { kind: "java", need: Number(m[1]), have: Number(m[2]) };
  // "...compiled by a more recent version of the Java Runtime (class file version 65.0), this version of the
  // Java Runtime only recognizes class file versions up to 61.0"
  m = /class file version (\d+)\.\d+\), this version of the Java Runtime only recognizes class file versions up to (\d+)/.exec(l);
  if (m) return { kind: "java", need: Number(m[1]) - 44, have: Number(m[2]) - 44 };
  m = /class file version (\d+)\.\d+/.exec(l);
  if (m && /UnsupportedClassVersionError|more recent version of the Java/.test(l)) return { kind: "java", need: Number(m[1]) - 44 };
  return null;
}

/** The Java major version an image offers, read from its label ("Java 21") or its tag (":21", "java_21"). */
export function javaOf(img: { label: string; image: string }): number | null {
  for (const s of [img.label, img.image]) {
    const m = /java[ _-]?(\d{1,2})\b/i.exec(s) ?? /:(\d{1,2})(?:[-_.].*)?$/.exec(s);
    if (m) return Number(m[1]);
  }
  return null;
}

/** The image to switch to for Java `need`: the oldest one that is new enough (closest to what the game expects). */
export function pickJavaImage<T extends { label: string; image: string }>(images: readonly T[], need: number): T | null {
  let best: { img: T; v: number } | null = null;
  for (const img of images) {
    const v = javaOf(img);
    if (v !== null && v >= need && (!best || v < best.v)) best = { img, v };
  }
  return best?.img ?? null;
}

// ---- server.properties --------------------------------------------------------

/** Parses a Java .properties file into its key/value pairs (comments and blank lines skipped). */
export function parseProperties(text: string): Map<string, string> {
  const out = new Map<string, string>();
  for (const { key, value } of logicalLines(text)) if (key !== null) out.set(key, value);
  return out;
}

interface Logical {
  raw: string; // the physical line(s), joined with \n
  key: string | null;
  value: string;
}

function logicalLines(text: string): Logical[] {
  const phys = text.replace(/\r\n?/g, "\n").split("\n");
  if (phys.length && phys[phys.length - 1] === "") phys.pop();
  const out: Logical[] = [];
  for (let i = 0; i < phys.length; i++) {
    let raw = phys[i] ?? "";
    let joined = raw.replace(/^\s+/, "");
    if (joined === "" || joined.startsWith("#") || joined.startsWith("!")) {
      out.push({ raw, key: null, value: "" });
      continue;
    }
    // A line ending in an odd number of backslashes continues on the next.
    while (/(?:^|[^\\])(?:\\\\)*\\$/.test(joined) && i + 1 < phys.length) {
      i++;
      const next = phys[i] ?? "";
      raw += "\n" + next;
      joined = joined.slice(0, -1) + next.replace(/^\s+/, "");
    }
    const m = /^((?:\\.|[^=:\s\\])*)\s*[=:]?\s*(.*)$/s.exec(joined);
    out.push({ raw, key: unescape(m?.[1] ?? joined), value: unescape(m?.[2] ?? "") });
  }
  return out;
}

function unescape(s: string): string {
  return s.replace(/\\(u[0-9a-fA-F]{4}|.)/g, (_, c: string) => {
    if (c.length === 5) return String.fromCharCode(parseInt(c.slice(1), 16));
    return ({ n: "\n", t: "\t", r: "\r", f: "\f" } as Record<string, string>)[c] ?? c;
  });
}

function escapeValue(v: string): string {
  let out = "";
  for (const ch of v) {
    const c = ch.codePointAt(0) ?? 0;
    if (ch === "\\") out += "\\\\";
    else if (ch === "\n") out += "\\n";
    else if (ch === "\r") out += "\\r";
    else if (ch === "\t") out += "\\t";
    else if (ch === "=" || ch === ":" || ch === "#" || ch === "!") out += "\\" + ch;
    else if (c > 0x7e || c < 0x20) {
      // Old servers read the file as Latin-1; \u escapes work everywhere.
      for (const unit of ch.split("").map((u) => u.charCodeAt(0))) out += "\\u" + unit.toString(16).padStart(4, "0");
    } else out += ch;
  }
  return out.startsWith(" ") ? "\\" + out : out;
}

/**
 * Sets keys in a .properties file, keeping every other line (comments, order, unknown keys) as it was.
 * New keys go at the end.
 */
export function setProperties(text: string, changes: Record<string, string>): string {
  const todo = new Map(Object.entries(changes));
  const lines = logicalLines(text).map((l) => {
    if (l.key === null || !todo.has(l.key)) return l.raw;
    const v = todo.get(l.key) ?? "";
    todo.delete(l.key);
    return `${l.key}=${escapeValue(v)}`;
  });
  for (const [k, v] of todo) lines.push(`${k}=${escapeValue(v)}`);
  return lines.join("\n") + "\n";
}

export interface PropField {
  key: string;
  label: string;
  type: "text" | "number" | "bool" | "select";
  options?: string[];
  min?: number;
  max?: number;
  hint?: string;
  def: string;
  /** A text that may span this many lines (the MOTD has two). */
  lines?: number;
}

/** The settings people change most, grouped. Anything else is under "All settings". */
export const PROPERTY_GROUPS: { title: string; fields: PropField[] }[] = [
  {
    title: "General",
    fields: [
      {
        key: "motd",
        label: "Message of the day",
        type: "text",
        lines: 2,
        def: "A Minecraft Server",
        hint: "Shown in the server list, up to two lines. § codes add colour.",
      },
      { key: "max-players", label: "Max players", type: "number", min: 1, max: 10000, def: "20" },
      {
        key: "online-mode",
        label: "Online mode",
        type: "bool",
        def: "true",
        hint: "Check accounts with Mojang. Turn off only behind a proxy.",
      },
      { key: "white-list", label: "Whitelist", type: "bool", def: "false", hint: "Only listed players can join." },
      { key: "enforce-whitelist", label: "Kick players not on the whitelist", type: "bool", def: "false" },
      { key: "enable-status", label: "Show in server lists", type: "bool", def: "true" },
      { key: "hide-online-players", label: "Hide who is online", type: "bool", def: "false" },
    ],
  },
  {
    title: "Gameplay",
    fields: [
      { key: "gamemode", label: "Game mode", type: "select", options: ["survival", "creative", "adventure", "spectator"], def: "survival" },
      { key: "force-gamemode", label: "Force the game mode on join", type: "bool", def: "false" },
      { key: "difficulty", label: "Difficulty", type: "select", options: ["peaceful", "easy", "normal", "hard"], def: "easy" },
      { key: "hardcore", label: "Hardcore", type: "bool", def: "false" },
      { key: "pvp", label: "PvP", type: "bool", def: "true" },
      {
        key: "allow-flight",
        label: "Allow flight",
        type: "bool",
        def: "false",
        hint: "Stops kicks for flying when mods or plugins allow it.",
      },
      { key: "spawn-protection", label: "Spawn protection radius", type: "number", min: 0, max: 1000, def: "16" },
      { key: "enable-command-block", label: "Command blocks", type: "bool", def: "false" },
      { key: "allow-nether", label: "Nether", type: "bool", def: "true" },
      { key: "spawn-monsters", label: "Monsters", type: "bool", def: "true" },
    ],
  },
  {
    title: "Performance",
    fields: [
      {
        key: "view-distance",
        label: "View distance",
        type: "number",
        min: 2,
        max: 32,
        def: "10",
        hint: "Chunks sent to players. The biggest lever on CPU and memory.",
      },
      {
        key: "simulation-distance",
        label: "Simulation distance",
        type: "number",
        min: 2,
        max: 32,
        def: "10",
        hint: "Chunks where mobs and crops tick.",
      },
      { key: "max-tick-time", label: "Watchdog (ms)", type: "number", min: -1, max: 600000, def: "60000", hint: "-1 turns it off." },
      { key: "network-compression-threshold", label: "Compression threshold", type: "number", min: -1, max: 65535, def: "256" },
      { key: "entity-broadcast-range-percentage", label: "Entity range (%)", type: "number", min: 10, max: 1000, def: "100" },
      { key: "sync-chunk-writes", label: "Synchronous chunk writes", type: "bool", def: "true" },
    ],
  },
];

/** Keys the panel sets itself on every start; editing them here would be overwritten. */
export const MANAGED_KEYS = new Set(["server-port", "query.port"]);

/** Checks a value for a field; returns a message or null. */
export function checkField(f: PropField, v: string): string | null {
  if (f.type === "number") {
    if (!/^-?\d+$/.test(v.trim())) return "Enter a whole number.";
    const n = Number(v);
    if (f.min !== undefined && n < f.min) return `At least ${f.min}.`;
    if (f.max !== undefined && n > f.max) return `At most ${f.max}.`;
  }
  if (f.type === "text" && v.length > 1024) return "Too long.";
  if (f.type === "text" && v.split("\n").length > (f.lines ?? 1)) return f.lines ? `At most ${f.lines} lines.` : "Keep it on one line.";
  return null;
}

// ---- MOTD ------------------------------------------------------------------------

export interface Segment {
  text: string;
  color?: string;
  bold?: boolean;
  italic?: boolean;
  underline?: boolean;
  strike?: boolean;
  obfuscated?: boolean;
}

const MC_COLORS: Record<string, string> = {
  "0": "#000000",
  "1": "#0000AA",
  "2": "#00AA00",
  "3": "#00AAAA",
  "4": "#AA0000",
  "5": "#AA00AA",
  "6": "#FFAA00",
  "7": "#AAAAAA",
  "8": "#555555",
  "9": "#5555FF",
  a: "#55FF55",
  b: "#55FFFF",
  c: "#FF5555",
  d: "#FF55FF",
  e: "#FFFF55",
  f: "#FFFFFF",
};

/** Splits text with § codes into styled runs, line by line. (The game ignores "&" codes, so they stay text.) */
export function motdLines(text: string): Segment[][] {
  return text.split("\n").map((line) => {
    const out: Segment[] = [];
    let cur: Segment = { text: "" };
    const push = () => {
      if (cur.text) out.push(cur);
    };
    for (let i = 0; i < line.length; i++) {
      const ch = line.charAt(i);
      if (ch === "§" && i + 1 < line.length) {
        const code = line.charAt(i + 1).toLowerCase();
        if (code in MC_COLORS || "klmnor".includes(code)) {
          push();
          const base: Segment = code in MC_COLORS || code === "r" ? { text: "" } : { ...cur, text: "" };
          if (code in MC_COLORS) base.color = MC_COLORS[code];
          if (code === "k") base.obfuscated = true;
          if (code === "l") base.bold = true;
          if (code === "m") base.strike = true;
          if (code === "n") base.underline = true;
          if (code === "o") base.italic = true;
          cur = base;
          i++;
          continue;
        }
      }
      cur.text += ch;
    }
    push();
    return out;
  });
}

// ---- Player lists ------------------------------------------------------------------

export interface ListedPlayer {
  uuid: string;
  name: string;
  level?: number;
  bypassesPlayerLimit?: boolean;
  created?: string;
  source?: string;
  expires?: string;
  reason?: string;
}
export interface BannedIP {
  ip: string;
  created?: string;
  source?: string;
  expires?: string;
  reason?: string;
}

export type ListKind = "whitelist" | "ops" | "bans" | "ipbans";

export const LIST_FILES: Record<ListKind, string> = {
  whitelist: "/whitelist.json",
  ops: "/ops.json",
  bans: "/banned-players.json",
  ipbans: "/banned-ips.json",
};

/** Reads a server's JSON list; a missing or broken file is an empty list. */
export function parseList<T>(text: string | undefined): T[] {
  if (!text?.trim()) return [];
  try {
    const v: unknown = JSON.parse(text);
    return Array.isArray(v) ? (v as T[]) : [];
  } catch {
    return [];
  }
}

/** The console command that changes a list while the server runs. */
export function listCommand(kind: ListKind, action: "add" | "remove", who: string, reason?: string): string {
  const r = reason?.trim() ? ` ${reason.trim()}` : "";
  switch (kind) {
    case "whitelist":
      return `whitelist ${action} ${who}`;
    case "ops":
      return action === "add" ? `op ${who}` : `deop ${who}`;
    case "bans":
      return action === "add" ? `ban ${who}${r}` : `pardon ${who}`;
    case "ipbans":
      return action === "add" ? `ban-ip ${who}${r}` : `pardon-ip ${who}`;
  }
}

/** Minecraft's date format in ban lists: "2026-11-20 10:44:00 +0000". */
export function mcDate(d = new Date()): string {
  const p = (n: number) => String(n).padStart(2, "0");
  return `${d.getUTCFullYear()}-${p(d.getUTCMonth() + 1)}-${p(d.getUTCDate())} ${p(d.getUTCHours())}:${p(d.getUTCMinutes())}:${p(d.getUTCSeconds())} +0000`;
}

export const isPlayerName = (s: string) => /^[A-Za-z0-9_]{1,16}$/.test(s);
export const isIP = (s: string) => /^(\d{1,3}\.){3}\d{1,3}$/.test(s) || /^[0-9a-fA-F:]+:[0-9a-fA-F:]*$/.test(s);

/** Folders a world named `level` spreads over (Vanilla keeps the other dimensions inside it). */
export function worldFolders(level: string): string[] {
  const l = level.trim() || "world";
  return [l, `${l}_nether`, `${l}_the_end`];
}
