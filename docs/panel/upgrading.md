# Upgrading

::: warning Pre-alpha
There are no releases or version numbers yet, and the API may change between commits. Read the commit log between the commit you run and the one you upgrade to before you upgrade.
:::

## What happens on upgrade

- **The database migrates itself.** At start, the panel applies any new schema migrations, then serves. There is no separate migrate step.
- **Migrations only go forward.** The panel never undoes one. To go back to an older build, restore the backup you took before upgrading.
- **Nodes reconnect by themselves** when the panel restarts. Game servers keep running while the panel is down; consoles and the interface come back when it does.
- **Restarting a daemon does not stop game servers.** Their containers keep running, and the daemon picks them up again when it starts.

## Upgrading the panel

1. **Back up** the database and key file ([Backing up the panel](/panel/backing-up)):
   ```sh
   sudo systemctl start gamepanel-backup.service
   ```
2. **Build** the new binaries (see [Installing the panel](/panel/installing#_1-get-the-binaries)):
   ```sh
   git fetch && git checkout <new commit>
   make web
   CGO_ENABLED=0 go build -o bin/panel ./cmd/panel
   CGO_ENABLED=0 go build -o bin/daemon ./cmd/daemon
   ```
3. **Replace and restart:**
   ```sh
   sudo install -m 0755 bin/panel /usr/local/bin/panel
   sudo systemctl restart gamepanel
   journalctl -u gamepanel -n 20
   ```
4. **Check**: sign in, open a server's console, and look at **Nodes**: every node should be online again within a few seconds.

## Upgrading nodes

Upgrade the daemons right after the panel, so both sides run the same commit. On each node:

```sh
sudo install -m 0755 daemon /usr/local/bin/gamepanel-daemon
sudo systemctl restart gamepanel-daemon
```

Game servers keep running through the restart.

::: tip
Mixing builds is not tested. The daemon reports its version to the panel when it connects, but the panel does not check it yet, so keep panel and daemons on the same commit.
:::

## Going back

1. Stop the panel.
2. Install the old binary.
3. Restore the database backup from before the upgrade ([Restoring](/panel/backing-up#restoring)). An older panel must not run on a database a newer one has migrated.
4. Start the panel, then put the old daemons back.

## Version notes

There are no release notes yet. Changes that need you to do something are called out in the commit messages and pull requests on GitHub.
