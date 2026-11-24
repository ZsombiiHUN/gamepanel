// @vitest-environment jsdom
import { describe, expect, it } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { bob, sampleServers } from "./test/fixtures";
import { FakeApi, problem, renderApp } from "./test/harness";

// The sidebar lists every server too, so look for cards inside the page itself.
const page = () => within(screen.getByRole("main"));
const base = () => ({ "GET /api/servers": sampleServers() });

describe("sign-in gate", () => {
  it("sends a signed-out visitor to the sign-in page without loading any page data", async () => {
    const api = new FakeApi({ ...base(), "GET /api/auth/me": () => problem(401, "not signed in") });
    const { router } = renderApp(api, "/");
    expect(await screen.findByRole("heading", { name: "Welcome back" })).toBeInTheDocument();
    expect(router.state.location.pathname).toBe("/login");
    expect(api.callsTo("GET /api/servers")).toHaveLength(0);
  });

  it("keeps the address the visitor asked for out of the way: deep links to admin pages also land on sign-in", async () => {
    const api = new FakeApi({ "GET /api/auth/me": () => problem(401, "not signed in") });
    renderApp(api, "/audit?q=login");
    expect(await screen.findByRole("heading", { name: "Welcome back" })).toBeInTheDocument();
    expect(api.callsTo("GET /api/audit")).toHaveLength(0);
  });

  it("skips the sign-in page for someone who is already signed in", async () => {
    const { router } = renderApp(new FakeApi(base()), "/login");
    expect(await screen.findByRole("heading", { name: "Servers" })).toBeInTheDocument();
    expect(router.state.location.pathname).toBe("/");
  });

  it("shows a real error, not the sign-in page, when the session check itself fails", async () => {
    const api = new FakeApi({ "GET /api/auth/me": () => problem(500, "db is down") });
    renderApp(api, "/");
    await waitFor(() => expect(screen.queryByRole("heading", { name: "Welcome back" })).not.toBeInTheDocument());
    expect(await screen.findByText(/db is down/i)).toBeInTheDocument();
  });

  it("says the panel is not responding when a proxy answers instead, without showing its page", async () => {
    const api = new FakeApi({
      "GET /api/auth/me": () =>
        new Response("<html><h1>502 Bad Gateway</h1>nginx/1.27.0</html>", { status: 502, headers: { "content-type": "text/html" } }),
    });
    renderApp(api, "/");
    expect(await screen.findByText(/not responding/i, {}, { timeout: 8000 })).toBeInTheDocument();
    expect(screen.queryByText(/nginx|Bad Gateway/)).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Reload" })).toBeInTheDocument();
  }, 10000);
});

describe("admin pages", () => {
  it.each([
    ["/nodes", "GET /api/nodes"],
    ["/eggs", "GET /api/eggs"],
    ["/audit", "GET /api/audit"],
    ["/servers/new", "GET /api/nodes"],
  ])("%s is closed to a regular user, and asks the API for nothing", async (path, endpoint) => {
    const api = new FakeApi({ ...base(), "GET /api/auth/me": bob });
    renderApp(api, path);
    expect(await screen.findByRole("alert")).toHaveTextContent("Administrators only");
    expect(api.callsTo(endpoint)).toHaveLength(0);
  });

  it("an admin gets in", async () => {
    renderApp(new FakeApi({ ...base(), "GET /api/audit": [] }), "/audit");
    expect(await screen.findByRole("heading", { name: "Audit log" })).toBeInTheDocument();
  });
});

describe("state in the address", () => {
  it("opens the servers page already filtered by what the link says", async () => {
    renderApp(new FakeApi(base()), "/?q=server&status=stopped");
    expect(await screen.findByRole("searchbox", { name: "Filter servers" })).toHaveValue("server");
    expect(screen.getByRole("button", { name: /^Stopped/ })).toHaveAttribute("aria-pressed", "true");
    expect(await page().findByRole("link", { name: "Server 2" })).toBeInTheDocument();
    expect(page().queryByRole("link", { name: "Skyblock" })).not.toBeInTheDocument();
  });

  it("ignores a status it does not know", async () => {
    renderApp(new FakeApi(base()), "/?status=bogus");
    expect(await screen.findByRole("button", { name: /^All/ })).toHaveAttribute("aria-pressed", "true");
  });

  it("writes the filter to the address once typing pauses, and the status chip at once", async () => {
    const { router } = renderApp(new FakeApi(base()), "/");
    await userEvent.type(await screen.findByRole("searchbox", { name: "Filter servers" }), "sky");
    expect(await page().findByRole("link", { name: "Skyblock" })).toBeInTheDocument();
    await waitFor(() => expect(router.state.location.search).toMatchObject({ q: "sky" }));
    await userEvent.click(screen.getByRole("button", { name: /^Stopped/ }));
    await waitFor(() => expect(router.state.location.search).toMatchObject({ q: "sky", status: "stopped" }));
  });

  it("Clear filter empties both the box and the address", async () => {
    const { router } = renderApp(new FakeApi(base()), "/?q=nothing-matches");
    await userEvent.click(await screen.findByRole("button", { name: /Clear filter/ }));
    await waitFor(() => expect(router.state.location.search).toEqual({}));
    expect(screen.getByRole("searchbox", { name: "Filter servers" })).toHaveValue("");
  });

  it("opens the audit log on the page and search the link names", async () => {
    const api = new FakeApi({ ...base(), "GET /api/audit": [] });
    renderApp(api, "/audit?q=login&page=2");
    await waitFor(() => expect(api.callsTo("GET /api/audit").length).toBeGreaterThan(0));
    const c = api.callsTo("GET /api/audit")[0]!;
    expect(c.query.get("q")).toBe("login");
    expect(c.query.get("offset")).toBe("200");
  });

  it("starts the file browser in the folder the link names", async () => {
    const uuid = sampleServers()[0]!.uuid;
    const api = new FakeApi({
      ...base(),
      [`GET /api/servers/${uuid}`]: sampleServers()[0]!,
      [`GET /api/servers/${uuid}/files`]: (c: { query: URLSearchParams }) =>
        c.query.get("path") === "/plugins" ? [{ name: "Essentials.jar", dir: false, size: 5, mod_unix: 1 }] : [],
    });
    renderApp(api, `/servers/${uuid}?tab=files&dir=/plugins`);
    expect(await screen.findByText("Essentials.jar")).toBeInTheDocument();
  });

  it("falls back to the console for a tab it does not know", async () => {
    const uuid = sampleServers()[0]!.uuid;
    const api = new FakeApi({ ...base(), [`GET /api/servers/${uuid}`]: sampleServers()[0]! });
    const { router } = renderApp(api, `/servers/${uuid}?tab=bogus`);
    await screen.findByRole("region", { name: "Console" });
    expect(router.state.location.search).toMatchObject({ tab: "console" });
  });

  it("moves a subuser off a tab they may not open, instead of showing its permission errors", async () => {
    const viewer = { ...sampleServers()[0]!, owner: false, permissions: ["console.read", "activity.read"] };
    const api = new FakeApi({ ...base(), [`GET /api/servers/${viewer.uuid}`]: viewer });
    const { router } = renderApp(api, `/servers/${viewer.uuid}?tab=files`);
    await screen.findByRole("region", { name: "Console" });
    expect(router.state.location.search).toMatchObject({ tab: "console" });
    expect(api.callsTo(`GET /api/servers/${viewer.uuid}/files`)).toHaveLength(0);
  });
});
