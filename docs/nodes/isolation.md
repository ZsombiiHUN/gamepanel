# Server isolation

A node runs servers of different owners side by side, and treats every one of them as hostile (see the [security model](/security/model)). Besides dropping all capabilities and running as an unprivileged user, the daemon keeps each server away from the others and from the node itself.

## What a server can reach

| From a game server to | |
|---|---|
| The internet | Yes |
| Its own ports | Yes |
| Another server's **published** port, at the node's address | Yes: a proxy (Velocity, BungeeCord) reaches its backends this way |
| Another server's container, any port | **No** |
| The node's services: SSH, the panel, SFTP, anything listening on the node | **No**, unless listed in `GAMEPANEL_HOST_PORTS` |
| Ports other Docker containers publish on the node (a database, Portainer, the panel run by Compose) | **No**, unless listed in `GAMEPANEL_HOST_PORTS` |
| Cloud metadata (`169.254.169.254` and the rest of `169.254.0.0/16`) | **No** |
| Private networks (10/8, 172.16/12, 192.168/16, 100.64/10) | Yes, unless `GAMEPANEL_BLOCK_PRIVATE=1` |

Players reach servers exactly as before.

## How

- Every server and install container joins one Docker network, `gamepanel0` (host interface `gp0`), created by the daemon with inter-container traffic turned off (`com.docker.network.bridge.enable_icc=false`).
- The daemon keeps two iptables chains for traffic from `gp0`:
  - `GAMEPANEL-INPUT`, jumped to from `INPUT`: lets servers open the ports servers publish and those in `GAMEPANEL_HOST_PORTS`, and drops everything else addressed to the node.
  - `GAMEPANEL-FORWARD`, jumped to from Docker's `DOCKER-USER`: drops link-local addresses and, with `GAMEPANEL_BLOCK_PRIVATE=1`, private networks. Replies to connections that came from those networks still go out. It also drops connections to ports other containers publish on the node: Docker redirects those to the container before `INPUT`, so `GAMEPANEL-INPUT` never sees them. Other servers' published ports and `GAMEPANEL_HOST_PORTS` stay reachable.
- The chains are rebuilt whenever a server's container is created or deleted, and checked every minute, so a firewall reload (`ufw reload`, `firewall-cmd --reload`) that removes the jumps is repaired.

The daemon uses whichever iptables (`iptables`, `iptables-legacy` or `iptables-nft`) has Docker's `DOCKER-USER` chain.

Check it on a node:

```sh
sudo iptables -S GAMEPANEL-INPUT
sudo iptables -S GAMEPANEL-FORWARD
docker network inspect gamepanel0 -f '{{index .Options "com.docker.network.bridge.enable_icc"}}'   # false
```

## A database on the node

A MySQL or MariaDB on the node, installed there or run in a Docker container that publishes its port, is a node service, so servers cannot reach it until you allow its port:

```ini
GAMEPANEL_HOST_PORTS=3306/tcp
```

Point servers at the node's address or at the network's gateway (`{{config.docker.interface}}` in eggs). A database on another machine needs nothing.

## Upgrading from 0.2

Servers built by an earlier daemon keep running in their old container on Docker's default network. Each is rebuilt on `gamepanel0` the next time it starts (or restarts), with nothing for you to do. Until then they are not isolated, so restart them when convenient.

## When the firewall cannot run

The daemon refuses to start when it cannot set up the firewall: `iptables` is missing, Docker does not manage iptables (`"iptables": false` in `daemon.json`), or the `gp0` interface is not on the daemon's machine (Docker Desktop, rootless Docker). With `GAMEPANEL_FIREWALL=off` it runs anyway, with only inter-container traffic turned off; use that for development, or when your own firewall does the same job.

Check what your setup does with `scripts/isolation-smoke.sh` from the source tree: it runs two servers in a throwaway Docker host and tries every row of the table above, then again with the firewall off to prove the checks can fail.
