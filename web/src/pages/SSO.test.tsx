// @vitest-environment jsdom
import { describe, expect, it } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { admin } from "../test/fixtures";
import { FakeApi, problem, renderApp } from "../test/harness";

const signedOut = () => problem(401, "sign in required");
const options = {
  password_reset: false,
  signup: "invite",
  sso: [
    { id: "github", name: "GitHub" },
    { id: "oidc", name: "Authentik" },
  ],
  passkeys: false,
};

describe("single sign-on on the sign-in page", () => {
  it("offers each provider as a link that starts the round trip", async () => {
    renderApp(new FakeApi({ "GET /api/auth/me": signedOut, "GET /api/auth/options": options }), "/login");
    expect(await screen.findByRole("link", { name: "Continue with GitHub" })).toHaveAttribute("href", "/api/auth/sso/github/start");
    expect(screen.getByRole("link", { name: "Continue with Authentik" })).toBeInTheDocument();
  });
  it("explains why a sign-in did not work", async () => {
    renderApp(new FakeApi({ "GET /api/auth/me": signedOut, "GET /api/auth/options": options }), "/login?sso_error=not_linked");
    expect(await screen.findByRole("alert")).toHaveTextContent(/not linked to an account here/);
  });
  it("asks for the account's code after the provider", async () => {
    const api = new FakeApi({ "GET /api/auth/me": signedOut, "GET /api/auth/options": options, "POST /api/auth/sso/verify": admin });
    renderApp(api, "/login?sso=totp");
    await userEvent.type(await screen.findByLabelText("Authentication code"), "123456");
    api.on({ "GET /api/auth/me": admin, "GET /api/servers": [] });
    await userEvent.click(screen.getByRole("button", { name: "Verify and sign in" }));
    await waitFor(() => expect(api.callsTo("POST /api/auth/sso/verify")[0]?.body).toEqual({ code: "123456" }));
  });
});

describe("sign-in methods on the account page", () => {
  const base = { "GET /api/servers": [], "GET /api/auth/keys": [], "GET /api/auth/options": options };
  it("links unlinked providers and unlinks linked ones", async () => {
    const api = new FakeApi({
      ...base,
      "GET /api/auth/identities": {
        identities: [{ provider: "github", email: "me@example.com", created_at: 0 }],
        providers: options.sso,
        has_password: true,
      },
      "DELETE /api/auth/identities/github": undefined,
    });
    renderApp(api, "/account");
    expect(await screen.findByText("Linked as me@example.com")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Link" })).toHaveAttribute("href", "/api/auth/sso/oidc/start?mode=link");
    await userEvent.click(screen.getByRole("button", { name: "Unlink" }));
    await waitFor(() => expect(api.callsTo("DELETE /api/auth/identities/github")).toHaveLength(1));
  });
  it("asks a password-less account to set its first password", async () => {
    renderApp(
      new FakeApi({ ...base, "GET /api/auth/identities": { identities: [], providers: options.sso, has_password: false } }),
      "/account",
    );
    expect(await screen.findByRole("button", { name: "Set password" })).toBeInTheDocument();
    expect(screen.queryByLabelText("Current password")).toBeNull();
  });
});
