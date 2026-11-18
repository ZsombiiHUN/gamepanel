import path from "node:path";
import { expect, STATE, shellReady, test } from "./support";

// Signed in as the regular user, who can use exactly one server and only read its console and files.
test.use({ storageState: path.join(STATE, "user.json") });

test("sees only the servers shared with them, and no admin controls", async ({ page, panel }) => {
  await page.goto("/");
  await expect(page.getByRole("article")).toHaveCount(1);
  await expect(page.getByRole("article")).toContainText(panel.ready.shared_to);
  await expect(page.getByRole("navigation", { name: "Admin" })).toHaveCount(0);
  await expect(page.getByRole("link", { name: /New server/ })).toHaveCount(0);
  await expect(page.getByRole("navigation", { name: "Servers" }).getByRole("link", { name: "Survival SMP" })).toHaveCount(0);
});

test("the card has no power button they can use", async ({ page }) => {
  await page.goto("/");
  await expect(page.getByRole("article").getByRole("button", { name: "Start" })).toBeDisabled();
});

test("on their server they get the tabs they are allowed, and no command box", async ({ page, panel }) => {
  const s = panel.seeded(panel.ready.shared_to);
  await page.goto(`/servers/${s.uuid}?tab=console`);
  await expect(page.getByRole("heading", { name: s.name })).toBeVisible();
  const nav = page.getByRole("navigation", { name: "Servers" });
  await expect(nav.getByRole("link", { name: "Console" })).toBeVisible();
  await expect(nav.getByRole("link", { name: "Files" })).toBeVisible();
  for (const hidden of ["Backups", "Schedules", "Access"]) await expect(nav.getByRole("link", { name: hidden })).toHaveCount(0);
  await expect(page.getByRole("textbox", { name: "Console command" })).toHaveCount(0);
  await expect(page.getByRole("button", { name: "Start", exact: true })).toBeDisabled();
});

test("another user's server looks like it does not exist", async ({ page, panel }) => {
  const other = panel.seeded("Survival SMP");
  await page.goto(`/servers/${other.uuid}`);
  await expect(page.getByRole("alert")).toBeVisible();
  await expect(page.getByRole("heading", { name: other.name })).toHaveCount(0);
});

test("admin pages refuse them, and the API agrees", async ({ page, request }) => {
  for (const p of ["/nodes", "/eggs", "/audit", "/servers/new"]) {
    await page.goto(p);
    await expect(page.getByRole("table")).toHaveCount(0);
    await expect(page.getByRole("button", { name: /Add node|Import egg|Create server/ })).toHaveCount(0);
  }
  for (const api of ["/api/nodes", "/api/audit"]) expect((await request.get(api)).status(), api).toBe(403);
});

test("the quick switcher does not offer admin pages or other people's servers", async ({ page }) => {
  await page.goto("/");
  await shellReady(page);
  await page.keyboard.press("Control+k");
  const box = page.getByRole("combobox", { name: "Search servers and pages" });
  await box.fill("audit");
  await expect(page.getByText(/Nothing matches/)).toBeVisible();
  await box.fill("survival");
  await expect(page.getByText(/Nothing matches/)).toBeVisible();
  await box.fill("creative");
  await expect(page.getByRole("option")).toHaveCount(1);
});

test("files are readable but the server's files are not writable without permission", async ({ page, panel, request }) => {
  const s = panel.seeded(panel.ready.shared_to);
  await page.goto(`/servers/${s.uuid}?tab=files`);
  await expect(page.getByRole("searchbox", { name: "Search files" })).toBeVisible();
  const res = await request.put(`/api/servers/${s.uuid}/files/content`, { data: { path: "x.txt", content: "no" } });
  expect(res.status()).toBe(403);
});
