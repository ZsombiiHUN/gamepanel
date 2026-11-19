# Requirements

## The panel

| | |
|---|---|
| Operating system | Linux (amd64 or arm64). The panel is a pure Go binary with no C dependencies, so other systems may work, but Linux is what is tested |
| Memory | Small. The panel is one process with an SQLite database |
| Disk | The database grows with usage history (30 days per server), activity, audit entries and webhook logs. Plan for a few hundred MB on a busy panel, plus room for its backups |
| Network | HTTPS reachable by your users and by every node |
| Domain name | Needed for passkeys and single sign-on (an IP address will not do), and strongly recommended for HTTPS |
| Reverse proxy | Caddy, nginx or another proxy that handles HTTPS and websockets. See [Reverse proxy and HTTPS](/panel/reverse-proxy) |

Optional:

- An SMTP server or relay, for password resets and invitations by email.
- An S3-compatible bucket (AWS S3, Cloudflare R2, Backblaze B2, Wasabi, MinIO...), to keep backups off the nodes.
- A MySQL or MariaDB server, to give game servers their own databases.

## A node

| | |
|---|---|
| Operating system | Linux with a recent kernel. Windows and macOS Docker hosts are **not verified** |
| Docker | Docker Engine, reachable through the usual socket (`DOCKER_HOST` is honoured). Podman is **not verified** |
| Privileges | Run the daemon as **root**. It hands server folders to uid 1000 (the user most game images run as) and enters container network namespaces for bandwidth limits |
| Virtualisation | A full VM (KVM, VMware, Hyper-V) or bare metal. Containers-in-containers (LXC, OpenVZ) need Docker nesting to work and are not tested |
| Network limits | `tc`, `ip` and `nsenter` on the host (packages `iproute2` and `util-linux`), and the `ifb` kernel module for limits on incoming traffic. Without them servers run unlimited and say so in their console. See [Network limits](/nodes/network-limits) |
| Ports | The game ports you give the node, and the SFTP port (2022 by default). **No inbound port for the panel** |
| Disk | Game files, Docker images and local backups. Put the data directory on a disk with room to spare |
| Memory and CPU | Whatever your game servers need, plus a little for the daemon and Docker |

## Building from source

Until there are release downloads, you build the binaries yourself:

| Tool | Version |
|---|---|
| Go | 1.27.4 or newer (with `GOTOOLCHAIN=auto`, an older Go fetches it) |
| Node.js | 22 |
| pnpm | 10 |
| make | any |

You can build on any machine and copy the two binaries to your servers. Built with `CGO_ENABLED=0`, they have no runtime dependencies. See [Installing the panel](/panel/installing#_1-get-the-binaries).
