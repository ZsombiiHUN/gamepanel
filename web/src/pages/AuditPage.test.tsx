// @vitest-environment jsdom
import { describe, expect, it } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { FakeApi, renderApp } from "../test/harness";
import { bob, sampleServers } from "../test/fixtures";

const ev = (id: number, action: string, extra: Record<string, unknown> = {}) => ({
  id,
  at: 1_700_000_000 + id,
  actor_id: 1,
  actor_email: "admin@example.com",
  action,
  target: "t",
  ip: "10.0.0.1",
  detail: "",
  ...extra,
});

function auditApi(rows: (q: string, offset: number) => unknown[]) {
  return new FakeApi({
    "GET /api/servers": sampleServers(),
    "GET /api/audit": (c) => rows(c.query.get("q") ?? "", Number(c.query.get("offset") ?? 0)),
  });
}
const search = () => screen.getByRole("searchbox", { name: "Search the audit log" });
const queries = (api: FakeApi) => api.callsTo("GET /api/audit").map((c) => c.query.get("q"));

describe("Audit log", () => {
  it("shows events, newest first as the API returns them, and flags failures", async () => {
    const api = auditApi(() => [ev(3, "login.fail"), ev(2, "server.create", { target: "abc", detail: "Survival SMP" }), ev(1, "login.ok")]);
    renderApp(api, "/audit");
    const table = await screen.findByRole("table");
    const rows = within(table).getAllByRole("row").slice(1);
    expect(rows.map((r) => within(r).getAllByRole("cell")[1]!.textContent)).toEqual(["login.fail", "server.create", "login.ok"]);
    expect(within(rows[1]!).getByText(/\(Survival SMP\)/)).toBeInTheDocument();
    expect(queries(api)).toEqual([null]); // no q until the user searches
  });

  it("is for admins only", async () => {
    const api = new FakeApi({ "GET /api/auth/me": bob, "GET /api/servers": [] });
    renderApp(api, "/audit");
    await screen.findByRole("navigation", { name: "Servers" });
    expect(screen.queryByRole("table")).not.toBeInTheDocument();
    expect(api.callsTo("GET /api/audit")).toHaveLength(0);
  });

  it("searches the whole log on the server, not just the loaded page", async () => {
    const api = auditApi((q) =>
      q === "bob" ? [ev(7, "login.fail", { actor_email: "bob@example.com" })] : [ev(9, "login.ok"), ev(8, "login.ok")],
    );
    renderApp(api, "/audit");
    await screen.findByRole("table");
    await userEvent.type(search(), "bob");
    await waitFor(() => expect(screen.getAllByRole("row")).toHaveLength(2));
    expect(screen.getByText("bob@example.com")).toBeInTheDocument();
    expect(queries(api).at(-1)).toBe("bob");
  });

  it("waits for a pause in typing instead of asking the server for every key", async () => {
    const api = auditApi(() => [ev(1, "login.ok")]);
    renderApp(api, "/audit");
    await screen.findByRole("table");
    await userEvent.type(search(), "login.fail");
    await waitFor(() => expect(queries(api)).toContain("login.fail"));
    expect(queries(api)).toEqual([null, "login.fail"]);
  });

  it("trims the search term", async () => {
    const api = auditApi(() => [ev(1, "login.ok")]);
    renderApp(api, "/audit");
    await screen.findByRole("table");
    await userEvent.type(search(), "  node  ");
    await waitFor(() => expect(queries(api).at(-1)).toBe("node"));
  });

  it("says nothing matched, keeps the table, and recovers when cleared", async () => {
    const api = auditApi((q) => (q ? [] : [ev(1, "login.ok")]));
    renderApp(api, "/audit");
    await screen.findByRole("table");
    await userEvent.type(search(), "zzz");
    expect(await screen.findByText("No events match “zzz”.")).toBeInTheDocument();
    expect(screen.getByRole("table")).toBeInTheDocument();
    await userEvent.keyboard("{Escape}");
    await waitFor(() => expect(screen.queryByText(/No events match/)).not.toBeInTheDocument());
    expect(await screen.findByText("login.ok")).toBeInTheDocument();
  });

  it("shows the friendly empty state only when there is no log at all", async () => {
    renderApp(
      auditApi(() => []),
      "/audit",
    );
    expect(await screen.findByText("No events yet")).toBeInTheDocument();
  });

  it("pages through results and goes back to the first page when the search changes", async () => {
    const full = Array.from({ length: 100 }, (_, i) => ev(1000 - i, "login.ok"));
    const api = auditApi((q, offset) => (offset === 0 ? full : q ? [] : [ev(1, "logout")]));
    renderApp(api, "/audit");
    await screen.findByRole("table");
    await userEvent.click(screen.getByRole("button", { name: /Older/ }));
    expect(await screen.findByText(/Page 2/)).toBeInTheDocument();
    expect(api.callsTo("GET /api/audit").at(-1)!.query.get("offset")).toBe("100");
    await userEvent.type(search(), "login");
    await waitFor(() => expect(screen.getByText(/Page 1 · 100 events matching “login”/)).toBeInTheDocument());
    expect(api.callsTo("GET /api/audit").at(-1)!.query.get("offset")).toBe("0");
  });

  it("does not drop back to page 1 when you page right after opening (the search debounce must not reset it)", async () => {
    const full = Array.from({ length: 100 }, (_, i) => ev(1000 - i, "login.ok"));
    const api = auditApi((_q, offset) => (offset === 0 ? full : [ev(1, "logout")]));
    renderApp(api, "/audit");
    await screen.findByRole("table");
    await userEvent.click(screen.getByRole("button", { name: /Older/ }));
    expect(await screen.findByText(/Page 2/)).toBeInTheDocument();
    await new Promise((r) => setTimeout(r, 450)); // longer than the debounce
    expect(screen.getByText(/Page 2/)).toBeInTheDocument();
    expect(api.callsTo("GET /api/audit").at(-1)!.query.get("offset")).toBe("100");
  });

  it("disables Older when the page is not full, and Newer on the first page", async () => {
    renderApp(
      auditApi(() => [ev(1, "login.ok")]),
      "/audit",
    );
    await screen.findByRole("table");
    expect(screen.getByRole("button", { name: /Older/ })).toBeDisabled();
    expect(screen.getByRole("button", { name: /Newer/ })).toBeDisabled();
  });
});
