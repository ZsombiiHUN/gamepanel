/** Fill {{VAR}} placeholders the way the node does; unknown ones stay as they are. */
export function fillCommand(command: string, values: Record<string, string>) {
  return command.replace(/\{\{\s*([A-Za-z_][A-Za-z0-9_]*)\s*\}\}/g, (all, name: string) => values[name] ?? all);
}

/** Rule names and arguments, e.g. "required|in:a,b" -> [["required",""],["in","a,b"]]. Regexes are skipped. */
function rules(s: string): [string, string][] {
  // A regex may itself contain "|", so cut it out (up to its closing delimiter) before splitting.
  return s
    .replace(/regex:([^\w\s]).*\1[a-z]*(?=\||$)/g, "")
    .split("|")
    .filter((r) => r.trim())
    .map((r) => {
      const i = r.indexOf(":");
      return i < 0 ? [r.trim().toLowerCase(), ""] : [r.slice(0, i).trim().toLowerCase(), r.slice(i + 1)];
    });
}

export type InputKind = { type: "text" } | { type: "number" } | { type: "select"; options: string[]; labels?: Record<string, string> };

/** The best input for a variable: a toggle-like select for booleans, a select for "in:" lists, else text. */
export function inputKind(ruleText: string, def: string): InputKind {
  const rs = rules(ruleText);
  const inList = rs.find(([n]) => n === "in");
  if (inList) return { type: "select", options: inList[1].split(",").map((x) => x.trim().replace(/^["']|["']$/g, "")) };
  if (rs.some(([n]) => n === "boolean" || n === "bool")) {
    // Keep the egg's own spelling: some read "1"/"0", others "true"/"false".
    const numeric = def === "1" || def === "0";
    const [on, off] = numeric ? ["1", "0"] : ["true", "false"];
    return { type: "select", options: [on, off], labels: { [on]: "Enabled", [off]: "Disabled" } };
  }
  if (rs.some(([n]) => n === "integer" || n === "int" || n === "numeric")) return { type: "number" };
  return { type: "text" };
}
