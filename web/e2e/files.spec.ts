import { expect, test, unique } from "./support";

async function serverWithFiles(panel: import("./support").Panel) {
  const s = await panel.createServer(unique("Files test"));
  await panel.writeFile(s.uuid, "server.properties", "motd=hello\n");
  await panel.writeFile(s.uuid, "ops.json", "[]\n");
  await panel.writeFile(s.uuid, "config/paper-global.yml", "a: 1\n");
  await panel.writeFile(s.uuid, "plugins/Essentials/config.yml", "essentials: true\n");
  await panel.writeFile(s.uuid, "plugins/WorldEdit/config.yml", "worldedit: true\n");
  await panel.writeFile(s.uuid, "logs/latest.log", "started\n");
  return s;
}
const search = (page: import("@playwright/test").Page) => page.getByRole("searchbox", { name: "Search files" });
const rows = (page: import("@playwright/test").Page) => page.getByRole("table").getByRole("row");

test("search finds files in every subfolder and shows where they are", async ({ page, panel }) => {
  const s = await serverWithFiles(panel);
  await page.goto(`/servers/${s.uuid}?tab=files`);
  await expect(page.getByRole("button", { name: "server.properties", exact: true })).toBeVisible();
  await search(page).fill("config");
  await expect(page.getByText(/3 matches/)).toBeVisible();
  await expect(rows(page).filter({ hasText: "/plugins/Essentials" })).toHaveCount(1);
  await expect(rows(page).filter({ hasText: "/plugins/WorldEdit" })).toHaveCount(1);
  await expect(page.getByRole("button", { name: "server.properties", exact: true })).toHaveCount(0);
});

test("opening a result goes to the file; opening a folder result goes into the folder", async ({ page, panel }) => {
  const s = await serverWithFiles(panel);
  await page.goto(`/servers/${s.uuid}?tab=files`);
  await search(page).fill("Essentials");
  await page.getByRole("button", { name: /^Essentials/ }).click();
  await expect(page.getByRole("button", { name: "config.yml", exact: true })).toBeVisible();
  await expect(search(page)).toHaveValue("");
  await expect(page.getByRole("navigation", { name: "Breadcrumb" })).toContainText("plugins");

  await page.getByRole("button", { name: "Root folder" }).click();
  await search(page).fill("latest");
  await page.getByRole("button", { name: /^latest\.log/ }).click();
  await expect(page.getByRole("heading", { name: "/logs/latest.log" })).toBeVisible();
  await expect(page.locator(".cm-content")).toContainText("started");
});

test("search from inside a folder stays inside that folder", async ({ page, panel }) => {
  const s = await serverWithFiles(panel);
  await page.goto(`/servers/${s.uuid}?tab=files`);
  await page.getByRole("button", { name: "plugins", exact: true }).click();
  await expect(page.getByRole("button", { name: "Essentials", exact: true })).toBeVisible();
  await search(page).fill("config");
  await expect(page.getByText(/2 matches/)).toBeVisible();
});

test("no match says where it looked, and Escape clears the search", async ({ page, panel }) => {
  const s = await serverWithFiles(panel);
  await page.goto(`/servers/${s.uuid}?tab=files`);
  await search(page).fill("nothing-like-this");
  await expect(page.getByText(/No files or folders named “nothing-like-this” in any folder/)).toBeVisible();
  await search(page).press("Escape");
  await expect(page.getByRole("button", { name: "server.properties", exact: true })).toBeVisible();
});

test("edit a file, save it, and the change is really on disk", async ({ page, panel }) => {
  const s = await serverWithFiles(panel);
  await page.goto(`/servers/${s.uuid}?tab=files`);
  await page.getByRole("button", { name: "server.properties", exact: true }).click();
  const editor = page.locator(".cm-content");
  await expect(editor).toContainText("motd=hello");
  await expect(page.getByRole("button", { name: "Save" })).toBeDisabled(); // nothing changed yet
  await editor.click();
  await page.keyboard.press("Control+End");
  await page.keyboard.type("max-players=20");
  await expect(page.getByText("(modified)")).toBeVisible();
  await page.getByRole("button", { name: "Save" }).click();
  await expect(page.getByRole("status")).toHaveText("Saved.");
  expect(await panel.readFile(s.uuid, "server.properties")).toContain("max-players=20");
});

