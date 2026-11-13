Pier starts a database in seconds, with a strong generated password, data on a persistent volume and a scheduled backup. You do not write a compose file or memorize the variables of each image.

## Available engines

| Engine | Backup and restore by Pier |
|---|---|
| PostgreSQL | Yes |
| MySQL | Yes |
| MariaDB | Yes |
| MongoDB | Yes |
| Redis | RDB snapshot; restore inside the database itself |
| KeyDB | RDB snapshot; restore inside the database itself |
| Dragonfly | RDB snapshot; restore inside the database itself |
| ClickHouse | No |

For ClickHouse, use your own backup and recovery strategy. Redis/KeyDB/Dragonfly snapshots do not prove compatibility between versions or engines; see [backups](/en/docs/backups/#redis-keydb-and-dragonfly).

## Create a database

In a project, click **New resource**, choose **Database** and then the engine. Give it a name and confirm. Pier generates:

- the user `pier`;
- the database `app`;
- a 32-character password, stored encrypted.

## Connect

- **From another resource on the same server:** use the internal URL shown on the database page. Even better, use a [reference](/en/docs/applications/#database-references) such as `${{principal.url}}`. It follows the current password and address.
- **From outside the server:** set a **Public port** (1024 to 65535). The database is then published on the host and the page shows the external URL. Without a public port, nothing is exposed.

> Opening a database to the internet increases the risk. Prefer an SSH tunnel (`ssh -L 5432:127.0.0.1:PORTA usuario@servidor`) or allow the port in the firewall only for your IP.

Changes to image, port and limits take effect when the database restarts. User and password do not change after they are created, because the volume is initialized with them.

## Backups

On the database page:

- **Back up now** creates a dump in the background. The size, or the error, appears in the list.
- **Schedule (cron):** a cron expression, such as `0 3 * * *` for every day at 3 a.m., and how many backups to keep. The oldest ones are deleted.
- **Download:** any finished backup can be downloaded through the browser.

The files stay on the database's own server. If the server is lost, the backups go with it. That is why you should turn on the external copy.

### Copy to S3

Set up an S3-compatible destination, with HTTPS: AWS S3, Cloudflare R2, Backblaze B2, MinIO, Wasabi and others. Then turn on the copy in each database. Every finished backup is sent to the destination. When retention deletes a backup, the remote object is deleted too. The credentials are stored encrypted and never appear in logs.

### Restore test

A backup that was never restored is only a hope. With the test turned on, by default every Sunday at 4 a.m., Pier restores the latest backup in a disposable container and runs a simple query. If it fails, you get an alert.

## Restore

For PostgreSQL, MySQL, MariaDB and MongoDB, choose a finished backup and click **Restore**. Type the database name to confirm and choose the destination:

- **Into the same database:** overwrites the current data.
- **Into a new database:** creates another database with the backup contents and new credentials. It is the safe way to check the data before switching.

During the restore, the database is disconnected from the internal network and comes back at the end, even if the restore fails. Pause the applications that write to it first.

## Remove

Removing a database asks what to do with the data. The same choice applies to the volume **and** to the backup files:

- **Checked:** deletes both.
- **Unchecked:** keeps both on the server.

With a backup or a restore in progress, the removal is refused. In a [protected environment](/en/docs/security/#protected-and-frozen-environments), you must also type the environment name.
