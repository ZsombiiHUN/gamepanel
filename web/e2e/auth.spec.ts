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
    await page.getByRole("button", { name: "Sign in" }).click();
    await expect(page.getByRole("alert")).toContainText(/invalid email or password/i);
    await expect(page).toHaveURL(/\/login$/);
    await expect(page.getByRole("heading", { name: "Servers" })).toHaveCount(0);
  });

  test("signing in lands on the servers page, survives a reload, and sign-out locks it again", async ({ page, panel }) => {
    await page.goto("/login");
    await page.getByLabel("Email").fill(panel.ready.user.email);
    await page.getByLabel("Password").fill(panel.ready.user.password);
    await page.getByRole("button", { name: "Sign in" }).click();
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
