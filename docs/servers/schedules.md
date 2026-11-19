# Schedules

The **Schedules** tab runs tasks on a timer: restart every night, announce it first, back up every six hours. You need the `schedules.manage` permission.

## A schedule

| Field | Meaning |
|---|---|
| Name | A label |
| Cron | When it runs: five fields, `minute hour day-of-month month day-of-week`, in **UTC** |
| Enabled | Off keeps it but does not run it |
| Only when online | Skip the run if the server is not running |
| Tasks | Up to 20, run in order |

A server can have up to 20 schedules. **Run now** runs one at once.

### Cron examples

| Cron | Runs |
|---|---|
| `0 4 * * *` | Every day at 04:00 UTC |
| `*/30 * * * *` | Every 30 minutes |
| `0 */6 * * *` | Every 6 hours |
| `0 3 * * 1` | Mondays at 03:00 UTC |
| `0 0 1 * *` | The first of each month |

Shortcuts like `@daily` are not accepted. Times are UTC, so convert from your time zone.

## Tasks

| Task | Payload |
|---|---|
| **Command** | A console command, one line, up to 1000 characters |
| **Power** | `start`, `stop`, `restart` or `kill` |
| **Backup** | Optional patterns to leave out, as for [backups](/servers/backups#creating-a-backup). Scheduled backups delete the oldest unlocked backup when the server is at its limit |

Each task can wait up to **900 seconds** (15 minutes) after the one before it, and can be set to **continue on failure** so the next task runs even if this one fails.

### Example: a nightly restart with warnings

| # | Task | Payload | Wait |
|---|---|---|---|
| 1 | Command | `say Restarting in 5 minutes` | 0 |
| 2 | Command | `say Restarting in 1 minute` | 240 |
| 3 | Command | `save-all` | 50 |
| 4 | Power | `restart` | 10 |

Cron: `0 4 * * *`.

## Whose permissions

A schedule runs with the permissions of **the person who last saved it**. If that person loses access to the server, or the needed permission, its tasks fail. When their account is deleted, their schedules on other people's servers are turned off.

The last run's time and result are shown on each schedule.
