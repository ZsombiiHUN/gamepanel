# Troubleshooting

Start with the logs:

```sh
journalctl -u gamepanel -n 100          # the panel
journalctl -u gamepanel-daemon -n 100   # a node
```

Both log in `key=value` form; errors say what failed and usually why.

## The panel will not start

| Message | Fix |
|---|---|
| `invalid GAMEPANEL_TRUSTED_PROXIES` | Use IPs or CIDRs, comma-separated: `127.0.0.1,::1` |
| `GAMEPANEL_SIGNUP must be invite or open` | Fix the value |
| `GAMEPANEL_REQUIRE_MFA must be off, admins or all` | Fix the value |
| `invalid GAMEPANEL_SESSION_MAX` / `..._IDLE` | Use a Go duration of at least `1m`: `30m`, `12h` |
| `GAMEPANEL_SECRET_KEY must be 64 hex characters` | Generate one with `openssl rand -hex 32` |
| `... is not a valid key file` | The key file next to the database is damaged. Restore it from your backup |
| `invalid S3 backup settings` / `invalid SMTP settings` | The message names the setting |
| `serve: listen tcp ...: address already in use` | Something else uses the port; change `GAMEPANEL_ADDR` or stop the other program |
| Permission denied on the database | The database folder must be writable by the panel's user (`/var/lib/gamepanel`) |

## I cannot sign in

| Symptom | Cause and fix |
|---|---|
| "too many sign-in attempts, try again in a minute" | 10 tries per minute per address. Wait a minute. If everyone hits this, the panel sees all users as the proxy's address: set `GAMEPANEL_TRUSTED_PROXIES` |
| Signed in, then sent straight back to the sign-in page | The browser drops the session cookie. Over HTTPS through a proxy, check `GAMEPANEL_TRUSTED_PROXIES` and that the proxy sends `X-Forwarded-Proto: https`. Over plain HTTP on another machine, the `Secure` cookie cannot be used; use HTTPS |
| "This request came from another site and was blocked" | The `Origin` of the request does not match the `Host` the panel sees. The proxy must keep the original `Host` header |
| Lost password, lost authenticator, all admins locked out | See [Accounts → Locked out?](/admin/accounts#locked-out) |
| Passkey button missing or failing | Passkeys need `GAMEPANEL_PUBLIC_URL` with a domain name (not an IP) over HTTPS, and the browser must be on that same address |

## A node stays offline

Check the daemon's log on the node.

| Log | Cause and fix |
|---|---|
| `GAMEPANEL_NODE_TOKEN is required` | Set the token in `/etc/gamepanel/daemon.env` |
| `cannot reach the Docker daemon` | Start Docker: `systemctl start docker`. Check `docker info` works as root |
| `panel connection lost ... 401` or `403` | Wrong token, or the node was deleted. Create the node again and use the new token |
| `panel connection lost ... connection refused` / `no such host` | `GAMEPANEL_PANEL_URL` is wrong, or the panel is down. Use the panel's address, such as `https://panel.example.com` (the daemon adds `/daemon/connect`) |
| `panel connection lost ... status code 101 but got 200` | `GAMEPANEL_PANEL_URL` has a path that is not `/daemon/connect`. Use only the panel's address |
| `panel connection lost ... certificate` | The panel's HTTPS certificate is not trusted by the node. Use a real certificate (Caddy and Let's Encrypt do this) |
| Connects, then drops every minute | The reverse proxy closes idle websockets. Raise its timeout (`proxy_read_timeout 1h` in nginx) |

The daemon retries by itself with a growing wait (up to 30 seconds), so once the cause is fixed it reconnects without a restart.

## A server will not install

The install script's output is in the server's console.

| Symptom | Cause and fix |
|---|---|
| Image pull fails | The image name is wrong, the registry is down, or the node has no internet or no disk. The console says which. The next start tries again |
| Install failed, script error in the console | Usually a variable value (a version that does not exist, say). Fix it under **Startup**, then **Reinstall** under **Settings** |
| Install stops after 30 minutes | The install limit. Large downloads on a slow line may not fit; check the node's connection |
| Install failed after a node restart | An install interrupted by a restart counts as failed. **Reinstall** |

## A server will not start or players cannot connect

| Symptom | Cause and fix |
|---|---|
| "the install failed; reinstall the server before starting it" | **Reinstall** under **Settings** |
| "this server is suspended" | An admin suspended it. See [Suspension](/admin/suspension) |
| Stops by itself, console says disk limit | The server is over its disk limit. Delete files or ask for more |
| Crashed, console says out of memory | Raise the memory limit, or lower what the game uses (for Java, `-Xmx` below the limit) |
| Crashes again within a minute and stays stopped | The crash loop guard. Read the console for the real error |
| Running, but nobody can connect | The game is not listening on the server's port. Restart it so its egg writes the port into its config; an egg that does not needs `server-port` (or its game's equivalent) set by hand. See [Ports](/servers/network). Then check the node's firewall allows the port, TCP and UDP |
| Shows **Unknown** | Its node is not connected. See [A node stays offline](#a-node-stays-offline) |
| Offline after the node rebooted | Servers are not started automatically at boot yet. Start them |

## Console, uploads and downloads

| Symptom | Cause and fix |
|---|---|
| Console connects, then drops after a minute | The proxy closes idle websockets: raise its read timeout |
| Console does not connect at all | The proxy does not pass websockets: it needs the `Upgrade` and `Connection` headers |
| Uploads over 100 MB fail | A proxy or CDN caps request size. In nginx: `client_max_body_size 0`. Behind Cloudflare, use SFTP |
| "some output is skipped" in the console | The server prints over 2,000 lines a second. The server is fine; only the display skips |
| File will not open in the editor | Files over 10 MiB are not opened in the editor. Download it, or use SFTP |
| "Pull from URL" refused | Private and local addresses are blocked unless the node sets `GAMEPANEL_PULL_ALLOW_PRIVATE=1` |

## SFTP

| Symptom | Cause and fix |
|---|---|
| Login refused | The username is `<email>.<first 8 characters of the server id>` and the password an **API key**, not your account password |
| Login refused after many tries | 10 failures per 5 minutes per address; wait |
| Cannot connect at all | The node's SFTP port (2022) is closed in its firewall, or the address points at Cloudflare's proxy. Set the node's SFTP host to a name that points straight at the node |
| Can read but not write | Your access has `files.read` but not `files.write` |
| Session closed | The key was revoked, your access changed, or the server was suspended |

## Email does not arrive

- `GAMEPANEL_PUBLIC_URL` must be set, or no email is sent (the panel warns at start).
- Look for SMTP errors in the panel log.
- Check the sender domain's SPF and DKIM; many providers silently drop mail without them.

## Getting help

Search the [issues on GitHub](https://github.com/ZsombiiHUN/gamepanel/issues), or open one with the panel and daemon logs (remove tokens, keys and addresses first). Report security problems privately, as [SECURITY.md](https://github.com/ZsombiiHUN/gamepanel/blob/main/SECURITY.md) describes.
