# Production checklist

GamePanel is **pre-alpha**. It works end to end and is well tested, but it has not run in production. If you run it for other people anyway, go through this list first. Every item links to the page that explains it.

## Before you open it to users

- [ ] **HTTPS in front of the panel.** The panel speaks plain HTTP. Put [Caddy or nginx](/panel/reverse-proxy) in front and set `GAMEPANEL_TRUSTED_PROXIES`, so session cookies get the `__Host-` prefix and `Secure`, and HSTS is sent.
- [ ] **`GAMEPANEL_PUBLIC_URL` set** to the `https://` address. Emails, passkeys and single sign-on need it.
- [ ] **Nodes connect with `wss://`.** The daemon's default is `ws://127.0.0.1:8080/daemon/connect`. A node on another machine must use `wss://panel.example.com/daemon/connect`, or its token travels in the clear. The daemon does not refuse `ws://` for you yet.
- [ ] **Admins use two factors.** Set `GAMEPANEL_REQUIRE_MFA=admins` (or `all`). It is off by default.
- [ ] **The panel listens only on loopback** (`GAMEPANEL_ADDR=127.0.0.1:8080`, the default) when a proxy is on the same machine.
- [ ] **Sign-up stays on `invite`** unless you really want anyone to make an account.
- [ ] **The firewall** on each node allows only the game port range and the SFTP port. On the panel machine, only 80 and 443.

## Data you cannot lose

- [ ] **Panel backups run** and you have restored one at least once. The panel's state is the database **and** its key file. See [Backing up the panel](/panel/backing-up).
- [ ] **Game server backups go off the node** if a lost node would hurt. See [Backups in S3](/panel/s3-backups).
- [ ] **The key file or `GAMEPANEL_SECRET_KEY` is stored somewhere safe.** Without it, nobody's authenticator codes can be checked.

## Running it

- [ ] **systemd units** (or another supervisor) restart the panel and daemons after a crash or reboot. Examples: [panel](/panel/installing#_5-run-it-as-a-service), [node](/nodes/installing#run-it-as-a-service).
- [ ] **Someone watches it.** There is no health endpoint or metrics endpoint yet. At least monitor that the panel's address answers and that the systemd units are active.
- [ ] **You know how to upgrade** and how to go back. See [Upgrading](/panel/upgrading).
- [ ] **Disk space** on the panel's database disk and the nodes' data disks is watched.

## Known gaps

These are not built yet. Plan around them:

| Gap | What to do meanwhile |
|---|---|
| No health or metrics endpoint | Check the login page over HTTPS from your monitoring |
| No built-in panel backup command | Use `sqlite3 .backup` as shown in [Backing up the panel](/panel/backing-up) |
| Activity and audit logs are never pruned | Watch the database size; it grows slowly |
| Pterodactyl application API not built | Billing modules need the [GamePanel API](/server-creation#selling-servers-the-shop-api) |
| Not verified on Podman, Windows or macOS Docker hosts | Use Linux with Docker Engine |
| Egg `config.files` rules for XML files are not applied | Set the port in that XML file yourself. See [Eggs](/admin/eggs#config-files) |
| No server transfer between nodes | Move servers by backup and re-upload. See [Data directory](/nodes/data-directory#moving-or-rebuilding-a-node) |
