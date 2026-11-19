import { expect, test, unique } from "./support";

// Filters, search, paging and folders live in the address: reload keeps them, Back undoes them, a link shares them.

test("a server filter survives a reload and can be shared as a link", async ({ page }) => {
  await page.goto("/");
  const box = page.getByRole("searchbox", { name: "Filter servers" });
  await box.fill("lobby 1");
  await expect(page).toHaveURL(/[?&]q=lobby(\+|%20)1/);
  await page.reload();
  await expect(box).toHaveValue("lobby 1");
  await expect(
    page
      .getByRole("main")
      .getByRole("link", { name: /^Lobby 1/ })
      .first(),
  ).toBeVisible();
  await expect(page.getByRole("main").getByRole("link", { name: "Skyblock" })).toHaveCount(0);
});

test("a link with a status filter opens already filtered", async ({ page, panel }) => {
  // The status chips only appear once servers are in more than one state.
  const s = await panel.createServer(unique("Url status"));
  await panel.power(s.uuid, "start");
  await panel.waitFor(s.uuid, "running");
  await page.goto("/?status=running");
  await expect(page.getByRole("button", { name: /^Running/ })).toHaveAttribute("aria-pressed", "true");
  await expect(page.getByRole("main").getByRole("link", { name: s.name })).toBeVisible();
  await expect(page.getByRole("main").getByRole("link", { name: "Skyblock" })).toHaveCount(0);
});

test("Back undoes opening a folder", async ({ page, panel }) => {
  const s = await panel.createServer(unique("Url folders"));
  await panel.writeFile(s.uuid, "plugins/Essentials/config.yml", "x: 1\n");
  await page.goto(`/servers/${s.uuid}?tab=files`);
  await page.getByRole("button", { name: "plugins", exact: true }).click();
  await expect(page).toHaveURL(/dir=%2Fplugins|dir=\/plugins/);
  await expect(page.getByRole("button", { name: "Essentials", exact: true })).toBeVisible();
  await page.goBack();
  await expect(page.getByRole("button", { name: "plugins", exact: true })).toBeVisible();
  await expect(page).not.toHaveURL(/dir=/);
});

test("a folder link opens that folder, and a reload keeps the file search", async ({ page, panel }) => {
  const s = await panel.createServer(unique("Url deep"));
  await panel.writeFile(s.uuid, "plugins/Essentials/config.yml", "x: 1\n");
  await page.goto(`/servers/${s.uuid}?tab=files&dir=/plugins`);
  await expect(page.getByRole("button", { name: "Essentials", exact: true })).toBeVisible();
  await page.getByRole("searchbox", { name: "Search files" }).fill("config");
  await expect(page.getByText(/1 match/)).toBeVisible();
  await expect(page).toHaveURL(/q=config/);
  await page.reload();
  await expect(page.getByRole("searchbox", { name: "Search files" })).toHaveValue("config");
  await expect(page.getByText(/1 match/)).toBeVisible();
});

test("the audit log keeps its search and page across a reload", async ({ page }) => {
  await page.goto("/audit");
  const box = page.getByRole("searchbox", { name: "Search the audit log" });
  await box.fill("login");
  await expect(page).toHaveURL(/q=login/);
  await page.reload();
  await expect(box).toHaveValue("login");
  await expect(page.getByRole("region", { name: "Audit events" })).toBeVisible();
});

test("an unknown tab in the link falls back to the console", async ({ page, panel }) => {
  const s = await panel.createServer(unique("Url tab"));
  await page.goto(`/servers/${s.uuid}?tab=nonsense`);
  await expect(page.getByRole("region", { name: "Console" })).toBeVisible();
});
