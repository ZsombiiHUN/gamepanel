import fs from "node:fs";
import path from "node:path";
import { request } from "@playwright/test";
import { panelURL, stateDir, WORKERS } from "./slots";
import type { Ready } from "./support";

/** Waits for every worker's panel to finish seeding, then signs in once per role on each and saves the cookies for the tests. */
export default async function globalSetup() {
  await Promise.all(Array.from({ length: WORKERS }, (_, slot) => signIn(slot)));
}

async function signIn(slot: number) {
  const dir = stateDir(slot);
  const file = path.join(dir, "ready.json");
  for (let i = 0; !fs.existsSync(file); i++) {
    if (i > 600) throw new Error(`test panel ${slot} never finished seeding`);
    await new Promise((r) => setTimeout(r, 100));
  }
  const ready: Ready = JSON.parse(fs.readFileSync(file, "utf8"));
  for (const [name, who] of [["admin", ready.admin], ["user", ready.user]] as const) {
    const ctx = await request.newContext({ baseURL: panelURL(slot) });
    const res = await ctx.post("/api/auth/login", { data: { email: who.email, password: who.password } });
    if (!res.ok()) throw new Error(`sign in as ${name} on panel ${slot} failed: ${res.status()} ${await res.text()}`);
    await ctx.storageState({ path: path.join(dir, `${name}.json`) });
    await ctx.dispose();
  }
}
