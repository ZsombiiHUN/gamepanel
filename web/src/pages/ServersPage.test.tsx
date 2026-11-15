// @vitest-environment jsdom
import { describe, expect, it } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { FakeApi, renderApp } from "../test/harness";
import { bob, sampleServers } from "../test/fixtures";

async function open(api = new FakeApi({ "GET /api/servers": sampleServers() })) {
  const view = renderApp(api, "/");
  await screen.findByRole("heading", { name: "Servers" });
  await screen.findByText("Creative Plots", { selector: "a" });
  return { api, ...view };
}
const cards = () => screen.getAllByRole("article").map((a) => within(a).getAllByRole("link")[0]!.textContent);
const filter = () => screen.getByRole("searchbox", { name: "Filter servers" });

describe("Servers page", () => {
  it("lists servers alphabetically with numbers in natural order", async () => {
    await open();
    expect(cards()).toEqual(["Creative Plots", "Rust Wipe Friday", "Server 2", "Server 10", "Skyblock", "Survival SMP", "Valheim Friends"]);
  });

  it("does not reorder cards when a server changes state", async () => {
    const list = sampleServers();
    const api = new FakeApi({ "GET /api/servers": () => list });
    const { client } = await open(api);
    const before = cards();
    list.find((s) => s.name === "Skyblock")!.state = "running";
    await client.invalidateQueries({ queryKey: ["servers"] });
    await waitFor(() => expect(screen.getAllByText("Running").length).toBeGreaterThan(1));
    expect(cards()).toEqual(before);
  });

  it("shows the game and port on each card", async () => {
    await open();
    const card = screen.getByText("Valheim Friends", { selector: "a" }).closest("article")!;
    expect(within(card).getByText(/Valheim · Port 25568/)).toBeInTheDocument();
  });

  it("summarizes how many servers are in each state", async () => {
    await open();
    expect(screen.getByText("1 running, 1 starting or installing, 1 need attention, 4 stopped")).toBeInTheDocument();
  });

  describe("filter", () => {
    it.each([
      ["name", "sky", ["Skyblock"]],
      ["name, any case", "VALHEIM", ["Valheim Friends"]],
      ["game", "terraria", ["Server 2", "Server 10"]],
      ["node", "us-east", ["Rust Wipe Friday"]],
      ["port", "25569", ["Rust Wipe Friday"]],
      ["status", "running", ["Survival SMP"]],
      ["several words", "terraria 25571", ["Server 2"]],
    ])("finds servers by %s", async (_n, q, want) => {
      await open();
      await userEvent.type(filter(), q);
      expect(cards()).toEqual(want);
    });

    it("explains an empty result and offers a way out", async () => {
      await open();
      await userEvent.type(filter(), "zzz");
      expect(screen.queryAllByRole("article")).toHaveLength(0);
      expect(screen.getByText("No servers match")).toBeInTheDocument();
      expect(screen.getByText(/“zzz”/)).toBeInTheDocument();
      await userEvent.click(screen.getByRole("button", { name: /Clear filter/ }));
      expect(filter()).toHaveValue("");
      expect(screen.getAllByRole("article")).toHaveLength(7);
    });

    it("clears on Escape", async () => {
      await open();
      await userEvent.type(filter(), "sky");
      expect(cards()).toEqual(["Skyblock"]);
      await userEvent.keyboard("{Escape}");
      expect(filter()).toHaveValue("");
      expect(screen.getAllByRole("article")).toHaveLength(7);
    });

    it("hides the add-server tile while filtering, since it is not a result", async () => {
      await open();
      expect(screen.getAllByRole("link", { name: "New server" }).length).toBeGreaterThan(1);
      await userEvent.type(filter(), "sky");
      expect(screen.getAllByRole("link", { name: "New server" })).toHaveLength(2); // sidebar + header, no tile
    });
  });

  describe("status chips", () => {
    it("filter by group and say which is selected", async () => {
      await open();
      const group = screen.getByRole("group", { name: "Filter by status" });
      const stopped = within(group).getByRole("button", { name: /^Stopped/ });
      expect(within(group).getByRole("button", { name: /^All/ })).toHaveAttribute("aria-pressed", "true");
      await userEvent.click(stopped);
      expect(stopped).toHaveAttribute("aria-pressed", "true");
      expect(cards()).toEqual(["Creative Plots", "Rust Wipe Friday", "Server 2", "Skyblock"]);
    });
    it("show counts, and omit groups with no servers", async () => {
      await open();
      const group = screen.getByRole("group", { name: "Filter by status" });
      expect(within(group).getByRole("button", { name: /^All\s*7/ })).toBeInTheDocument();
      expect(within(group).getByRole("button", { name: /^Running\s*1/ })).toBeInTheDocument();
      expect(within(group).getByRole("button", { name: /^Needs attention\s*1/ })).toBeInTheDocument();
    });
    it("combine with the text filter", async () => {
      await open();
      await userEvent.click(within(screen.getByRole("group", { name: "Filter by status" })).getByRole("button", { name: /^Stopped/ }));
      await userEvent.type(filter(), "terraria");
      expect(cards()).toEqual(["Server 2"]);
    });
    it("are not shown when everything is in one group", async () => {
      await open(new FakeApi({ "GET /api/servers": sampleServers().slice(1, 3) }));
      expect(screen.queryByRole("group", { name: "Filter by status" })).not.toBeInTheDocument();
    });
    it("explain an empty result for a status", async () => {
      const api = new FakeApi({ "GET /api/servers": sampleServers() });
      await open(api);
      await userEvent.click(within(screen.getByRole("group", { name: "Filter by status" })).getByRole("button", { name: /^Running/ }));
      await userEvent.type(filter(), "skyblock");
      expect(screen.getByText("No servers match")).toBeInTheDocument();
    });
  });

  describe("power button", () => {
    it("starts a stopped server", async () => {
      const api = new FakeApi({ "GET /api/servers": sampleServers() });
      await open(api);
      const card = screen.getByText("Skyblock", { selector: "a" }).closest("article")!;
      const uuid = (within(card).getAllByRole("link")[0] as HTMLAnchorElement).getAttribute("href")!.split("/servers/")[1]!.split("?")[0]!;
      api.on({ [`POST /api/servers/${uuid}/power`]: undefined });
      await userEvent.click(within(card).getByRole("button", { name: "Start" }));
      await waitFor(() => expect(api.callsTo(`POST /api/servers/${uuid}/power`)).toHaveLength(1));
      expect(api.callsTo(`POST /api/servers/${uuid}/power`)[0]!.body).toEqual({ action: "start" });
    });
    it("is disabled while the server installs, or when the node is offline", async () => {
      const list = sampleServers();
      list.find((s) => s.name === "Rust Wipe Friday")!.node_online = false;
      await open(new FakeApi({ "GET /api/servers": list }));
      const installing = screen.getByText("Valheim Friends", { selector: "a" }).closest("article")!;
      expect(within(installing).getByRole("button", { name: "Installing…" })).toBeDisabled();
      const offline = screen.getByText("Rust Wipe Friday", { selector: "a" }).closest("article")!;
      expect(within(offline).getByRole("button", { name: "Start" })).toBeDisabled();
      expect(within(offline).getByText(/node offline/)).toBeInTheDocument();
    });
  });

  describe("roles and empty states", () => {
    it("tells an admin with no servers how to start", async () => {
      renderApp(new FakeApi({ "GET /api/servers": [] }), "/");
      expect(await screen.findByText("No servers yet")).toBeInTheDocument();
      expect(screen.getByText("Create one to get started.")).toBeInTheDocument();
      expect(screen.queryByText("No servers match")).not.toBeInTheDocument();
    });
    it("tells a regular user to ask an admin, and offers no way to create", async () => {
      renderApp(new FakeApi({ "GET /api/auth/me": bob, "GET /api/servers": [] }), "/");
      expect(await screen.findByText("Ask an admin to create one for you.")).toBeInTheDocument();
      expect(screen.queryByRole("link", { name: "New server" })).not.toBeInTheDocument();
    });
    it("shows an error from the API instead of a blank page", async () => {
      const { problem } = await import("../test/harness");
      renderApp(new FakeApi({ "GET /api/servers": () => problem(500, "db is on fire") }), "/");
      expect(await screen.findByText(/db is on fire/)).toBeInTheDocument();
    });
  });
});
