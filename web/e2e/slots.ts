import os from "node:os";
import path from "node:path";

// Every worker gets a test panel of its own, so tests in different workers never
// see each other's servers, users or settings. Worker slot i (Playwright's
// TEST_PARALLEL_INDEX, which a replacement worker takes over after a failure)
// talks to the panel on PORT + i, and keeps its ready file and cookies in .state/<i>.

export const PORT = Number(process.env.E2E_PORT ?? 4173);

// One per core: much of a test is spent waiting on the panel, so this beats half the cores
// (93s against 114s on 4 cores), and more than 6 gained nothing on 8 cores.
export const WORKERS = Number(process.env.E2E_WORKERS ?? Math.min(6, os.availableParallelism()));

export const panelURL = (slot: number) => `http://127.0.0.1:${PORT + slot}`;
export const stateDir = (slot: number) => path.join(import.meta.dirname, ".state", String(slot));

/** This worker's slot. 0 outside a worker (the config and global setup look at every slot themselves). */
export const SLOT = Number(process.env.TEST_PARALLEL_INDEX ?? 0);
