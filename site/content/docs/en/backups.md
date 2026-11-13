There are two kinds of backup, and you need both:

- **Instance backup:** Pier itself. It keeps accounts, projects, settings, encrypted passwords and the key that opens those passwords.
- **Data backup:** what your applications store, such as databases and volumes.

## Instance backup

In **Settings › Instance backup**:

- **Download encrypted backup:** creates a `.pierbak` file. You choose a passphrase of at least 16 characters.
- **Automatic local backup:** choose the destination folder on the server, a schedule in cron format (for example `0 2 * * *`) and how many backups to keep.

The file contains a consistent copy of the Pier database (`pier.db`) and the credentials key. It is encrypted with AES-256-GCM, using a key derived from the passphrase.

> **Keep the passphrase off the server**, in a password manager. Without it, the backup cannot be opened. And copy the `.pierbak` files to another machine: a backup on the same disk is lost together with it.

With the CLI, the same backup:

```sh
pierctl instance backup --out pier-$(date +%F).pierbak --passphrase-file ./frase-senha.txt
pierctl instance status        # when the last backup ran
```

Until the first backup exists, the panel shows a warning at the top.

### What is not included

The instance backup does **not** include:

- the database data, which has its own backup, described below;
- the volumes of applications and stacks;
- the issued certificates, which are requested again when they are missing.

## Restore the instance

The restore is done with Pier **stopped**, so that two copies are not writing to the same database. On the server, in the `pier` folder:

1. Stop the panel:

   ```sh
   docker compose stop pier
   ```

2. Put the file and the passphrase in a folder, for example `/root/restauracao`. The passphrase goes in a text file:

   ```sh
   sudo install -d -m 700 /root/restauracao
   sudo cp pier-2026-11-11.pierbak /root/restauracao/instancia.pierbak
   # Create /root/restauracao/frase with a secure editor; do not expose the passphrase in the shell history.
   sudo chmod 600 /root/restauracao/frase
   ```

3. Run the restore with the same Pier image. The command uses the data directory from `.env`:

   ```sh
   . ./.env
   docker compose run --rm --no-deps \
     -v /root/restauracao:/restauracao:ro \
     pier restore --file /restauracao/instancia.pierbak \
       --passphrase-file /restauracao/frase --data-dir "$PIER_DATA_DIR"
   ```

4. Start the panel and sign in with your usual account:

   ```sh
   docker compose up -d pier
   ```

Before replacing any file, the command checks the passphrase, the authenticity of the file and the integrity of the database. The old files are not deleted: they get the suffix `.before-restore-…`. Keep them until you confirm that everything is fine, and then delete the `/root/restauracao` folder.

If the panel is still running, the restore is refused.

> If you set `PIER_SECRET_KEY` in `.env`, it must be **exactly** the one from the instance that created the backup. Without `PIER_SECRET_KEY`, the key that came in the backup is used.

### Move to another server

To take Pier to another machine:

1. Install Pier on the new server, without creating an account.
2. Restore the backup as above.
3. Point the DNS to the new IP.

Applications must be deployed again on the new server. Database data comes back through their own backups.

## Database backups

Each PostgreSQL, MySQL, MariaDB or MongoDB database has a scheduled backup, a copy to S3, a weekly restore test and a one-click restore. See [Databases](/en/docs/databases/#backups).

## Application volumes

In the application or stack, open **Volume backups**. Volumes appear after the first deploy. Configure the schedule, retention, S3 copy and which directories should be included. By default, directories outside the Pier data path are left out; system paths remain forbidden.

Use **Stop the containers during the backup** when you need consistent data: the application is unavailable while the file is created, including in scheduled runs. Without this option, a file can change during the copy. For active databases, prefer the database's logical backup.

The backup is a `.tar.gz` of the selected volumes/directories; it is not the encrypted instance backup. Protect the file and the external destination. To restore, choose a finished backup and type the exact name of the resource. The restore stops the resource, replaces the contents of the volumes and starts the containers at the end. It does not merge files and it is not an automatic migration to another server. Test on disposable data before restoring production.

Follow the operation until it finishes and check the application files. A backup on the same disk does not protect against losing the server.

## Redis, KeyDB and Dragonfly

These databases have RDB snapshot backups in the same retention, download and S3 copy flow. The restore happens in the database itself, replacing the data set, and the database is unavailable meanwhile. It removes earlier persistence files that could take precedence over the snapshot. Do not treat the file as a universal restore across versions or engines. ClickHouse does not get a restore through this flow.

## A routine that works

1. Instance backup scheduled every day, with a daily copy off the server.
2. A scheduled backup for each database, with a copy to S3 and the restore test turned on.
3. "Backup not run" alerts arriving in a channel you read.
4. Once a quarter, do a real restore on a test server.
