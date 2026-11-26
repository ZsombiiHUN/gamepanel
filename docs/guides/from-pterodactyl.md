# Coming from Pterodactyl

GamePanel does the same job as Pterodactyl and Pelican and reads the same eggs, but it is built differently. This page maps what you know to how GamePanel does it.

`panel import-pterodactyl` moves a whole Pterodactyl 1.x panel over: accounts with their passwords and two-factor, locations, nodes, eggs, servers with their ports, limits and variables, subusers, schedules, database hosts and databases. On each node, the daemon takes over the server files Wings kept, so nothing is reinstalled and every server keeps its UUID, port and world. Your billing system keeps working: point its Pterodactyl module at GamePanel (see [Billing](#billing)).

## What comes over

| | |
|---|---|
| **Accounts** | Email, admin flag and password: people sign in with the password they had. With the `APP_KEY`, two-factor too (recovery codes do not carry over; people make new ones). Usernames and names are dropped: sign-in is by email |
| **Locations** | Short code and description, with their ids |
| **Nodes** | Name, location, memory and disk capacity, overallocation, SFTP host and port, with their ids. Each gets a new token |
| **Eggs** | Every egg, with inherited scripts and config resolved, including `config.files` rules, with their ids |
| **Servers** | Id, UUID, external id, name, owner, egg, image, startup, variables, limits, primary and extra ports, suspension and install state. Block IO weight is left unset, as current Wings no longer applies it |
| **Subusers** | With their permissions mapped. A GamePanel permission that covers several of Pterodactyl's is only given when all of them were (start, stop and restart for **power**, say), so nobody can do more than before |
| **Schedules** | With their tasks |
| **Database hosts and databases** | With the `APP_KEY`, which their passwords are encrypted with |

Not imported: activity logs, API keys, mounts and backup records (backup files stay where they are). Accounts get new ids (an admin may already be there); billing modules find them by external id or email. Read the dry run's report: it lists everything that is changed or left out, for your panel.

## Before you start

- **A new GamePanel panel**, installed (see [Installing the panel](/panel/installing)) and started once, with no nodes, eggs or servers yet. You do not need to make an admin account: yours comes over. If you made one, give it an email that is not in Pterodactyl.
- **The same GamePanel version** on the panel and every node.
- **Your Pterodactyl `.env`** (for the database login and `APP_KEY`), or a database user that can read Pterodactyl's database (see [Command line](/panel/cli#panel-import-pterodactyl)).
- **A Pterodactyl backup**: its database and `/var/lib/pterodactyl` on each node. The import only reads Pterodactyl's database, and on each node the server folders are moved, not changed.
- Tell your users. Their servers restart once, and anything they change in Pterodactyl after the import is not carried over.

## 1. Preview

Run a dry run on the GamePanel panel host. It writes nothing:

```sh
sudo panel import-pterodactyl --env /var/www/pterodactyl/.env
# or, with Pterodactyl's database on another host:
GAMEPANEL_PTERO_DSN='reader:secret@tcp(ptero.example.com:3306)/panel' \
GAMEPANEL_PTERO_APP_KEY='base64:...' sudo -E panel import-pterodactyl
```

The report lists what would come over, then three sections: **Must be fixed before --apply** (for example two accounts whose emails differ only in case), **Imported differently: check these**, and **Good to know**. Fix the first in Pterodactyl and run it again until it says `Result: ready`.

## 2. Import

Add `--apply`:

```sh
sudo panel import-pterodactyl --env /var/www/pterodactyl/.env --apply
```

It writes everything in one transaction, so either all of it is imported or none of it. It finds the panel's database in `/etc/gamepanel/panel.env`. At the end it prints, for each node, a **new token** and the commands to run there. The tokens are shown only this once.

## 3. Switch each node

On each node, as root:

```sh
systemctl disable --now wings
curl -fsSL https://github.com/ZsombiiHUN/gamepanel/releases/latest/download/install.sh \
  | bash -s -- node --panel panel.example.com --token <token> --adopt-wings /var/lib/pterodactyl/volumes
```

- Stopping Wings leaves its game servers running.
- `--adopt-wings` moves each server's folder into the daemon's data directory before the daemon first starts. Files keep their owner, since a server runs as whoever owns its folder, as under Wings. Nothing is reinstalled: the daemon builds each server's container and skips the egg's install script.
- When the daemon connects, it stops each server that was running under Wings the way Wings would, with the egg's stop command so the game saves its world. Then it starts the server again under GamePanel, on the same port.

If `/var/lib/pterodactyl` and the daemon's data directory (`/var/lib/gamepanel-daemon`) are on different disks, the folders are copied instead of moved. Stop the servers in Pterodactyl first, because a copy of a running server's files is not safe. Delete the copies under `/var/lib/pterodactyl/volumes` once everything works. To run the takeover by hand, use `gamepanel-daemon adopt-wings --from /var/lib/pterodactyl/volumes` with the settings from `/etc/gamepanel/daemon.env`. Run it **before** the daemon first starts: a server the panel creates before its files are there is installed from scratch.

If the node's SFTP port was not 2022, set `GAMEPANEL_SFTP_ADDR=0.0.0.0:<port>` in `/etc/gamepanel/daemon.env` and restart `gamepanel-daemon`, so SFTP clients keep their address.

## 4. Check and finish

- Sign in with your Pterodactyl email and password.
- Open a few servers: the console, files and players should be as they were.
- Point your domain or users at the new panel.
- Remove Pterodactyl and Wings when you are happy. Wings' stopped containers can go with `docker rm`.

**Things people notice**: SFTP logins change (the user name is `<email>.<server id>` and the password is an API key), and API keys must be made again.

## Names

| Pterodactyl | GamePanel |
|---|---|
| Panel (PHP, Laravel) | Panel (one Go binary) |
| Wings | Daemon |
| MySQL/MariaDB + Redis + queue worker + cron | One SQLite file |
| Location | [Location](/admin/nodes#locations) |
| Node | Node |
| Allocation | Port |
| Nest + Egg | Egg (every egg is in nest 1 in the application API) |
| Application API key | An admin account's API key |
| Client API key | An API key |
| Subuser | Subuser (under **Access**) |
| Mount | Not supported |
| Server transfer | Not supported yet |

## Architecture differences

| | Pterodactyl | GamePanel |
|---|---|---|
| Who connects to whom | The panel calls Wings over HTTPS; Wings needs an open port and a certificate | **The daemon dials the panel**. A node needs no open control port and no certificate of its own |
| Node config | `config.yml` with a token, copied from the panel | Two environment variables: the token and the panel address |
| Browser to node | The browser talks to Wings directly for the console websocket and file transfers | **The browser only talks to the panel**, which relays to the node |
| Egg variables | Substituted into the startup command as text | Passed only as environment variables; `{{VAR}}` becomes `${VAR}` |
| Install status | Reported by Wings | The same, plus webhooks |
| Billing | Through the application API | Through the same [application API](/api/pterodactyl#application-api), or [GamePanel's API](/server-creation#selling-servers-the-shop-api) with safe retries and webhooks |

## Eggs

The import brings your eggs. To add more, import them as they are, from Pterodactyl (**Nests → egg → Export**) or from [pelican-eggs](https://github.com/pelican-eggs). Their `config.files` rules are applied before each start, as Wings does, except for XML files. See [Eggs](/admin/eggs).

## Moving one server by hand

To move a single server without an import:

1. In Pterodactyl, stop the server and make a backup, or download the files over SFTP.
2. In GamePanel, create a server with the same egg, limits and variables. Let the install finish.
3. Stop it, upload the files (SFTP is best for large worlds) and extract them if they are an archive.
4. Start it and check the console.
5. Point players at the new address and port.

## Tools and bots

- **Client API**: tools that use Pterodactyl's client API for power, commands, files, resources and backups work with a GamePanel key. See [Pterodactyl client API](/api/pterodactyl).
- **Console websocket**: not compatible.
- **Billing modules** (WHMCS, Paymenter, Blesta): work with an admin's API key. See [Billing](#billing).

## Billing

GamePanel answers the part of Pterodactyl's [application API](/api/pterodactyl#application-api) that WHMCS, Paymenter and Blesta modules use. After an import:

1. Make an API key on your (admin) account: **Account → API keys**.
2. In the billing system, change the Pterodactyl server's address to GamePanel's and its application API key to that key. Leave the products as they are.

Products keep working because the import keeps the ids they point at: locations, nodes, eggs and servers have the same ids as in Pterodactyl, and servers keep their external ids (the billing system's service ids). Nest ids in products are accepted as they are; GamePanel has no nests.

What differs: a memory limit of 0 (unlimited) is refused, a weak password from the billing system is replaced by a link to choose one, and block IO weight is ignored. The [mapping table](/api/pterodactyl#how-it-maps) has the details.

## Things that work differently

- **SFTP password**: an API key, not your account password. See [SFTP](/nodes/sftp).
- **Running state**: with an egg's "done" text, a server shows **starting** until the line appears, as in Pterodactyl.
- **Strangers get 404**: someone without access to a server cannot tell it exists.
- **Crash restarts**: a server that crashes is restarted once; if it crashes again within a minute it stays stopped.
- **Schedules**: run with the permissions of whoever last saved them, in UTC.
