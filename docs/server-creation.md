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
- [Selling servers: the shop API](#selling-servers-the-shop-api)
- [Webhooks](#webhooks)
- [Suspension](#suspension)
- [How it is tested](#how-it-is-tested)
- [Code map](#code-map)

## The usual hosting flow

On a game hosting site the panel is rarely where a server is ordered. The usual
chain, the same one Pterodactyl hosts use, is:

1. **A customer buys a plan** on the host's shop or billing system (WHMCS,
   Paymenter, Blesta, a custom checkout). The plan fixes the game and the
   limits: memory, CPU, disk, ports, backups, databases.
2. **The shop calls the panel's API** once the payment clears, with an
   administrator's API key. It makes sure the customer has a panel account
   (`POST /api/users`), then asks for a server with the plan's egg and
   limits, owned by that customer, tagged with the order number
   (`external_id`) so a retry cannot make a second one. It names a node, or
   lets the panel pick the one with the most room.
3. **The panel checks and records the server**, picks a port on the node, and
   sends the server to the node.
4. **The node prepares it in the background**: pulls the game's image, builds
   the container, runs the egg's install script (downloading the game files).
5. **The panel tells the shop** through a signed webhook when the install
   ends (`server.installed` or `server.install_failed`), and starts the
   server first if the shop asked for `start_on_completion`.
6. **The customer signs in** and finds the server under their account, shown
   as installing until the install ends, then ready (or already running).

Later events go the same way: the shop changes limits on an upgrade
(`PUT /api/servers/{uuid}/limits`), suspends the server when an invoice goes
unpaid (`POST /api/servers/{uuid}/suspend`) and lifts it when it is paid, and
deletes the server when the plan ends (`DELETE /api/servers/{uuid}`). See
[Selling servers: the shop API](#selling-servers-the-shop-api).

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
  "external_id": "order-1001",
  "start_on_completion": true,
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
| `node_id` | no | The node to run it on. Leave out (or 0) to let the panel pick: the online node with the most free memory that has room and a free port. |
| `node_ids` | no | With no `node_id`: the nodes the panel may pick from (a region, say). Default: all. |
| `external_id` | no | Your own id for the server (an order number), up to 191 characters, unique. A create whose `external_id` is already used returns that server with `200` and changes nothing, so a shop can retry safely. |
| `start_on_completion` | no | Start the server once it is installed. |
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

The reply is `201` with the server, including its `uuid`, `state` (normally
`installing`) and `install_status` (`installing`). A retry with the same
`external_id` gets `200` and the first server. The full schema is in
[`web/openapi.json`](../web/openapi.json) under `createServer`.

The reply comes as soon as the node has accepted the server, not when it is
ready. To know when it is, take the `server.installed` /
`server.install_failed` [webhook](#webhooks), or poll
`GET /api/servers/{uuid}` until `install_status` is `installed` or `failed`.
`install_status` is kept in the panel's database, so it is right after a panel
restart too (`state` is live and shows `unknown` while the node is away).

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
| With no node given: some allowed node is online | panel | 503 |
| With no node given: some online node has room and a port | panel | 422 "no node has room for this server" |
| `external_id` not used yet (else the existing server is returned, 200) | panel | — |
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

`suspended` is not a state but a flag on the server (`"suspended": true`); the
interface shows it in place of the state. Separately, `install_status`
(`installing`, `installed`, `failed`) records how the last install went.

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
| Node choice | A node and allocation, or `deploy` with locations and port ranges for automatic choice | A node, or automatic among all or listed nodes; the port is automatic or given |
| Variable checks | Egg rules checked on create | Egg rules checked on create (this page) |
| Database | Transaction; on a Wings error the server is deleted again | Transaction; on a node error the server is deleted again |
| Node reply | Wings replies at once, then pulls and installs in the background | The same |
| Node to panel | Panel calls Wings over HTTPS; Wings needs an open port | The node dials the panel; no open port on the node |
| Install container | Runs the egg's script with configurable limits | Runs the egg's script without capabilities, with limits and a 30-minute cap |
| Install status | Wings reports success or failure to the panel | Reported as server state, output on the console |
| Start after install | Optional `start_on_completion` | The same |
| External id | `external_id`; a duplicate is a validation error | `external_id`; a duplicate returns the existing server (safe retries) |
| Suspension | Suspend and unsuspend | The same; the node also refuses to start a suspended server |
| Accounts | `POST /api/application/users` | `POST /api/users`, with a "choose your password" link when no password is given |
| Telling the shop | The shop polls | Signed webhooks, retried for a day, plus polling |

## Selling servers: the shop API

Everything a shop or billing module needs, with one administrator API key
(`Authorization: Bearer gp_...`). Taking payments stays in the shop, as with
Pterodactyl; the panel creates, suspends and removes what the shop sold.

### What the shop does at each step

| When | Call |
|---|---|
| Customer pays for the first time | `POST /api/users` (find or create the account), then `POST /api/servers` with `owner_id`, `external_id` and the plan's limits |
| Request timed out, not sure it went through | Send exactly the same request again: the same `external_id` returns what was made (`200`) |
| Install finished | `server.installed` webhook (or poll `install_status`) |
| Upgrade or downgrade | `PUT /api/servers/{uuid}/limits` (applies at the next start) |
| Invoice overdue | `POST /api/servers/{uuid}/suspend` |
| Invoice paid | `POST /api/servers/{uuid}/unsuspend` |
| Customer changes email or password in the shop | `PATCH /api/users/{id}` |
| Plan cancelled | `DELETE /api/servers/{uuid}`, then, if they have nothing else, `DELETE /api/users/{id}` |
| Find things by the shop's ids | `GET /api/servers?external_id=order-1001`, `GET /api/users?external_id=cust-42` or `?email=` |

### Accounts

`POST /api/users`:

```json
{ "email": "steve@example.com", "external_id": "cust-42", "password": "optional" }
```

| Field | Meaning |
|---|---|
| `email` | The sign-in email. Stored in lower case. |
| `password` | Optional. Must pass the panel's password policy (12+ characters, not a common or leaked password). |
| `external_id` | Optional, the shop's customer id, unique. A create whose `external_id` already exists with the same email returns that account (`200`); with another email it is refused (`409`). |
| `admin` | Optional, makes an administrator. A shop should never need it. |
| `send_email` | Default `true`: when no password is given and the panel can send email, it mails the setup link. |

Without a password, the reply has a `setup_link` (valid 7 days) where the
customer chooses their password; the panel emails it too when SMTP and the
public URL are set up. After 7 days they use "forgot password". An email that
already has an account gets `409` naming that account's id, so the shop can
use it.

`PATCH /api/users/{id}` changes `email`, `external_id` (`""` clears it) or
`password`. A new password signs the account out everywhere. The email and
password of **administrator** accounts cannot be changed this way: a stolen
shop key must not be a way into the other admins' accounts.

`DELETE /api/users/{id}` refuses an account that still owns servers, and your
own. Its access to other people's servers goes with it, and schedules it saved
on those servers are turned off (they ran with its permissions).

### Choosing the node

Leave `node_id` out and the panel picks, among the online nodes (only those in
`node_ids`, if given):

1. Nodes that track memory capacity, the one with the most free memory first
   (counting the overallocation percentage);
2. then nodes that do not track capacity, the least used first.

It takes the first that has room for the memory and disk and a free port.
None online: `503`. None with room: `422 no node has room for this server`.

### Safe retries

A shop's request can time out after the panel created the server. With
`external_id`, sending the same request again is safe: the panel answers
`200` with the server it already made and changes nothing. Two identical
creates at the same moment also end in one server: the database's unique index
on `external_id` lets only one in, and the other gets the winner.

One edge remains: if the first create is still waiting for its node when the
retry arrives, the retry gets the server as it is, and if the node then
refuses the first create, that server is rolled back. A shop that sees a
`404` for a server it was given should simply create again.

Without `external_id` the panel cannot tell a retry from a second order.

### Customer accounts made another way

Invitations (`POST /api/invites`) and open sign-up still work, for hosts that
want the customer to make their own account first. Hand a server to an
existing account with `PUT /api/servers/{uuid}/owner` (by email).

### Not done yet

- **Pterodactyl Application API compatibility** (`/api/application/...`), so
  existing WHMCS, Paymenter and Blesta modules for Pterodactyl work unchanged.
  The client API is already compatible. Until then a shop needs a module
  written for this API (the calls above).
- **A page for accounts and webhooks** in the interface. Both are API only for
  now; suspension has a button in each server's settings.

## Webhooks

The panel POSTs a JSON event to each registered address when something a shop
cares about happens. Register one with `POST /api/webhooks`:

```json
{ "url": "https://shop.example.com/gamepanel/webhook", "events": ["*"], "description": "Billing" }
```

The reply includes a `secret` (`whsec_...`), shown only here and when rotated
(`PUT /api/webhooks/{id}` with `"rotate_secret": true`). Keep it in the shop.

| Event | When |
|---|---|
| `server.created` | The node accepted a new server |
| `server.installed` | Its install finished (also after a reinstall) |
| `server.install_failed` | Its install failed |
| `server.suspended`, `server.unsuspended` | The suspension changed (suspending a suspended server sends nothing) |
| `server.deleted` | The server was deleted |
| `user.created`, `user.deleted` | An account was made or removed through `/api/users` |
| `ping` | Only from `POST /api/webhooks/{id}/ping`, to test the address |

Each delivery looks like:

```http
POST /gamepanel/webhook HTTP/1.1
Content-Type: application/json
User-Agent: GamePanel-Webhook/1
X-GamePanel-Event: server.installed
X-GamePanel-Delivery: 17
X-GamePanel-Signature: t=1763491200,v1=5f2b...

{"id":"7c9e...","event":"server.installed","created_at":1763491199,
 "data":{"uuid":"b910...","name":"Survival","external_id":"order-1001","owner_id":42,
         "node_id":1,"suspended":false,"install_status":"installed"}}
```

**Checking the signature.** `v1` is the hex HMAC-SHA256 of
`<t>.<raw body>`, keyed with the secret. Compute it over the body exactly as
received, compare in constant time, and refuse a `t` more than a few minutes
old so a recorded delivery cannot be sent again:

```php
[$t, $v1] = sscanf($_SERVER['HTTP_X_GAMEPANEL_SIGNATURE'], 't=%d,v1=%s');
$body = file_get_contents('php://input');
$ok = abs(time() - $t) < 300
   && hash_equals(hash_hmac('sha256', "$t.$body", $secret), $v1);
```

**Delivery.** Answer any 2xx within 10 seconds. Anything else (or no answer)
is tried again after 30 s, 1 min, 2 min, ... up to 6 h between tries, 12 tries
in all (about a day), then given up. Events are stored before they are sent,
so a panel restart loses nothing. A retry carries the same `id`; drop events
whose `id` you have already handled. Redirects are not followed. Deliveries
go out one at a time, so a slow receiver delays the others.

`GET /api/webhooks/{id}/deliveries` shows the last 100 with their status,
tries, the last HTTP status and error. `POST .../deliveries/{d}/retry` sends a
delivered or given-up one again. Finished deliveries are kept 30 days.

Webhook addresses are set by administrators and the panel will call any
address it can reach, internal ones included. Only give administrator API
keys to systems you trust with that.

## Suspension

`POST /api/servers/{uuid}/suspend` (admin), or **Suspend server** in the
server's settings. Nothing is deleted. While suspended:

- the server is stopped (gracefully, then forcibly after 30 s) and cannot be
  started: not by its owner, not by an admin, not by a schedule, not by the
  node's crash restart, not at the end of an install it arrived during, not
  after the node restarts;
- its owner and subusers still see it in their list, marked **Suspended**,
  with a note to contact their host, but every action is refused with
  `403 this server is suspended`: console, commands, power, files, SFTP,
  backups, schedules, startup settings, network, databases;
- open consoles and SFTP sessions are closed at their next access check
  (within seconds);
- admins can still look at and change everything except starting it.

`POST /api/servers/{uuid}/unsuspend` lifts it. The server is not started by
this. Both calls are idempotent and send a webhook only when something
changed.

The panel's record is what counts. If the node is offline when the server is
suspended, the panel still refuses everything at once, and the node is told
when it reconnects.

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
| `internal/e2e/shop_test.go` `TestShopSellsAServer` | The whole shop flow: account and server created and retried without duplicates, the panel picks the node, the server starts once installed, signed webhooks for every step, suspension locks the customer out (console, files, backups, startup, power) and blocks even an admin's start, unsuspension, then cancellation |
| `internal/e2e/shop_test.go` `TestShopAccounts` | Accounts without a password get a working setup link; duplicate emails and external ids are refused; a new password signs out everywhere; admin accounts cannot be changed or deleted through this; customers cannot use any of it |
| `internal/e2e/shop_test.go` `TestShopPicksANode`, `TestWebhookFailuresAreRetried` | Node choice skips offline and full nodes; a failed delivery is logged with the receiver's answer and not retried before its time; webhooks only get the events they asked for |
| `internal/daemon/suspend_test.go` | The node starts a server after its install when asked; a suspension stops a running server and blocks starts, also after a node restart and at the end of an install it arrived during |
| `web/e2e/suspend.spec.ts` | Suspending and lifting a suspension from the server's settings in a real browser |
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
| External ids, node choice, suspension | `internal/panel/api/shop.go`, `internal/panel/store/shop.go` |
| Accounts | `internal/panel/api/accounts.go` |
| Webhooks, install status | `internal/panel/api/webhooks.go` |
| Suspension on the node | `internal/daemon/daemon.go` (`start`, `update`, `stopSuspended`) |
