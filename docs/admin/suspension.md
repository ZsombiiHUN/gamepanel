# Suspension

Suspending a server freezes it without deleting anything: for an unpaid invoice, a rules breach, or while you look into something.

Suspend from the server's **Settings** (**Suspend server**), or with `POST /api/servers/{uuid}/suspend`. Lift it with **Unsuspend** or `POST /api/servers/{uuid}/unsuspend`. Both are admin only.

## While a server is suspended

- It is **stopped** (gracefully, then forcibly after 30 seconds) and **cannot be started**: not by its owner, not by an admin, not by a schedule, not by crash restart, not at the end of an install, not after the node restarts.
- Its owner and subusers still see it, marked **Suspended**, with a note to contact their host. Every action is refused with `403 this server is suspended`: console, commands, power, files, SFTP, backups, schedules, startup, network, databases.
- Open consoles and SFTP sessions are closed within seconds.
- Admins can still look at and change everything, except starting it.

Lifting a suspension does **not** start the server.

The panel's record is what counts. If the node is offline when you suspend, the panel refuses everything at once and tells the node when it reconnects.

Both calls are idempotent, and send the `server.suspended` / `server.unsuspended` [webhook](/server-creation#webhooks) only when something changed.
