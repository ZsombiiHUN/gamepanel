# Contributing

Thanks for helping. Keep changes small and focused.

## Setup
Needs Go (see `go.mod`), Node 22 + pnpm 10, and Docker for the runtime tests.

```sh
make web build   # build UI and binaries
make test        # go test -race ./...
make e2e         # browser tests (first: cd web && pnpm e2e:install)
bash scripts/docker-smoke.sh   # real Docker lifecycle test
```

## Pull requests
- One concern per PR, with tests. Security-sensitive code (file access, auth,
  container settings) needs a test that proves the protection.
- Run `go vet ./...` and the tests before pushing.
- API changes: regenerate `web/openapi.json` and the web types.
- By contributing you agree your work is licensed under AGPL-3.0.

## Reporting security issues
See [SECURITY.md](SECURITY.md).
