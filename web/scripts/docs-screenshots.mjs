// Takes the docs screenshots (docs/public/screenshots) from a running panel
// with real game servers, so the pictures show what users will see:
//
//   GP_EMAIL=admin@example.com GP_PASSWORD=... GP_SERVER=<uuid of a running Minecraft server> \
//     node scripts/docs-screenshots.mjs <out dir>
//
// GP_URL defaults to the Vite dev server (http://localhost:5173). It writes
// <name>-dark.png and <name>-light.png at 1440×900, 2×. Convert them to WebP
// (quality about 80) and copy them over docs/public/screenshots. Before taking
// them, give the server a backup or two so the Backups page is not empty.
import { chromium } from "@playwright/test";

const out = process.argv[2];
const base = process.env.GP_URL ?? "http://localhost:5173";
const { GP_EMAIL: email, GP_PASSWORD: password, GP_SERVER: server } = process.env;
if (!out || !email || !password || !server) {
	console.error("usage: GP_EMAIL=... GP_PASSWORD=... GP_SERVER=<uuid> node scripts/docs-screenshots.mjs <out dir>");
	process.exit(2);
}

const pages = [
	["servers", "/"],
	["new", "/servers/new"],
	["console", `/servers/${server}?tab=console`],
	["startup", `/servers/${server}?tab=startup`],
	["files", `/servers/${server}?tab=files`],
	["backups", `/servers/${server}?tab=backups`],
	["account", "/account"],
	["nodes", "/nodes"],
	["eggs", "/eggs"],
];

const browser = await chromium.launch();
for (const colorScheme of ["dark", "light"]) {
	const ctx = await browser.newContext({
		viewport: { width: 1440, height: 900 },
		deviceScaleFactor: 2,
		colorScheme,
		reducedMotion: "reduce",
	});
	const page = await ctx.newPage();
	await page.goto(`${base}/login`);
	await page.getByLabel("Email").fill(email);
	await page.getByLabel("Password", { exact: true }).fill(password);
	await page.keyboard.press("Enter");
	await page.waitForURL((u) => !u.pathname.startsWith("/login"));
	for (const [name, path] of pages) {
		await page.goto(base + path);
		// Pages with a live websocket never go idle; the fixed wait below covers them.
		await page.waitForLoadState("networkidle", { timeout: 5000 }).catch(() => undefined);
		if (name === "console") {
			// Start from a clean console and show a few everyday commands.
			const input = page.getByPlaceholder(/command/i);
			await input.waitFor();
			await page.waitForTimeout(2000);
			await page.getByRole("button", { name: /clear/i }).first().click();
			for (const c of ["list", "tps", "say Welcome back, everyone", "save-all"]) {
				await input.fill(c);
				await input.press("Enter");
				await page.waitForTimeout(1200);
			}
			await page.waitForTimeout(15000); // let the live charts draw a line
		} else {
			await page.waitForTimeout(2500);
		}
		await page.mouse.move(0, 899);
		await page.screenshot({ path: `${out}/${name}-${colorScheme}.png` });
		console.log(`${name}-${colorScheme}.png`);
	}
	await ctx.close();
}
await browser.close();
