# Changelog

All notable changes to GamePanel are listed here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow
[Semantic Versioning](https://semver.org/). Before 1.0, a minor version
(0.**x**.0) may change the API or need you to do something on upgrade; such
changes are listed under **Upgrade notes**.

Upgrade the panel and every node to the same version together. See
[Upgrading](docs/panel/upgrading.md).

## [Unreleased]

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

[Unreleased]: https://github.com/ZsombiiHUN/gamepanel/compare/v0.1.1...HEAD
[0.1.1]: https://github.com/ZsombiiHUN/gamepanel/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/ZsombiiHUN/gamepanel/releases/tag/v0.1.0
