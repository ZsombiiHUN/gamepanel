# How it works

GamePanel has two programs: the **panel**, which you install once, and the **daemon**, which runs on every machine that hosts game servers (a **node**).

<SystemMap />

## The panel

The panel is a single binary, `panel`. It holds:

- the **web interface**, built into the binary, so there is nothing else to serve;
- the **API** under `/api/`, which the interface itself uses;
- the **daemon endpoint** under `/daemon/`, where nodes connect;
- the **database**, one SQLite file (`GAMEPANEL_DB`), plus a **key file** next to it that encrypts two-factor secrets.

It listens on plain HTTP (`127.0.0.1:8080` by default). In production, a reverse proxy in front of it handles HTTPS. See [Reverse proxy and HTTPS](/panel/reverse-proxy).

The panel also runs the background work: schedules, webhook deliveries, and folding usage history into longer buckets.

### One panel process

Run **one** panel process per database. Several things live in its memory, not the database:

- node connections (each node keeps one websocket to one panel);
- sign-in rate limits and failed-login counters (they start again from zero when the panel restarts);
- single sign-on and passkey sign-ins in progress, and upload and download transfers in flight;
- the list of unknown containers each node reported.

Two panels behind a load balancer would split nodes between them, each knowing only half, and each counting failed logins separately. SQLite also allows only one writer. For availability, restart the one process quickly (systemd does) and [back up the database](/panel/backing-up); for capacity, one panel serves many nodes, since the game servers' work happens on the nodes.

## The daemon

The daemon, `daemon`, runs on each node. It:

- keeps **one websocket open to the panel** (`GAMEPANEL_PANEL_URL`). The panel sends it orders (create, start, stop, write a file) over that connection and it sends back state, console output and usage. If the connection drops, it reconnects, waiting 1 s, 2 s, 4 s ... up to 30 s between tries. Servers keep running meanwhile;
- creates and runs **one Docker container per game server**, with the server's files bind-mounted from its data directory;
- runs the egg's **install script** in a separate, short-lived container;
- serves **SFTP** on its own port (2022 by default) and checks each login with the panel;
- enforces **disk quotas** and, if the tools are there, **network limits**;
- keeps **backups** in its data directory, or uploads them to S3 through addresses the panel signs for it.

Because the daemon dials out, a node needs **no open port for the panel**. It only needs the game ports, and the SFTP port if you use SFTP.

## Uploads and downloads

Large transfers (file uploads and downloads, backup downloads) do not go over the control websocket. The node opens a second connection to the panel for each one and the panel streams between it and your browser. Your browser only ever talks to the panel.

## Who can do what

- **Administrators** can see and do everything, including creating servers, nodes and eggs.
- **Owners** have every permission on their own servers, except changing limits and things only an admin may do.
- **Subusers** have only the permissions the owner gave them on that server. See [Sharing access](/servers/subusers).

Someone who has no access to a server gets `404 Not Found` for it, the same as for a server that does not exist, so server IDs cannot be probed.

## The stack

| Part | Built with |
|---|---|
| Panel HTTP and API | Go `net/http` with [huma](https://github.com/danielgtaylor/huma) (OpenAPI 3.1) |
| Database | SQLite through [modernc.org/sqlite](https://gitlab.com/cznic/sqlite) (pure Go, no CGO), migrations with goose |
| Websockets | coder/websocket |
| Docker | `moby/moby/client` |
| Interface | React 19, Vite, TanStack Query and Router, Tailwind 4, xterm.js, CodeMirror, embedded with `go:embed` |
