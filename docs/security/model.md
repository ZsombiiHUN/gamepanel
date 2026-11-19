# Security model

A game panel runs code that strangers upload, for people who did not write it, on machines you care about. This page says what GamePanel protects, how, and where the limits are.

For account security mapped to OWASP ASVS 5.0.0, with tests and known gaps, see [Account security and ASVS](/asvs).

## Who is trusted

| Party | Trusted with |
|---|---|
| Panel admins | Everything. An admin can run any image with any startup command on any node |
| Server owners | Their servers, within the limits an admin set |
| Subusers | Only the permissions they were given, on that server |
| Game server code | **Nothing.** Treated as hostile |
| Nodes | Their own servers. A node cannot act on another node's servers |
| The network | Nothing; use HTTPS and `wss://` |

## Game servers are untrusted

Each game server runs in its own Docker container with:

- **every Linux capability dropped**, and `no-new-privileges`;
- the user that owns its folder (uid 1000), not root;
- an init process, a process limit (1024 by default) and a memory limit, with no swap unless an admin allows it;
- only its own folder mounted;
- its own ports published, nothing else.

Install scripts run in a separate container with the same restrictions, a 30-minute limit and at least 1 GB of memory.

Containers share the host kernel. A kernel bug can still let a container escape; keep nodes patched, and do not run game servers on a machine that holds anything more valuable than other game servers.

## Files

All file access on a node, from the panel, SFTP, backups, restores and archive extraction, goes through Go's `os.Root`, which refuses paths that leave the server's folder, including through symlinks the server itself created. This is the class of bug behind several Pterodactyl Wings vulnerabilities, and it is tested against hostile symlinks. Backups skip symlinks and special files instead of following them. Restores are capped at 200 GiB to stop decompression bombs.

## Egg variables never reach a shell as text

Pterodactyl pastes variable values into the startup command, which has led to command injection and token leaks. GamePanel turns `{{VAR}}` into `${VAR}` and passes values only as environment variables, so a value is never parsed as shell syntax. No panel or node secret is ever substituted into a command or install script. Only variables the egg declares can be set, so nobody can inject `LD_PRELOAD` or override `SERVER_PORT`.

## Nodes

- A node's token is stored hashed in the panel and shown once.
- The node dials out; it has no open control port to attack.
- The panel checks every order against its own records, and a node may only ask about its own servers (for example, an SFTP login check for a server on another node is refused).
- Nodes never get S3 credentials, only presigned URLs for single objects.
- "Pull from URL" cannot reach private or local addresses unless the node operator allows it.

## The web interface and API

- Sessions are opaque random tokens; only their hash is stored. Cookies are `HttpOnly`, `SameSite=Lax`, and over HTTPS `__Host-` prefixed and `Secure`.
- Changes must come from the panel's own origin (checked against `Origin` and `Sec-Fetch-Site`), on top of `SameSite`.
- Websockets check their origin, so another site cannot open a console in your name.
- API answers are never cached. Responses carry `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY` and HSTS over HTTPS.
- Strangers get `404` for servers they cannot see, so IDs cannot be probed.
- Rate limits on sign-in, password resets, two-factor codes and SFTP logins.

## Webhooks and database hosts

Admins choose webhook addresses and database hosts, and the panel will connect to any address it can reach, including internal ones. That is by design (they are admin settings), but it means an admin API key is as powerful as an admin: give it only to systems you trust that much.

## What is not protected yet

- The daemon accepts a plain `ws://` panel address for a remote node. Always use `wss://`.
- API keys have no scopes: a key can do everything its account can.
- Sign-up as `open` lets anyone make an account; the default is invitation only.
- Two factors are not required unless `GAMEPANEL_REQUIRE_MFA` is set.

## Reporting a vulnerability

Report it privately, as described in [SECURITY.md](https://github.com/ZsombiiHUN/gamepanel/blob/main/SECURITY.md). Please do not open a public issue.
