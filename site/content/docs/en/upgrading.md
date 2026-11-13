Upgrading Pier means switching the code version and running the installer again. Your data stays where it is and the database migrations run on their own.

## Versions

Pier follows [semantic versioning](https://semver.org/). Each beta version is a `vX.Y.Z` tag in the repository, with the notes in [Releases]({{repo}}/releases). The current beta version is **{{version}}**.

While the number starts with `0.`, a new version can change behaviors. Read the notes before you upgrade.

## Before you upgrade

1. Make an [instance backup](/en/docs/backups/) and keep it outside the server.
2. Stop the panel with `docker compose stop pier` and copy Pier's database. Do not copy an active SQLite without coordinating its WAL. Migrations cannot be undone, so this copy is your way back to the previous version:

   ```sh
   . ./.env
   sudo cp "$PIER_DATA_DIR/pier.db" "$PIER_DATA_DIR/pier.db.antes-da-atualizacao"
   ```

3. Check that there are no deploys in progress.

## Upgrade

In the `pier` folder, on the server, replace `vX.Y.Z` with the new version:

```sh
git fetch --depth 1 origin tag vX.Y.Z
git checkout vX.Y.Z
./install.sh
```

The installer downloads the GHCR image of the selected version and recreates the container. If the download fails, see [what to do](/en/docs/installation/#if-the-image-download-fails) or build the code locally with `PIER_BUILD=1 ./install.sh`. The `.env` is kept. The panel is offline for a few seconds, but the applications keep responding.

Check the running version:

```sh
docker compose exec pier wget -q -O - http://127.0.0.1:8700/api/v1/version
```

## Go back to the previous version

1. Stop the panel: `docker compose stop pier`.
2. Put back the database you copied before the upgrade:

   ```sh
   . ./.env
   sudo cp "$PIER_DATA_DIR/pier.db.antes-da-atualizacao" "$PIER_DATA_DIR/pier.db"
   sudo rm -f "$PIER_DATA_DIR/pier.db-wal" "$PIER_DATA_DIR/pier.db-shm"
   ```

3. Go back to the old code and reinstall:

   ```sh
   git checkout vVERSAO-ANTERIOR
   ./install.sh
   ```

What changed between the copy and the rollback, such as a new deploy, is left out.

## Release notes for 0.5.0

If you come from an installation older than 0.5.0, you need three extra steps. Routing became stricter: the proxy only forwards to containers marked by Pier with the server token.

1. **Panel network.** The installer creates the `pier-panel` network on its own. If you start Pier without the installer, create the network first:

   ```sh
   docker network inspect pier-panel >/dev/null 2>&1 || docker network create pier-panel
   ```

2. **Proxy.** In **Infrastructure › local server › Proxy**, recreate the proxy. Do the same on the other servers.
3. **Resources with a domain.** Deploy again every application and stack that has a domain. Until the new deploy, they stop being routed.

Other new things in 0.5.0: release command, build secrets, promotion between environments, deploy by tag, processes and replicas, `pier.yaml`, `pierctl up`, restore and test of backups, instance backup, runtime and uptime alerts, audit log and protected environments.

## Upgrade pierctl

Download the binary of the new version, as in the [pierctl installation](/en/docs/pierctl/#install), and replace the old one. The CLI and the server speak the same API (v1). Keep both on the same version to have all the commands.
