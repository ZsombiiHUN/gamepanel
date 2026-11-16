// @vitest-environment jsdom
import { describe, expect, it, vi } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { components } from "../api/client";
import { bob, server } from "../test/fixtures";
import { FakeApi, problem, renderApp } from "../test/harness";

vi.mock("../api/ws", () => ({ useServerSocket: () => ({ status: "open", state: null, stats: null }) }));

type StartupView = components["schemas"]["StartupView"];
const UUID = "11111111-2222-4333-8444-555555555555";
const srv = { ...server({ name: "Paper" }), uuid: UUID, permissions: ["startup.read", "startup.update", "startup.docker-image"] };

function view(over: Partial<StartupView> = {}): StartupView {
  return {
    command: "java -Xmx{{SERVER_MEMORY}}M -jar {{JAR}}",
    egg_command: "java -Xmx{{SERVER_MEMORY}}M -jar {{JAR}}",
    custom_command: false,
    builtins: { SERVER_MEMORY: "1024", SERVER_PORT: "25565" },
    image: "yolks:java_21",
    images: [{ label: "Java 21", image: "yolks:java_21" }, { label: "Java 17", image: "yolks:java_17" }],
    custom_image: false,
    variables: [
      { env: "JAR", name: "Server jar", description: "The jar to run", value: "server.jar", default: "server.jar", editable: true, rules: "required|string|max:20", rules_summary: "required, max 20", admin_only: false },
      { env: "PVP", name: "PvP", description: "", value: "true", default: "true", editable: true, rules: "required|boolean", rules_summary: "required, true or false", admin_only: false },
      { env: "VERSION", name: "Version", description: "", value: "latest", default: "latest", editable: false, rules: "required|string", rules_summary: "required", admin_only: false },
    ],
    can_edit_command: false,
    can_change_image: true,
    can_edit_variables: true,
    pending: false,
    ...over,
  } as StartupView;
}

function open(startup: StartupView, extra: Record<string, unknown> = {}) {
  const api = new FakeApi({
    "GET /api/auth/me": bob,
    "GET /api/servers": [srv],
    [`GET /api/servers/${UUID}`]: srv,
    [`GET /api/servers/${UUID}/startup`]: startup,
    ...extra,
  });
  renderApp(api, `/servers/${UUID}?tab=startup`);
  return api;
}

describe("Startup tab", () => {
  it("shows the command with values filled in, and the placeholders on request", async () => {
    open(view());
    expect(await screen.findByText("java -Xmx1024M -jar server.jar")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Show placeholders" }));
    expect(screen.getByText("java -Xmx{{SERVER_MEMORY}}M -jar {{JAR}}")).toBeInTheDocument();
  });

  it("sends only the variables that changed", async () => {
    const api = open(view(), { [`PUT /api/servers/${UUID}/startup`]: view({ pending: true }) });
    const jar = await screen.findByLabelText("Server jar");
    await userEvent.clear(jar);
    await userEvent.type(jar, "paper.jar");
    await userEvent.selectOptions(screen.getByLabelText("PvP"), "Disabled");
    expect(screen.getByText("2 unsaved changes.")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Save variables" }));
    await waitFor(() => expect(api.callsTo(`PUT /api/servers/${UUID}/startup`)).toHaveLength(1));
    expect(api.callsTo(`PUT /api/servers/${UUID}/startup`)[0]!.body).toEqual({ env: { JAR: "paper.jar", PVP: "false" } });
    // The reply says the server is running: the change waits for a restart.
    expect(await screen.findByText(/take effect the next time the server starts/)).toBeInTheDocument();
  });

  it("keeps locked variables read-only", async () => {
    open(view());
    expect(await screen.findByLabelText("Version")).toHaveAttribute("readonly");
  });

  it("shows the reason when the panel refuses a value", async () => {
    open(view(), { [`PUT /api/servers/${UUID}/startup`]: () => problem(422, "Server jar: must be at most 20 characters") });
    const jar = await screen.findByLabelText("Server jar");
    await userEvent.type(jar, "-way-too-long-for-the-rule");
    await userEvent.click(screen.getByRole("button", { name: "Save variables" }));
    expect(await screen.findByText(/must be at most 20 characters/)).toBeInTheDocument();
  });

  it("switches the image from the egg's list", async () => {
    const api = open(view(), { [`PUT /api/servers/${UUID}/startup`]: view({ image: "yolks:java_17" }) });
    await userEvent.selectOptions(await screen.findByLabelText("Image"), "yolks:java_17");
    await waitFor(() => expect(api.callsTo(`PUT /api/servers/${UUID}/startup`)[0]?.body).toEqual({ image: "yolks:java_17" }));
    // Only admins can type an image of their own.
    expect(screen.queryByRole("option", { name: "Custom image..." })).toBeNull();
  });

  it("shows an admin-chosen image as locked", async () => {
    open(view({ image: "registry.example.com/custom:1", custom_image: true, can_change_image: false }));
    expect(await screen.findByText("registry.example.com/custom:1")).toBeInTheDocument();
    expect(screen.getByText(/An administrator chose this image/)).toBeInTheDocument();
    expect(screen.queryByLabelText("Image")).toBeNull();
  });
});
