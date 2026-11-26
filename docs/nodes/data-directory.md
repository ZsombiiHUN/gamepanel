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
│   ├── <server uuid>/
│   │   └── <backup uuid>.tar.gz local backups
│   └── wings/                  backups taken over from Wings, until their server claims them
└── sftp_host_ed25519           the SFTP host key
```

## Server files

`servers/<uuid>` is bind-mounted into the server's container (as `/home/container`, the path Pterodactyl's images expect) and into the install container as `/mnt/server`. It is handed to the user in `GAMEPANEL_CONTAINER_USER` (the installer makes `gamepanel-game` for it; uid 1000 when it is not set), and the game runs as that user.

Do not change files here while the server runs, and keep their owner when you do, or the game may not be able to write them. The file manager and SFTP take care of both.

When `GAMEPANEL_CONTAINER_USER` changes from uid 1000 to another user, each server's files that uid 1000 owns move to the new user the next time the server starts.

## Settings and markers

The `state/` files let a daemon restart without forgetting anything:

- `<uuid>.json` is the full description of the server: image, startup command, environment, limits, ports, install script.
- `<uuid>.installed` exists once the install finished. A server with an install script but no marker counts as **install failed** (for example, the daemon was killed mid-install) and must be reinstalled.
- `<uuid>.stale` means the container does not match the settings yet (it was never built, or a change waits for the next start). The next start rebuilds it.

Do not edit these by hand. The panel sends a fresh description whenever something changes.

## Local backups

Backups made while S3 is not configured are kept in `backups/<server uuid>/`. They count against the node's disk, not the server's disk limit. With [S3 backups](/panel/s3-backups), new backups go to the bucket instead.

`adopt-wings` puts the archives of Wings' local backups in `backups/wings/`, since Wings names them by the backup alone. When the panel creates an imported server on the node, the server's backups move to its own folder. Archives still in `backups/wings/` once every server is up belong to no imported server (Pterodactyl had deleted it, or the backup had failed) and can be deleted.

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
