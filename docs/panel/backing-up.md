# Backing up the panel

Everything the panel knows is in two files:

| File | What it holds |
|---|---|
| The database, `GAMEPANEL_DB` (e.g. `/var/lib/gamepanel/panel.db`) | Accounts, servers, nodes, eggs, schedules, backups list, audit log, everything |
| The key file, `<database>.key` (e.g. `panel.db.key`), or the value of `GAMEPANEL_SECRET_KEY` | The key that encrypts two-factor secrets |

**Back up both.** A database without its key restores fine, but nobody with authenticator two-factor can sign in until an admin runs [`panel reset-2fa`](/panel/cli#panel-reset-2fa-email) for them.

Game server files are **not** in the panel. They live on the nodes; protect them with [server backups](/servers/backups), ideally in [S3](/panel/s3-backups).

## Do not just copy the file

The database runs in SQLite's WAL mode: recent changes sit in `panel.db-wal` until they are folded into `panel.db`. Copying `panel.db` with `cp` while the panel runs can give you an old or inconsistent copy.

Use SQLite's online backup instead. It makes a consistent copy while the panel keeps running. Install the `sqlite3` command (`apt install sqlite3`, `dnf install sqlite`), then:

```sh
sudo -u gamepanel sqlite3 /var/lib/gamepanel/panel.db ".backup '/var/backups/gamepanel/panel-$(date +%F).db'"
sudo cp /var/lib/gamepanel/panel.db.key /var/backups/gamepanel/
```

## Daily backups with systemd

`/usr/local/sbin/gamepanel-backup`:

```sh
#!/bin/sh
set -eu
dest=/var/backups/gamepanel
keep=14
stamp=$(date +%F-%H%M)

install -d -m 0700 "$dest"
sqlite3 /var/lib/gamepanel/panel.db ".backup '$dest/panel-$stamp.db'"
sqlite3 "$dest/panel-$stamp.db" "PRAGMA integrity_check" | grep -qx ok
cp /var/lib/gamepanel/panel.db.key "$dest/panel.db.key"
gzip "$dest/panel-$stamp.db"

# keep the newest $keep
ls -1t "$dest"/panel-*.db.gz | tail -n +$((keep + 1)) | xargs -r rm --
```

`/etc/systemd/system/gamepanel-backup.service`:

```ini
[Unit]
Description=Back up the GamePanel database

[Service]
Type=oneshot
ExecStart=/usr/local/sbin/gamepanel-backup
```

`/etc/systemd/system/gamepanel-backup.timer`:

```ini
[Unit]
Description=Daily GamePanel database backup

[Timer]
OnCalendar=daily
RandomizedDelaySec=30m
Persistent=true

[Install]
WantedBy=timers.target
```

```sh
sudo chmod 0700 /usr/local/sbin/gamepanel-backup
sudo systemctl daemon-reload
sudo systemctl enable --now gamepanel-backup.timer
sudo systemctl start gamepanel-backup.service   # run one now
```

Then copy `/var/backups/gamepanel` **off the machine**: with restic, borg, rclone to a bucket, or your host's snapshot feature. A backup on the same disk does not survive the disk.

::: warning
The backups contain password hashes, session hashes and two-factor secrets (encrypted with the key that sits next to them). Keep them as private as the panel itself.
:::

## Restoring

1. Stop the panel: `sudo systemctl stop gamepanel`
2. Move the current files aside:
   ```sh
   cd /var/lib/gamepanel
   sudo mkdir -p old && sudo mv panel.db panel.db-wal panel.db-shm old/ 2>/dev/null || true
   ```
3. Put the backup in place, with its key:
   ```sh
   sudo sh -c 'gunzip -c /var/backups/gamepanel/panel-2026-11-19-0300.db.gz > /var/lib/gamepanel/panel.db'
   sudo cp /var/backups/gamepanel/panel.db.key /var/lib/gamepanel/panel.db.key
   sudo chown gamepanel:gamepanel /var/lib/gamepanel/panel.db /var/lib/gamepanel/panel.db.key
   sudo chmod 0600 /var/lib/gamepanel/panel.db /var/lib/gamepanel/panel.db.key
   ```
4. Start it: `sudo systemctl start gamepanel`

Nodes reconnect by themselves. What changed after the backup was taken is lost from the panel's side: a server created since then is still on its node and shows up there as **unknown**, which an admin can remove from the node's page.

**Test a restore** on a spare machine at least once. A backup you have never restored is a guess.

## Moving the panel to another machine

1. Install the panel on the new machine ([Installing the panel](/panel/installing)), but do not start it.
2. Stop the old panel, take a backup as above, and restore it on the new machine.
3. Point the domain at the new machine and start the panel.

Nodes connect to the domain, so they follow as soon as DNS does. If a node connects to an IP address, change its `GAMEPANEL_PANEL_URL`.
