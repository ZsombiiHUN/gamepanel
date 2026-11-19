# Limits and timeouts

Every fixed limit in one place. Limits an admin sets per server are on [Console and power → Limits](/servers/console-and-power#limits).

## Accounts and sign-in

| Limit | Value |
|---|---|
| Password length | 12 to 256 characters |
| Sign-in attempts per client address | 10 per minute |
| Wrong passwords or codes per account | 20 per 15 minutes |
| Wrong authenticator codes in a row before waits | 5, then 1 min, 2 min, 4 min ... up to 24 h |
| Password reset requests | 10 per 15 minutes per address and per email |
| Session idle timeout | 30 minutes (`GAMEPANEL_SESSION_IDLE`) |
| Session maximum | 12 hours (`GAMEPANEL_SESSION_MAX`) |
| Password reset link | 1 hour, once |
| Invitations and setup links | 7 days |
| Single sign-on round trip | 10 minutes; the two-factor step after it, 5 minutes |
| Passkey sign-in or setup | 5 minutes |
| API keys per account | 25 |
| API key expiry | 0 (never) to 3650 days |

## Servers

| Limit | Value |
|---|---|
| Name | 1 to 64 characters, one line |
| Memory | 64 MB to 1 TB |
| CPU | Up to 6400% (64 cores) |
| Processes | 1024 by default |
| IO weight | 10 to 1000 |
| Backups | 20 by default |
| `external_id` | Up to 191 characters, unique |
| Power action | 15 minutes |
| Crash restart delay | 2 seconds |
| Crash loop window | 60 seconds |
| Install script | 30 minutes, at least 1 GB memory, 4096 processes |

## Console

| Limit | Value |
|---|---|
| Lines shown when opening | 500 |
| Line length | 8 KiB, longer lines are cut |
| Output rate | 2,000 lines per second, the rest of that second is skipped |

## Files

| Limit | Value |
|---|---|
| Opening in the editor | 10 MiB |
| Pull from URL | 30 minutes |
| Upload or download through the panel | 12 hours |
| Disk usage cache | 30 seconds |

## Backups

| Limit | Value |
|---|---|
| Backup or restore | 2 hours |
| Running backups per server | 1 |
| Unpacked size of a restore | 200 GiB |
| Ignore patterns | 2000 characters |
| S3 upload URLs valid for | 6 hours |
| S3 download link valid for | 15 minutes |

## Schedules

| Limit | Value |
|---|---|
| Schedules per server | 20 |
| Tasks per schedule | 20 |
| Wait before a task | 0 to 900 seconds |
| Command task | 1 line, up to 1000 characters |

## SFTP

| Limit | Value |
|---|---|
| Connections per address | 20 |
| Failed logins per address | 10 per 5 minutes |
| Tries per connection | 3 |
| Open files per session | 256 |
| Channels per connection | 8 |

## Webhooks

| Limit | Value |
|---|---|
| Answer within | 10 seconds |
| Tries | 12, waiting 30 s, 1 min, 2 min ... up to 6 h between them (about a day) |
| Delivery log | Last 100 per webhook shown; finished ones kept 30 days |

## Panel and nodes

| Limit | Value |
|---|---|
| Panel request to a node | 5 minutes |
| Database host operations | 30 seconds |
| Daemon reconnect wait | 1 s, doubling up to 30 s |
| Usage history | 30 days: per minute for 2 days, then per 15 minutes |
| Panel shutdown grace | 5 seconds |