test("leaving with unsaved changes asks first, and Cancel keeps you in the editor", async ({ page, panel }) => {
  const s = await serverWithFiles(panel);
  await page.goto(`/servers/${s.uuid}?tab=files`);
  await page.getByRole("button", { name: "ops.json", exact: true }).click();
  await page.locator(".cm-content").click();
  await page.keyboard.type("x");
  const asked: string[] = [];
  page.once("dialog", (d) => { asked.push(d.message()); void d.dismiss(); });
  await page.getByRole("button", { name: "Back" }).click();
  expect(asked.join()).toMatch(/unsaved/i);
  await expect(page.getByRole("heading", { name: "/ops.json" })).toBeVisible();
  page.once("dialog", (d) => void d.accept());
  await page.getByRole("button", { name: "Back" }).click();
  await expect(page.getByRole("button", { name: "ops.json", exact: true })).toBeVisible();
});

test("deleting a search result removes that file only, after confirming", async ({ page, panel }) => {
  const s = await serverWithFiles(panel);
  await page.goto(`/servers/${s.uuid}?tab=files`);
  await search(page).fill("config.yml");
  await expect(page.getByText(/2 matches/)).toBeVisible();
  const del = async () => {
    await page.getByRole("button", { name: "Actions for plugins/WorldEdit/config.yml" }).click();
    await page.getByRole("menuitem", { name: "Delete" }).click();
  };
  await del();
  const dialog = page.getByRole("dialog", { name: /^Delete/ });
  await expect(dialog).toContainText("cannot be undone");
  await dialog.getByRole("button", { name: "Cancel" }).click();
  expect(await panel.readFile(s.uuid, "plugins/WorldEdit/config.yml")).toContain("worldedit");

  await del();
  await page.getByRole("dialog", { name: /^Delete/ }).getByRole("button", { name: "Delete", exact: true }).click();
  await expect(page.getByText(/1 match\b/)).toBeVisible();
  expect(await panel.readFile(s.uuid, "plugins/Essentials/config.yml")).toContain("essentials");
});

test("uploading with the file picker streams the file to the server", async ({ page, panel }) => {
  const s = await serverWithFiles(panel);
  await page.goto(`/servers/${s.uuid}?tab=files&dir=/plugins`);
  const chooser = page.waitForEvent("filechooser");
  await page.getByRole("button", { name: "Upload files" }).click();
  await (await chooser).setFiles([{ name: "hello.txt", mimeType: "text/plain", buffer: Buffer.from("uploaded from a browser") }]);
  await expect(page.getByRole("heading", { name: /Uploads finished/ })).toBeVisible();
  await expect(page.getByRole("button", { name: "hello.txt", exact: true })).toBeVisible();
  expect(await panel.readFile(s.uuid, "plugins/hello.txt")).toBe("uploaded from a browser");
});

test("renaming from the row menu", async ({ page, panel }) => {
  const s = await serverWithFiles(panel);
  await page.goto(`/servers/${s.uuid}?tab=files`);
  await page.getByRole("button", { name: "Actions for ops.json" }).click();
  await page.getByRole("menuitem", { name: "Rename" }).click();
  const name = page.getByRole("dialog", { name: /^Rename/ }).getByRole("textbox", { name: "New name" });
  await name.fill("admins.json");
  await name.press("Enter");
  await expect(page.getByRole("button", { name: "admins.json", exact: true })).toBeVisible();
  await expect(page.getByRole("button", { name: "ops.json", exact: true })).toHaveCount(0);
});
