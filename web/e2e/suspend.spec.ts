import { expect, test } from "./support";

// Admins suspend a server from its settings (shops do the same through the API).
test("suspending stops a server and marks it; lifting it lets it start again", async ({ page, panel }) => {
  const s = await panel.createServer();
  await panel.waitFor(s.uuid, "offline");
  await panel.power(s.uuid, "start");
  await panel.waitFor(s.uuid, "running");

  await page.goto(`/servers/${s.uuid}?tab=settings`);
  await page.getByRole("button", { name: "Suspend server" }).click();
  await page.getByRole("dialog").getByRole("button", { name: "Suspend server" }).click();
  await expect(page.getByRole("status").filter({ hasText: "This server is suspended" })).toBeVisible();
  await expect(page.getByText("Suspended", { exact: true }).first()).toBeVisible();
  await panel.waitFor(s.uuid, "offline");
  await expect(page.getByRole("button", { name: "Start", exact: true })).toBeDisabled();

  await page.goto("/?q=" + encodeURIComponent(s.name));
  const card = page.getByRole("article").filter({ hasText: s.name });
  await expect(card).toContainText("Suspended");
  await expect(card.getByRole("button", { name: "Start" })).toBeDisabled();

  await page.goto(`/servers/${s.uuid}?tab=settings`);
  await page.getByRole("button", { name: "Lift suspension" }).click();
  await expect(page.getByRole("status").filter({ hasText: "This server is suspended" })).toHaveCount(0);
  await expect(page.getByRole("button", { name: "Start", exact: true })).toBeEnabled();
});
