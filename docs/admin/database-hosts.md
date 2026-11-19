# Database hosts

Many games and plugins want a MySQL or MariaDB database (LuckPerms, CoreProtect, Discord bots, websites). With a database host, server owners can make their own databases from the server's **Databases** tab, each with its own user that can only reach that database.

## Preparing the database server

Use MySQL 8 or MariaDB 10.6+ on a machine your nodes can reach. Make a user the panel will use to create databases and users. It needs rights to create databases and users and to grant privileges on them:

```sql
CREATE USER 'gamepanel'@'<panel address>' IDENTIFIED BY 'a-long-random-password';
GRANT ALL PRIVILEGES ON *.* TO 'gamepanel'@'<panel address>' WITH GRANT OPTION;
FLUSH PRIVILEGES;
```

`<panel address>` is where the **panel** connects from (`127.0.0.1` if it is the same machine). The panel connects to the database host directly; nodes do not.

Game servers connect to it from their **node**, so the database server must listen on an address nodes can reach (`bind-address` in its config), and its firewall must allow port 3306 from your nodes. Do not open it to the whole internet unless you have to.

## Adding the host

Open **Database hosts** (admin) and add:

| Field | Meaning |
|---|---|
| Name | A label |
| Host, Port | Where both the panel and game servers reach it. Use an address that works from the nodes, not `127.0.0.1`, unless everything is on one machine |
| Username, Password | The account made above |
| Max databases | `0` = no limit |
| Node | Prefer this host for servers on that node; empty = any |

The panel **tests the connection before saving**. A host can be removed only when it has no databases left.

## Letting servers use it

Each server has a **database limit**, `0` by default. Set it when creating the server (`database_limit`) or later in its limits. The owner (or a subuser with `databases.manage`) can then create up to that many.

When a database is made:

- its name is `s<first 8 characters of the server id>_<the name they chose>`, and its user a similar random name;
- the panel picks the host preferred for the server's node, or any host with room;
- the user gets every privilege on that one database (select, insert, update, delete, create, drop, alter, index, lock tables, temporary tables, execute, views, routines, events, triggers, references) and nothing on any other;
- the owner chooses where connections may come from: `%` (anywhere, the default) or an address.

Passwords can be shown, copied and rotated. Subusers with only `databases.read` see the databases but not their passwords. Deleting a database also deletes its user.

## Tested with

`TestServerDatabases` runs against a real MariaDB when `GAMEPANEL_TEST_MYSQL` is set, including that a database's user cannot reach anything else.
