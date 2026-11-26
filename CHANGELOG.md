# Changelog

All notable changes to GamePanel are listed here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow
[Semantic Versioning](https://semver.org/). Before 1.0, a minor version
(0.**x**.0) may change the API or need you to do something on upgrade; such
changes are listed under **Upgrade notes**.

Upgrade the panel and every node to the same version together. See
[Upgrading](docs/panel/upgrading.md).

## [Unreleased]

### Security

- Game servers are isolated from each other and from the node. Every server
  and install container joins a `gamepanel0` network with inter-container
  traffic off, and the daemon keeps iptables chains so servers cannot reach
  the node's own services (SSH, the panel, SFTP, databases) or cloud metadata
  (`169.254.0.0/16`). Servers still reach the internet, and other servers'
  published ports at the node's address (proxies). New settings:
  `GAMEPANEL_FIREWALL`, `GAMEPANEL_HOST_PORTS`, `GAMEPANEL_BLOCK_PRIVATE`,
  `GAMEPANEL_NETWORK`. See [Server isolation](docs/nodes/isolation.md).
- Game servers run as a system user of their own (`GAMEPANEL_CONTAINER_USER`;
  the installer creates `gamepanel-game`), not uid 1000, which on most
  machines is a person's login account. Existing servers' files move to it at
  their next start.
- Container logs rotate (local driver, 3 × 10 MB). Docker's default log grew
  without limit, outside the server's disk limit, so a server printing a lot
  could fill the node's disk.

### Upgrade notes

- The daemon needs `iptables` and refuses to start when it cannot set up the
  firewall. Where it cannot see Docker's network (Docker Desktop, rootless
  Docker), set `GAMEPANEL_FIREWALL=off`.
- Servers can no longer reach services on the node. A database on the node
  that servers use needs `GAMEPANEL_HOST_PORTS=3306/tcp` (its port).
- Running servers keep their old container until they next start; each is
  then rebuilt on the new network. Restart servers to isolate them sooner.
- `install.sh upgrade` creates the `gamepanel-game` user and sets
  `GAMEPANEL_CONTAINER_USER`. Manual and Docker Compose installs should set
  it themselves (docs/nodes/configuration.md).

## [0.2.1] - 2026-11-26

### Added

