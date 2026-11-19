# Databases

The **Databases** tab gives the server MySQL/MariaDB databases, if an admin has set up a [database host](/admin/database-hosts) and given the server a database limit.

| Action | Needs |
|---|---|
| See the databases (not their passwords) | `databases.read` |
| Create and delete, show, copy and rotate passwords | `databases.manage` |

## Creating one

Give it a name (letters, digits and `_`). The panel adds the server's prefix, so the real name looks like `s1a2b3c4_luckperms`. Choose where connections may come from: `%` for anywhere (the default), or an address.

The tab then shows the host, port, database, user and password to put in your plugin's config. The user can do anything inside that database and nothing outside it.

## Passwords

**Show** reveals the password, **Copy** copies it, **Rotate** sets a new one. After a rotation, update the password wherever the database is used and restart the server.

## Deleting

Deleting a database deletes its user and all its data. There is no undo, and server backups do not include databases: export what you need first.
