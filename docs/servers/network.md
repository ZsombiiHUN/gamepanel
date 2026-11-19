# Ports

The **Network** tab lists the server's ports. Every server has one **primary** port, which the startup command gets as `SERVER_PORT`. Each port is open for both TCP and UDP.

| Action | Needs |
|---|---|
| See the ports | `network.read` |
| Add a port, remove one, write a note, make one primary | `network.manage` |

An owner can add ports up to the server's **port limit** (extra ports beyond the primary one), which an admin sets. New ports come from the node's port range.

Notes help you remember what each port is for ("query", "RCON", "voice").

## The port inside the container

A port is published on the same number inside the container as outside. The game must listen on that port:

- Eggs that pass `{{SERVER_PORT}}` on the command line do so automatically.
- Games that read the port from a config file need it set there. For **Minecraft**, set `server-port=` in `server.properties` to the primary port. See [Eggs → Ports and config files](/admin/eggs#ports-and-config-files).

When you switch the primary port, update the game's config the same way.

## When changes apply

Port changes to a running server apply at its next start.
