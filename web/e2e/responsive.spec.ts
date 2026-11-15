import { expect, isDrawer, test } from "./support";

test.beforeEach(async ({ page }) => {
  test.skip(!isDrawer(page), "drawer behaviour only");
  await page.goto("/");
});

test("the menu button opens the drawer, and choosing a page closes it", async ({ page }) => {
  const aside = page.getByRole("complementary", { name: "Navigation" });
  await expect(aside).not.toBeInViewport();
  const open = page.getByRole("button", { name: "Open menu" });
  await expect(open).toHaveAttribute("aria-expanded", "false");
  await open.click();
  await expect(aside).toBeInViewport();
  await page.getByRole("link", { name: "Nodes", exact: true }).click();
  await expect(page).toHaveURL(/\/nodes$/);
  await expect(aside).not.toBeInViewport();
});

test("tapping outside the drawer, or the close button, dismisses it", async ({ page }) => {
  const aside = page.getByRole("complementary", { name: "Navigation" });
  await page.getByRole("button", { name: "Open menu" }).click();
  await expect(aside).toBeInViewport();
  await page.mouse.click(380, 400); // the dimmed area to the right of the drawer
  await expect(aside).not.toBeInViewport();
  await page.getByRole("button", { name: "Open menu" }).click();
  await page.getByRole("button", { name: "Close menu" }).click();
  await expect(aside).not.toBeInViewport();
});

test("the server cards stack in one column and fit the screen", async ({ page }) => {
  await expect(page.getByRole("article").first()).toBeVisible();
  const boxes = await page.getByRole("article").evaluateAll((els) => els.slice(0, 4).map((e) => e.getBoundingClientRect()));
  const vw = page.viewportSize()!.width;
  for (const b of boxes) {
    expect(b.left).toBeGreaterThanOrEqual(0);
    expect(b.right).toBeLessThanOrEqual(vw);
  }
  expect(new Set(boxes.map((b) => Math.round(b.left))).size).toBe(1);
});

test("filter, search and open a server with touch", async ({ page, panel }) => {
  await page.getByRole("searchbox", { name: "Filter servers" }).fill("rust wipe");
  const card = page.getByRole("article");
  await expect(card).toHaveCount(1);
  await card.getByRole("link", { name: "Console" }).tap();
  await expect(page).toHaveURL(new RegExp(panel.seeded("Rust Wipe Friday").uuid));
});

test("the quick switcher is reachable from the drawer", async ({ page, panel }) => {
  await page.getByRole("button", { name: "Open menu" }).click();
  await page.getByRole("button", { name: "Search servers and pages" }).click();
  const box = page.getByRole("combobox", { name: "Search servers and pages" });
  await expect(box).toBeFocused();
  await box.fill("skyblock");
  await page.getByRole("option").first().tap();
  await expect(page).toHaveURL(new RegExp(panel.seeded("Skyblock").uuid));
});

test("tap targets are at least 32px tall", async ({ page }) => {
  await page.getByRole("button", { name: "Open menu" }).click();
  for (const name of ["Nodes", "Eggs", "Audit log"]) {
    const box = (await page.getByRole("link", { name, exact: true }).boundingBox())!;
    expect(box.height, name).toBeGreaterThanOrEqual(32);
  }
});
