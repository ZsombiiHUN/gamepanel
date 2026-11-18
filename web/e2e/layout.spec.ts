import { expect, isDrawer, openMenuIfDrawer, settled, test } from "./support";

// These run at 1440x900, 1366x768 and 390x844. Layout bugs only show up in a real browser.

const pages = ["/", "/servers/new", "/nodes", "/eggs", "/audit", "/account"];
const tabs = ["console", "files", "mods", "backups", "schedules", "startup", "network", "databases", "activity", "users", "settings"];

test.describe("sidebar", () => {
  test("keeps the admin links and account in view however many servers there are", async ({ page, panel }) => {
    expect(panel.ready.servers.length).toBeGreaterThanOrEqual(26);
    await page.goto("/");
    await openMenuIfDrawer(page);
    const vh = page.viewportSize()!.height;
    for (const name of ["Nodes", "Eggs", "Audit log"]) {
      const box = (await page.getByRole("link", { name, exact: true }).boundingBox())!;
      expect(box.y + box.height, `${name} is below the fold`).toBeLessThanOrEqual(vh);
    }
    const signOut = (await page.getByRole("button", { name: "Sign out" }).boundingBox())!;
    expect(signOut.y + signOut.height).toBeLessThanOrEqual(vh);
  });

  test("scrolls the server list on its own", async ({ page }) => {
    await page.goto("/");
    await openMenuIfDrawer(page);
    const list = page.getByRole("navigation", { name: "Servers" });
    const m = await list.evaluate((el) => ({ scroll: el.scrollHeight, client: el.clientHeight, overflow: getComputedStyle(el).overflowY }));
    expect(m.overflow).toBe("auto");
    expect(m.scroll).toBeGreaterThan(m.client); // 26 servers do not fit
    // The sidebar as a whole does not scroll.
    const aside = await page.getByRole("complementary", { name: "Navigation" }).evaluate((el) => el.scrollHeight - el.clientHeight);
    expect(aside).toBe(0);
  });

  test("can reach the last server in the list by scrolling it", async ({ page }) => {
    await page.goto("/");
    await openMenuIfDrawer(page);
    const last = page.getByRole("navigation", { name: "Servers" }).getByRole("link", { name: "Valheim Hardcore" });
    await last.scrollIntoViewIfNeeded();
    await expect(last).toBeInViewport();
  });
});

test.describe("page width", () => {
  for (const path of pages) {
    test(`${path} does not scroll sideways`, async ({ page }) => {
      await page.goto(path);
      await settled(page);
      const over = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
      expect(over, "horizontal overflow in px").toBeLessThanOrEqual(0);
    });
  }
  for (const tab of tabs) {
    test(`server tab "${tab}" does not scroll sideways`, async ({ page, panel }) => {
      await page.goto(`/servers/${panel.seeded("Survival SMP").uuid}?tab=${tab}`);
      await settled(page);
      const over = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
      expect(over, "horizontal overflow in px").toBeLessThanOrEqual(0);
    });
  }
});

test.describe("console", () => {
  test("the command box is on screen without scrolling, and stays there", async ({ page, panel }) => {
    const s = await panel.createServer();
    await panel.power(s.uuid, "start");
    await panel.waitFor(s.uuid, "running");
    await page.goto(`/servers/${s.uuid}?tab=console`);
    const input = page.getByRole("textbox", { name: "Console command" });
    await expect(input).toBeVisible();
    const vh = page.viewportSize()!.height;
    const at = async () => (await input.boundingBox())!;
    expect((await at()).y + (await at()).height, "command box starts below the fold").toBeLessThanOrEqual(vh);
    // Still reachable at the other end of the page.
    await page.evaluate(() => window.scrollTo(0, document.body.scrollHeight));
    expect((await at()).y + (await at()).height).toBeLessThanOrEqual(vh);
    await page.evaluate(() => window.scrollTo(0, 0));
    expect((await at()).y + (await at()).height).toBeLessThanOrEqual(vh);
    await panel.power(s.uuid, "kill");
  });

  test("on a phone the console comes before the charts", async ({ page, panel }) => {
    test.skip(!isDrawer(page), "layout order only differs on small screens");
    await page.goto(`/servers/${panel.seeded("Survival SMP").uuid}?tab=console`);
    const consoleTop = (await page.getByRole("region", { name: "Console" }).boundingBox())!.y;
    const chartsTop = (await page.getByRole("region", { name: "Usage" }).boundingBox())!.y;
    expect(consoleTop).toBeLessThan(chartsTop);
  });

  test("on a wide screen the charts stay above the console", async ({ page, panel }) => {
    test.skip(isDrawer(page), "wide screens only");
    await page.goto(`/servers/${panel.seeded("Survival SMP").uuid}?tab=console`);
    const consoleTop = (await page.getByRole("region", { name: "Console" }).boundingBox())!.y;
    const chartsTop = (await page.getByRole("region", { name: "Usage" }).boundingBox())!.y;
    expect(chartsTop).toBeLessThan(consoleTop);
  });
});

test.describe("keyboard access to scrolling areas", () => {
  test("a table that scrolls sideways can be focused, so keyboard users can scroll it", async ({ page, panel }) => {
    await page.goto(`/servers/${panel.seeded("Survival SMP").uuid}?tab=files`);
    const region = page.getByRole("region", { name: "Files" });
    await expect(region).toHaveAttribute("tabindex", "0");
    await region.focus();
    await expect(region).toBeFocused();
  });
});
