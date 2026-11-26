# Pterodactyl API

GamePanel answers two of Pterodactyl's APIs in Pterodactyl's format, so tools written for Pterodactyl work with GamePanel:

- the **client API** (`/api/client`), used by Discord bots, mobile apps and scripts, with any GamePanel API key;
- the **application API** (`/api/application`), the part billing modules use (WHMCS, Paymenter, Blesta), with an **administrator's** API key.

Point the tool at your panel's address and give it a GamePanel key (`gp_...`) where it asks for a Pterodactyl key. After an [import](/guides/from-pterodactyl), the Pterodactyl keys people had (`ptlc_...`, `ptla_...`) keep working too. Errors come back in Pterodactyl's error format (`{"errors":[{"code":"…","status":"422","detail":"…"}]}`); refused fields name themselves in `meta.source_field`, as Pterodactyl's `ValidationException` does.

## Client API

| Endpoint | Notes |
|---|---|
| `GET /api/client/account` | `username` is the part of the email before `@`; first and last name are empty |
| `GET /api/client` | Your servers |
| `GET /api/client/servers/{id}` | Details, limits, feature limits, ports |
| `GET /api/client/servers/{id}/resources` | Live usage and state |
| `POST /api/client/servers/{id}/power` | `start`, `stop`, `restart`, `kill` |
| `POST /api/client/servers/{id}/command` | Send a console command |
| `GET /api/client/servers/{id}/files/list` | List a folder |
| `GET /api/client/servers/{id}/files/contents` | Read a file |
| `POST /api/client/servers/{id}/files/write` | Write a file |
| `GET /api/client/servers/{id}/backups` | List backups |

`{id}` is the server's identifier (the first 8 characters of its UUID) or the full UUID. States map to Pterodactyl's four: `starting`, `running` and `stopping` as they are, everything else as `offline`.

The same permissions apply as anywhere else: a subuser's key can only do what that subuser may.

## Application API

