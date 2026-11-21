// @vitest-environment jsdom
import { useEffect } from "react";
import { describe, expect, it, vi, beforeEach } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { bob, server } from "../test/fixtures";
import { FakeApi, renderApp, type Call } from "../test/harness";

// The console socket: each test sets the lines it prints and the state it reports.
const live = vi.hoisted(() => ({ lines: [] as string[], state: null as string | null }));
vi.mock("../api/ws", () => ({
  useServerSocket: (_uuid: string, h: { onConsole: (l: string) => void }) => {
    useEffect(() => {
      for (const l of live.lines) h.onConsole(l);
      // eslint-disable-next-line react-hooks/exhaustive-deps
    }, []);
    return { status: "open", state: live.state, stats: null };
  },
}));

const UUID = "11111111-2222-4333-8444-555555555555";
const ALL = [
  "power",
  "console.read",
  "console.send",
  "files.read",
  "files.write",
  "startup.read",
  "startup.update",
  "startup.docker-image",
  "settings.reinstall",
];
const mc = { ...server({ name: "Survival" }), uuid: UUID, features: ["eula", "java_version", "pid_limit", "minecraft"], permissions: ALL };

const PROPS =
  "#Minecraft server properties\nmotd=A Minecraft Server\nmax-players=20\npvp=true\nwhite-list=false\nonline-mode=true\nserver-port=25565\nlevel-name=world\n";

function startup(over: Record<string, unknown> = {}) {
  return {
    command: "gp-java",
    egg_command: "gp-java",
    custom_command: false,
    builtins: {},
    image: "ghcr.io/zsombiihun/gamepanel-java:25",
    images: [
      { label: "Java 25", image: "ghcr.io/zsombiihun/gamepanel-java:25" },
      { label: "Java 21", image: "ghcr.io/zsombiihun/gamepanel-java:21" },
      { label: "Java 17", image: "ghcr.io/zsombiihun/gamepanel-java:17" },
    ],
    custom_image: false,
    variables: [
      {
        env: "EULA",
        name: "Accept the Minecraft EULA",
        description: "",
        value: "false",
        default: "false",
        editable: true,
        rules: "",
        rules_summary: "",
        admin_only: false,
      },
    ],
    can_edit_command: false,
    can_change_image: true,
    can_edit_variables: true,
    pending: false,
    ...over,
  };
}

function files(content: Record<string, string>) {
  return (c: Call) => {
    const p = c.query.get("path") ?? "";
    if (p in content) return { path: p, content: content[p] };
    return new Response(JSON.stringify({ status: 404, detail: "not found" }), {
      status: 404,
      headers: { "content-type": "application/problem+json" },
    });
  };
}

function open(tab: string, extra: Record<string, unknown> = {}, srv: object = mc) {
  const api = new FakeApi({
    "GET /api/auth/me": bob,
    "GET /api/servers": [srv],
    [`GET /api/servers/${UUID}`]: srv,
    [`GET /api/servers/${UUID}/startup`]: startup(),
    [`GET /api/servers/${UUID}/files/content`]: files({ "/server.properties": PROPS, "/whitelist.json": "[]" }),
    [`GET /api/servers/${UUID}/files`]: [
      { name: "world", dir: true, size: 0, mode: 493, mod_unix: 0 },
      { name: "plugins", dir: true, size: 0, mode: 493, mod_unix: 0 },
    ],
    [`GET /api/servers/${UUID}/minecraft/versions`]: {
      flavor: "paper",
      variable: "MINECRAFT_VERSION",
      current: "1.21.11",
      reset: { BUILD_NUMBER: "latest" },
      versions: ["26.3", "1.21.11", "1.21.10"],
    },
    [`GET /api/servers/${UUID}/metrics`]: { points: [] },
    ...extra,
  });
  renderApp(api, `/servers/${UUID}?tab=${tab}`);
  return api;
}

beforeEach(() => {
  live.lines = [];
  live.state = null;
});

describe("Minecraft console notices", () => {
  it("offers to accept the EULA and start", async () => {
    live.lines = ["[gamepanel] accept the Minecraft EULA (https://aka.ms/MinecraftEULA) first: set it under Startup."];
    const api = open("console", { [`PUT /api/servers/${UUID}/startup`]: startup(), [`POST /api/servers/${UUID}/power`]: undefined });
    await userEvent.click(await screen.findByRole("button", { name: "I accept, start the server" }));
    await waitFor(() => expect(api.callsTo(`POST /api/servers/${UUID}/power`)).toHaveLength(1));
    expect(api.callsTo(`PUT /api/servers/${UUID}/startup`)[0]?.body).toEqual({ env: { EULA: "true" } });
    expect(api.callsTo(`POST /api/servers/${UUID}/power`)[0]?.body).toEqual({ action: "start" });
    await waitFor(() => expect(screen.queryByRole("alert")).toBeNull());
  });

  it("offers the right Java image", async () => {
    live.lines = [
      "[gamepanel] this server needs Java 21 or newer but the image has Java 17. Pick the Java 21 image under Startup, then start again.",
    ];
    const api = open("console", { [`PUT /api/servers/${UUID}/startup`]: startup(), [`POST /api/servers/${UUID}/power`]: undefined });
    await userEvent.click(await screen.findByRole("button", { name: "Use Java 21 and start" }));
    await waitFor(() => expect(api.callsTo(`POST /api/servers/${UUID}/power`)).toHaveLength(1));
    expect(api.callsTo(`PUT /api/servers/${UUID}/startup`)[0]?.body).toEqual({ image: "ghcr.io/zsombiihun/gamepanel-java:21" });
  });

  it("forgets a problem from an earlier run", async () => {
    live.lines = [
      "[gamepanel] accept the Minecraft EULA (https://aka.ms/MinecraftEULA) first: set it under Startup.",
      "[gamepanel] Java 25 | limit 2048 MB | heap 1444 MB | GC g1",
      '[14:00:12 INFO]: Done (14.539s)! For help, type "help"',
    ];
    open("console");
    await screen.findByRole("region", { name: "Console" });
    expect(screen.queryByRole("alert")).toBeNull();
  });

  it("stays quiet while the server runs", async () => {
    live.lines = ["You need to agree to the EULA in order to run the server."];
    live.state = "running";
    open("console");
    await screen.findByRole("region", { name: "Console" });
    expect(screen.queryByRole("alert")).toBeNull();
  });

  it("never shows for other games", async () => {
    live.lines = ["You need to agree to the EULA in order to run the server."];
    open("console", {}, { ...mc, features: [] });
    await screen.findByRole("region", { name: "Console" });
    expect(screen.queryByRole("alert")).toBeNull();
    expect(screen.queryByRole("link", { name: "Minecraft" })).toBeNull();
  });
});

