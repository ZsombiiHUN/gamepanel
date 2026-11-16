// @vitest-environment jsdom
import { describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { bob, server } from "../test/fixtures";
import { FakeApi, renderApp } from "../test/harness";

vi.mock("../api/ws", () => ({ useServerSocket: () => ({ status: "open", state: null, stats: null }) }));

const UUID = "11111111-2222-4333-8444-555555555555";
const srv = { ...server({ name: "Paper" }), uuid: UUID, permissions: ["databases.read", "databases.manage"] };
const db = { id: 7, host_id: 1, name: "s11111111_luckperms", username: "u11111111_ab12cd34", remote: "%", host: "10.0.0.5", port: 3306, created_at: 1_700_000_000 };
const view = (over = {}) => ({ databases: [db], limit: 2, can_manage: true, available: true, ...over });

function open(v: ReturnType<typeof view>, extra: Record<string, unknown> = {}) {
  const api = new FakeApi({
    "GET /api/auth/me": bob,
    "GET /api/servers": [srv],
    [`GET /api/servers/${UUID}`]: srv,
    [`GET /api/servers/${UUID}/databases`]: v,
    ...extra,
  });
  renderApp(api, `/servers/${UUID}?tab=databases`);
  return api;
}

describe("Databases tab", () => {
  it("shows connection details but hides the password until asked", async () => {
    const api = open(view(), { [`GET /api/servers/${UUID}/databases/7/password`]: { ...db, password: "s3cretPassw0rd" } });
    expect(await screen.findByText("10.0.0.5:3306")).toBeInTheDocument();
    expect(screen.queryByText("s3cretPassw0rd")).toBeNull();
    await userEvent.click(screen.getByRole("button", { name: "Show the password of s11111111_luckperms" }));
    expect(await screen.findByText("s3cretPassw0rd")).toBeInTheDocument();
    expect(api.callsTo(`GET /api/servers/${UUID}/databases/7/password`)).toHaveLength(1);
    await userEvent.click(screen.getByRole("button", { name: "Hide the password of s11111111_luckperms" }));
    expect(screen.queryByText("s3cretPassw0rd")).toBeNull();
  });
  it("creates a database and shows its password straight away", async () => {
    const created = { ...db, id: 8, name: "s11111111_coreprotect", password: "fr3shPassword" };
    const api = open(view(), { [`POST /api/servers/${UUID}/databases`]: created });
    await userEvent.click(await screen.findByRole("button", { name: "New database" }));
    const dlg = within(screen.getByRole("dialog", { name: "New database", hidden: true }));
    await userEvent.type(dlg.getByRole("textbox", { name: "Name", hidden: true }), "coreprotect");
    await userEvent.click(dlg.getByRole("button", { name: "Create database", hidden: true }));
    await waitFor(() => expect(api.callsTo(`POST /api/servers/${UUID}/databases`)[0]?.body).toEqual({ name: "coreprotect", remote: "%" }));
  });
  it("rejects names the server would refuse", async () => {
    open(view());
    await userEvent.click(await screen.findByRole("button", { name: "New database" }));
    const dlg = within(screen.getByRole("dialog", { name: "New database", hidden: true }));
    await userEvent.type(dlg.getByRole("textbox", { name: "Name", hidden: true }), "my db");
    expect(dlg.getByRole("button", { name: "Create database", hidden: true })).toBeDisabled();
  });
  it("cannot create past the limit or without a host", async () => {
    open(view({ limit: 1 }));
    expect(await screen.findByRole("button", { name: "New database" })).toBeDisabled();
  });
  it("is read-only without databases.manage", async () => {
    open(view({ can_manage: false }));
    expect(await screen.findByText("You can see these databases but not their passwords.")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Show the password/ })).toBeNull();
  });
});
