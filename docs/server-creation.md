# Server creation

How a game server comes to exist in GamePanel: who asks for it, what the panel
checks, what the node does, how it fails, and how it is tested. This page is
the source for the wiki article of the same name.

- [The usual hosting flow](#the-usual-hosting-flow)
- [Creating a server through the API](#creating-a-server-through-the-api)
- [What happens, step by step](#what-happens-step-by-step)
- [What is checked](#what-is-checked)
- [States](#states)
- [When something fails](#when-something-fails)
- [Compared with Pterodactyl](#compared-with-pterodactyl)
- [Selling servers: what a shop integration needs today](#selling-servers-what-a-shop-integration-needs-today)
- [How it is tested](#how-it-is-tested)
- [Code map](#code-map)

## The usual hosting flow

On a game hosting site the panel is rarely where a server is ordered. The usual
chain, the same one Pterodactyl hosts use, is:

1. **A customer buys a plan** on the host's shop or billing system (WHMCS,
   Paymenter, Blesta, a custom checkout). The plan fixes the game and the
   limits: memory, CPU, disk, ports, backups, databases.
2. **The shop calls the panel's API** once the payment clears, with an
   administrator's API key. It makes sure the customer has a panel account,
   then asks for a server with the plan's egg, node and limits, owned by that
   customer.
3. **The panel checks and records the server**, picks a port on the node, and
   sends the server to the node.
4. **The node prepares it in the background**: pulls the game's image, builds
   the container, runs the egg's install script (downloading the game files).
5. **The customer signs in** and finds the server under their account, shown
   as installing until the install ends, then offline and ready to start.

Later events go the same way: the shop changes limits on an upgrade
(`PUT /api/servers/{uuid}/limits`) and deletes the server when the plan ends
(`DELETE /api/servers/{uuid}`).

An administrator can also create servers by hand from **Servers → New
server**, which uses the same API call.

## Creating a server through the API

`POST /api/servers`, administrators only. Authenticate with an API key made
under **Account → API keys** by an administrator account:

```http
POST /api/servers HTTP/1.1
Authorization: Bearer gp_...
Content-Type: application/json

{
  "name": "Steve's survival world",
  "owner_id": 42,
  "node_id": 1,
  "egg_id": 3,
  "memory_mb": 4096,
  "cpu_pct": 200,
  "disk_mb": 20480,
  "backup_limit": 5,
  "port_limit": 2,
  "database_limit": 1,
  "env": { "MINECRAFT_VERSION": "1.21.1", "SERVER_JARFILE": "server.jar" }
}
```

| Field | Required | Meaning |
|---|---|---|
| `name` | yes | 1–64 characters, one line, plain text. Trimmed. |
| `owner_id` | no | The account that owns the server. Defaults to the caller. |
| `node_id` | yes | The node to run it on. There is no automatic node choice yet. |
| `egg_id` | yes | The egg (game and install script). |
| `memory_mb` | yes | 64 MB to 1 TB. Counted against the node's memory capacity. |
| `cpu_pct` | no | 100 = one core, 0 = unlimited. |
| `swap_mb` | no | 0 = none, -1 = unlimited. |
| `cpuset` | no | Pin to cores, e.g. `0-1`. |
| `pids_limit` | no | Processes, default 1024. |
| `io_weight` | no | 0 = default, else 10–1000. |
| `disk_mb` | no | 0 = unlimited. Required when the node tracks disk capacity. |
| `backup_limit` | no | Default 20. |
| `port_limit` | no | Ports the owner may add beyond the primary one. |
| `database_limit` | no | Databases the owner may create. |
| `bandwidth_mbit` | no | Network limit each way, 0 = unlimited. |
| `port` | no | Primary port. 0 picks the first free port in the node's range. |
| `env` | no | Values for the egg's variables. Variables left out take the egg's default. |

The reply is `201` with the server, including its `uuid` and `state`
(normally `installing`). The full schema is in
[`web/openapi.json`](../web/openapi.json) under `createServer`.

The reply comes as soon as the node has accepted the server, not when it is
ready. To know when it is, poll `GET /api/servers/{uuid}` until `state` is
`offline` (ready), or `install_failed`.

## What happens, step by step

```
shop / admin          panel                               node (daemon)                   Docker
     │ POST /api/servers │                                      │                              │
     │──────────────────▶│ 1 check name, owner, egg, variables  │                              │
     │                   │ 2 one transaction: check node        │                              │
     │                   │   capacity, pick port, insert server │                              │
     │                   │ 3 server.create ────────────────────▶│ 4 make data folder, write    │
     │                   │                                      │   settings, mark "stale"     │
     │                   │◀──────────────────────── accepted ───│                              │
     │◀─────────── 201 ──│ 5 audit log entry                    │ 6 in the background:         │
     │                   │                                      │   pull image, build ─────────▶│
     │                   │◀────── state: installing, console ───│   container                  │
     │                   │                                      │ 7 run the install script in  │
     │                   │                                      │   its own container ─────────▶│
     │                   │◀──────────── state: offline ─────────│ 8 mark installed             │
```

1. **Input checks** (panel, before anything is saved). See
   [What is checked](#what-is-checked).
2. **Database transaction.** Node capacity is checked (memory and, if the node
   tracks it, disk, both with the node's overallocation percentage) and the
   port is picked, in the same transaction as the insert. The panel keeps one
   database connection for writes, so two creates at once run one after the
   other and cannot take the same port or the same last megabytes.
3. **Sent to the node** over the websocket the node keeps open to the panel.
   The node receives the complete server description: image, startup command,
   stop command, environment (egg defaults overlaid with the given values,
   plus `SERVER_MEMORY` and `SERVER_PORT`), limits, ports (each open for TCP
   and UDP) and the install script.
4. **The node registers the server.** It makes the data folder
   (`<data>/servers/<uuid>`, handed to uid 1000, which game images usually
   run as), saves the description (`<data>/state/<uuid>.json`), and marks the
   server *stale*: its container does not exist yet. The marker is written
   first, so a node that crashes here still knows the container is missing.
5. **The panel replies `201`** and records `server.create` in the audit log.
6. **Container build** (node, background). The image is pulled and the
   container created with every capability dropped, `no-new-privileges`, an
   init process, and the memory, swap, CPU, process, IO and port settings.
   Pulling a large image can take minutes; this is why it is not done while
   the panel waits. On success the stale marker is removed.
7. **Install script** (only if the egg has one). It runs in a separate
   container (the egg's install image, also without capabilities, at least
   1 GB memory, 4096 processes, stopped after 30 minutes), with the server's
   folder at `/mnt/server` and the script at `/mnt/install`. Its output is the
   server's console. Afterwards the files are handed to the folder's owner.
8. **Done.** The node records the install (`<uuid>.installed`) and reports the
   server `offline`, ready to start.

## What is checked

| Check | Where | Answer when it fails |
|---|---|---|
| Caller is an administrator | panel | 403 |
| Name: not empty after trimming, one line, no control or text-direction characters (U+202E and friends, which can make one name read like another) | panel | 422 |
| Owner account exists | panel | 422 "there is no account with that owner id" |
| Egg exists | panel | 422 "unknown egg" |
| Every key in `env` is one of the egg's variables (so `SERVER_PORT`, `LD_PRELOAD` and the like cannot be set) | panel | 422 "this egg has no variable …" |
| Every variable's value, given or default, passes the egg's rules (`required`, `regex`, `in`, `integer`, `between`, `max` …) | panel | 422 naming the variable |
| Limits within their ranges (memory 64 MB–1 TB, CPU ≤ 6400 %, …) | panel | 422 |
| Node exists | panel | 404 |
| Node has memory (and disk, if tracked) left | panel | 422 saying how much is left |
| Port within the node's range and free; any port free if none given | panel | 422 |
| Node is connected | panel | 503 "the node is offline" |
| Server id is a UUID | node | 422 |

Variable values never pass through a shell: the startup command refers to them
as `${VAR}` and they reach the process only as environment variables.

## States

| State | Meaning |
|---|---|
| `installing` | The node is building the container and/or running the install script. Nothing else (start, update, delete, restore) can run meanwhile. |
| `install_failed` | The install script failed or was interrupted (for example the node restarted mid-install). The server will not start; **Reinstall** runs the script again. |
| `offline` | Ready. |
| `starting`, `running`, `stopping`, `crashed` | Normal running states. |
| `unknown` | The node is not connected. |

## When something fails

| What fails | What happens |
|---|---|
| Input check | Nothing is saved. 422 with the reason. |
| Node offline, or refuses | The server row and its port are deleted again (rolled back); 503 or the node's reason. |
| The node cannot write its folder or settings | The node removes what it wrote and refuses; the panel rolls back. |
| Image pull or container build (registry down, wrong image, no disk) | The server still exists. Its console says why. It stays stale, so its **next start builds the container again**, and if that fails too, the start fails with the reason. A node restart in between keeps the stale marker. |
| Install script exits non-zero or runs over 30 minutes | `install_failed`; the console shows the output. Fix the egg or the variables, then **Reinstall**. |
| Node restarts during the install | The server comes back as `install_failed` and can be reinstalled. |
| Panel restarts after replying | Nothing lost: the server is in the database and the node continues on its own. |

A server the node has but the panel does not (for example deleted while the
node was offline) is listed on the node's admin page as unknown, and can be
removed there.

## Compared with Pterodactyl

Pterodactyl, the panel GamePanel's eggs come from, splits this between the
panel (PHP) and Wings (its node agent). The flow is the same shape:

| | Pterodactyl | GamePanel |
|---|---|---|
| Who calls | Billing modules use the Application API (`POST /api/application/servers`) with an application key | `POST /api/servers` with an administrator's API key |
| Node choice | A node and allocation, or `deploy` with locations and port ranges for automatic choice | A node; the port is automatic or given |
| Variable checks | Egg rules checked on create | Egg rules checked on create (this page) |
| Database | Transaction; on a Wings error the server is deleted again | Transaction; on a node error the server is deleted again |
| Node reply | Wings replies at once, then pulls and installs in the background | The same |
| Node to panel | Panel calls Wings over HTTPS; Wings needs an open port | The node dials the panel; no open port on the node |
| Install container | Runs the egg's script with configurable limits | Runs the egg's script without capabilities, with limits and a 30-minute cap |
| Install status | Wings reports success or failure to the panel | Reported as server state, output on the console |
| Start after install | Optional `start_on_completion` | Not yet |
| External id | `external_id` for the billing system's own reference | Not yet |
| Suspension | Suspend and unsuspend (for unpaid invoices) | Not yet |

## Selling servers: what a shop integration needs today

What works now, for a shop or billing module:

1. Create the customer's account: send an invitation as an administrator
   with `POST /api/invites` (API only; there is no page for it yet), or let
   them sign up if open sign-up is on.
2. Create the server with `owner_id` set to that account, or create it and
   then hand it over with `PUT /api/servers/{uuid}/owner` (by email).
3. Poll `GET /api/servers/{uuid}` until `offline`.
4. On upgrade, `PUT /api/servers/{uuid}/limits`. On cancellation,
   `DELETE /api/servers/{uuid}`.

What is missing, roughly in order of importance for a shop:

- **Suspension.** No way to stop a server and lock its owner out for an unpaid
  invoice without deleting it.
- **External id / safe retries.** If the shop's request times out after the
  panel created the server, retrying makes a second server. Pterodactyl's
  `external_id` lets a shop find the first one; an idempotency key would
  prevent the duplicate outright.
- **Creating accounts directly.** Accounts come from invitations or sign-up;
  there is no "create this user with this email" call for a shop.
- **Automatic node choice.** The shop must pick the node; the panel does not
  yet place a server on the node with the most room.
- **Start on completion**, and a **webhook** when the install ends, instead of
  polling.
- **Pterodactyl Application API compatibility**, so existing WHMCS and
  Paymenter modules work unchanged (the client API is already compatible).

## How it is tested

| Test | What it proves |
|---|---|
| `internal/e2e/create_test.go` `TestCreateServerChecksItsInput` | Bad variables, unknown variables, a required one left empty, an unknown owner, and bad names are refused with a reason and leave nothing; good values pass, defaults fill in, names are trimmed |
| `internal/e2e/create_test.go` `TestCreateOnAnOfflineNodeIsRolledBack` | No server or port row is left when the node is offline |
| `internal/e2e/limits_test.go` | Limits accepted and refused at create; node capacity accounting |
| `internal/panel/store/store_test.go` `TestPortAllocation`, `limits_test.go` | Port picking and capacity rules |
| `internal/e2e/install_test.go` `TestInstallScriptLifecycle` | Install success (files on disk, server starts) and failure (`install_failed`, start refused) |
| `internal/daemon/provision_test.go` | The node replies before the image is pulled and blocks other operations meanwhile; a failed pull leaves the server stale, says why, survives a node restart, and the next start builds the container; install and reinstall work without a container; a create that fails half way leaves nothing on disk |
| `web/e2e/create-server.spec.ts` | The form in a real browser: required fields, egg variables with defaults, the new server in the list ready to start |
| `scripts/docker-smoke.sh` | Against a real Docker engine: image pull, limits, dropped capabilities, ports |

Run them with `make test`, `make e2e` and `bash scripts/docker-smoke.sh`.

## Code map

| Step | Code |
|---|---|
| API handler and input checks | `internal/panel/api/servers.go` (`createServer`, `serverName`, `checkCreateEnv`) |
| Variable rules | `internal/panel/store/startup.go` (`ValidateVar`) |
| Transaction, capacity, port | `internal/panel/store/models.go` (`CreateServer`), `limits.go` (`checkCapacity`), `ports.go` (`pickPort`) |
| What the node receives | `internal/panel/api/daemonconn.go` (`specFor`, `pushServer`), `egg.go` (`buildEnv`) |
| Node: register, build, install | `internal/daemon/daemon.go` (`create`, `provision`, `runInstall`, `rebuildIfStale`) |
| Docker | `internal/daemon/runtime/docker.go` (`Create`, `Install`) |
| Create form | `web/src/pages/CreateServerPage.tsx` |
