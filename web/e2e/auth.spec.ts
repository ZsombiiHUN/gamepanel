import { ANONYMOUS, expect, test } from "./support";

test.describe("signed out", () => {
  test.use({ storageState: ANONYMOUS });

  test("any page sends you to the sign-in page", async ({ page }) => {
    for (const path of ["/", "/nodes", "/audit", "/servers/new"]) {
      await page.goto(path);
      await expect(page).toHaveURL(/\/login$/);
      await expect(page.getByRole("heading", { name: "Welcome back" })).toBeVisible();
    }
  });

  test("a wrong password is refused with a message and nothing leaks", async ({ page, panel }) => {
    await page.goto("/login");
    await page.getByLabel("Email").fill(panel.ready.admin.email);
    await page.getByLabel("Password").fill("not-the-password");
    await page.getByRole("button", { name: "Sign in", exact: true }).click();
    await expect(page.getByRole("alert")).toContainText(/invalid email or password/i);
    await expect(page).toHaveURL(/\/login$/);
    await expect(page.getByRole("heading", { name: "Servers" })).toHaveCount(0);
  });

  test("signing in lands on the servers page, survives a reload, and sign-out locks it again", async ({ page, panel }) => {
    await page.goto("/login");
    await page.getByLabel("Email").fill(panel.ready.user.email);
    await page.getByLabel("Password").fill(panel.ready.user.password);
    await page.getByRole("button", { name: "Sign in", exact: true }).click();
    await expect(page.getByRole("heading", { name: "Servers" })).toBeVisible();
    await page.reload();
    await expect(page.getByRole("heading", { name: "Servers" })).toBeVisible();

    await page.getByRole("button", { name: "Sign out" }).click();
    await expect(page).toHaveURL(/\/login$/);
    await page.goBack();
    await expect(page).toHaveURL(/\/login$/); // the back button does not show the old page
    await page.goto("/");
    await expect(page).toHaveURL(/\/login$/);
  });

  test("the account page lists your sessions and signs out one you pick", async ({ page, browser, panel }) => {
    const signIn = async (p: typeof page) => {
      await p.goto("/login");
      await p.getByLabel("Email").fill(panel.ready.user.email);
      await p.getByLabel("Password").fill(panel.ready.user.password);
      await p.getByRole("button", { name: "Sign in", exact: true }).click();
      await expect(p.getByRole("heading", { name: "Servers" })).toBeVisible();
    };
    const other = await browser.newContext({ storageState: ANONYMOUS, userAgent: "Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Mobile/15E148 Safari/604.1" });
    const phone = await other.newPage();
    await signIn(phone);
    await signIn(page);

    await page.goto("/account");
    const list = page.getByRole("list", { name: "Signed-in sessions" });
    await expect(list.getByText("This device")).toBeVisible();
    await expect(list.getByText("Safari on iPhone").first()).toBeVisible();
    await list.getByRole("button", { name: /^Sign out Safari on iPhone/ }).first().click();
    await expect(list.getByText("Safari on iPhone")).toHaveCount(0);

    expect((await phone.request.get("/api/auth/me")).status()).toBe(401);
    expect((await page.request.get("/api/auth/me")).status()).toBe(200);
    await other.close();
  });

  test("the sign-in form is usable with the keyboard alone", async ({ page, panel }) => {
    await page.goto("/login");
    await expect(page.getByLabel("Email")).toBeFocused();
    await page.keyboard.type(panel.ready.user.email);
    await page.keyboard.press("Tab");
    await page.keyboard.type(panel.ready.user.password);
    await page.keyboard.press("Enter");
    await expect(page.getByRole("heading", { name: "Servers" })).toBeVisible();
  });
});

test.describe("signed in", () => {
  test("an unknown address shows a not-found page with a way back", async ({ page }) => {
    await page.goto("/no/such/page");
    await expect(page.getByText("Page not found")).toBeVisible();
    await page.getByRole("link", { name: "Back to your servers" }).click();
    await expect(page.getByRole("heading", { name: "Servers" })).toBeVisible();
  });

  test("a server that does not exist shows an error, not a blank page", async ({ page }) => {
    await page.goto("/servers/00000000-0000-4000-8000-000000000000");
    await expect(page.getByRole("alert")).toBeVisible();
  });
});
