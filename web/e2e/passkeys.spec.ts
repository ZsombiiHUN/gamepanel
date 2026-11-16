import { expect } from "@playwright/test";
import { ANONYMOUS, readState, test } from "./support";

// A real browser with Chromium's virtual authenticator: the whole passkey
// path, from navigator.credentials to the panel's verification. Passkeys are
// bound to a domain, so this test opens the panel on localhost.
test.use({ storageState: ANONYMOUS });

test("add a passkey, then sign in with it and nothing else", async ({ page, browserName }) => {
  test.skip(browserName !== "chromium", "the virtual authenticator is a Chromium feature");
  const state = readState();
  const origin = state.url.replace("127.0.0.1", "localhost");
  const cdp = await page.context().newCDPSession(page);
  await cdp.send("WebAuthn.enable");
  await cdp.send("WebAuthn.addVirtualAuthenticator", {
    options: { protocol: "ctap2", transport: "internal", hasResidentKey: true, hasUserVerification: true, isUserVerified: true, automaticPresenceSimulation: true },
  });

  await page.goto(origin + "/login");
  await page.getByLabel("Email").fill(state.admin.email);
  await page.getByLabel("Password").fill(state.admin.password);
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await expect(page).toHaveURL(origin + "/");

  await page.goto(origin + "/account");
  await page.getByRole("button", { name: "Add a passkey" }).click();
  const dialog = page.getByRole("dialog", { name: "Add a passkey" });
  await dialog.getByLabel("Name").fill("Test key");
  await dialog.getByLabel("Your password").fill(state.admin.password);
  await dialog.getByRole("button", { name: "Continue" }).click();
  await expect(page.getByText("Test key")).toBeVisible();

  // Sign out, then back in with only the passkey.
  await page.context().clearCookies();
  await page.goto(origin + "/login");
  await page.getByRole("button", { name: "Sign in with a passkey" }).click();
  await expect(page).toHaveURL(origin + "/");
  await page.goto(origin + "/account");
  await expect(page.getByText(/last used/)).toBeVisible();
});
