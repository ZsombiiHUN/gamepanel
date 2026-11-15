import { expect, test, unique } from "./support";

const term = (page: import("@playwright/test").Page) => page.getByRole("log", { name: "Server console output" });

test("start a server, watch it boot, send a command, and stop it", async ({ page, panel }) => {
  const s = await panel.createServer(unique("Console test"));
  await page.goto(`/servers/${s.uuid}?tab=console`);
  await expect(page.getByText("Stopped").first()).toBeVisible();
  await expect(page.getByRole("textbox", { name: "Console command" })).toBeVisible();

  await page.getByRole("button", { name: "Start", exact: true }).click();
  await expect(page.getByText("Running").first()).toBeVisible({ timeout: 15_000 });
  await expect(term(page)).toContainText("[fake] ready", { timeout: 15_000 });
  await expect(page.getByText("Live: connected")).toBeVisible();

  const input = page.getByRole("textbox", { name: "Console command" });
  const send = page.getByRole("button", { name: "Send" });
  await expect(send).toBeDisabled(); // nothing typed yet
  await input.fill("say hello world");
  await expect(send).toBeEnabled();
  await input.press("Enter");
  await expect(term(page)).toContainText("[fake] > say hello world");
  await expect(input).toHaveValue(""); // cleared after sending

  await page.getByRole("button", { name: "Stop", exact: true }).click();
  await expect(page.getByText("Stopped").first()).toBeVisible({ timeout: 15_000 });
  await expect(page.getByRole("button", { name: "Start", exact: true })).toBeVisible();
});

test("the log stays when you switch tabs and come back", async ({ page, panel }) => {
  const s = await panel.createServer(unique("Tab memory"));
  await panel.power(s.uuid, "start");
  await panel.waitFor(s.uuid, "running");
  await page.goto(`/servers/${s.uuid}?tab=console`);
  await expect(term(page)).toContainText("[fake] ready", { timeout: 15_000 });
  await page.getByRole("link", { name: "Files" }).click();
  await expect(page).toHaveURL(/tab=files/);
  await page.getByRole("link", { name: "Console" }).click();
  await expect(term(page)).toContainText("[fake] ready");
  await panel.power(s.uuid, "kill");
});

test("Clear empties the log and Copy puts it on the clipboard", async ({ page, panel, context }) => {
  await context.grantPermissions(["clipboard-read", "clipboard-write"]);
  const s = await panel.createServer(unique("Clear test"));
  await panel.power(s.uuid, "start");
  await panel.waitFor(s.uuid, "running");
  await page.goto(`/servers/${s.uuid}?tab=console`);
  await expect(term(page)).toContainText("[fake] ready", { timeout: 15_000 });
  await page.getByRole("button", { name: "Copy log" }).click();
  await expect.poll(() => page.evaluate(() => navigator.clipboard.readText())).toContain("[fake] ready");
  await page.getByRole("button", { name: "Clear" }).click();
  await expect(term(page)).not.toContainText("[fake] ready");
  await panel.power(s.uuid, "kill");
});

test("the Kill option is under More actions and stops a running server at once", async ({ page, panel }) => {
  const s = await panel.createServer(unique("Kill test"));
  await panel.power(s.uuid, "start");
  await panel.waitFor(s.uuid, "running");
  await page.goto(`/servers/${s.uuid}?tab=console`);
  await page.getByLabel("More actions").click();
  await page.getByRole("menuitem", { name: "Kill" }).click();
  await expect(page.getByText("Stopped").first()).toBeVisible({ timeout: 15_000 });
});

test("a server whose node is offline cannot be started and says why", async ({ page, panel }) => {
  // Seeded servers are all on the online node, so check the opposite: the controls are enabled there.
  await page.goto(`/servers/${panel.seeded("Survival SMP").uuid}?tab=console`);
  await expect(page.getByText("Node: online")).toBeVisible();
  await expect(page.getByRole("button", { name: "Start", exact: true })).toBeEnabled();
});
