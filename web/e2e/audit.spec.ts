import path from "node:path";
import { expect, STATE, test, unique } from "./support";

const search = (page: import("@playwright/test").Page) => page.getByRole("searchbox", { name: "Search the audit log" });
const rows = (page: import("@playwright/test").Page) => page.getByRole("table").getByRole("row");

test.describe("audit log search", () => {
  // One old, rare event buried under more than a page of newer ones.
  let needle: string;
  test.beforeAll(async ({ playwright, baseURL }) => {
    const api = await playwright.request.newContext({ baseURL, storageState: path.join(STATE, "admin.json") });
    needle = unique("needle-egg").replace(/\s/g, "-");
    const egg = await api.post("/api/eggs/import", {
      data: { name: needle, docker_images: { d: "img" }, startup: "run", config: { stop: "stop" } },
    });
    expect(egg.status()).toBe(201);
    for (let i = 0; i < 105; i++) {
      const n = await api.post("/api/nodes", { data: { name: `filler-${i}`, port_start: 30000, port_end: 30001 } });
      await api.delete(`/api/nodes/${((await n.json()) as { node: { id: number } }).node.id}`);
    }
    await api.dispose();
  });

  test("the first page is full of newer events and does not show the old one", async ({ page }) => {
    await page.goto("/audit");
    await expect(rows(page).nth(1)).toBeVisible();
    await expect(rows(page)).toHaveCount(101); // header + 100
    await expect(page.getByRole("cell", { name: needle })).toHaveCount(0);
    await expect(page.getByRole("button", { name: /Older/ })).toBeEnabled();
  });

  test("searching finds it anyway, across the whole log", async ({ page }) => {
    await page.goto("/audit");
    await expect(rows(page).nth(1)).toBeVisible();
    await search(page).fill(needle);
    await expect(rows(page)).toHaveCount(2);
    await expect(rows(page).nth(1)).toContainText("egg.import");
    await expect(page.getByText(`matching “${needle}”`)).toBeVisible();
    await expect(page.getByRole("button", { name: /Older/ })).toBeDisabled();
  });

  test("matches the user, the action and the address, with every word required", async ({ page, panel }) => {
    await page.goto("/audit");
    await search(page).fill("node.delete");
    await expect(rows(page).nth(1)).toContainText("node.delete");
    expect(await rows(page).count()).toBeGreaterThan(50);
    await search(page).fill(`${panel.ready.admin.email} egg.import`);
    await expect(rows(page).nth(1)).toContainText("egg.import");
    await expect(rows(page).filter({ hasText: "node.delete" })).toHaveCount(0);
    await search(page).fill("127.0.0.1 login.ok");
    await expect(rows(page).nth(1)).toContainText("login.ok");
  });

  test("an empty result says so and Escape brings the log back", async ({ page }) => {
    await page.goto("/audit");
    await search(page).fill("no-such-event-anywhere");
    await expect(page.getByText("No events match “no-such-event-anywhere”.")).toBeVisible();
    await search(page).press("Escape");
    await expect(rows(page).nth(1)).toBeVisible();
    await expect(page.getByText(/No events match/)).toHaveCount(0);
  });

  test("percent and underscore are searched for literally", async ({ page }) => {
    await page.goto("/audit");
    await search(page).fill("%");
    await expect(page.getByText("No events match “%”.")).toBeVisible();
    await search(page).fill("_");
    await expect(page.getByText("No events match “_”.")).toBeVisible();
  });

  test("paging works, and a new search starts again from the first page", async ({ page }) => {
    await page.goto("/audit");
    await expect(rows(page).nth(1)).toBeVisible();
    await page.getByRole("button", { name: /Older/ }).click();
    await expect(page.getByText(/^Page 2/)).toBeVisible();
    await expect(page.getByRole("cell", { name: needle })).toHaveCount(0); // 210 newer node events sit above it
    await page.getByRole("button", { name: /Older/ }).click();
    await expect(page.getByText(/^Page 3/)).toBeVisible();
    await expect(page.getByRole("cell", { name: needle })).toBeVisible(); // found by paging, too
    await search(page).fill("node.create");
    await expect(page.getByText(/^Page 1 · 100 events matching/)).toBeVisible();
  });
});
