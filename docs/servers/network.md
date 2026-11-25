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
- Eggs that write the port into a config file, such as Minecraft's `server.properties`, write the new one before each start. See [Eggs → Config files](/admin/eggs#config-files).

When you switch the primary port, restart the server so it picks up the new one. A game whose egg does neither needs the port set in its config by hand.

## When changes apply

Port changes to a running server apply at its next start.
