---
description: How GamePanel compares with Pterodactyl, Pelican, PufferPanel, Crafty Controller and AMP, including where GamePanel is behind.
---

# GamePanel compared

How GamePanel compares with the other self-hosted game server panels, and where it is behind. If you only want to know whether to switch today, read [Where GamePanel is behind](#where-gamepanel-is-behind) first.

::: info Checked on 25 November 2026
These versions were compared: GamePanel v0.1.1, Pterodactyl Panel v1.15.2 with Wings v1.13.5, Pelican v1.0.0-rc.4, and PufferPanel v3.0.9. The other projects move fast, so if something here is out of date, [open an issue](https://github.com/ZsombiiHUN/gamepanel/issues/new/choose) or edit this page.
:::

## In one paragraph each

- **[Pterodactyl](https://pterodactyl.io)** is the standard. It has run in production for years, has the biggest library of eggs, tutorials and add-ons, and most game hosts and billing systems support it. It is a PHP (Laravel) panel with MySQL or MariaDB, Redis, a queue worker and cron, and a Go daemon called Wings on each node. Version 2 is in beta.
- **[Pelican](https://pelican.dev)** is a fork of Pterodactyl that moves faster. It adds plugins, passkeys, single sign-on, translations, and SQLite or PostgreSQL support. It is still at release candidate stage on the way to 1.0.
- **[PufferPanel](https://pufferpanel.com)** is a Go panel with its own JSON templates, not eggs. It can run game servers in Docker or directly on the host. Version 3 dropped Windows.
- **[Crafty Controller](https://craftycontrol.com)** is a Python panel for Minecraft only, Java and Bedrock. It runs on Windows, macOS and Linux.
- **[AMP](https://cubecoders.com/AMP)** from CubeCoders is commercial and closed source, with a paid licence. It supports many games on Windows and Linux.
- **GamePanel** is one Go binary for the panel, one SQLite file, and a small daemon per node that dials out to the panel. It reads Pterodactyl eggs, including the pelican-eggs library, as they are. It is **pre-alpha**.

## Side by side

| | GamePanel | Pterodactyl 1.x | Pelican | PufferPanel 3 |
|---|---|---|---|---|
| Status | Pre-alpha | Stable | 1.0 release candidate | Stable |
| Licence | AGPL-3.0 | MIT | AGPL-3.0 | Apache-2.0 |
| Panel is written in | Go, React | PHP (Laravel), React | PHP (Laravel, Filament) | Go |
| To run the panel you install | One binary (or one container) | PHP 8.2+, PHP-FPM, a web server, MySQL or MariaDB, Redis, Composer, a queue worker, cron | PHP 8.4+, PHP-FPM, a web server, a queue worker; database and Redis optional | One package (or one container) |
| Panel database | SQLite | MySQL, MariaDB | SQLite, MySQL, MariaDB, PostgreSQL | SQLite, MySQL, MariaDB, PostgreSQL, SQL Server |
| Game servers run in | Docker | Docker | Docker | Docker, or directly on the host |
| Server recipes | Pterodactyl eggs | Eggs | Eggs | PufferPanel templates |
| Inbound ports on a node | SFTP and game ports only | Wings API (8080), SFTP, game ports | Wings API, SFTP, game ports | Node web port, SFTP, game ports |
| Browser talks to | The panel only | The panel, and each node directly for console and files | The panel, and each node directly for console and files | Not documented |
| Two-factor | Authenticator app, passkeys | Authenticator app | Authenticator app, passkeys | Authenticator app |
| Single sign-on | GitHub, Discord, any OpenID Connect | Add-ons only | Yes | Not documented |
| Server transfer between nodes | No | Yes | Yes | Not documented |
| Mounts | No | Yes | Yes | Not documented |
| Plugins or extensions | No | Third-party (version 2 adds extensions) | Yes, with an in-app plugin hub | No |
| Languages | English | English | Many | Several |
| Mod installer | Modrinth, built in | Add-ons | Plugins | Not documented |
| API | OpenAPI 3.1, plus Pterodactyl's client API and the application API's billing part | Client and application API | Client and application API | REST API |
| Windows nodes | No | No | No | No (dropped in 3.0) |

"Not documented" means we could not find it in that project's docs. It does not mean the feature is missing.

## What GamePanel does differently

**It is small to run.** The panel is one static Go binary and its state is one SQLite file and one key file. There is no PHP, web server, Redis, queue worker or cron to keep alive. Upgrading means replacing a binary, and backing up means copying two files. See [Backing up the panel](/panel/backing-up).

**Nodes dial out.** Each daemon opens one websocket to the panel. A node needs no open control port and no TLS certificate of its own, so it can sit behind NAT or a home router. The browser only ever talks to the panel, so you have one address to secure. See [How it works](/getting-started/architecture).

**It is strict about security by default.** File access on nodes goes through Go's `os.Root` and is tested against symlink tricks, the class of bug behind several Wings advisories. Egg variables are passed as environment variables and never reach a shell. Containers drop all Linux capabilities. People without access to a server get a 404, not a 403, so they cannot tell it exists. Every account control is mapped to its [OWASP ASVS](/asvs) requirement and test. See [Security model](/security/model).

**It keeps your eggs.** Pterodactyl's PTDL v1 and v2 eggs, which is also the format of the [pelican-eggs](https://github.com/pelican-eggs) library, import unchanged, install scripts included. Tools built for Pterodactyl's client API work with a GamePanel API key, and after an import with the Pterodactyl key they had. See [Pterodactyl client API](/api/pterodactyl).

**Minecraft gets extra care.** The bundled [Minecraft eggs](/admin/minecraft-eggs) pick JVM flags, set the port and check the EULA on every start. The server page adds a status card with the MOTD preview and online players, a form for `server.properties`, and Modrinth search that installs mods and plugins checked against Modrinth's hashes.

PufferPanel is also one Go binary, and Pelican also runs on SQLite. Of the egg-based panels in the table, GamePanel is the only one without a PHP stack, and the only one whose nodes need no inbound control port.

## Where GamePanel is behind

Read this before you move anything that matters.

- **It has never run in production.** The test suite is large, with Go tests under the race detector, component tests and a Playwright suite in a real browser. But Pterodactyl has had years of real use and real bug reports, and GamePanel has not. See the [production checklist](/getting-started/production-checklist).
- **XML config files in eggs are not edited.** GamePanel applies an egg's `config.files` before each start, as Wings does, for properties, ini, yaml, json and plain files. The few eggs that edit an XML file need the port set there by hand. See [Config files](/admin/eggs#config-files).
- **No server transfers and no mounts.** To move a server to another node, back it up and restore it there by hand. See [Moving or rebuilding a node](/nodes/data-directory#moving-or-rebuilding-a-node).
- **The Pterodactyl import is new.** `panel import-pterodactyl --apply` and `adopt-wings` move a whole panel and its nodes, and a smoke test runs them against a real Pterodactyl and Wings, but they have not moved a large production panel yet. Keep a backup. Pelican can take over a Pterodactyl install in place.
- **Billing modules are new here.** WHMCS, Paymenter and Blesta modules for Pterodactyl work through the [part of the application API](/api/pterodactyl#application-api) they use, tested against their source but not yet in a live shop.
- **No plugins.** Pelican has a plugin hub and Pterodactyl has a large add-on market. Changing GamePanel means changing its code.
- **English only.** The interface is ready for translations, but none exist yet.
- **SQLite only.** This is fine for one panel with many servers, but you cannot point several panels at one shared database.
- **The console websocket is not Pterodactyl-compatible.** Bots that stream a console over Pterodactyl's websocket do not work. Bots that use the REST client API do.
- **Browser file transfers go through the panel.** Uploads and downloads in the browser stream through the panel's connection, while Pterodactyl sends them straight to the node. For very large worlds, use [SFTP](/nodes/sftp), which goes to the node directly.
- **No health or metrics endpoint**, and no CurseForge support.
- **Linux and Docker only.** No Windows game servers. Crafty and AMP run on Windows.
- **A small community.** If you get stuck, you get help from the issue tracker and [Discussions](https://github.com/ZsombiiHUN/gamepanel/discussions), not from a large Discord or years of forum threads.

## Which one should you use?

- **You sell servers and need a billing system that has run for years:** Pterodactyl. GamePanel works with the same WHMCS, Paymenter and Blesta modules, but has not run in a live shop yet.
- **You run Pterodactyl and want plugins, passkeys and an in-place upgrade:** Pelican.
- **You run only Minecraft, possibly on Windows:** Crafty Controller.
- **You want one commercial product with support, on Windows or Linux:** AMP.
- **You want the smallest stack, nodes behind NAT, and strict defaults, and you can live with pre-alpha:** GamePanel. Start with [Try it locally](/getting-started/quick-start), and [tell us](https://github.com/ZsombiiHUN/gamepanel/discussions) what broke.

## Other panels

- **Pterodactyl 2.0** is in beta, with a new interface and extensions. Its developers recommend keeping production panels on 1.x for now. It uses the same Wings as 1.x.
- **[Calagopus](https://github.com/calagopus/panel)** rewrites Pterodactyl in Rust. It uses Pterodactyl-style eggs and has migration paths from Pterodactyl and Pelican.
