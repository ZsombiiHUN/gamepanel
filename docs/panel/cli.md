# Command line

`panel` with no arguments runs the panel. It also has a few commands for the panel host, for jobs you cannot do from the browser.

Every command opens the same database as the running panel, so it needs the same settings: at least `GAMEPANEL_DB`, and `GAMEPANEL_SECRET_KEY` if you use it. Run commands as the panel's user. With the setup from [Installing the panel](/panel/installing):

```sh
sudo -u gamepanel sh -c 'set -a; . /etc/gamepanel/panel.env; exec /usr/local/bin/panel <command> <args>'
```

The commands are safe to run while the panel is running.

::: warning
Without `GAMEPANEL_DB`, a command opens (and creates) `./panel.db` in the current folder, and a `panel.db.key` next to it. `version`, `help`, `healthcheck` and `import-pterodactyl` are the exceptions: they open nothing (`import-pterodactyl --apply` opens the existing database and never creates one).
:::

## `panel version`

Prints the version, commit and build date, e.g. `panel v0.1.0 (commit 1a2b3c4d5e6f, built 2026-11-22, go1.27.4 linux/amd64)`. `daemon version` does the same for the daemon. Include both in bug reports.

## `panel help`

Lists the commands.

## `panel create-admin <email>`

Makes an administrator account with a random password and prints the password **once**, to your terminal only. It is never logged or stored in the clear.

```txt
Admin created.
  email:    you@example.com
  password: 3vQx...
Change it after signing in.
```

Use it for the first account, or when every admin is locked out.

## `panel reset-link <email>`

Prints a one-time password reset link for an account, valid for 1 hour. This is how to reset a password on a panel without [email](/panel/email). The link starts with `GAMEPANEL_PUBLIC_URL`; without it, it starts with `<panel address>` for you to fill in.

```txt
Password reset link for you@example.com (valid for 1h0m0s, works once):
https://panel.example.com/reset#token=...
```

Using the link signs the account out everywhere.

## `panel reset-2fa <email>`

Turns off authenticator two-factor for an account and deletes its recovery codes. This is the last way back for someone who lost their authenticator **and** their recovery codes. It is recorded in the audit log as `totp.reset`, by `cli`.

Check who is asking before you run it: it removes a protection. Passkeys are not touched.

## `panel import-pterodactyl`

Reads a Pterodactyl 1.x panel's database and prints what importing it would do: what comes over, what changes, and what is left out. Without `--apply` it is a **dry run** and writes nothing. With `--apply` it writes the import into this panel's database in one transaction: all of it or none. Pterodactyl's database is only ever read. See [Coming from Pterodactyl](/guides/from-pterodactyl).

```sh
# On the Pterodactyl host: read the database login and APP_KEY from its .env
panel import-pterodactyl --env /var/www/pterodactyl/.env

# Or name the database yourself
GAMEPANEL_PTERO_DSN='reader:secret@tcp(127.0.0.1:3306)/panel' \
GAMEPANEL_PTERO_APP_KEY='base64:...' panel import-pterodactyl
```

| Flag | |
|---|---|
| `--env FILE` | Pterodactyl's `.env`; `DB_*` and `APP_KEY` are read from it |
| `--dsn DSN` | `user:password@tcp(host:3306)/panel`, or `GAMEPANEL_PTERO_DSN` |
| `--app-key KEY` | Pterodactyl's `APP_KEY`, or `GAMEPANEL_PTERO_APP_KEY`. Without it, two-factor is reported as turned off, and database hosts and API keys as skipped, because Pterodactyl encrypts them with this key |
| `--unlimited-memory MB` | Memory for servers that had no limit (default 4096); GamePanel always sets one |
| `--apply` | Import into the panel database in `GAMEPANEL_DB`. The panel must have no nodes, servers, eggs or database hosts yet; accounts already there stay, unless one has the same email as an imported one. Prints each node's new token, **once** |

`--apply` finds the panel's database through `GAMEPANEL_DB`, or else in `/etc/gamepanel/panel.env` as `install.sh` writes it, so on a panel installed that way `sudo panel import-pterodactyl --env /var/www/pterodactyl/.env --apply` is enough. The panel must have started once (it makes its key file then), and it may keep running.

The database is read in one read-only transaction, so it is safe while Pterodactyl runs. Prefer the environment variables or `--env` to `--dsn` and `--app-key`, which other users on the host can see in the process list. For a database user that can only read:

```sql
CREATE USER 'gp_import'@'127.0.0.1' IDENTIFIED BY '...';
GRANT SELECT ON panel.* TO 'gp_import'@'127.0.0.1';
```

## `panel healthcheck`

Asks the panel listening on `GAMEPANEL_ADDR` (default `127.0.0.1:8080`; a wildcard address is asked on loopback) for `/readyz`, and exits `0` when it is ready. The Docker image uses it as its `HEALTHCHECK`. See [Monitoring](/panel/monitoring).

## `panel --addr <address>`

Runs the panel on another address than `GAMEPANEL_ADDR`.