- `curl -fsSL .../install.sh | sudo bash` with no command is a guided
  install: it asks whether to install the panel, a node or both, checks the
  machine (memory, disk, ports, Docker) and whether the domain points at it,
  asks only what it needs, shows the terms (AGPL-3.0, and the Let's Encrypt
  Subscriber Agreement when Caddy gets the certificate) and the plan, and
  waits for a yes. Steps show a spinner and their result; their output goes
  to `/var/log/gamepanel-install.log` (the admin's password never does), and
  a failed step shows its last log lines. With a command, it asks only for
  options left out. A node install now waits until the panel accepts the
  token, and says so when it does not.
- Run again where GamePanel is installed, the installer shows what runs there
  and whether a newer release is out, and offers Upgrade, Repair (reinstall
  the running version; data and settings stay), Change settings (the panel's
  domain, HTTPS with Caddy, a node's token or panel), Add a node here, and
  Uninstall. The same work without questions: `install.sh repair`,
  `install.sh configure --domain|--https|--token|--panel`, and
  `install.sh uninstall [--purge] --yes`. Uninstall keeps the database, server
  files and settings unless asked (`--purge` copies the database to
  `/var/backups/gamepanel` first) and never removes Docker or Caddy. See
  [Repair, settings and uninstalling](docs/panel/managing.md).

### Changed

- The installer needs the terms accepted: on a terminal it asks; without one,
  add `--accept-terms` or `--yes`. `install.sh terms` prints them, and a dated
  copy of what was accepted is kept in `/etc/gamepanel/terms-accepted.txt`.
  Unattended installs (cloud-init, scripts) that did not pass `--yes` must add
  it.
- A node install starts Docker when it is installed but not running.

### Fixed

- Installing a node over a kept `daemon.env` ignored a new `--token` or
  `--panel`; they now replace the old values. Installing the panel over an
  existing one backs up its database first, as `upgrade` does.

## [0.2.0] - 2026-11-26

### Added

- Docs: [GamePanel compared](docs/guides/compare.md) sets GamePanel beside
  Pterodactyl, Pelican, PufferPanel, Crafty Controller and AMP, including
  where GamePanel is behind. The README opens with a 30-second demo.
- `panel import-pterodactyl` moves a Pterodactyl 1.x panel to GamePanel (#95).
  It reads Pterodactyl's database read-only (the login and `APP_KEY` can come
  from Pterodactyl's `.env`) and reports which users, nodes, eggs, servers,
  ports, subusers, schedules and databases come over, what changes and what
  is left out. With `--apply` it writes them into a new panel in one
  transaction, keeping server UUIDs and ports, passwords and (with the
  `APP_KEY`) two-factor, and prints a token for each node.
- `gamepanel-daemon adopt-wings` (or `install.sh node --adopt-wings DIR`)
  takes over the server folders Wings kept on a node: files keep their owner
  and nothing is reinstalled. Servers still running under Wings are stopped
  with their egg's stop command, so they save their world, and started again
  under GamePanel. `scripts/ptero-import-smoke.sh` runs the whole move against
  a real Pterodactyl and Wings.
- Eggs' `config.files` are applied before each start, as Pterodactyl's Wings
  does: Minecraft's `server.properties` gets the server's port, BungeeCord's
  `config.yml` its bind address, and so on, for the `properties`, `ini`,
  `yaml`, `json` and `file` parsers (#86). A missing file is created, and
  comments and key order are kept. Values come only from the server's own
  settings: unlike Wings, the node never fills a placeholder from its own
  configuration.
- Startup commands can use `{{server.build.default.port}}`,
  `{{server.build.memory}}`, `{{server.build.default.ip}}` and
  `{{server.uuid}}`; each becomes the environment variable that holds it
  (#84). Servers also get Wings' `SERVER_IP` and `P_SERVER_UUID` variables.
- Pterodactyl's application API (`/api/application`), the part WHMCS,
  Paymenter and Blesta modules use (#88): accounts, servers (create, details,
  build, startup, suspend, unsuspend, reinstall, delete), nodes and
  allocations, locations, nests and eggs, in Pterodactyl's shapes and error
  format. It takes an administrator's API key, never a browser session, and
  runs through the same code as GamePanel's own API, so the activity log and
  webhooks see it. Each server gets a numeric id that is never reused. See
  [Pterodactyl API](docs/api/pterodactyl.md).
- The Pterodactyl import keeps API keys and backups. With the `APP_KEY`,
  Pterodactyl's account and application keys (`ptlc_`, `ptla_`) keep working
  as they are, so bots and billing modules need no new key; keys limited to
  IP addresses or to part of the application API are reported and left out.
  Finished backups come over: `adopt-wings` takes the archives Wings kept on
  each node (`--backups`, default `/var/lib/pterodactyl/backups`), and
  backups in S3 are found where Pterodactyl put them once GamePanel uses the
  same bucket.
- Locations group nodes by data centre or region: `location_ids` on
  `POST /api/servers`, `/api/locations`, and a picker in each node's settings.
- The Pterodactyl import keeps locations, and the ids of locations, nodes,
  eggs and servers, so a billing system's products keep pointing at them.
- `/server/<identifier>`, Pterodactyl's address for a server, opens it in
  GamePanel, so links billing modules give customers work.
- Accounts can carry a bcrypt password hash from Pterodactyl (`$2y$`), so
  people moved over by the coming Pterodactyl import keep their password. The
  first sign-in replaces it with an Argon2id hash; bcrypt hashes are never
  written, and ones with a cost above 14 are refused.

### Fixed

- Suspending a running server now stops it even when the suspension comes
  with other changes, such as a new image. Before, such a server kept running
  until its next restart.
- Errors from the Pterodactyl client API now carry the status as a number
  (`"status": "404"`), as Pterodactyl's do, instead of its text.
- After a restart, a server could be left without a console, and a stop sent
  right after the restart could be lost and wait out the full 30-second
  graceful stop. A run's console now belongs to that run: it is let go as soon
  as the run ends, and an old run's stream can no longer take the new run's.

### Security

- Downloading a file from a URL no longer connects to 6to4 (`2002::/16`)
  addresses that wrap a private IPv4 address, or to IPv4-compatible (`::/96`)
  addresses. Only nodes with such a tunnel set up by hand could have reached
  their own network that way.
- `install.sh node` runs Docker's install script only from a pinned
  docker/docker-install commit whose SHA-256 matches, instead of piping
  get.docker.com straight into `sh`.

## [0.1.1] - 2026-11-24

### Security

- Releases sign `checksums.txt` with Sigstore (`checksums.txt.sigstore.json`) and
  publish GitHub build provenance for every archive. See
  [Verifying a release](docs/panel/installing.md#verifying-a-release).
- The panel and daemon images are built from base images pinned by digest.

## [0.1.0] - 2026-11-24

The first release. GamePanel is pre-alpha: it works end to end and is well
tested, but it has not run in production yet.

### Added

- **Panel and daemon**: one Go binary each; SQLite database; nodes dial out
  to the panel over a websocket.
- **Servers**: power controls, live console, CPU, memory and disk charts with
  30 days of history, per-server limits including network bandwidth, crash
  detection and restart, suspension.
- **Eggs**: Pterodactyl PTDL v1 and v2 eggs import unchanged, install
  scripts included; GamePanel's own Minecraft eggs and Java images.
- **Files and SFTP**: browse, edit, search, upload folders, compress and
  extract, pull from a URL; all access goes through `os.Root`.
- **Mods**: Modrinth search and one-click install, verified by SHA-512.
- **Startup, network, databases, backups (local or S3), schedules.**
- **Access**: subusers, invitations, activity log, audit log.
- **Accounts**: passkeys, TOTP with recovery codes, single sign-on (GitHub,
  Discord, OpenID Connect), password reset, API keys, session limits; mapped
  to OWASP ASVS 5.0.0 in `docs/asvs.md`.
- **APIs**: OpenAPI 3.1 for everything the UI does; a Pterodactyl-compatible
  client API; a shop API for billing integrations.
- **Releases**: `panel version` / `daemon version`, static linux amd64 and
  arm64 archives with checksums, `ghcr.io/zsombiihun/gamepanel` and
  `gamepanel-daemon` images, `compose.yml`, systemd units and an installer
  (`scripts/install.sh`).
- **Translations**: every UI string is in one English file, ready for Crowdin
  (`crowdin.yml`). The UI follows the browser's language once others exist.

### Changed

- **Daemon**: `GAMEPANEL_PANEL_URL` accepts the panel's plain address
  (`https://panel.example.com`); `http(s)` becomes `ws(s)` and
  `/daemon/connect` is added. A bad scheme stops the daemon at start-up
  instead of failing every reconnect.
- **New server**: the node and game are picked for you when there is only one.

### Fixed

- A deleted or inaccessible server shows a "Server not found" page with a way
  back, instead of a bare error and a "… / Console" breadcrumb.
- Files toolbar fits on one line on phones.
- The "New backup" form closes once the backup is made; before, it stayed open
  over the list and hid the Restore buttons. It and the server's ⋯ menu now
  also close on Escape or a click outside, and the menu closes after a choice.
- A subuser following a link to a tab they may not open (Files, Backups, …)
  lands on one they may, instead of a permission error.
- A new server whose game has no install script now says
  `[gamepanel] install complete` on its console, like one that has.
- "Backups kept" counted "of 20" in English in every language.

[Unreleased]: https://github.com/ZsombiiHUN/gamepanel/compare/v0.2.1...HEAD
[0.2.1]: https://github.com/ZsombiiHUN/gamepanel/compare/v0.2.0...v0.2.1
[0.2.0]: https://github.com/ZsombiiHUN/gamepanel/compare/v0.1.1...v0.2.0
[0.1.1]: https://github.com/ZsombiiHUN/gamepanel/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/ZsombiiHUN/gamepanel/releases/tag/v0.1.0