For billing modules. It takes an **administrator's API key** (Account → API keys while signed in as an admin), never a browser session; a customer's key gets `403`. Everything it does goes through the same code as [GamePanel's own shop API](/server-creation#selling-servers-the-shop-api): the node is told, the activity log records it under the key's account, and [webhooks](/server-creation#webhooks) fire.

### Setting up a billing module

| Module | Where | What to enter |
|---|---|---|
| WHMCS ([pterodactyl/whmcs](https://github.com/pterodactyl/whmcs)) | Products/Services → Servers | Hostname: your panel's host name; Password: the admin API key; tick "Secure" for HTTPS |
| Paymenter (Pterodactyl extension) | Extensions → Pterodactyl | Host: `https://panel.example.com`; API key: the admin API key |
| Blesta ([module-pterodactyl](https://github.com/blesta/module-pterodactyl)) | Modules → Pterodactyl → Add server | Host name; Application API key: the admin API key; Account API key: any GamePanel key (it only checks it) |

Then set up products as for Pterodactyl: pick a **location**, a **nest** and an **egg**, and the limits. Before that:

1. **Make a location** and put your nodes in it (Nodes → a node's settings → Location, or `POST /api/locations`). Modules deploy into a location; GamePanel then picks the online node there with the most free memory.
2. **Import your eggs** (Eggs → Import). Every egg is in nest 1.

Links the module shows customers (`/server/<identifier>`) open the server in GamePanel.

### Endpoints

| Endpoint | Notes |
|---|---|
| `GET /api/application/users` | `filter[email]`, `filter[external_id]`; paginated (`page`, `per_page`) |
| `GET /api/application/users/{id}`, `…/users/external/{external_id}` | `404` when there is none |
| `POST /api/application/users` | `email`, `external_id`, `password` (optional). `username`, `first_name` and `last_name` are accepted and not kept |
| `PATCH /api/application/users/{id}` | Email, external id, password |
| `DELETE /api/application/users/{id}` | Refused while the account owns servers |
| `GET /api/application/servers` | `filter[external_id]`, `filter[uuid]`, `filter[uuidShort]`, `filter[name]`; `include=allocations` |
| `GET /api/application/servers/{id}`, `…/servers/external/{external_id}` | |
| `POST /api/application/servers` | `deploy` (locations, port range) or `allocation` (default, additional); `201` |
| `PATCH /api/application/servers/{id}/details` | Name, description, owner (`user`), external id |
| `PATCH /api/application/servers/{id}/build` | Limits, feature limits, primary allocation, `add_allocations`, `remove_allocations` |
| `PATCH /api/application/servers/{id}/startup` | Variables, image, startup command |
| `POST /api/application/servers/{id}/suspend`, `…/unsuspend` | `204` |
| `POST /api/application/servers/{id}/reinstall` | `204` |
| `DELETE /api/application/servers/{id}`, `…/{id}/force` | `force` also deletes a server whose node is offline |
| `GET /api/application/nodes`, `…/nodes/{id}` | |
| `GET /api/application/nodes/deployable` | `memory`, `disk`, `location_ids[]`; online nodes with room. `include=allocations` lists up to 1,000 ports each |
| `GET /api/application/nodes/{id}/allocations` | The node's port range, the ones in use `assigned` |
| `GET`, `POST /api/application/locations`; `GET`, `PATCH`, `DELETE …/locations/{id}` | A location with nodes cannot be deleted |
| `GET /api/application/nests`, `…/nests/{id}` | One nest, id 1 |
| `GET /api/application/nests/{nest}/eggs`, `…/eggs/{egg}`; `GET /api/application/eggs`, `…/eggs/{egg}` | `include=variables` |

Numbers may be sent as JSON numbers or numeric strings, as Laravel accepts.

### How it maps

| Pterodactyl | GamePanel |
|---|---|
| Server `id` | A number each server gets when it is created. It is never reused, even after the server is deleted. [Imported](/guides/from-pterodactyl) servers keep their Pterodactyl id. |
| Server `identifier` | The first 8 characters of the UUID |
| Nest | GamePanel has none: every egg is in nest 1, and the nest in a path (`/nests/5/eggs/12`) is not checked, so products made on Pterodactyl keep working |
| Location | A location. Imported ones keep their id. |
| Allocation | A port in a node's range. Its id is `node_id × 65536 + port`; there are no allocation records to create first |
| `limits.memory` | Memory limit. `0` (unlimited) is refused: GamePanel always sets one, at least 64 MB |
| `limits.swap`, `disk`, `cpu`, `threads` | The same (`threads` pins CPUs; leaving it out keeps the current pinning) |
| `limits.io` | Not used: current Wings does not apply it either, and a node without the `io.weight` controller would refuse to start the server |
| `feature_limits.allocations` | Ports in total, the primary one included |
| `feature_limits.backups` | At least 1: GamePanel keeps one backup a server may have |
| `feature_limits.databases` | `null` is 0 |
| `oom_disabled`, `skip_scripts`, `deploy.dedicated_ip` | Accepted and not used: the OOM killer stays on, the egg's install script always runs, and servers listen on every address |
| `environment` | Values for the egg's variables, checked against its rules. Names the egg does not have are ignored, as Pterodactyl does |
| A create whose `external_id` exists | `422` on `external_id`, as Pterodactyl answers (GamePanel's own API returns the existing server instead) |
| No node in the locations has room, or none is online | `400` `NoViableNodeException` |

Two things are stricter than Pterodactyl, on purpose:

- **Passwords** must pass GamePanel's [password policy](/account/security) (12 characters or more, not a known leaked password). A new account whose password from the billing system is too weak is still made, without that password: the person gets an email with a link to choose one, as when no password is sent. Changing a password to a weak one is refused (`422` on `password`).
- **Administrators' email and password** cannot be changed through the API (`403`), and administrator rights cannot be given or taken (`422` on `root_admin`): a leaked billing key must not be a way into the other admins' accounts.

Changing a server's egg (`PATCH …/startup` with another `egg`) is refused; create a new server instead.

## Not supported

- The rest of the client API (schedules, databases, subusers, network, startup, and the file actions not listed above).
- The **client websocket** (`/api/client/servers/{id}/websocket`), so tools that stream the console from Pterodactyl will not.
- The rest of the application API: database hosts, mounts, server databases and node configuration. Nodes and eggs are managed in GamePanel.
