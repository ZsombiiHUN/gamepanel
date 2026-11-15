// @vitest-environment jsdom
import { describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { server } from "../test/fixtures";
import { FakeApi, problem, renderApp } from "../test/harness";

// CodeMirror needs real layout; the editor's behaviour is not what these tests are about.
vi.mock("@uiw/react-codemirror", () => ({
  default: ({ value, onChange }: { value: string; onChange: (v: string) => void }) => <textarea aria-label="editor" value={value} onChange={(e) => onChange(e.target.value)} />,
}));

// The page needs no live socket here.
vi.mock("../api/ws", () => ({ useServerSocket: () => ({ status: "open", state: null, stats: null }) }));

const UUID = "11111111-2222-4333-8444-555555555555";
const minecraft = { ...server({ name: "Survival" }), uuid: UUID };
type Entry = { name: string; dir: boolean; size: number; mod_unix: number };
const f = (name: string): Entry => ({ name, dir: false, size: 12, mod_unix: 1_700_000_000 });
const d = (name: string): Entry => ({ name, dir: true, size: 0, mod_unix: 1_700_000_000 });

function fileApi(tree: Record<string, Entry[] | Response>, extra: Record<string, unknown> = {}) {
  return new FakeApi({
    "GET /api/servers": [minecraft],
    [`GET /api/servers/${UUID}`]: minecraft,
    [`GET /api/servers/${UUID}/files`]: (c) => {
      const out = tree[c.query.get("path") ?? "/"];
      return out ?? problem(404, "no such directory");
    },
    ...extra,
  });
}
const listings = (api: FakeApi) => api.callsTo(`GET /api/servers/${UUID}/files`).map((c) => c.query.get("path"));

const mc = {
  "/": [d("config"), d("plugins"), d("logs"), f("server.properties"), f("ops.json")],
  "/config": [f("paper-global.yml")],
  "/plugins": [d("Essentials"), d("WorldEdit")],
  "/plugins/Essentials": [f("config.yml"), f("userdata.json")],
  "/plugins/WorldEdit": [f("config.yml"), f("schematic.yml")],
  "/logs": [f("latest.log")],
};
const searchBox = () => screen.getByRole("searchbox", { name: "Search files" });
const names = () => within(screen.getByRole("table")).getAllByRole("row").slice(1).map((r) => within(r).getAllByRole("button")[0]!.textContent!.trim());

async function open(tree: Record<string, Entry[] | Response> = mc, extra: Record<string, unknown> = {}, ready = "server.properties") {
  const api = fileApi(tree, extra);
  const view = renderApp(api, `/servers/${UUID}?tab=files`);
  await screen.findByText(ready);
  return { api, ...view };
}

describe("Files tab: browsing", () => {
  it("lists folders before files", async () => {
    await open();
    expect(names()).toEqual(["config", "logs", "plugins", "ops.json", "server.properties"]);
  });
  it("opens a folder, shows the breadcrumb, and goes back up", async () => {
    await open();
    await userEvent.click(screen.getByRole("button", { name: "plugins" }));
    await screen.findByText("Essentials");
    expect(within(screen.getByRole("navigation", { name: "Breadcrumb" })).getByText("plugins")).toHaveAttribute("aria-current", "page");
    await userEvent.click(screen.getByRole("button", { name: /Parent directory/ }));
    await screen.findByText("server.properties");
  });
  it("says when a folder is empty", async () => {
    await open({ ...mc, "/logs": [] });
    await userEvent.click(screen.getByRole("button", { name: "logs" }));
    expect(await screen.findByText("Empty directory.")).toBeInTheDocument();
  });
});

describe("Files tab: search", () => {
  it("finds matches in subfolders and shows where each one lives", async () => {
    await open();
    await userEvent.type(searchBox(), "config");
    await waitFor(() => expect(screen.getByText(/3 matches/)).toBeInTheDocument());
    expect(names()).toEqual(["config, in /", "config.yml, in /plugins/Essentials", "config.yml, in /plugins/WorldEdit"]);
  });
  it("is case-insensitive", async () => {
    await open();
    await userEvent.type(searchBox(), "ESSENTIALS");
    await waitFor(() => expect(screen.getByText(/1 match\b/)).toBeInTheDocument());
  });
  it("waits for a pause in typing before crawling", async () => {
    const { api } = await open();
    await userEvent.type(searchBox(), "config");
    await waitFor(() => expect(screen.getByText(/3 matches/)).toBeInTheDocument());
    // one crawl: each folder listed once beyond the initial root listing (which is cached and reused)
    const counts = listings(api).reduce<Record<string, number>>((m, p) => ((m[p!] = (m[p!] ?? 0) + 1), m), {});
    expect(Object.values(counts).every((n) => n === 1)).toBe(true);
  });
  it("explains when nothing matches, naming where it looked", async () => {
    await open();
    await userEvent.type(searchBox(), "nope");
    expect(await screen.findByText(/No files or folders named “nope” in any folder/)).toBeInTheDocument();
  });
  it("searches only inside the current folder when you are in one", async () => {
    const { api } = await open();
    await userEvent.click(screen.getByRole("button", { name: "plugins" }));
    await screen.findByText("Essentials");
    const before = listings(api).length;
    expect(searchBox()).toHaveAttribute("placeholder", "Search in this folder");
    await userEvent.type(searchBox(), "config");
    await waitFor(() => expect(screen.getByText(/2 matches/)).toBeInTheDocument());
    const crawled = listings(api).slice(before);
    expect(crawled.every((p) => p!.startsWith("/plugins"))).toBe(true);
  });
  it("opens a folder result and clears the search", async () => {
    await open();
    await userEvent.type(searchBox(), "Essentials");
    await userEvent.click(await screen.findByRole("button", { name: /^Essentials/ }));
    await screen.findByText("userdata.json");
    expect(searchBox()).toHaveValue("");
  });
  it("opens a file result in the editor", async () => {
    const { api } = await open(mc, { [`GET /api/servers/${UUID}/files/content`]: { content: "hello: world\n" } });
    await userEvent.type(searchBox(), "latest");
    await userEvent.click(await screen.findByRole("button", { name: /^latest\.log/ }));
    expect(await screen.findByRole("textbox", { name: "editor" })).toHaveValue("hello: world\n");
    expect(screen.getByRole("heading", { name: "/logs/latest.log" })).toBeInTheDocument();
    expect(api.callsTo(`GET /api/servers/${UUID}/files/content`)[0]!.query.get("path")).toBe("/logs/latest.log");
  });
  it("Escape clears the search and shows the folder again", async () => {
    await open();
    await userEvent.type(searchBox(), "config");
    await screen.findByText(/3 matches/);
    await userEvent.keyboard("{Escape}");
    expect(searchBox()).toHaveValue("");
    expect(names()).toEqual(["config", "logs", "plugins", "ops.json", "server.properties"]);
  });
  it("skips a folder it cannot read and still finds the rest", async () => {
    const tree = { ...mc, "/plugins/Essentials": problem(403, "permission denied") };
    await open(tree);
    await userEvent.type(searchBox(), "config");
    await waitFor(() => expect(screen.getByText(/2 matches/)).toBeInTheDocument());
  });
  it("says when it stopped early because the tree is huge", async () => {
    const wide: Record<string, Entry[]> = { "/": Array.from({ length: 250 }, (_, i) => d(`dir${i}`)) };
    for (let i = 0; i < 250; i++) wide[`/dir${i}`] = [];
    const { api } = await open(wide, {}, "dir0");
    await userEvent.type(searchBox(), "zzz");
    expect(await screen.findByText(/stopped early/, undefined, { timeout: 5000 })).toBeInTheDocument();
    expect(listings(api).length).toBeLessThanOrEqual(201);
  });
  it("deletes a result by its full path, and drops it from the results", async () => {
    // A real server stops listing what was deleted; the page refetches after the delete.
    const tree = structuredClone(mc) as Record<string, Entry[]>;
    const { api } = await open(tree, {
      [`DELETE /api/servers/${UUID}/files`]: (c: { query: URLSearchParams }) => {
        const path = c.query.get("path")!;
        const at = path.lastIndexOf("/");
        const dir = path.slice(0, at) || "/";
        tree[dir] = tree[dir]!.filter((e) => e.name !== path.slice(at + 1));
      },
    });
    await userEvent.type(searchBox(), "config.yml");
    await screen.findByText(/2 matches/);
    await userEvent.click(screen.getByRole("button", { name: "Delete plugins/WorldEdit/config.yml" }));
    await userEvent.click(within(screen.getByLabelText(/^Delete config.yml/, { selector: "dialog" })).getByRole("button", { name: "Delete", hidden: true }));
    await waitFor(() => expect(api.callsTo(`DELETE /api/servers/${UUID}/files`)).toHaveLength(1));
    expect(api.callsTo(`DELETE /api/servers/${UUID}/files`)[0]!.query.get("path")).toBe("/plugins/WorldEdit/config.yml");
    await waitFor(() => expect(screen.getByText(/1 match\b/)).toBeInTheDocument());
  });
});
