// @vitest-environment jsdom
import { describe, expect, it } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { FakeApi, renderApp } from "../test/harness";
import { bob, sampleServers } from "../test/fixtures";

async function open(path = "/", over: Record<string, unknown> = {}) {
  const servers = sampleServers();
  const api = new FakeApi({ "GET /api/servers": servers, ...over });
  const view = renderApp(api, path);
  await screen.findByRole("navigation", { name: "Servers" });
  await screen.findAllByRole("link", { name: /Skyblock/ });
  return { api, servers, ...view };
}
// A closed <dialog> has no accessible name, so find it directly.
const palette = () => document.querySelector<HTMLDialogElement>('dialog[aria-label="Quick switcher"]')!;
/** Titles of the visible options, in order. */
const options = () =>
  within(screen.getByRole("listbox", { name: "Results", hidden: true }))
    .getAllByRole("option", { hidden: true })
    .map((o) => o.querySelector(".flex-1")!.textContent.trim());

describe("sidebar", () => {
  it("scrolls only the server list, so admin links and the account stay reachable with many servers", async () => {
    await open();
    const list = screen.getByRole("navigation", { name: "Servers" });
    const adminNav = screen.getByRole("navigation", { name: "Admin" });
    // The list is the scroll container and does not contain the pinned sections.
    expect(list.className).toMatch(/overflow-y-auto/);
    expect(list.className).toMatch(/min-h-0/);
    expect(list).not.toContainElement(screen.getByRole("link", { name: /Nodes/ }));
    expect(adminNav.className).toMatch(/shrink-0/);
    const aside = screen.getByRole("complementary", { name: "Navigation", hidden: true });
    expect(aside.className).toMatch(/overflow-hidden/); // never scrolls as a whole
    expect(aside.className).not.toMatch(/overflow-y-auto/);
    const account = within(aside).getByRole("link", { name: /admin@example.com/ });
    expect(account.closest("div")!.className).toMatch(/shrink-0/);
    expect(within(aside).getByRole("button", { name: "Sign out" })).toBeInTheDocument();
  });

  it("lists servers alphabetically", async () => {
    await open();
    const names = within(screen.getByRole("navigation", { name: "Servers" }))
      .getAllByRole("link")
      .map((l) => l.textContent.trim())
      .filter((t) => !/All servers|New server/.test(t));
    expect(names).toEqual(["Creative Plots", "Rust Wipe Friday", "Server 2", "Server 10", "Skyblock", "Survival SMP", "Valheim Friends"]);
  });

  it("shows admin links to admins only", async () => {
    await open();
    expect(screen.getByRole("navigation", { name: "Admin" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Audit log" })).toBeInTheDocument();
  });
  it("hides admin links from regular users", async () => {
    await open("/", { "GET /api/auth/me": bob });
    expect(screen.queryByRole("navigation", { name: "Admin" })).not.toBeInTheDocument();
    expect(screen.queryByRole("link", { name: /New server/ })).not.toBeInTheDocument();
  });
});

describe("keyboard shortcuts", () => {
  it("/ focuses the page's search box", async () => {
    await open();
    expect(screen.getByRole("searchbox", { name: "Filter servers" })).not.toHaveFocus();
    await userEvent.keyboard("/");
    expect(screen.getByRole("searchbox", { name: "Filter servers" })).toHaveFocus();
  });
  it("/ is typed normally when already in a text field", async () => {
    await open();
    await userEvent.click(screen.getByRole("searchbox", { name: "Filter servers" }));
    await userEvent.keyboard("a/b");
    expect(screen.getByRole("searchbox", { name: "Filter servers" })).toHaveValue("a/b");
  });
  it("/ does nothing on pages without a search box", async () => {
    await open("/nodes", { "GET /api/nodes": [] });
    await screen.findByRole("heading", { name: "Nodes" });
    await userEvent.keyboard("/");
    expect(document.body).toHaveFocus();
  });
  it("Ctrl+K and Cmd+K open the quick switcher, and again closes it", async () => {
    await open();
    await userEvent.keyboard("{Control>}k{/Control}");
    expect(palette()).toHaveAttribute("open");
    await userEvent.keyboard("{Control>}k{/Control}");
    expect(palette()).not.toHaveAttribute("open");
    await userEvent.keyboard("{Meta>}k{/Meta}");
    expect(palette()).toHaveAttribute("open");
  });
  it("the sidebar search button opens it too", async () => {
    await open();
    await userEvent.click(screen.getByRole("button", { name: "Search servers and pages" }));
    expect(palette()).toHaveAttribute("open");
    expect(screen.getByRole("combobox", { name: "Search servers and pages" })).toHaveFocus();
  });
});

describe("quick switcher", () => {
  async function opened(path = "/") {
    const ctx = await open(path);
    await userEvent.keyboard("{Control>}k{/Control}");
    return ctx;
  }

  it("lists every server alphabetically, then the pages", async () => {
    await opened();
    const o = options();
    expect(o).toEqual([
      "Creative Plots",
      "Rust Wipe Friday",
      "Server 2",
      "Server 10",
      "Skyblock",
      "Survival SMP",
      "Valheim Friends",
      "All servers",
      "Account",
      "New server",
      "Nodes",
      "Eggs",
      "Database hosts",
      "Audit log",
    ]);
  });
  it("narrows as you type and matches game, node and port", async () => {
    await opened();
    await userEvent.type(screen.getByRole("combobox"), "terraria");
    expect(options()).toEqual(["Server 2", "Server 10"]);
    await userEvent.clear(screen.getByRole("combobox"));
    await userEvent.type(screen.getByRole("combobox"), "us-east");
    expect(options()).toEqual(["Rust Wipe Friday"]);
  });
  it("finds admin pages by what they are for", async () => {
    await opened();
    await userEvent.type(screen.getByRole("combobox"), "security");
    expect(options()).toEqual(["Audit log"]);
  });
  it("says so when nothing matches", async () => {
    await opened();
    await userEvent.type(screen.getByRole("combobox"), "qqq");
    expect(screen.getByText(/Nothing matches “qqq”/)).toBeInTheDocument();
  });
  it("moves with the arrow keys and opens with Enter", async () => {
    const { router, servers } = await opened();
    await userEvent.type(screen.getByRole("combobox"), "sky");
    await userEvent.keyboard("{Enter}");
    const sky = servers.find((s) => s.name === "Skyblock")!;
    await waitFor(() => expect(router.state.location.pathname).toBe(`/servers/${sky.uuid}`));
    expect(palette()).not.toHaveAttribute("open");
  });
  it("tracks the highlighted option for screen readers and clamps at the ends", async () => {
    await opened();
    const box = screen.getByRole("combobox");
    const first = screen.getAllByRole("option", { hidden: true })[0]!;
    expect(box).toHaveAttribute("aria-activedescendant", first.id);
    expect(first).toHaveAttribute("aria-selected", "true");
    await userEvent.keyboard("{ArrowUp}");
    expect(first).toHaveAttribute("aria-selected", "true");
    await userEvent.keyboard("{ArrowDown}");
    expect(screen.getAllByRole("option", { hidden: true })[1]).toHaveAttribute("aria-selected", "true");
    await userEvent.keyboard("{End}");
    const all = screen.getAllByRole("option", { hidden: true });
    expect(all.at(-1)).toHaveAttribute("aria-selected", "true");
    await userEvent.keyboard("{ArrowDown}");
    expect(all.at(-1)).toHaveAttribute("aria-selected", "true");
  });
  it("Enter on an empty result list does nothing", async () => {
    const { router } = await opened();
    await userEvent.type(screen.getByRole("combobox"), "qqq{Enter}");
    expect(router.state.location.pathname).toBe("/");
    expect(palette()).toHaveAttribute("open");
  });
  it("opens a page chosen with the mouse", async () => {
    const { router } = await opened();
    await userEvent.type(screen.getByRole("combobox"), "audit");
    await userEvent.click(screen.getByRole("option", { name: "Audit log", hidden: true }));
    await waitFor(() => expect(router.state.location.pathname).toBe("/audit"));
  });
  it("offers the current server's tabs first when on a server", async () => {
    const servers = sampleServers();
    const sky = servers.find((s) => s.name === "Skyblock")!;
    const api = new FakeApi({
      "GET /api/servers": servers,
      [`GET /api/servers/${sky.uuid}`]: sky,
      [`GET /api/servers/${sky.uuid}/files`]: [],
    });
    renderApp(api, `/servers/${sky.uuid}?tab=settings`);
    await screen.findByRole("navigation", { name: "Servers" });
    await userEvent.keyboard("{Control>}k{/Control}");
    await userEvent.type(screen.getByRole("combobox"), "files");
    expect(options()[0]).toBe("Files");
    expect(screen.getByText("This server (Skyblock)")).toBeInTheDocument();
  });
  it("leaves out admin pages for regular users", async () => {
    await open("/", { "GET /api/auth/me": bob });
    await userEvent.keyboard("{Control>}k{/Control}");
    await userEvent.type(screen.getByRole("combobox"), "audit");
    expect(screen.getByText(/Nothing matches/)).toBeInTheDocument();
  });
});