describe("Minecraft tab", () => {
  it("saves server settings without touching other lines", async () => {
    const api = open("minecraft", { [`PUT /api/servers/${UUID}/files/content`]: undefined });
    const max = await screen.findByLabelText("Max players");
    await userEvent.clear(max);
    await userEvent.type(max, "50");
    await userEvent.click(screen.getByRole("switch", { name: "PvP" }));
    expect(screen.getByText("2 unsaved changes")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(api.callsTo(`PUT /api/servers/${UUID}/files/content`)).toHaveLength(1));
    const body = api.callsTo(`PUT /api/servers/${UUID}/files/content`)[0]?.body as { path: string; content: string };
    expect(body.path).toBe("/server.properties");
    expect(body.content).toBe(PROPS.replace("max-players=20", "max-players=50").replace("pvp=true", "pvp=false"));
    expect(await screen.findByText(/Saved\. They apply when the server starts/)).toBeInTheDocument();
  });

  it("checks numbers before saving", async () => {
    open("minecraft");
    const view = await screen.findByLabelText("View distance");
    await userEvent.clear(view);
    await userEvent.type(view, "99");
    expect(screen.getByText("At most 32.")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Save" })).toBeDisabled();
  });

  it("edits the whitelist file while the server is stopped", async () => {
    const api = open("minecraft", {
      [`GET /api/servers/${UUID}/minecraft/players/Alex`]: { name: "Alex", uuid: "6ab43178-89fd-4905-97f6-0f67d9d76fd9", offline: false },
      [`PUT /api/servers/${UUID}/files/content`]: undefined,
    });
    const players = await screen.findByRole("region", { name: "Players" });
    await userEvent.type(within(players).getByLabelText("Player name"), "Alex");
    await userEvent.click(within(players).getByRole("button", { name: "Add" }));
    await waitFor(() => expect(api.callsTo(`PUT /api/servers/${UUID}/files/content`)).toHaveLength(1));
    const body = api.callsTo(`PUT /api/servers/${UUID}/files/content`)[0]?.body as { path: string; content: string };
    expect(body.path).toBe("/whitelist.json");
    expect(JSON.parse(body.content)).toEqual([{ uuid: "6ab43178-89fd-4905-97f6-0f67d9d76fd9", name: "Alex" }]);
  });

  it("uses commands while the server runs", async () => {
    live.state = "running";
    const api = open("minecraft", {
      [`POST /api/servers/${UUID}/command`]: undefined,
      [`GET /api/servers/${UUID}/minecraft/status`]: {
        version: "Paper 1.21.11",
        protocol: 774,
        online: 1,
        max: 20,
        players: [{ name: "Steve", id: "x" }],
        motd: "§aWelcome",
        latency_ms: 2,
      },
    });
    expect(await screen.findByText("Welcome")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Kick Steve" }));
    await waitFor(() => expect(api.callsTo(`POST /api/servers/${UUID}/command`)[0]?.body).toEqual({ command: "kick Steve" }));

    const players = screen.getByRole("region", { name: "Players" });
    await userEvent.type(within(players).getByLabelText("Player name"), "Alex");
    await userEvent.click(within(players).getByRole("button", { name: "Add" }));
    await waitFor(() => expect(api.callsTo(`POST /api/servers/${UUID}/command`)[1]?.body).toEqual({ command: "whitelist add Alex" }));
    expect(api.callsTo(`PUT /api/servers/${UUID}/files/content`)).toHaveLength(0);
  });

  it("switches version and reinstalls", async () => {
    const api = open("minecraft", { [`PUT /api/servers/${UUID}/startup`]: startup(), [`POST /api/servers/${UUID}/reinstall`]: undefined });
    await userEvent.selectOptions(await screen.findByLabelText("Switch to"), "26.3");
    await userEvent.click(screen.getByRole("button", { name: "Switch version" }));
    await userEvent.click(await screen.findByRole("button", { name: "Switch and reinstall" }));
    await waitFor(() => expect(api.callsTo(`POST /api/servers/${UUID}/reinstall`)).toHaveLength(1));
    expect(api.callsTo(`PUT /api/servers/${UUID}/startup`)[0]?.body).toEqual({
      env: { BUILD_NUMBER: "latest", MINECRAFT_VERSION: "26.3" },
    });
  });
});
