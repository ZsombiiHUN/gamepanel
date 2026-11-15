# GamePanel (working name)

[![CI](https://github.com/ZsombiiHUN/gamepanel/actions/workflows/ci.yml/badge.svg)](https://github.com/ZsombiiHUN/gamepanel/actions/workflows/ci.yml)
[![License: AGPL v3](https://img.shields.io/badge/license-AGPL--3.0-blue.svg)](LICENSE)

A game server management panel: a **panel** (API + web UI, one Go binary) and a
**daemon** (runs on each node, drives Docker). It aims to be a more secure,
simpler alternative to Pterodactyl / Pelican, and already imports Pterodactyl
eggs, including their install scripts (for example Paper Minecraft installs and
boots). **Pre-alpha:** not ready for production. Backups, schedules, SFTP,
subusers, passkeys/SSO and a Pterodactyl API compatibility layer are not built yet.

## Quick start (development, no Docker needed)

```sh
make web build                       # builds the UI, then bin/panel and bin/daemon
export GAMEPANEL_DB=./panel.db
bin/panel create-admin you@example.com   # prints a random password once
bin/panel reset-2fa you@example.com      # offline recovery if a user lost their authenticator and recovery codes
bin/panel                            # http://127.0.0.1:8080
```

Sign in, open **Nodes -> create node**, copy the token, then:

```sh
GAMEPANEL_NODE_TOKEN=<token> GAMEPANEL_RUNTIME=fake bin/daemon   # fake = no containers
# real servers (needs a running Docker daemon):
GAMEPANEL_NODE_TOKEN=<token> bin/daemon
```

Import a Pterodactyl egg under **Eggs**, then **Create server**.

| Variable | Default | |
|---|---|---|
| `GAMEPANEL_ADDR` | `127.0.0.1:8080` | panel listen address |
| `GAMEPANEL_DB` | `./panel.db` | SQLite path |
| `GAMEPANEL_COOKIE_SECURE` | unset | set `1` behind HTTPS |
| `GAMEPANEL_TRUSTED_PROXIES` | unset | comma-separated IPs/CIDRs of your reverse proxy. Only then is `X-Forwarded-For` believed (needed for correct per-IP rate limits and audit IPs). The proxy must also preserve the `Host` header |
| `GAMEPANEL_SECRET_KEY` | key file `<db>.key` | 64 hex chars; encrypts two-factor secrets at rest. If unset, a key file is created next to the database (mode 0600). **Back it up with the database**, or enrolled authenticators cannot be read |
| `GAMEPANEL_PANEL_URL` | `ws://127.0.0.1:8080/daemon/connect` | daemon -> panel |
| `GAMEPANEL_NODE_TOKEN` | required | from node creation |
| `GAMEPANEL_DATA_DIR` | `./daemon-data` | server files |
| `GAMEPANEL_RUNTIME` | `docker` | `fake` for development |

Run behind a TLS reverse proxy in production and use `wss://` for the daemon.

## Design

- **Daemon dials out.** One websocket per node; no inbound control port.
- **Stack:** Go `net/http` + huma (OpenAPI 3.1), SQLite (modernc) + goose,
  coder/websocket, Docker via `moby/moby/client`; React 19 + Vite + TanStack +
  Tailwind 4 + xterm.js + CodeMirror, embedded with `go:embed`.
- **Pterodactyl eggs import unchanged** (PTDL v1/v2). `{{VAR}}` becomes
  `${VAR}` and values reach the process only as environment variables, so a
  variable can never inject shell syntax and no secret is ever substituted.
- **File access** goes only through `os.Root` (`internal/daemon/fsx`), tested
  against hostile symlinks (the Wings CVE class).
- Sessions are opaque cookies (hash stored), passwords argon2id, node tokens
  stored hashed, login rate-limited, websocket origin-checked, containers run
  with all capabilities dropped, `no-new-privileges`, a PID limit and no swap.

## Status: MVP

Verified by tests (`make test`, race detector on) and a live run of both binaries:
auth, access control (non-owners get 404), node enrollment, egg import, server
create/delete, power, console streaming, command input, file read/write, symlink
escape rejection, cross-origin websocket rejection.

**Docker runtime** is verified against a real engine by `scripts/docker-smoke.sh`
(image pull, env, memory/CPU/PID limits, dropped capabilities, port publishing,
data-dir write access, console input, stats, graceful stop, restart, kill,
delete with cleanup).

**Not verified yet:** the **web UI** type-checks and builds but has not been
opened in a browser. Windows/macOS Docker hosts, Podman and bind-mount edge cases
are untested.

**Not built yet:** subusers/permissions, 2FA/passkeys, backups, scheduled tasks,
SFTP, disk quotas, mod manager, billing modules, Pterodactyl API compatibility
layer, Postgres, multi-port allocations, Windows servers, metrics.

## Contributing and security

See [CONTRIBUTING.md](CONTRIBUTING.md) and [SECURITY.md](SECURITY.md).
Licensed under [AGPL-3.0](LICENSE).
