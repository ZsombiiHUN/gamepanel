# Sharing access

A server's owner can let other people help run it, each with only the permissions they need. Open the server's **Access** tab (owners and admins only).

## Adding someone

Enter their email and tick their permissions.

- If they **have an account**, they get access at once.
- If they **do not**, they get an **invitation**: emailed when the panel can send email, and shown as a link you can pass on. When they make their account through it, the access comes with it. Invitations last 7 days and can be withdrawn.

You can change someone's permissions or remove them at any time. Removal takes effect at once: open consoles and SFTP sessions close within seconds.

## Permissions

| Permission | Allows |
|---|---|
| `console.read` | See the console output and server status |
| `console.send` | Send console commands |
| `power` | Start, stop, restart and kill the server |
| `files.read` | Browse and read files (also SFTP read) |
| `files.write` | Upload, create, edit, move, compress and delete files (also SFTP write), and install mods |
| `backups.read` | See backups |
| `backups.create` | Create backups |
| `backups.restore` | Restore backups |
| `backups.delete` | Delete and lock backups |
| `backups.download` | Download backups |
| `schedules.manage` | Create, edit, run and delete schedules |
| `settings.reinstall` | Reinstall the server |
| `settings.rename` | Rename the server and change its description |
| `activity.read` | See the server's activity log |
| `startup.read` | See the startup command and the variables the egg shows |
| `startup.update` | Change startup variables the egg lets users edit |
| `startup.docker-image` | Pick another of the egg's Docker images |
| `network.read` | See the server's ports |
| `network.manage` | Add, label and remove ports, and choose the primary one |
| `databases.read` | See the server's databases (not their passwords) |
| `databases.manage` | Create and delete databases, and see and change their passwords |

Two things are never given to subusers: **managing access** and **deleting the server**. Only the owner and admins can.

## What subusers see

A subuser sees the server in their list and only the tabs their permissions allow. Someone with no access at all gets "not found" for the server, exactly as if it did not exist.

## Handing a server over

An admin can make someone else the owner (`PUT /api/servers/{uuid}/owner`).
