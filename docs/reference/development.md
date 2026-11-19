# Development

How to build, run and test GamePanel from source. The code standards CI enforces are in [CONTRIBUTING.md](https://github.com/ZsombiiHUN/gamepanel/blob/main/CONTRIBUTING.md).

## Tools

- Go 1.27.4+ (with `GOTOOLCHAIN=auto`, an older Go fetches it)
- Node 22 and pnpm 10
- Docker, for real servers and the smoke tests
- make

## Layout

| Path | What |
|---|---|
| `cmd/panel`, `cmd/daemon` | The two programs |
| `internal/panel/api` | HTTP API, auth, websockets, webhooks, the Pterodactyl client API |
| `internal/panel/store` | SQLite access and migrations |
| `internal/panel/hub` | Connected nodes and request routing |
| `internal/panel/{s3,mail,dbhost,modrinth}` | Outside services |
| `internal/daemon` | The daemon: servers, console, files, backups, SFTP, disk limits |
| `internal/daemon/runtime` | Docker (and a fake runtime for tests), network limits |
| `internal/daemon/fsx` | Safe file access through `os.Root` |
| `internal/proto` | Messages between panel and daemon |
| `internal/e2e` | Tests that run a real panel and daemon together |
| `web` | The React interface, built into `internal/web/dist` and embedded |
| `docs` | This site |

## Build and run

```sh
make web build      # interface, then bin/panel and bin/daemon

export GAMEPANEL_DB=./.dev/panel.db
bin/panel create-admin you@example.com
bin/panel

# another terminal, with a token from Nodes → Create node
GAMEPANEL_NODE_TOKEN=... GAMEPANEL_RUNTIME=fake bin/daemon

# another terminal: the interface with hot reload on :5173, proxying /api to :8080
cd web && pnpm dev
```

Go changes need `make build` and a restart; the interface reloads by itself.

## Tests and checks

| Command | Runs |
|---|---|
| `make test` | Every Go test with the race detector |
| `make check` | golangci-lint, govulncheck, Go tests with a coverage floor, and the web checks (lint, Prettier, types, build) |
| `make e2e` | Playwright against a real panel with a fake daemon, with accessibility scans at three screen sizes. First time: `cd web && pnpm e2e:install` |
| `bash scripts/docker-smoke.sh` | The Docker runtime against a real engine |
| `bash scripts/bandwidth-smoke.sh` | Network limits with iperf3, in Docker-in-Docker |
| `make hooks` | A pre-commit hook that formats and lints staged files |

Some tests reach real services when told where they are:

| Variable | Test |
|---|---|
| `GAMEPANEL_TEST_MYSQL` (+ `_USER`, `_PASSWORD`) | `TestServerDatabases` against MariaDB/MySQL |
| `GAMEPANEL_TEST_S3_ENDPOINT` (+ `_BUCKET`, `_ACCESS_KEY`, `_SECRET_KEY`) | `TestS3Backups` against an S3-compatible store |

## The API description

`web/openapi.json` is generated from the Go code (`make openapi`), and the interface's TypeScript types are generated from it. Change the Go handler, then regenerate.

## These docs

```sh
cd docs
pnpm install
pnpm dev       # http://localhost:5173 with hot reload
pnpm build     # static site in docs/.vitepress/dist; fails on dead links
```

Pages are Markdown. The sidebar is in `docs/.vitepress/config.mts`. The look is in `docs/.vitepress/theme/`: `custom.css` uses the panel's own colours and fonts (keep them in step with `web/src/index.css`), `Home.vue` is the front page, and `<SystemMap />` is the parts figure.

The [API reference](/api/reference) is drawn by Scalar from `web/openapi.json`, so `make openapi` updates it. Its sections come from the path, in `docs/.vitepress/openapi.ts`; a new area of the API shows up under "Other" until it gets a line there. The build also writes `/llms.txt`, `/llms-full.txt` and a `.md` copy of every page for AI assistants; give a page a `description` in its frontmatter and that line appears in `llms.txt` too. Keep the style of the rest: short sentences, plain words, tables for anything with more than two facts, and check every number against the code.
