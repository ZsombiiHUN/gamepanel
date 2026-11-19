# Audit log

**Audit log** (admin) lists what happened on the whole panel: sign-ins and failed sign-ins, account changes, nodes, eggs, servers, and everything done on servers. It is searchable. The API is `GET /api/audit`.

Each server also has its own history, readable by its owner and by subusers with `activity.read`. See [Activity](/servers/activity).

## What is recorded

Each entry has the time, who did it (account email, or `cli` for the [command line](/panel/cli)), the client address, the action, its target and a detail.

| Group | Actions |
|---|---|
| Sign-in | `login.ok`, `login.fail`, `login.blocked`, `login.recovery_code`, `reauth.fail`, `sessions.revoke`, `sessions.revoke_others` |
| Account | `account.create`, `account.register`, `account.update`, `account.delete`, `password.change`, `password.reset`, `password.set`, `totp.enable`, `totp.disable`, `totp.reset`, `totp.recovery_regenerated`, `passkey.add`, `passkey.delete`, `sso.link`, `sso.unlink`, `apikey.create`, `apikey.delete` |
| Admin | `node.create`, `node.delete`, `node.capacity`, `node.sftp`, `egg.import`, `invite.create`, `invite.delete`, `webhook.create`, `webhook.update`, `webhook.delete` |
| Servers | `server.create`, `server.delete`, `server.power`, `server.command`, `server.crash`, `server.rename`, `server.reinstall`, `server.startup`, `server.limits`, `server.owner`, `server.external_id`, `server.suspend`, `server.unsuspend` |
| Files | `file.write`, `file.upload`, `file.delete`, `file.move`, `file.copy`, `file.mkdir`, `file.chmod`, `file.compress`, `file.decompress`, `file.pull`, `mod.install` |
| Backups | `backup.create`, `backup.delete`, `backup.restore`, `backup.download`, `backup.lock`, `backup.unlock` |
| Other | `schedule.create`, `schedule.update`, `schedule.delete`, `schedule.run`, `subuser.add`, `subuser.update`, `subuser.remove`, `database.create`, `database.delete`, `sftp.login`, `sftp.fail` |

## Client addresses

Addresses are real client addresses only when the panel trusts your reverse proxy (`GAMEPANEL_TRUSTED_PROXIES`). Otherwise every entry shows the proxy's address. See [Reverse proxy and HTTPS](/panel/reverse-proxy).

## Retention

Audit and activity entries are kept **forever** for now; there is no automatic pruning. They take little space, but watch the database size on a busy panel.
