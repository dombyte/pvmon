# Backups and operation

## Data directory

The data directory contains the database file of `storage.path`. The default is `./data`.

| Entry | Content |
| --- | --- |
| `pvmon.db` | Database |
| `secret.key` | Key for the stored device passwords |
| `backups/` | Backups of the database and the key |

## Backups

pvmon makes a backup at the interval `storage.backup_interval` (default 24 h). It does a
check of each backup. It keeps the newest backups, as set in `storage.max_backups`
(default 3).

Copy the backups to a second storage device.

## Restore a backup

pvmon has no command for this task.

::: warning
Do not delete the current database before the restored database operates correctly.
:::

1. Stop pvmon.
2. Move `pvmon.db`, `pvmon.db-wal` and `pvmon.db-shm` to a different directory. The last
   two files are not always there.
3. Copy `pvmon-<time>.db` from `backups/` to `pvmon.db`.
4. Move `secret.key` to the directory of step 2.
5. Copy `pvmon-<time>.secret.key` from `backups/` to `secret.key`.
6. Start pvmon.

## Retention

::: warning
pvmon does not delete old data. The setting `storage.retention` has no function in this
version.
:::

As an example, a site with one hybrid inverter, a grid meter and two wallboxes uses
approximately 22 MB each year.

## Update

1. Replace the program file, or download the new image.
2. Start pvmon.

Before pvmon changes the database structure, it makes a backup. An older version does not
open a newer database.

## Health and logs

- `GET /health` gives the status 200 (`ok` or `degraded`) or 503.
- pvmon writes logs in JSON format to stdout. To also write a log file, set `log.file`.
- pvmon can use 30 seconds to stop. Set the stop timeout of your supervisor to a larger
  value.
