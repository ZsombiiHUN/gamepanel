# Pterodactyl client API

GamePanel answers part of Pterodactyl's **client API** (`/api/client`) in Pterodactyl's format, so Discord bots, mobile apps and scripts written for Pterodactyl can work with a GamePanel API key. Point them at your panel's address and give them a GamePanel key (`gp_...`) where they ask for a Pterodactyl client key.

## Supported

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

Errors come back in Pterodactyl's error format. States map to Pterodactyl's four: `starting`, `running` and `stopping` as they are, everything else as `offline`.

The same permissions apply as anywhere else: a subuser's key can only do what that subuser may.

## Not supported

- The rest of the client API (schedules, databases, subusers, network, startup, and the file actions not listed above).
- The **client websocket** (`/api/client/servers/{id}/websocket`), so tools that stream the console from Pterodactyl will not.
- The **application API** (`/api/application/...`), which billing modules such as WHMCS, Paymenter and Blesta use. Until it exists, a shop needs a module written for [GamePanel's own API](/server-creation#selling-servers-the-shop-api).
