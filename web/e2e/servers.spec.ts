import { expect, test, unique, watchErrors } from "./support";

const cards = (page: import("@playwright/test").Page) => page.getByRole("article");
const names = async (page: import("@playwright/test").Page) =>
  cards(page)
    .getByRole("link")
    .evaluateAll((els) =>
      els.filter((e) => e.closest("h1,article")?.tagName === "ARTICLE" && !e.classList.contains("btn")).map((e) => e.textContent!.trim()),
    );
const filter = (page: import("@playwright/test").Page) => page.getByRole("searchbox", { name: "Filter servers" });

test.beforeEach(async ({ page }) => {
  await page.goto("/");
  await expect(cards(page).first()).toBeVisible();
});

test("lists every server with its game and port, alphabetically", async ({ page, panel }) => {
  const errors = watchErrors(page);
  await expect(cards(page)).toHaveCount(panel.ready.servers.length);
  const shown = await names(page);
  const sorted = [...shown].sort((a, b) => a.localeCompare(b, undefined, { numeric: true, sensitivity: "base" }));
  expect(shown).toEqual(sorted);
  const s = panel.seeded("Rust Wipe Friday");
  await expect(cards(page).filter({ hasText: s.name })).toContainText(`Rust · Port ${s.port}`);
  expect(errors).toEqual([]);
});

test("numbered names sort naturally (2 before 10)", async ({ page }) => {
  const shown = await names(page);
  expect(shown.indexOf("Lobby 1")).toBeLessThan(shown.indexOf("Lobby 2"));
  expect(shown.indexOf("Lobby 2")).toBeLessThan(shown.indexOf("Lobby 10"));
});

test.describe("filter", () => {
  test("by name", async ({ page }) => {
    await filter(page).fill("sky");
    expect((await names(page)).sort()).toEqual(["SkyWars", "Skyblock"]);
  });
  test("by game, including servers whose name does not say which game they run", async ({ page, panel }) => {
    await filter(page).fill("valheim");
    const want = panel.ready.servers
      .filter((s) => s.egg === "Valheim" || /valheim/i.test(s.name))
      .map((s) => s.name)
      .sort();
    expect(want.length).toBeGreaterThan(2);
    expect((await names(page)).sort()).toEqual(want);
    expect(
      want.some((n) => !/valheim/i.test(n)),
      "needs a server whose name does not mention its game",
    ).toBe(true);
  });
  test("by port", async ({ page, panel }) => {
    const s = panel.seeded("Terraria Hardmode");
    await filter(page).fill(String(s.port));
    expect(await names(page)).toEqual(["Terraria Hardmode"]);
  });
  test("by node", async ({ page, panel }) => {
    await filter(page).fill("eu-west");
    await expect(cards(page)).toHaveCount(panel.ready.servers.length);
  });
  test("by several words at once", async ({ page }) => {
    await filter(page).fill("rust modded");
    expect(await names(page)).toEqual(["Rust Modded"]);
  });
  test("shows what happened when nothing matches, and Clear brings everything back", async ({ page, panel }) => {
    await filter(page).fill("zzzzz");
    await expect(page.getByText("No servers match")).toBeVisible();
    await expect(cards(page)).toHaveCount(0);
    await page.getByRole("button", { name: "Clear filter" }).click();
    await expect(filter(page)).toHaveValue("");
    await expect(cards(page)).toHaveCount(panel.ready.servers.length);
  });
  test("Escape clears it", async ({ page, panel }) => {
    await filter(page).fill("sky");
    await filter(page).press("Escape");
    await expect(filter(page)).toHaveValue("");
    await expect(cards(page)).toHaveCount(panel.ready.servers.length);
  });
  test("the / key jumps to it from anywhere on the page, but types normally inside other fields", async ({ page }) => {
    await expect(filter(page)).not.toBeFocused();
    await page.keyboard.press("/");
    await expect(filter(page)).toBeFocused();
    await page.keyboard.type("a/b");
    await expect(filter(page)).toHaveValue("a/b");
  });
  test("the add-server tile is not shown among filtered results", async ({ page }) => {
    const tile = page.getByRole("main").getByRole("link", { name: "New server" });
    await expect(tile).toHaveCount(2); // header button + tile
    await filter(page).fill("sky");
    await expect(tile).toHaveCount(1);
  });
});

test.describe("status", () => {
  test("chips filter by state, and a started server keeps its place in the list", async ({ page, panel }) => {
    const s = await panel.createServer(unique("Status test"));
    await page.reload();
    const before = await names(page);
    const group = page.getByRole("group", { name: "Filter by status" });
    await expect(group).toHaveCount(0); // everything is stopped, so there is nothing to choose between

    await panel.power(s.uuid, "start");
    await panel.waitFor(s.uuid, "running");
    await expect(group.getByRole("button", { name: /^Running/ })).toBeVisible({ timeout: 10_000 });
    await expect(group.getByRole("button", { name: /^All/ })).toHaveAttribute("aria-pressed", "true");
    expect(await names(page)).toEqual(before); // same order after the state change

    await group.getByRole("button", { name: /^Running/ }).click();
    await expect(group.getByRole("button", { name: /^Running/ })).toHaveAttribute("aria-pressed", "true");
    await expect(cards(page)).toHaveCount(1);
    await expect(cards(page).filter({ hasText: s.name })).toBeVisible();
    await group.getByRole("button", { name: /^Stopped/ }).click();
    await expect(cards(page).filter({ hasText: s.name })).toHaveCount(0);
    await panel.power(s.uuid, "kill");
  });

  test("Start and Stop work from the card", async ({ page, panel }) => {
    const s = await panel.createServer(unique("Card power"));
    await page.reload();
    const card = cards(page).filter({ hasText: s.name });
    await card.getByRole("button", { name: "Start" }).click();
    await expect(card.getByText("Running", { exact: true })).toBeVisible({ timeout: 15_000 });
    await card.getByRole("button", { name: "Stop" }).click();
    await expect(card.getByText("Stopped", { exact: true })).toBeVisible({ timeout: 15_000 });
  });
});
