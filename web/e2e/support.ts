import fs from "node:fs";
import path from "node:path";
import { expect, test as base, request, type APIRequestContext, type Page } from "@playwright/test";

export interface Ready {
  url: string;
  admin: { email: string; password: string };
  user: { email: string; password: string };
  node_id: number;
  servers: { uuid: string; name: string; egg: string; port: number }[];
  shared_to: string;
}

export const STATE = path.join(import.meta.dirname, ".state");
export const readState = (): Ready => JSON.parse(fs.readFileSync(path.join(STATE, "ready.json"), "utf8"));
export const ANONYMOUS = { cookies: [], origins: [] };

let seq = 0;
/** A name no other test (or earlier run in the same panel) has used. */
export const unique = (prefix: string) => `${prefix} ${Date.now().toString(36)}${(seq++).toString(36)}`;

export class Panel {
  private created: string[] = [];
  constructor(readonly api: APIRequestContext, readonly ready: Ready) {}

  /** Registers a server created some other way (e.g. through the UI) for cleanup. */
  track(uuid: string) {
    this.created.push(uuid);
  }

  /** Removes every server this test created, so tests that count or list servers see only the seeded ones. */
  async cleanup() {
    for (const uuid of this.created.splice(0)) {
      await this.api.post(`/api/servers/${uuid}/power`, { data: { action: "kill" } }).catch(() => undefined);
      await expect.poll(() => this.state(uuid), { timeout: 10_000 }).not.toMatch(/^(running|starting|stopping)$/).catch(() => undefined);
      await this.api.delete(`/api/servers/${uuid}`).catch(() => undefined);
    }
  }

  seeded(name: string) {
    const s = this.ready.servers.find((x) => x.name === name);
    if (!s) throw new Error(`no seeded server named ${name}`);
    return s;
  }

  async eggId(name = "Paper Minecraft"): Promise<number> {
    const eggs = await (await this.api.get("/api/eggs")).json();
    return eggs.find((e: { name: string; id: number }) => e.name === name).id;
  }

  /** Creates a throwaway server, so a test can start, edit or delete it without disturbing the shared ones. */
  async createServer(name = unique("Test server"), opts: { egg?: string; memory_mb?: number } = {}) {
    const res = await this.api.post("/api/servers", { data: { name, node_id: this.ready.node_id, egg_id: await this.eggId(opts.egg), memory_mb: opts.memory_mb ?? 256, disk_mb: 1024 } });
    expect(res.status(), await res.text()).toBe(201);
    const s = await res.json();
    this.created.push(s.uuid);
    return { uuid: s.uuid as string, name, port: s.port as number };
  }

  async power(uuid: string, action: "start" | "stop" | "kill") {
    const res = await this.api.post(`/api/servers/${uuid}/power`, { data: { action } });
    expect(res.status(), await res.text()).toBeLessThan(300);
  }

  async state(uuid: string): Promise<string> {
    return (await (await this.api.get(`/api/servers/${uuid}`)).json()).state;
  }
  async waitFor(uuid: string, state: string) {
    await expect.poll(() => this.state(uuid), { timeout: 15_000 }).toBe(state);
  }

  async writeFile(uuid: string, filePath: string, content = "x: 1\n") {
    const res = await this.api.put(`/api/servers/${uuid}/files/content`, { data: { path: filePath, content } });
    expect(res.status(), await res.text()).toBe(204);
  }
  async readFile(uuid: string, filePath: string): Promise<string> {
    const res = await this.api.get(`/api/servers/${uuid}/files/content`, { params: { path: filePath } });
    return (await res.json()).content;
  }
}

export const test = base.extend<{ panel: Panel }>({
  panel: async ({ baseURL }, use) => {
    const ready = readState();
    const api = await request.newContext({ baseURL, storageState: path.join(STATE, "admin.json") });
    const panel = new Panel(api, ready);
    await use(panel);
    await panel.cleanup();
    await api.dispose();
  },
});
export { expect };

/** True on phone-sized viewports, where the sidebar is a drawer behind a menu button. */
export const isDrawer = (page: Page) => (page.viewportSize()?.width ?? 1440) < 1024;

export async function openMenuIfDrawer(page: Page) {
  if (isDrawer(page)) {
    const open = page.getByRole("button", { name: "Open menu" });
    if (await open.isVisible()) await open.click();
  }
}

/** Counts failed API calls and uncaught errors so a test can assert the page behaved. */
export function watchErrors(page: Page) {
  const errors: string[] = [];
  page.on("pageerror", (e) => errors.push(`uncaught: ${e.message}`));
  page.on("response", (r) => {
    if (r.status() >= 500) errors.push(`${r.status()} ${r.url()}`);
  });
  return errors;
}
