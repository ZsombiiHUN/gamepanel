# Data directory

Everything a daemon keeps is under `GAMEPANEL_DATA_DIR` (e.g. `/var/lib/gamepanel-daemon`):

```txt
<data dir>/
├── servers/
│   └── <server uuid>/          the server's files, mounted into its container
├── state/
│   ├── <server uuid>.json      the server's settings as the panel last sent them
│   ├── <server uuid>.installed the install script finished
│   ├── <server uuid>.stale     the container must be (re)built at next start
│   └── install-*/              temporary, while an install script runs
├── backups/
│   └── <server uuid>/
│       └── <backup uuid>.tar.gz local backups
└── sftp_host_ed25519           the SFTP host key
```

## Server files

`servers/<uuid>` is bind-mounted into the server's container (as `/home/container`, the path Pterodactyl's images expect) and into the install container as `/mnt/server`. It is handed to **uid 1000**, the user most game images run as.

Do not change files here while the server runs, and keep ownership at uid 1000 when you do, or the game may not be able to write them. The file manager and SFTP take care of both.

## Settings and markers

The `state/` files let a daemon restart without forgetting anything:

- `<uuid>.json` is the full description of the server: image, startup command, environment, limits, ports, install script.
- `<uuid>.installed` exists once the install finished. A server with an install script but no marker counts as **install failed** (for example, the daemon was killed mid-install) and must be reinstalled.
- `<uuid>.stale` means the container does not match the settings yet (it was never built, or a change waits for the next start). The next start rebuilds it.

Do not edit these by hand. The panel sends a fresh description whenever something changes.

## Local backups

Backups made while S3 is not configured are kept in `backups/<server uuid>/`. They count against the node's disk, not the server's disk limit. With [S3 backups](/panel/s3-backups), new backups go to the bucket instead.

## Disk usage

A bind mount has no quota of its own, so the daemon enforces each server's disk limit itself. It measures the server's folder (and caches the result), and checks:

- before any write through the file manager, SFTP or a backup restore,
- before a server starts,
- regularly while it runs, **stopping** a server that has grown past its limit.

Between checks a game can go a little over; the regular check bounds how far.

## Moving or rebuilding a node

Moving a node's servers to another machine is **not supported yet**. The daemon expects each server's container to exist (or to be marked stale), and a fresh machine has none, so copying the data directory alone is not enough and has not been tested.

Until there is a transfer feature, move a server by downloading a [backup](/servers/backups), creating a new server with the same egg on the target node, then uploading the archive there and extracting it in the file manager.

Keep `sftp_host_ed25519` when you rebuild a node, so SFTP clients do not warn about a changed host key.

## Removing a node

Delete its servers in the panel first; a node with servers cannot be deleted. Then delete the node, stop the daemon, and remove the data directory.
