# Upgrading

::: warning Pre-alpha
Before 1.0, a minor release (0.**x**.0) may change the API or need you to do something. Read the [changelog](https://github.com/ZsombiiHUN/gamepanel/blob/main/CHANGELOG.md) from your version to the new one first; anything you must do is under **Upgrade notes**. `panel version` and `daemon version` say what you run.
:::

## With the installer

If you installed with [the installer](/panel/installing#the-quick-way-the-installer), run it again on the panel machine and on every node:

```sh
curl -fsSL https://github.com/ZsombiiHUN/gamepanel/releases/latest/download/install.sh | sudo bash -s -- upgrade
```

Run without `upgrade` on a terminal, the installer offers **Upgrade** as its first choice. On the panel machine it stops the panel, copies the database and key file to `/var/backups/gamepanel/pre-<version>-<time>/` together with the old binary, installs the new one and starts it. On a node it replaces the daemon and restarts it; game servers keep running. Add `--version v0.2.0` to pick a release.

With Docker Compose, see [Docker Compose → Upgrading](/panel/docker#upgrading).

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
2. **Download** the new release and check it (see [Installing the panel](/panel/installing#_1-get-the-binaries)).
3. **Replace and restart:**
   ```sh
   sudo install -m 0755 panel /usr/local/bin/panel
   sudo systemctl restart gamepanel
   journalctl -u gamepanel -n 20
   ```
4. **Check**: sign in, open a server's console, and look at **Nodes**: every node should be online again within a few seconds.

## Upgrading nodes

Upgrade the daemons right after the panel, so both sides run the same version. On each node:

```sh
sudo install -m 0755 daemon /usr/local/bin/gamepanel-daemon
sudo systemctl restart gamepanel-daemon
```

Game servers keep running through the restart.

::: tip
Mixing versions is not tested. The daemon reports its version when it connects, and the panel logs a warning (`node runs a different version than the panel`) when it differs from its own.
:::

## Going back

1. Stop the panel.
2. Install the old binary.
3. Restore the database backup from before the upgrade ([Restoring](/panel/backing-up#restoring)). An older panel must not run on a database a newer one has migrated.
4. Start the panel, then put the old daemons back.

## Version notes

Every release is in the [changelog](https://github.com/ZsombiiHUN/gamepanel/blob/main/CHANGELOG.md) and on the [releases page](https://github.com/ZsombiiHUN/gamepanel/releases).
