// @vitest-environment jsdom
import { describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { admin, server } from "../test/fixtures";
import { FakeApi, renderApp } from "../test/harness";

vi.mock("../api/ws", () => ({ useServerSocket: () => ({ status: "open", state: null, stats: null }) }));

const UUID = "11111111-2222-4333-8444-555555555555";
const PERMS = ["console.read", "console.send", "power", "files.read", "files.write", "backups.read", "backups.create", "activity.read"].map((id) => ({ id, description: id }));

function setup(users: unknown[] = []) {
  const srv = { ...server({ name: "Survival" }), uuid: UUID, owner: true, owner_email: "admin@example.com" };
  const api = new FakeApi({
    "GET /api/auth/me": admin,
    "GET /api/servers": [srv],
    [`GET /api/servers/${UUID}`]: srv,
    "GET /api/permissions": PERMS,
    [`GET /api/servers/${UUID}/users`]: users,
    [`GET /api/servers/${UUID}/invites`]: [],
    [`POST /api/servers/${UUID}/invites`]: { added: true, emailed: false, link: "" },
    [`PUT /api/servers/${UUID}/users/7`]: {},
  });
  renderApp(api, `/servers/${UUID}?tab=users`);
  return api;
}

describe("Access tab", () => {
  it("adds someone with a role", async () => {
    const api = setup();
    await userEvent.type(await screen.findByRole("textbox", { name: "Their email" }), "mod@example.com");
    await userEvent.click(screen.getByRole("radio", { name: /Moderator/ }));
    await userEvent.click(screen.getByRole("button", { name: "Add person" }));
    await waitFor(() =>
      expect(api.callsTo(`POST /api/servers/${UUID}/invites`)[0]?.body).toEqual({
        email: "mod@example.com",
        permissions: ["console.read", "console.send", "power", "backups.read", "backups.create", "activity.read"],
      }),
    );
    expect(await screen.findByText(/mod@example.com now has access/)).toBeInTheDocument();
  });
  it("ticks what a permission needs", async () => {
    const api = setup();
    await userEvent.type(await screen.findByRole("textbox", { name: "Their email" }), "a@example.com");
    await userEvent.click(screen.getByRole("radio", { name: /Viewer/ }));
    await userEvent.click(screen.getByRole("button", { name: "Pick individual permissions" }));
    await userEvent.click(screen.getByRole("checkbox", { name: /Upload, edit and delete/ }));
    expect(screen.getByRole("checkbox", { name: /Browse and download/ })).toBeChecked();
    await userEvent.click(screen.getByRole("button", { name: "Add person" }));
    await waitFor(() => expect(api.callsTo(`POST /api/servers/${UUID}/invites`)[0]?.body).toMatchObject({ permissions: ["console.read", "files.read", "files.write", "activity.read"] }));
  });
  it("shows the owner and each person's role, and saves edits", async () => {
    const api = setup([{ user_id: 7, email: "pal@example.com", permissions: ["console.read", "activity.read"] }]);
    const row = (await screen.findByText("pal@example.com")).closest("li")!;
    expect(within(row).getByText("Viewer")).toBeInTheDocument();
    expect(screen.getByText("Owner")).toBeInTheDocument();
    await userEvent.click(within(row).getByRole("button", { name: /Edit/ }));
    const save = within(row).getByRole("button", { name: "Save permissions" });
    expect(save).toBeDisabled();
    await userEvent.click(within(row).getByRole("radio", { name: /Full access/ }));
    await userEvent.click(save);
    await waitFor(() => expect(api.callsTo(`PUT /api/servers/${UUID}/users/7`)[0]?.body).toEqual({ permissions: PERMS.map((p) => p.id) }));
  });
});
