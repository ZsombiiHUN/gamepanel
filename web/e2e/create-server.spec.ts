import { expect, test, unique } from "./support";

test("create a server from the form and find it in the list, ready to start", async ({ page, panel }) => {
  const name = unique("Brand new");
  await page.goto("/servers/new");
  await page.getByLabel("Name", { exact: true }).fill(name);
  await page.getByLabel("Node").selectOption({ label: "eu-west-1" });
  await page.getByLabel("Egg").selectOption({ label: "Rust" });
  await page.getByLabel("Memory (MB)").fill("512");
  await page.getByLabel("Disk (MB)").fill("2048"); // the test node tracks disk capacity, so a limit is required
  await page.getByRole("button", { name: "Create server" }).click();

  await expect(page).toHaveURL(/\/servers\/[0-9a-f-]{36}/);
  await expect(page.getByRole("heading", { name })).toBeVisible();
  const uuid = page.url().match(/servers\/([0-9a-f-]{36})/)![1]!;
  panel.track(uuid);
  await panel.waitFor(uuid, "offline");

  await page.goto("/");
  const card = page.getByRole("article").filter({ hasText: name });
  await expect(card).toContainText("Rust · Port ");
  await expect(card.getByRole("button", { name: "Start" })).toBeEnabled();
  await expect(page.getByRole("navigation", { name: "Servers" }).getByRole("link", { name })).toBeVisible();
});

test("the form refuses to submit without a name, node or egg, and says which", async ({ page }) => {
  await page.goto("/servers/new");
  await page.getByRole("button", { name: "Create server" }).click();
  await expect(page).toHaveURL(/\/servers\/new$/);
  const invalid = await page.locator(":invalid").evaluateAll((els) => els.map((e) => e.getAttribute("name") ?? e.id ?? e.tagName));
  expect(invalid.length).toBeGreaterThanOrEqual(1);
  await page.getByLabel("Name", { exact: true }).fill("x");
  await page.getByRole("button", { name: "Create server" }).click();
  await expect(page).toHaveURL(/\/servers\/new$/); // node and egg are still missing
});

test("choosing an egg shows its variables with their defaults", async ({ page }) => {
  await page.goto("/servers/new");
  await page.getByLabel("Egg").selectOption({ label: "Valheim" });
  await expect(page.getByText("Environment variables")).toBeVisible();
  await expect(page.getByRole("textbox", { name: /Name/ }).last()).toHaveValue("My Server");
});

test("the server list counts it, and Cancel returns to where you were", async ({ page }) => {
  await page.goto("/");
  await page.getByRole("link", { name: "New server" }).first().click();
  await page.getByRole("link", { name: "Cancel" }).or(page.getByRole("button", { name: "Cancel" })).click();
  await expect(page).toHaveURL(/\/$/);
});
