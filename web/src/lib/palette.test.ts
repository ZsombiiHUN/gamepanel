// @vitest-environment jsdom
import { describe, expect, it } from "vitest";
import { isTypingTarget, rankItems, type PaletteItem } from "./palette";

const item = (label: string, keywords = "", group = "Servers"): PaletteItem => ({ id: label, label, keywords, group });
const labels = (xs: PaletteItem[]) => xs.map((x) => x.label);

describe("rankItems", () => {
  const items = [
    item("Creative Plots", "Paper Minecraft eu-west-1 25566"),
    item("Survival SMP", "Paper Minecraft eu-west-1 25565"),
    item("Rust Wipe Friday", "Rust us-east-1 25569"),
    item("Nodes", "machines daemon", "Pages"),
  ];

  it("keeps the original order for an empty query", () => {
    expect(labels(rankItems(items, ""))).toEqual(labels(items));
    expect(labels(rankItems(items, "  "))).toEqual(labels(items));
  });
  it("filters to items where every word matches", () => {
    expect(labels(rankItems(items, "paper survival"))).toEqual(["Survival SMP"]);
    expect(rankItems(items, "zzz")).toEqual([]);
  });
  it("matches keywords such as game, node and port", () => {
    expect(labels(rankItems(items, "us-east"))).toEqual(["Rust Wipe Friday"]);
    expect(labels(rankItems(items, "25565"))).toEqual(["Survival SMP"]);
  });
  it("puts a label that starts with the word before one that merely contains it", () => {
    const list = [item("Backups of Rust"), item("Rust Wipe Friday"), item("Trusted")];
    expect(labels(rankItems(list, "rust"))).toEqual(["Rust Wipe Friday", "Backups of Rust", "Trusted"]);
  });
  it("puts label matches before keyword-only matches", () => {
    const list = [item("Alpha", "files"), item("Files")];
    expect(labels(rankItems(list, "files"))).toEqual(["Files", "Alpha"]);
  });
  it("is stable for equal scores", () => {
    const list = [item("Server B"), item("Server A")];
    expect(labels(rankItems(list, "server"))).toEqual(["Server B", "Server A"]);
  });
  it("does not mutate its input", () => {
    const copy = [...items];
    rankItems(items, "rust");
    expect(items).toEqual(copy);
  });
});

describe("isTypingTarget", () => {
  it("is true for form fields and editors", () => {
    for (const html of [
      "<input>",
      "<textarea></textarea>",
      "<select></select>",
      '<div contenteditable="true"></div>',
      '<div class="cm-editor"><div class="cm-content" id="inner"></div></div>',
    ]) {
      document.body.innerHTML = html;
      const el = (document.getElementById("inner") ?? document.body.firstElementChild) as HTMLElement;
      // jsdom does not implement isContentEditable; emulate it.
      if (el.getAttribute("contenteditable")) Object.defineProperty(el, "isContentEditable", { value: true });
      expect(isTypingTarget(el), html).toBe(true);
    }
  });
  it("is false for buttons, links and the page itself", () => {
    document.body.innerHTML = "<button>x</button><a href='#'>y</a>";
    expect(isTypingTarget(document.querySelector("button"))).toBe(false);
    expect(isTypingTarget(document.querySelector("a"))).toBe(false);
    expect(isTypingTarget(document.body)).toBe(false);
    expect(isTypingTarget(null)).toBe(false);
  });
});
