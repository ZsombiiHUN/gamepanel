// @vitest-environment jsdom
import { describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { admin, bob, server } from "../test/fixtures";
import { FakeApi, renderApp } from "../test/harness";

vi.mock("../api/ws", () => ({ useServerSocket: () => ({ status: "open", state: null, stats: null }) }));

const UUID = "11111111-2222-4333-8444-555555555555";

describe("Settings tab: name", () => {
  it("renames the server", async () => {
    const srv = { ...server({ name: "Survival" }), uuid: UUID, owner: false, permissions: ["settings.rename"] };
    const api = new FakeApi({
      "GET /api/auth/me": bob,
      "GET /api/servers": [srv],
      [`GET /api/servers/${UUID}`]: srv,
      [`PUT /api/servers/${UUID}/details`]: { ...srv, name: "Survival S3" },
    });
    renderApp(api, `/servers/${UUID}?tab=settings`);
    const name = await screen.findByRole("textbox", { name: "Name" });
    await userEvent.clear(name);
    await userEvent.type(name, "Survival S3");
    await userEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(api.callsTo(`PUT /api/servers/${UUID}/details`)[0]?.body).toEqual({ name: "Survival S3", description: "" }));
  });
  it("hides renaming without the permission", async () => {
    const srv = { ...server({ name: "Survival" }), uuid: UUID, owner: false, permissions: ["console.read"] };
    const api = new FakeApi({ "GET /api/auth/me": bob, "GET /api/servers": [srv], [`GET /api/servers/${UUID}`]: srv });
    renderApp(api, `/servers/${UUID}?tab=settings`);
    await screen.findByText("Details");
    expect(screen.queryByRole("textbox", { name: "Name" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Change owner" })).toBeNull();
  });
});

describe("Settings tab: delete", () => {
  it("asks for the server's name before deleting", async () => {
    const srv = { ...server({ name: "Survival" }), uuid: UUID, owner: true };
    const api = new FakeApi({
      "GET /api/auth/me": admin,
      "GET /api/servers": [srv],
      [`GET /api/servers/${UUID}`]: srv,
      [`DELETE /api/servers/${UUID}`]: null,
    });
    renderApp(api, `/servers/${UUID}?tab=settings`);
    await userEvent.click(await screen.findByRole("button", { name: "Delete server" }));
    const dialog = screen.getByRole("dialog");
    const confirm = within(dialog).getByRole("button", { name: "Delete server" });
    expect(confirm).toBeDisabled();
    await userEvent.type(within(dialog).getByRole("textbox"), "Survival");
    await userEvent.click(confirm);
    await waitFor(() => expect(api.callsTo(`DELETE /api/servers/${UUID}`)).toHaveLength(1));
  });
  it("forces the delete when the node is offline, and says what happens", async () => {
    const srv = { ...server({ name: "Survival" }), uuid: UUID, owner: true, node_online: false };
    const api = new FakeApi({
      "GET /api/auth/me": admin,
      "GET /api/servers": [srv],
      [`GET /api/servers/${UUID}`]: srv,
      [`DELETE /api/servers/${UUID}`]: null,
    });
    renderApp(api, `/servers/${UUID}?tab=settings`);
    await userEvent.click(await screen.findByRole("button", { name: "Delete server" }));
    const dialog = screen.getByRole("dialog");
    expect(within(dialog).getByText(/node is offline/)).toBeTruthy();
    await userEvent.type(within(dialog).getByRole("textbox"), "Survival");
    await userEvent.click(within(dialog).getByRole("button", { name: "Delete server" }));
    await waitFor(() => expect(api.callsTo(`DELETE /api/servers/${UUID}`)[0]?.query.get("force")).toBe("true"));
  });
});
