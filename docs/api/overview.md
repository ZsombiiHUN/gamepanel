# API overview

Everything the interface does goes through the same HTTP API, so anything you can click, a script can do.

- **Base address**: `https://panel.example.com/api`
- **Format**: JSON in and out
- **Description**: OpenAPI 3.1. A running panel serves it at `/api/openapi.json`, with browsable docs at `/api/docs`, to signed-in users and API keys only (everyone else gets `404`). The same file is in the repository as [`web/openapi.json`](https://github.com/ZsombiiHUN/gamepanel/blob/main/web/openapi.json)

## Authentication

Create an API key under **Account → API keys**. You need your password (and a two-factor code if it is on). The key, which starts with `gp_`, is shown **once**; the panel keeps only a hash of it.

```sh
curl -H "Authorization: Bearer gp_..." https://panel.example.com/api/servers
```

- A key acts **as your account**, with everything your account may do. There are no scopes yet, so make a separate account for an integration that needs less.
- Keys can expire after a number of days (up to 3650), or never. The list shows when each was last used.
- Up to 25 keys per account.
- With `GAMEPANEL_REQUIRE_MFA` on, keys of accounts that have not set up a second factor stop working until they do.
- Revoke a key under **Account** at any time; SFTP sessions using it end within seconds.

Browsers use a session cookie instead. Requests that change something must come from the panel's own site (the `Origin` is checked), so other websites cannot act for a signed-in user.

## Errors

Errors use [RFC 9457 problem details](https://www.rfc-editor.org/rfc/rfc9457) (`application/problem+json`):

```json
{
  "title": "Unprocessable Entity",
  "status": 422,
  "detail": "this egg has no variable FOO"
}
```

| Status | Usually means |
|---|---|
| `400` | The request is malformed |
| `401` | Not signed in, or the key is wrong, expired or revoked |
| `403` | Signed in, but not allowed (missing permission, suspended server, not an admin) |
| `404` | Not found, **or no access**: servers you cannot see look exactly like servers that do not exist |
| `409` | Conflicts with the current state (e.g. the email is taken, the server is installing) |
| `422` | A value is invalid; `detail` says which and why |
| `429` | Too many requests, try later |
| `503` | The node is offline |

## Common calls

| Task | Call |
|---|---|
| List your servers | `GET /api/servers` |
| One server | `GET /api/servers/{uuid}` |
| Start, stop, restart, kill | `POST /api/servers/{uuid}/power` with `{"action": "restart"}` |
| Send a console command | `POST /api/servers/{uuid}/command` with `{"command": "say hi"}` |
| Read a file | `GET /api/servers/{uuid}/files/content?path=/server.properties` |
| Write a file | `PUT /api/servers/{uuid}/files/content` with `path` and `content` in the body |
| Start a backup | `POST /api/servers/{uuid}/backups` |
| Usage history | `GET /api/servers/{uuid}/metrics` |

### Example: restart a server

```sh
curl -X POST https://panel.example.com/api/servers/b910c3f2-.../power \
  -H "Authorization: Bearer $GAMEPANEL_KEY" \
  -H "Content-Type: application/json" \
  -d '{"action":"restart"}'
```

## For shops and billing systems

Creating servers, accounts, suspension, safe retries with `external_id`, and signed webhooks are described in [Creating servers](/server-creation#selling-servers-the-shop-api).

## For Pterodactyl tools

Bots and apps written for Pterodactyl's client API can work with a GamePanel key. See [Pterodactyl client API](/api/pterodactyl).

## All endpoints

The [API reference](/api/reference) lists every endpoint with its parameters, bodies and responses, and can send requests to your panel. It is generated from the same OpenAPI description, which you can [download](../openapi.json){target="_self" download}. By area:

| Area | Paths |
|---|---|
| Sign-in and account | `/auth/...` (login, sessions, two-factor, passkeys, API keys, single sign-on) |
| Servers | `/servers`, `/servers/{uuid}`, `/details`, `/power`, `/command`, `/reinstall`, `/metrics` |
| Files | `/servers/{uuid}/files...` |
| Backups | `/servers/{uuid}/backups...` |
| Schedules | `/servers/{uuid}/schedules...` |
| Startup | `/servers/{uuid}/startup` |
| Network | `/servers/{uuid}/network...` |
| Databases | `/servers/{uuid}/databases...`, `/database-hosts` (admin) |
| Mods | `/servers/{uuid}/mods...` |
| Access | `/servers/{uuid}/users...`, `/servers/{uuid}/invites...`, `/permissions` |
| Activity | `/servers/{uuid}/activity`, `/audit` (admin) |
| Admin | `/nodes...`, `/eggs...`, `/users...`, `/invites...`, `/webhooks...`, `/servers/{uuid}/limits`, `/owner`, `/suspend`, `/unsuspend`, `/external-id` |
