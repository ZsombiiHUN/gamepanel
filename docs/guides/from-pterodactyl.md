# Coming from Pterodactyl

GamePanel does the same job as Pterodactyl and Pelican and reads the same eggs, but it is built differently. This page maps what you know to how GamePanel does it.

::: warning
There is no automatic migration of a Pterodactyl database (users, servers, schedules). Move servers one at a time: create them in GamePanel with the same egg, then copy the files over.
:::

## Names

| Pterodactyl | GamePanel |
|---|---|
| Panel (PHP, Laravel) | Panel (one Go binary) |
| Wings | Daemon |
| MySQL/MariaDB + Redis + queue worker + cron | One SQLite file |
| Location | No equivalent; use `node_ids` to pick among nodes |
| Node | Node |
| Allocation | Port |
| Nest + Egg | Egg |
| Application API key | An admin account's API key |
| Client API key | An API key |
| Subuser | Subuser (under **Access**) |
| Mount | Not supported |
| Server transfer | Not supported yet |

## Architecture differences

| | Pterodactyl | GamePanel |
|---|---|---|
| Who connects to whom | The panel calls Wings over HTTPS; Wings needs an open port and a certificate | **The daemon dials the panel**. A node needs no open control port and no certificate of its own |
| Node config | `config.yml` with a token, copied from the panel | Two environment variables: the token and the panel address |
| Browser to node | The browser talks to Wings directly for the console websocket and file transfers | **The browser only talks to the panel**, which relays to the node |
| Egg variables | Substituted into the startup command as text | Passed only as environment variables; `{{VAR}}` becomes `${VAR}` |
| Install status | Reported by Wings | The same, plus webhooks |
| Billing | Through the application API | Through [GamePanel's API](/server-creation#selling-servers-the-shop-api) with safe retries and webhooks |

## Eggs

Import your eggs as they are, from your Pterodactyl panel (**Nests → egg → Export**) or from [pelican-eggs](https://github.com/pelican-eggs). Read [Eggs → What is not imported](/admin/eggs#what-is-not-imported-yet) first. The important difference:

**Config file rules (`config.files`) are not applied.** Pterodactyl rewrites files like Minecraft's `server.properties` with the server's port before each start. GamePanel does not, so set the port in those files yourself. Eggs that pass the port on the command line are not affected.

## Moving a server

1. In Pterodactyl, stop the server and make a backup, or download the files over SFTP.
2. In GamePanel, create a server with the same egg, limits and variables. Let the install finish.
3. Stop it, upload the files (SFTP is best for large worlds) and extract them if they are an archive.
4. Set the new port in the game's config files if the egg relies on `config.files`.
5. Start it and check the console.
6. Point players at the new address and port.

## Tools and bots

- **Client API**: tools that use Pterodactyl's client API for power, commands, files, resources and backups work with a GamePanel key. See [Pterodactyl client API](/api/pterodactyl).
- **Console websocket**: not compatible.
- **Billing modules** (WHMCS, Paymenter, Blesta): not compatible yet, because the application API is not built. They need a module for GamePanel's API.

## Things that work differently

- **SFTP password**: an API key, not your account password. See [SFTP](/nodes/sftp).
- **Running state**: a server counts as running as soon as its container runs, not when a "done" line appears.
- **Strangers get 404**: someone without access to a server cannot tell it exists.
- **Crash restarts**: a server that crashes is restarted once; if it crashes again within a minute it stays stopped.
- **Schedules**: run with the permissions of whoever last saved them, in UTC.
