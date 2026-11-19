// @vitest-environment jsdom
import { describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { admin, bob, server } from "../../test/fixtures";
import { FakeApi, renderApp } from "../../test/harness";

vi.mock("../../api/ws", () => ({ useServerSocket: () => ({ status: "open", state: null, stats: null }) }));

const UUID = "abcdef12-2222-4333-8444-555555555555";
const ready = { host: "node1.example.com", port: 2022, username: "bob@example.com.abcdef12", status: "ready", read: true, write: true };

function setup(info: object, me = bob, perms = ["files.read", "files.write"]) {
  const srv = { ...server({ name: "Survival" }), uuid: UUID, owner: false, permissions: perms };
  const api = new FakeApi({
    "GET /api/auth/me": me,
    "GET /api/servers": [srv],
    [`GET /api/servers/${UUID}`]: srv,
    [`GET /api/servers/${UUID}/sftp`]: info,
    [`GET /api/servers/${UUID}/files`]: [],
  });
  return api;
}

describe("SFTP connect", () => {
  it("opens the client with one click", async () => {
    renderApp(setup(ready), `/servers/${UUID}?tab=settings`);
    const open = await screen.findByRole("link", { name: "Open in SFTP client" });
    expect(open.getAttribute("href")).toBe("sftp://bob%40example.com.abcdef12@node1.example.com:2022/");
    expect(screen.getByText("Ready")).toBeTruthy();
    expect(screen.getByText("Read and write")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Copy username" })).toBeTruthy();
  });

  it("falls back to the panel's host and says so", async () => {
    renderApp(setup({ ...ready, host: "" }), `/servers/${UUID}?tab=settings`);
    const open = await screen.findByRole("link", { name: "Open in SFTP client" });
    expect(open.getAttribute("href")).toBe(`sftp://bob%40example.com.abcdef12@${window.location.hostname}:2022/`);
    expect(screen.getByText("Same as this panel.")).toBeTruthy();
  });

  it("points admins at the node settings", async () => {
    renderApp(setup({ ...ready, host: "" }, admin), `/servers/${UUID}?tab=settings`);
    expect(await screen.findByRole("link", { name: "Set the node's address" })).toBeTruthy();
  });

  it("cannot connect while the node is offline or SFTP is off", async () => {
    const { unmount } = renderApp(setup({ ...ready, status: "offline", port: 0 }), `/servers/${UUID}?tab=settings`);
    expect(await screen.findByText("Node offline")).toBeTruthy();
    expect(screen.getByRole<HTMLButtonElement>("button", { name: "Open in SFTP client" }).disabled).toBe(true);
    expect(screen.queryByRole("link", { name: "Open in SFTP client" })).toBeNull();
    unmount();
    renderApp(setup({ ...ready, status: "off" }), `/servers/${UUID}?tab=settings`);
    expect(await screen.findByText(/Ask an administrator to turn it on/)).toBeTruthy();
  });

  it("makes a key, shows it once and signs the client in with it", async () => {
    const api = setup(ready).on({ "POST /api/auth/keys": { token: "gp_secret/1", key: { id: 9 } }, "GET /api/auth/keys": [] });
    renderApp(api, `/servers/${UUID}?tab=settings`);
    await userEvent.click(await screen.findByRole("button", { name: "Create one" }));
    await userEvent.type(screen.getByLabelText("Your account password"), "another-long-password");
    await userEvent.click(screen.getByRole("button", { name: "Create key" }));
    await waitFor(() =>
      expect(api.callsTo("POST /api/auth/keys")[0]?.body).toEqual({
        name: "SFTP: Survival",
        expires_in_days: 365,
        password: "another-long-password",
      }),
    );
    const open = await screen.findByRole("link", { name: "Open and sign in" });
    expect(open.getAttribute("href")).toBe("sftp://bob%40example.com.abcdef12:gp_secret%2F1@node1.example.com:2022/");
    expect(within(screen.getByRole("status")).getByText("gp_secret/1")).toBeTruthy();
  });

  it("is a button on the Files tab, for read-only users too", async () => {
    const api = setup({ ...ready, write: false }, bob, ["files.read"]);
    renderApp(api, `/servers/${UUID}?tab=files`);
    await userEvent.click(await screen.findByRole("button", { name: "Connect with SFTP" }));
    const dialog = await screen.findByRole("dialog", { name: "Connect with SFTP" });
    expect(await within(dialog).findByText("Read only")).toBeTruthy();
    expect(within(dialog).getByRole("link", { name: "Open in SFTP client" })).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Upload files" })).toBeNull();
  });
});
