import fs from "node:fs";
import path from "node:path";
import { request, type FullConfig } from "@playwright/test";
import type { Ready } from "./support";

const STATE = path.join(import.meta.dirname, ".state");

/** Waits for the panel to finish seeding, then signs in once per role and saves the cookies for the tests. */
export default async function globalSetup(config: FullConfig) {
  const file = path.join(STATE, "ready.json");
  for (let i = 0; !fs.existsSync(file); i++) {
    if (i > 600) throw new Error("the test panel never finished seeding");
    await new Promise((r) => setTimeout(r, 100));
  }
  const ready: Ready = JSON.parse(fs.readFileSync(file, "utf8"));
  const baseURL = config.projects[0]!.use.baseURL!;
  for (const [name, who] of [["admin", ready.admin], ["user", ready.user]] as const) {
    const ctx = await request.newContext({ baseURL });
    const res = await ctx.post("/api/auth/login", { data: { email: who.email, password: who.password } });
    if (!res.ok()) throw new Error(`sign in as ${name} failed: ${res.status()} ${await res.text()}`);
    await ctx.storageState({ path: path.join(STATE, `${name}.json`) });
    await ctx.dispose();
  }
}
