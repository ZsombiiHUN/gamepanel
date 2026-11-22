import { readdirSync, readFileSync } from "node:fs";
import { join, relative } from "node:path";
import ts from "typescript";
import { describe, expect, it } from "vitest";
import en from "./locales/en/translation.json";

const SRC = join(import.meta.dirname, "..");

// Props that people read. A literal string in one of these belongs in the translation file.
const TEXT_PROPS = new Set([
  "title",
  "label",
  "placeholder",
  "aria-label",
  "alt",
  "desc",
  "description",
  "hint",
  "sub",
  "body",
  "note",
  "confirmLabel",
  "submitLabel",
]);

function sources(dir: string): string[] {
  return readdirSync(dir, { withFileTypes: true }).flatMap((e) => {
    const p = join(dir, e.name);
    if (e.isDirectory()) return e.name === "test" ? [] : sources(p);
    return e.name.endsWith(".tsx") && !e.name.endsWith(".test.tsx") ? [p] : [];
  });
}

/** Text written straight into JSX, which a translation would never reach. */
function untranslated(file: string): string[] {
  const sf = ts.createSourceFile(file, readFileSync(file, "utf8"), ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);
  const found: string[] = [];
  const where = (n: ts.Node) => `${relative(SRC, file)}:${sf.getLineAndCharacterOfPosition(n.getStart()).line + 1}`;
  const visit = (n: ts.Node) => {
    if (ts.isJsxText(n) && /[A-Za-z]{2,}/.test(n.text.replace(/&#?\w+;/g, ""))) found.push(`${where(n)} ${n.text.trim()}`);
    if (ts.isJsxAttribute(n) && n.initializer && ts.isStringLiteral(n.initializer) && TEXT_PROPS.has(n.name.getText())) {
      if (/[A-Za-z]{2,}/.test(n.initializer.text)) found.push(`${where(n)} ${n.name.getText()}="${n.initializer.text}"`);
    }
    ts.forEachChild(n, visit);
  };
  visit(sf);
  return found;
}

/** Every leaf key, with plural suffixes folded into their base key. */
function leaves(obj: object, prefix = ""): string[] {
  return Object.entries(obj).flatMap(([k, v]) => (typeof v === "string" ? [prefix + k] : leaves(v as object, `${prefix}${k}.`)));
}

describe("translations", () => {
  it("has no UI text outside the translation file", () => {
    // Use t("…") or <Trans>; see "Translations" in CONTRIBUTING.md.
    expect(sources(SRC).flatMap(untranslated)).toEqual([]);
  });

  it("uses every English key", () => {
    const code = sources(SRC)
      .concat(readdirSync(join(SRC, "lib")).map((f) => join(SRC, "lib", f)))
      .concat(readdirSync(join(SRC, "hooks")).map((f) => join(SRC, "hooks", f)))
      .map((f) => readFileSync(f, "utf8"))
      .join("\n");
    // Keys built at runtime (`tabs.${id}`) are found by their prefix.
    const dynamic = [...code.matchAll(/[`"]([a-zA-Z]+(?:\.[a-zA-Z]+)*)\.\$\{/g)].map((m) => `${m[1] ?? ""}.`);
    const unused = leaves(en)
      .map((k) => k.replace(/_(one|other)$/, ""))
      .filter((k) => !code.includes(`"${k}"`) && !dynamic.some((d) => k.startsWith(d)));
    expect([...new Set(unused)]).toEqual([]);
  });
});
