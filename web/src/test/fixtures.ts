import type { components } from "../api/client";

type ServerView = components["schemas"]["ServerView"];
type User = components["schemas"]["User"];

export const admin = { id: 1, email: "admin@example.com", is_admin: true } as User;
export const bob = { id: 2, email: "bob@example.com", is_admin: false } as User;

let n = 0;
export function server(over: Partial<ServerView> & { name: string }): ServerView {
  n++;
  return {
    uuid: `00000000-0000-4000-8000-${String(n).padStart(12, "0")}`,
    owner_id: 1,
    node_id: 1,
    egg_id: 1,
    port: 25565 + n,
    state: "offline",
    node_online: true,
    egg_name: "Paper Minecraft",
    node_name: "eu-west-1",
    memory_mb: 1024,
    swap_mb: 0,
    cpu_pct: 0,
    cpuset: "",
    disk_mb: 10240,
    io_weight: 0,
    pids_limit: 1024,
    backup_limit: 20,
    env: {},
    owner: true,
    permissions: ["power", "console.read", "console.send", "files.read", "backups.read", "schedules.manage"],
    ...over,
  } as ServerView;
}

/** The six servers used across the UI tests. */
export function sampleServers(): ServerView[] {
  return [
    server({ name: "Survival SMP", port: 25565, state: "running" }),
    server({ name: "Creative Plots", port: 25566 }),
    server({ name: "Skyblock", port: 25567 }),
    server({ name: "Valheim Friends", port: 25568, egg_name: "Valheim", state: "installing" }),
    server({ name: "Rust Wipe Friday", port: 25569, egg_name: "Rust", node_name: "us-east-1" }),
    server({ name: "Server 10", port: 25570, egg_name: "Terraria", state: "install_failed" }),
    server({ name: "Server 2", port: 25571, egg_name: "Terraria" }),
  ];
}
