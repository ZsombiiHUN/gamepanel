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
const minecraft = { ...server({ name: "Survival" }), uuid: UUID, permissions: ["files.read", "files.write", "console.read"] };
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
    expect(await screen.findByText(/^Empty directory\./)).toBeInTheDocument();
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
      [`POST /api/servers/${UUID}/files/delete`]: (c: { body: unknown }) => {
        for (const path of (c.body as { paths: string[] }).paths) {
          const at = path.lastIndexOf("/");
          const dir = path.slice(0, at) || "/";
          tree[dir] = tree[dir]!.filter((e) => e.name !== path.slice(at + 1));
        }
      },
    });
    await userEvent.type(searchBox(), "config.yml");
    await screen.findByText(/2 matches/);
    await userEvent.click(screen.getByRole("button", { name: "Actions for plugins/WorldEdit/config.yml" }));
    await userEvent.click(screen.getByRole("menuitem", { name: "Delete" }));
    await userEvent.click(within(screen.getByRole("dialog", { name: /^Delete/, hidden: true })).getByRole("button", { name: "Delete", hidden: true }));
    await waitFor(() => expect(api.callsTo(`POST /api/servers/${UUID}/files/delete`)).toHaveLength(1));
    expect(api.callsTo(`POST /api/servers/${UUID}/files/delete`)[0]!.body).toEqual({ paths: ["/plugins/WorldEdit/config.yml"] });
    await waitFor(() => expect(screen.getByText(/1 match\b/)).toBeInTheDocument());
  });
});

describe("Files tab: managing files", () => {
  const dialog = (name: RegExp) => within(screen.getByRole("dialog", { name, hidden: true }));
  it("renames from the row menu", async () => {
    const { api } = await open(mc, { [`POST /api/servers/${UUID}/files/move`]: undefined });
    await userEvent.click(screen.getByRole("button", { name: "Actions for ops.json" }));
    await userEvent.click(screen.getByRole("menuitem", { name: "Rename" }));
    const name = dialog(/^Rename/).getByRole("textbox", { name: "New name", hidden: true });
    await userEvent.clear(name);
    await userEvent.type(name, "admins.json");
    await userEvent.click(dialog(/^Rename/).getByRole("button", { name: "Rename", hidden: true }));
    await waitFor(() => expect(api.callsTo(`POST /api/servers/${UUID}/files/move`)[0]?.body).toEqual({ moves: [{ from: "/ops.json", to: "/admins.json" }] }));
  });
  it("acts on several selected items at once", async () => {
    const { api } = await open(mc, { [`POST /api/servers/${UUID}/files/compress`]: { path: "/archive.zip" } });
    await userEvent.click(screen.getByRole("checkbox", { name: "Select config" }));
    await userEvent.click(screen.getByRole("checkbox", { name: "Select ops.json" }));
    const bar = screen.getByRole("toolbar", { name: "Selected items" });
    expect(within(bar).getByText("2 selected")).toBeInTheDocument();
    await userEvent.click(within(bar).getByRole("button", { name: "Compress" }));
    await userEvent.click(dialog(/^Compress/).getByRole("button", { name: "Create archive", hidden: true }));
    await waitFor(() => expect(api.callsTo(`POST /api/servers/${UUID}/files/compress`)[0]?.body).toEqual({ root: "/", paths: ["/config", "/ops.json"], format: "zip" }));
  });
  it("selects everything in the folder", async () => {
    await open();
    await userEvent.click(screen.getByRole("checkbox", { name: "Select all" }));
    expect(screen.getByText("5 selected")).toBeInTheDocument();
  });
  it("offers extraction for archives and downloads for files", async () => {
    await open({ ...mc, "/": [...mc["/"]!, f("world.zip")] });
    expect(screen.getByRole("link", { name: "Download world.zip" })).toHaveAttribute("href", `/api/servers/${UUID}/files/download?path=%2Fworld.zip`);
    await userEvent.click(screen.getByRole("button", { name: "Actions for world.zip" }));
    expect(screen.getByRole("menuitem", { name: "Extract here" })).toBeInTheDocument();
    expect(screen.queryByRole("menuitem", { name: "Compress" })).toBeNull();
  });
  it("shows no write actions to someone who may only read", async () => {
    const readOnly = { ...minecraft, permissions: ["files.read"] };
    const api = fileApi(mc, { "GET /api/servers": [readOnly], [`GET /api/servers/${UUID}`]: readOnly });
    renderApp(api, `/servers/${UUID}?tab=files`);
    await screen.findByText("server.properties");
    expect(screen.queryByRole("button", { name: "Upload files" })).toBeNull();
    await userEvent.click(screen.getByRole("button", { name: "Actions for ops.json" }));
    expect(within(screen.getByRole("menu", { name: "Actions for ops.json" })).getAllByRole("menuitem").map((m) => m.textContent?.trim())).toEqual(["Download"]);
  });
  it("creates a folder", async () => {
    const { api } = await open(mc, { [`POST /api/servers/${UUID}/files/folder`]: undefined });
    await userEvent.click(screen.getByRole("button", { name: "New folder" }));
    await userEvent.type(dialog(/^New folder/).getByRole("textbox", { name: "Name", hidden: true }), "worlds");
    await userEvent.click(dialog(/^New folder/).getByRole("button", { name: "Create folder", hidden: true }));
    await waitFor(() => expect(api.callsTo(`POST /api/servers/${UUID}/files/folder`)[0]?.body).toEqual({ path: "/worlds" }));
  });
});
