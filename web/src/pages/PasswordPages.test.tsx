// @vitest-environment jsdom
import { afterEach, describe, expect, it } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { FakeApi, problem, renderApp } from "../test/harness";

const signedOut = () => problem(401, "sign in required");
afterEach(() => { window.location.hash = ""; });

describe("sign-in page links", () => {
  it("offers a reset link and sign-up only when the panel supports them", async () => {
    const api = new FakeApi({ "GET /api/auth/me": signedOut, "GET /api/auth/options": { password_reset: true, signup: "open", sso: [], passkeys: false } });
    renderApp(api, "/login");
    expect(await screen.findByRole("link", { name: "Forgot your password?" })).toHaveAttribute("href", "/forgot");
    expect(screen.getByRole("link", { name: "Create an account" })).toBeInTheDocument();
  });
  it("hides them otherwise", async () => {
    const api = new FakeApi({ "GET /api/auth/me": signedOut, "GET /api/auth/options": { password_reset: false, signup: "invite", sso: [], passkeys: false } });
    renderApp(api, "/login");
    await screen.findByRole("heading", { name: "Welcome back" });
    await waitFor(() => expect(api.callsTo("GET /api/auth/options")).toHaveLength(1));
    expect(screen.queryByRole("link", { name: "Forgot your password?" })).toBeNull();
  });
});

describe("forgot and reset", () => {
  it("never says whether the email has an account", async () => {
    const api = new FakeApi({ "GET /api/auth/me": signedOut, "POST /api/auth/forgot": undefined });
    renderApp(api, "/forgot");
    await userEvent.type(await screen.findByLabelText("Email"), "bob@example.com");
    await userEvent.click(screen.getByRole("button", { name: "Send the link" }));
    expect(await screen.findByText(/If bob@example.com has an account/)).toBeInTheDocument();
  });
  it("takes the token from the link's fragment and sets the password", async () => {
    window.location.hash = "#token=abcdefghijklmnopqrstuvwxyz";
    const api = new FakeApi({ "POST /api/auth/reset": undefined });
    renderApp(api, "/reset");
    await userEvent.type(await screen.findByLabelText("New password"), "a-brand-new-passphrase");
    await userEvent.type(screen.getByLabelText("Repeat it"), "a-brand-new-passphrase-typo");
    expect(screen.getByRole("button", { name: "Save new password" })).toBeDisabled();
    await userEvent.clear(screen.getByLabelText("Repeat it"));
    await userEvent.type(screen.getByLabelText("Repeat it"), "a-brand-new-passphrase");
    await userEvent.click(screen.getByRole("button", { name: "Save new password" }));
    expect(await screen.findByRole("heading", { name: "Password changed" })).toBeInTheDocument();
    expect(api.callsTo("POST /api/auth/reset")[0]!.body).toEqual({ token: "abcdefghijklmnopqrstuvwxyz", password: "a-brand-new-passphrase" });
  });
  it("explains a link without a token", async () => {
    renderApp(new FakeApi({}), "/reset");
    expect(await screen.findByRole("heading", { name: "This link is incomplete" })).toBeInTheDocument();
  });
});

describe("register", () => {
  it("shows what the invitation is for and uses the invited email", async () => {
    window.location.hash = "#token=abcdefghijklmnopqrstuvwxyz";
    const api = new FakeApi({
      "GET /api/auth/me": signedOut,
      "POST /api/auth/invite": { id: 1, email: "carol@example.com", server_name: "Survival", permissions: ["console.read"], is_admin: false, expires_at: 0, created_at: 0 },
      "POST /api/auth/register": { id: 3, email: "carol@example.com", is_admin: false },
    });
    renderApp(api, "/register");
    expect(await screen.findByText("You were invited to the game server Survival.")).toBeInTheDocument();
    expect(screen.getByLabelText("Email")).toHaveValue("carol@example.com");
    expect(screen.getByLabelText("Email")).toHaveAttribute("readonly");
    await userEvent.type(screen.getByLabelText("New password"), "carols-long-passphrase");
    await userEvent.type(screen.getByLabelText("Repeat it"), "carols-long-passphrase");
    api.on({ "GET /api/auth/me": { id: 3, email: "carol@example.com", is_admin: false }, "GET /api/servers": [] });
    await userEvent.click(screen.getByRole("button", { name: "Create account" }));
    await waitFor(() => expect(api.callsTo("POST /api/auth/register")[0]?.body).toEqual({ token: "abcdefghijklmnopqrstuvwxyz", password: "carols-long-passphrase" }));
  });
  it("says when an invitation has expired", async () => {
    window.location.hash = "#token=abcdefghijklmnopqrstuvwxyz";
    renderApp(new FakeApi({ "GET /api/auth/me": signedOut, "POST /api/auth/invite": () => problem(404, "this link is invalid or has expired; ask for a new one") }), "/register");
    expect(await screen.findByText(/invalid or has expired/)).toBeInTheDocument();
  });
});
