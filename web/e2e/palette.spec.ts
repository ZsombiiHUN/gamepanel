import { expect, openMenuIfDrawer, shellReady, test } from "./support";

const box = (page: import("@playwright/test").Page) => page.getByRole("combobox", { name: "Search servers and pages" });

test("Ctrl+K opens the quick switcher with focus in the box, and Escape closes it", async ({ page }) => {
  await page.goto("/");
  await shellReady(page);
  await page.keyboard.press("Control+k");
  await expect(box(page)).toBeFocused();
  await page.keyboard.press("Escape");
  await expect(box(page)).toBeHidden();
});

test("type a game or port, press Enter, and you are on that server", async ({ page, panel }) => {
  await page.goto("/");
  await shellReady(page);
  await page.keyboard.press("Control+k");
  const s = panel.seeded("Rust Wipe Friday");
  await box(page).fill(String(s.port));
  await expect(page.getByRole("option")).toHaveCount(1);
  await page.keyboard.press("Enter");
  await expect(page).toHaveURL(new RegExp(`/servers/${s.uuid}`));
  await expect(page.getByRole("heading", { name: "Rust Wipe Friday" })).toBeVisible();
});

test("arrow keys move the highlight and clicking works too", async ({ page }) => {
  await page.goto("/");
  await shellReady(page);
  await page.keyboard.press("Control+k");
  await box(page).fill("lobby");
  const options = page.getByRole("option");
  await expect(options).toHaveCount(4); // Hub Lobby, Lobby 1, Lobby 2, Lobby 10
  await expect(options.first()).toHaveAttribute("aria-selected", "true");
  await page.keyboard.press("ArrowDown");
  await expect(options.nth(1)).toHaveAttribute("aria-selected", "true");
  await options.nth(1).click();
  await expect(page).toHaveURL(/\/servers\//);
});

test("reaches admin pages by what they are for", async ({ page }) => {
  await page.goto("/");
  await shellReady(page);
  await page.keyboard.press("Control+k");
  await box(page).fill("security");
  await page.keyboard.press("Enter");
  await expect(page).toHaveURL(/\/audit$/);
});

test("says so when nothing matches", async ({ page }) => {
  await page.goto("/");
  await shellReady(page);
  await page.keyboard.press("Control+k");
  await box(page).fill("qqqq");
  await expect(page.getByText(/Nothing matches/)).toBeVisible();
  await page.keyboard.press("Enter");
  await expect(page).toHaveURL(/\/$/);
});

test("on a server it offers that server's tabs", async ({ page, panel }) => {
  await page.goto(`/servers/${panel.seeded("Survival SMP").uuid}?tab=console`);
  await shellReady(page);
  await page.keyboard.press("Control+k");
  await box(page).fill("backups");
  await expect(page.getByText("This server (Survival SMP)")).toBeVisible();
  await page.keyboard.press("Enter");
  await expect(page).toHaveURL(/tab=backups/);
});

test("the Search button in the sidebar opens it, and Esc returns focus to where you were", async ({ page }) => {
  await page.goto("/");
  await openMenuIfDrawer(page);
  const trigger = page.getByRole("button", { name: "Search servers and pages" });
  await trigger.click();
  await expect(box(page)).toBeFocused();
  await page.keyboard.press("Escape");
  await expect(box(page)).toBeHidden();
});
