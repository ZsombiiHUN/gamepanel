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

## Translations
The web UI's text lives in `web/src/i18n/locales/en/translation.json`, grouped by
the file that uses it. English is the source and the fallback.

- **In code**: `const { t } = useTranslation();` then `t("serversPage.title")`.
  Use `{{name}}` placeholders, `count` for plurals (`key_one` / `key_other`), and
  `<Trans>` when a sentence contains a link or `<code>`. Outside React (helpers
  in `lib/`), import `i18n` from `src/i18n` and call `i18n.t`.
- **Keys are checked**: the type check rejects a key that is not in the English
  file, and `src/i18n/i18n.test.ts` fails on text written straight into JSX and
  on English keys nothing uses.
- **Other languages** are translated on Crowdin (`crowdin.yml`), not by editing
  the JSON by hand. Each arrives as `locales/<lang>/translation.json` and is
  loaded automatically. The browser's language is used; `localStorage.lang`
  overrides it. Messages that come from the API are still English.

## Pull requests
- One concern per PR, with tests. Security-sensitive code (file access, auth,
  container settings) needs a test that proves the protection.
- Run `make check` before pushing.
- API changes: regenerate `web/openapi.json` and the web types.
- By contributing you agree your work is licensed under AGPL-3.0.

## Releasing (maintainers)
1. Move the `[Unreleased]` entries in `CHANGELOG.md` under a new
   `## [x.y.z] - YYYY-MM-DD` heading, with **Upgrade notes** for anything an
   admin must do, and update the compare links at the bottom.
2. Merge that to `main`, then tag it:
   `git tag -a vX.Y.Z -m vX.Y.Z && git push origin vX.Y.Z`.
3. `.github/workflows/release.yml` builds the archives, `checksums.txt` and
   both images. The release fails if `CHANGELOG.md` has no section for the tag.
   Try the build first with
   `goreleaser release --snapshot --clean` (GoReleaser v2).

## Reporting security issues
See [SECURITY.md](SECURITY.md).
