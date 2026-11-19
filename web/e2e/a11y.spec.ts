import AxeBuilder from "@axe-core/playwright";
import { ANONYMOUS, expect, openMenuIfDrawer, settled, shellReady, test } from "./support";

// Fails on serious and critical WCAG 2 A/AA problems. Add known, accepted issues to `accepted` with a reason.
const accepted: Record<string, string> = {};

async function scan(page: import("@playwright/test").Page, label: string) {
  const result = await new AxeBuilder({ page }).withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa"]).analyze();
  const bad = result.violations.filter((v) => (v.impact === "serious" || v.impact === "critical") && !accepted[v.id]);
  const summary = bad.map(
    (v) =>
      `${v.id} (${v.impact}): ${v.help}\n    ${v.nodes
        .slice(0, 3)
        .map((n) => n.target.join(" "))
        .join("\n    ")}`,
  );
  expect(summary, `${label}\n${summary.join("\n")}`).toEqual([]);
}

test.describe("signed out", () => {
  test.use({ storageState: ANONYMOUS });
  test("sign-in page", async ({ page }) => {
    await page.goto("/login");
    await expect(page.getByRole("heading", { name: "Welcome back" })).toBeVisible();
    await scan(page, "sign-in");
  });
  test("password reset page", async ({ page }) => {
    await page.goto("/reset#token=abcdefghijklmnopqrstuvwxyz");
    await expect(page.getByRole("heading", { name: "Choose a new password" })).toBeVisible();
    await scan(page, "reset");
  });
  test("forgot password page", async ({ page }) => {
    await page.goto("/forgot");
    await expect(page.getByRole("heading", { name: "Forgot your password?" })).toBeVisible();
    await scan(page, "forgot");
  });
});

for (const [name, path] of [
  ["servers", "/"],
  ["new server", "/servers/new"],
  ["nodes", "/nodes"],
  ["eggs", "/eggs"],
  ["database hosts", "/database-hosts"],
  ["audit log", "/audit"],
  ["account", "/account"],
] as const) {
  test(`${name} page`, async ({ page }) => {
    await page.goto(path);
    await settled(page);
    await scan(page, name);
  });
}

for (const tab of [
  "console",
  "files",
  "mods",
  "backups",
  "schedules",
  "startup",
  "network",
  "databases",
  "activity",
  "users",
  "settings",
]) {
  test(`server ${tab} tab`, async ({ page, panel }) => {
    await page.goto(`/servers/${panel.seeded("Survival SMP").uuid}?tab=${tab}`);
    await settled(page);
    await scan(page, `server ${tab}`);
  });
}

test("servers page with a filter that matches nothing", async ({ page }) => {
  await page.goto("/");
  await page.getByRole("searchbox", { name: "Filter servers" }).fill("zzz");
  await expect(page.getByText("No servers match")).toBeVisible();
  await scan(page, "empty filter");
});

test("quick switcher", async ({ page }) => {
  await page.goto("/");
  await shellReady(page);
  await openMenuIfDrawer(page);
  await page.keyboard.press("Control+k");
  await expect(page.getByRole("combobox", { name: "Search servers and pages" })).toBeFocused();
  await scan(page, "quick switcher");
});

test("file search results", async ({ page, panel }) => {
  const s = await panel.createServer();
  await panel.writeFile(s.uuid, "plugins/A/config.yml");
  await page.goto(`/servers/${s.uuid}?tab=files`);
  await page.getByRole("searchbox", { name: "Search files" }).fill("config");
  await expect(page.getByText(/1 match/)).toBeVisible();
  await scan(page, "file search");
});
