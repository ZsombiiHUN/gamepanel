# SFTP

Each daemon runs its own SFTP server, so people can work on their server's files with FileZilla, WinSCP, Cyberduck, `sftp` or an editor plugin. It is often the better choice for large uploads, which do not pass through the panel or a CDN.

## Connecting

The server's **Settings** tab and the SFTP button in **Files** show the exact details, and can make an API key for you (the API has them at `GET /api/servers/{uuid}/sftp`). They are:

| | |
|---|---|
| Host | The node's SFTP address (see below) |
| Port | `2022` unless changed |
| Username | `<your email>.<first 8 characters of the server's ID>`, e.g. `alex@example.com.b910c3f2` |
| Password | An **API key** from **Account → API keys**, not your account password |

Using an API key means:

- it works for accounts with two-factor or passkeys, which a password-only protocol could not ask for;
- your account password is never stored in an SFTP client, where it would often sit in plain text;
- you can revoke the key at any time, and give it an expiry.

What you may do follows your permissions on that server: `files.read` to browse and download, `files.write` to change anything. The owner and admins can do everything.

## While connected

- Every file operation goes through the same checks as the file manager: it stays inside the server's folder, symlinks cannot escape it, and the disk limit applies.
- The daemon re-checks open sessions with the panel regularly. A revoked key, removed permission or a suspension ends the session within seconds.
- Logins are recorded in the server's activity as `sftp.login`, failures as `sftp.fail`.

## Limits

| Limit | Value |
|---|---|
| Connections from one address at once | 20 |
| Failed logins from one address | 10 per 5 minutes, then refused until the window passes |
| Failed logins per account (panel side) | 20 per 15 minutes |
| Tries per connection | 3 |
| Open files per session | 256 |
| Channels per connection | 8 |

## The node's SFTP address

By default the panel tells people to connect to **the panel's host name** on the port the daemon reports. For a node on another machine that is wrong, so set the node's SFTP host (and port, if it is behind NAT or a port forward) on the node's admin page, or with `PUT /api/nodes/{id}/sftp`.

Give each node a DNS name such as `node1.example.com`. If the panel's domain goes through Cloudflare's proxy, SFTP cannot go through it: use a separate name that points straight at the node (grey cloud).

## The host key

On first start, the daemon creates an Ed25519 host key at `<data dir>/sftp_host_ed25519`. Clients remember it and warn if it changes. Keep it when you move or rebuild a node, or every client will warn once.

## Turning SFTP off

```ini
GAMEPANEL_SFTP_ADDR=off
```

Only password (API key) logins are accepted; public-key logins are not supported yet.
