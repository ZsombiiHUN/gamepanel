// @vitest-environment jsdom
import { describe, expect, it, vi } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { server } from "../test/fixtures";
import { FakeApi, renderApp } from "../test/harness";

vi.mock("../api/ws", () => ({ useServerSocket: () => ({ status: "open", state: null, stats: null }) }));

const UUID = "11111111-2222-4333-8444-555555555555";
const srv = { ...server({ name: "Survival", egg_name: "Paper Minecraft" }), uuid: UUID, permissions: ["files.read", "files.write"] };
const hit = {
  project_id: "Vebnzrzj",
  slug: "luckperms",
  title: "LuckPerms",
  author: "Luck",
  description: "Permissions.",
  downloads: 2400000,
  icon_url: "",
  project_type: "plugin",
  versions: [],
  categories: [],
};

describe("Mods tab", () => {
  it("starts from the egg's kind and loader, then installs the newest matching version", async () => {
    const api = new FakeApi({
      "GET /api/servers": [srv],
      [`GET /api/servers/${UUID}`]: srv,
      [`GET /api/servers/${UUID}/mods/search`]: { hits: [hit], total_hits: 1 },
      [`GET /api/servers/${UUID}/mods/Vebnzrzj/versions`]: [
        {
          id: "v54",
          project_id: "Vebnzrzj",
          name: "",
          version_number: "5.4.0",
          version_type: "release",
          game_versions: ["1.21.1"],
          loaders: ["paper"],
          date_published: "",
          files: [],
        },
      ],
      [`POST /api/servers/${UUID}/mods`]: { path: "/plugins/LuckPerms.jar", size: 1 },
    });
    renderApp(api, `/servers/${UUID}?tab=mods`);
    expect(await screen.findByText("LuckPerms")).toBeInTheDocument();
    const search = api.callsTo(`GET /api/servers/${UUID}/mods/search`)[0]!;
    expect(search.query.get("type")).toBe("plugin");
    expect(search.query.get("loader")).toBe("paper");
    await userEvent.click(screen.getByRole("button", { name: "Install" }));
    await userEvent.click(await screen.findByRole("button", { name: "Install" }));
    await waitFor(() => expect(api.callsTo(`POST /api/servers/${UUID}/mods`)[0]?.body).toEqual({ version_id: "v54", type: "plugin" }));
    expect(await screen.findByText(/Installed to/)).toBeInTheDocument();
  });
});
