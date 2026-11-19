# Contributing

Thanks for helping. Keep changes small and focused.

## Setup
Needs Go (see `go.mod`), Node 22 + pnpm 10, and Docker for the runtime tests.

```sh
make hooks       # once: format and lint staged files on every commit
make web build   # build UI and binaries
make check       # everything CI checks except the browser and Docker tests
make e2e         # browser tests (first: cd web && pnpm e2e:install)
bash scripts/docker-smoke.sh   # real Docker lifecycle test
```

## Code standards
CI fails on any of these, so run `make check` before pushing.

- **Go**: `make lint` runs golangci-lint with the rules in `.golangci.yml`
  (errors are never dropped, rows and bodies are closed, contexts are passed
  on, gosec, staticcheck, complexity at most 30). `make fmt` applies gofumpt
  and goimports. `make vuln` runs govulncheck.
- **Coverage**: `make cover` fails below `COVER_MIN` in the Makefile. Raise it
  when coverage goes up; do not lower it to get a change through.
- **Web**: `pnpm check` in `web/` runs ESLint (typescript-eslint strict and
  type-checked, React hooks, jsx-a11y), the Prettier check, the type check and
  the unit tests. `pnpm format` applies Prettier. Files stay under 500 lines.
- **Suppressions**: a `//nolint` or `eslint-disable` names the rule and says
  why on the same line. Unused ones fail the lint.
- Formatting-only commits go in `.git-blame-ignore-revs`.

## Pull requests
- One concern per PR, with tests. Security-sensitive code (file access, auth,
  container settings) needs a test that proves the protection.
- Run `make check` before pushing.
- API changes: regenerate `web/openapi.json` and the web types.
- By contributing you agree your work is licensed under AGPL-3.0.

## Reporting security issues
See [SECURITY.md](SECURITY.md).
