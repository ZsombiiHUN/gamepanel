// @vitest-environment jsdom
import { describe, expect, it, vi } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { bob, server } from "../test/fixtures";
import { FakeApi, renderApp } from "../test/harness";

vi.mock("../api/ws", () => ({ useServerSocket: () => ({ status: "open", state: null, stats: null }) }));

const UUID = "11111111-2222-4333-8444-555555555555";
const srv = { ...server({ name: "Paper" }), uuid: UUID, permissions: ["network.read", "network.manage"] };
const view = (over = {}) => ({
  ports: [{ port: 25565, notes: "", primary: true }, { port: 25566, notes: "Dynmap", primary: false }],
  limit: 2, can_manage: true, can_pick: false, range_from: 25565, range_to: 25600, pending: false, ...over,
});

function open(net: ReturnType<typeof view>, extra: Record<string, unknown> = {}) {
  const api = new FakeApi({
    "GET /api/auth/me": bob,
    "GET /api/servers": [srv],
    [`GET /api/servers/${UUID}`]: srv,
    [`GET /api/servers/${UUID}/network`]: net,
    ...extra,
  });
  renderApp(api, `/servers/${UUID}?tab=network`);
  return api;
}

describe("Network tab", () => {
  it("lists ports with the primary one marked", async () => {
    open(view());
    expect(await screen.findByText("25565")).toBeInTheDocument();
    expect(screen.getByText("Primary")).toBeInTheDocument();
    expect(screen.getByLabelText("Notes for port 25566")).toHaveValue("Dynmap");
    expect(screen.getByText("1 of 2 extra ports used.")).toBeInTheDocument();
  });
  it("adds the next free port without asking which", async () => {
    const api = open(view(), { [`POST /api/servers/${UUID}/network`]: view({ pending: true }) });
    await userEvent.click(await screen.findByRole("button", { name: "Add port" }));
    await waitFor(() => expect(api.callsTo(`POST /api/servers/${UUID}/network`)[0]?.body).toEqual({}));
    expect(await screen.findByText(/picks up port changes the next time it starts/)).toBeInTheDocument();
  });
  it("cannot add past the limit", async () => {
    open(view({ ports: [{ port: 25565, notes: "", primary: true }, { port: 25566, notes: "", primary: false }, { port: 25567, notes: "", primary: false }] }));
    expect(await screen.findByRole("button", { name: "Add port" })).toBeDisabled();
  });
  it("saves notes when the field loses focus", async () => {
    const api = open(view(), { [`PUT /api/servers/${UUID}/network/25566`]: view() });
    const notes = await screen.findByLabelText("Notes for port 25566");
    await userEvent.clear(notes);
    await userEvent.type(notes, "BlueMap");
    await userEvent.tab();
    await waitFor(() => expect(api.callsTo(`PUT /api/servers/${UUID}/network/25566`)[0]?.body).toEqual({ notes: "BlueMap" }));
  });
  it("makes a port primary", async () => {
    const api = open(view(), { [`PUT /api/servers/${UUID}/network/25566`]: view() });
    await userEvent.click(await screen.findByRole("button", { name: "Make primary" }));
    await waitFor(() => expect(api.callsTo(`PUT /api/servers/${UUID}/network/25566`)[0]?.body).toEqual({ primary: true }));
  });
  it("is read-only without network.manage", async () => {
    open(view({ can_manage: false }));
    expect(await screen.findByText("You can see these ports but not change them.")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Add port" })).toBeNull();
    expect(screen.queryByLabelText("Notes for port 25566")).toBeNull();
  });
});
