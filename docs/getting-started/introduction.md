# Introduction

GamePanel runs game servers in Docker and gives you a web interface to manage them: start and stop them, watch the console, edit files, take backups and share access with other people.

<Shot name="servers" alt="The server list: every server with its state, live usage and power buttons" />

It has two parts:

- **The panel** is one Go program. It serves the web interface and the API and keeps everything in one SQLite file. You run one panel.
- **The daemon** runs on every machine that hosts game servers (a *node*). It drives Docker and connects out to the panel. You run one daemon per node. The panel's machine can be a node too.

It is a simpler, stricter alternative to [Pterodactyl](https://pterodactyl.io) and [Pelican](https://pelican.dev). It reads their *egg* files (the recipes that say how to install and start a game) as they are, so the games you already run keep working. [GamePanel compared](/guides/compare) sets it beside Pterodactyl, Pelican, PufferPanel, Crafty and AMP, including where it is still behind.

## Who it is for

- **People running servers for themselves and friends**: one machine, one panel, a few servers.
- **Communities and clans** that share servers between several admins, each with their own permissions.
- **Small hosting businesses** that sell servers: the [shop API](/server-creation#selling-servers-the-shop-api) creates, suspends and removes servers when a billing system tells it to, and [webhooks](/server-creation#webhooks) tell the billing system when an install finishes.

## What it does

| Area | What you get |
|---|---|
| Servers | Power controls, live console, CPU, memory and disk charts with 30 days of history, per-server limits |
| Files | A file manager with an editor, search, drag-and-drop upload, archives, pull from URL, and SFTP |
| Mods | Search and install from Modrinth, checked against Modrinth's hashes |
| Startup | Egg variables (checked against the egg's rules), Docker image choice, custom startup for admins |
| Network | Several ports per server, with notes, and a bandwidth limit |
| Databases | MySQL/MariaDB databases per server, each with its own user |
| Backups | On the node or in any S3-compatible store; lock, restore, download |
| Schedules | Cron-style tasks: commands, power actions, backups |
| Access | Subusers with per-server permissions, invitations by email |
| Accounts | Passkeys, two-factor, single sign-on, API keys, session limits |
| Admin | Nodes with capacity tracking, eggs, database hosts, audit log |
| API | Everything the interface does, documented with OpenAPI 3.1, plus a Pterodactyl-compatible client API |

## How these docs are organised

- **[Getting started](/getting-started/architecture)** explains how the parts fit and what you need.
- **[Panel](/panel/installing)** and **[Nodes](/nodes/installing)** are for whoever installs and runs it.
- **[Administration](/admin/nodes)** is for panel admins: nodes, eggs, accounts, servers.
- **[Using servers](/servers/console-and-power)** is for everyone who has a server, owner or subuser.
- **[API and integrations](/api/overview)** is for scripts, bots and billing systems.
- **[Reference](/troubleshooting)** covers troubleshooting, the security model and every limit.

## Status

GamePanel is **pre-alpha**. It works end to end and has an extensive test suite (Go tests with the race detector, component tests, and a Playwright suite in a real browser), and the Docker runtime is checked against a real engine. It has not run in production. See the [production checklist](/getting-started/production-checklist) for what that means in practice.

**Not built yet:** Pterodactyl's application (admin) API and websocket, PostgreSQL, Windows game servers, billing, CurseForge.

**Not verified:** Windows and macOS Docker hosts, Podman, and unusual bind mounts.
