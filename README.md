English | [Português](README.pt-BR.md)

# Pier

[![License: Apache-2.0 + Commons Clause](https://img.shields.io/badge/license-Apache--2.0%20%2B%20Commons%20Clause-blue.svg)](LICENSE) [![CI](https://github.com/AllanAdegas/pier/actions/workflows/ci.yml/badge.svg)](https://github.com/AllanAdegas/pier/actions/workflows/ci.yml)

Self-hosted dashboard for putting projects on your own servers: applications, databases and compose services, with a domain, HTTPS and deploys that wait for readiness and roll back on failure. Written in Go and SolidJS.

Three levels on the same infrastructure — use what the project calls for:

| Level | What you operate | Where |
|---|---|---|
| Platform | Applications, databases and stacks: "put my project online" — with a catalog of 362 services | Projects |
| Plain Docker | Every container, image, volume and network on a server, whether Pier created it or not | Infrastructure › server › Docker |
| Kubernetes (optional) | Clusters imported by kubeconfig, or a single-node K3s created by Pier | Infrastructure › Clusters |

The last two take [Rancher](https://github.com/rancher/rancher) as a reference. For automation there is the `pierctl` CLI, in [AllanAdegas/pier-cli](https://github.com/AllanAdegas/pier-cli).

### Monitoring

In **Infrastructure › Monitoring** (`/monitoring`), follow each server: CPU, used/available RAM, data directory disk, swap, uptime, load average and containers. Kubernetes clusters have a separate summary of nodes, healthy pods and warnings. Readings refresh every 15 seconds while the page is visible; there is also a manual refresh.

Host usage requires Linux with `/proc` accessible. Missing support or a collection failure shows as unavailable, never as zero usage. The panel shows snapshots and persistent history. Applications with several targets have CPU and memory totals only when collection is complete; partial readings show up as gaps. Fixed host ports require an interruption while the version is switched.

> In beta (0.x). In production on a Linux VPS since November 11, 2026: panel, proxy and five stacks validated. Clean install, backup/restore, deploy/rollback, build secrets, promotion and uptime alerts were also exercised on local Docker, Linux/arm64. These checks do not cover every flow or the whole catalog.

## Install with Docker

All you need is Docker with the compose plugin:

```sh
git clone --depth 1 https://github.com/AllanAdegas/pier.git
cd pier
./install.sh
```

The script:
- creates the `.env`, with data in `/data/pier` on Linux and in `~/.pier` on macOS;
- pulls the published image `ghcr.io/allanadegas/pier:<version>` (amd64 and arm64), at the version of the cloned code (the latest release, if you cloned `main`);
- starts the container;
- waits for Pier to respond;
- shows the address and the **setup code** for the first account.

To build the image on your own machine instead of pulling it: `PIER_BUILD=1 ./install.sh`. For another port: `PIER_PORT=8790 ./install.sh`. To stop: `docker compose down`; what Pier deployed keeps running. The installer speaks English or Portuguese: it follows `LC_ALL`/`LC_MESSAGES`/`LANG`, or force it with `PIER_LANG=en` or `PIER_LANG=pt`.

### Upgrading

The panel shows a notice when a new version exists, with the commands. They are always these (in the folder where Pier was installed; data is kept):

```sh
git fetch --depth 1 origin tag vX.Y.Z
git checkout vX.Y.Z
./install.sh
```

There is no automatic upgrade from inside the panel, on purpose: to restart itself, Pier would need to control its own container through the Docker socket, which is equivalent to root on the server. A panel that replaces itself with an image downloaded from the network would greatly widen the damage from a compromised account or release. Upgrading is a command, run by whoever administers the machine.

The check queries GitHub's public releases API at most every 12 hours, without sending any data from the instance (generic User-Agent `pier-update-check`). To turn it off: `PIER_UPDATE_CHECK=0` in the `.env`.

## Run from source

```sh
make run          # builds the interface and the binary, starts at http://localhost:8700
```

On the first visit the panel asks you to create the administrator account, with the **setup code** that Pier writes to the log on startup (`setup_code=XXXX-XXXX-XXXX`; with Docker, `docker logs pier`). Without the code nobody can claim the instance, even if it is exposed.

The binary listens only on `127.0.0.1:8700`. To reach it from another machine, put a proxy with HTTPS in front of it or set `PIER_ADDR=:8700`, knowing the panel is then exposed.

### Run on your computer or on the server

Pier operates servers over SSH, so it can run on your computer and manage a VPS without installing anything on it except Docker. In this mode:

- push webhooks do not arrive, because your computer has no public address;
- schedules, backups, the monitor and notifications only run while Pier is open;
- closing Pier in the middle of a deploy interrupts the orchestration;
- the state stays on that machine: back up `PIER_DATA_DIR`.

For a team, automation and deploy on push, run Pier on the server. Each server must have **one** owning Pier.

## AI and agents (MCP)

Pier exposes an [MCP](https://modelcontextprotocol.io) server at `/mcp`. Agents such as Claude Code, Claude Desktop and Cursor can list resources, read logs, deploy, restart and create catalog stacks. **No tool removes anything.**

- **Authentication:** only by API token (`Authorization: Bearer …`), never by the panel session, and with the same `read`/`deploy`/`write` permissions. The **Security › AI and agents (MCP)** screen creates a dedicated token and generates the configuration.
- **Claude Code:** `claude mcp add --transport http pier http://127.0.0.1:8700/mcp --header "Authorization: Bearer <token>"`
- **Stdio-only clients:** `PIER_URL=http://127.0.0.1:8700 PIER_TOKEN=<token> pier mcp`, or `pierctl mcp` with the CLI context.
- **Caution:** deploy and container logs reach the agent and may contain instructions from third parties. Give the token only what the AI needs.

Build requirements: Go 1.27.4+ and Deno 2.x. To manage the machine itself: Docker with the compose plugin.

Each server's proxy uses ports 80 and 443 by default; on a development machine, change them in Infrastructure › server › Proxy before starting it.

Upgrading is replacing the binary and restarting: database migrations run on their own. There is no down migration — copy `pier.db` first if you want to be able to return to the previous version.

## Stacks: three ways to bring up a compose

In a project, **New resource** offers:

- **Catalog service** — 359 ready-made services, with search and categories. Passwords and keys are generated; the form asks for the domain and whatever the service requires. All of them are validated with `docker compose config`, but only those marked as verified were actually deployed: test what you are going to use.
- **Custom compose** — paste a `docker-compose.yaml`. The variables `PIER_PASSWORD_X`, `PIER_USER_X`, `PIER_SECRET_X`, `PIER_HEX_X` and `PIER_BASE64_X` are generated by Pier; `PIER_URL_<SERVICE>` and `PIER_HOST_<SERVICE>` receive the address of each service that has a domain.
- **Server folder** — point to a folder on the machine that already has the compose file. Pier runs the file from there: the `.env`, the `env_file` entries and the `secrets` files are used **where they are**, without being copied into the panel, and relative paths keep working. Edit the compose in the folder and deploy again. If the project already runs by hand, provide the compose project name so Pier takes over the same volumes.

Each service is a file in `internal/stack/templates/`. After editing or adding one:

```sh
PIER_COMPOSE_CHECK=1 go test ./internal/stack -run AcceptedByDockerCompose   # validates everything with Docker
```

## Kubernetes, only if you need it

Nothing in the application flow depends on a cluster. When you need one, in Infrastructure › Clusters:

- **Import** an existing cluster by pasting the kubeconfig. Credentials must be embedded (token or certificate); a kubeconfig that depends on `gcloud`, `aws` or another plugin is refused — create a ServiceAccount and use its token.
- **Create a K3s** single node on a server. It runs as a **privileged** container and publishes the API and the Ingress HTTP port on all interfaces: suitable for development and small workloads, not for a shared server.

Pier never reads `~/.kube/config`: it only uses the kubeconfig you paste or the one it generates itself.

## Permissions

Every user is an administrator. An API token can be `read`, `deploy` or `write`; **`write` is equivalent to root on the servers** (plain Docker, custom compose, privileged K3s) and to everything an imported kubeconfig allows.

## Contributing

Pier is source-available ([license](#license)) and built to keep going without a dedicated maintainer: CI validates, releases go out on their own and dependencies update through automation. Read [CONTRIBUTING.md](CONTRIBUTING.md), [GOVERNANCE.md](GOVERNANCE.md) and the [Code of Conduct](CODE_OF_CONDUCT.md). Vulnerabilities: [SECURITY.md](SECURITY.md).

## Development

```sh
make dev-api      # backend at 127.0.0.1:8700 (data in ./.data)
make dev-web      # interface with reload at :5173
make test         # go vet, Go tests and interface type checking
```

## Configuration

| Variable | Default | What it is for |
|---|---|---|
| `PIER_ADDR` | `127.0.0.1:8700` | Listen address (the Docker image uses `:8700` and the compose file publishes on `127.0.0.1`) |
| `PIER_DATA_DIR` | `./data` | SQLite database, key and files of the local server |
| `PIER_SECRET_KEY` | generated in `<data>/secret.key` | Derives the key that encrypts secrets (SHA-256). If you set your own, use a long random value, such as `openssl rand -hex 32`: a short or predictable phrase weakens the encryption |
| `PIER_DEPLOY_CONCURRENCY` | `2` | Simultaneous deploys |
| `PIER_DEV` | — | `1` turns on debug logging |
| `PIER_UPDATE_CHECK` | `1` | `0` turns off the new-version check (and any outbound request from the notice) |
| `PIER_VERSION` | version of the cloned code | Docker only: version of the `ghcr.io/allanadegas/pier` image; `install.sh` fills it in |
| `PIER_LANG` | from `LC_ALL`/`LC_MESSAGES`/`LANG` | Only for `install.sh`: `en` or `pt` forces the installer language |

## Structure

```
cmd/pier/        entry point and dependency wiring
internal/        one package per context (application, database, stack, server,
                 engine = plain Docker, cluster + kube = Kubernetes,
                 mcp = MCP server over the API, …)
web/             SolidJS interface
```

## License

Source-available: [Apache License 2.0 with the Commons Clause](LICENSE). Use it freely, including commercially inside your company; **selling** Pier or a product or service based on it, even with small changes, requires a [commercial license](COMMERCIAL.md). Versions up to 0.9.0 remain under plain Apache-2.0. Third-party notices in [NOTICE](NOTICE) and [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).

## Documentation

The user guide lives in [`site/`](site): the launch website, with documentation written for whoever is going to install and operate Pier. `cd site && deno task serve` opens it at `http://localhost:8080`. Published version: [allanadegas.github.io/pier/en/](https://allanadegas.github.io/pier/en/) (install: [installation guide](https://allanadegas.github.io/pier/en/docs/installation/); first steps: [getting started](https://allanadegas.github.io/pier/en/docs/getting-started/)).

### Security update: panel network

The panel uses only the Docker network `pier-panel`. The managed proxy connects to
that network and to the application networks. The panel is published through the
Traefik file provider: when it starts with `PIER_DOMAIN`, Pier writes
`<local server directory>/proxy/dynamic/pier-panel.yaml` with 0600 permissions.
That file points to `http://pier-panel:8700` and configures HTTPS/ACME; no routing
capability or token needs to enter the compose file. `PIER_DOMAIN` must be only the
domain. Without that variable, Pier removes its panel route.

When upgrading an earlier installation, create the network (`install.sh` does this):

```sh
docker network inspect pier-panel >/dev/null 2>&1 || docker network create pier-panel
docker compose -f compose.yaml -f compose.proxy.yaml up --detach --build --force-recreate
```

Then also recreate the proxy in Infrastructure → local server → Proxy. The panel
and the proxy must be recreated to abandon the old shared network `pier`.
Existing applications and stacks must be redeployed to receive the labels that the
proxy's new filter requires. Set the public HTTPS URL in Settings; this turns on
HSTS. The HTTPS override forces Secure cookies. Outside it,
`PIER_COOKIE_SECURE=1` gives the same protection behind another TLS terminator.

Build limits also pass through the compose file: `PIER_BUILD_CPUS` (default 1),
`PIER_BUILD_MEMORY` (2g), `PIER_BUILD_MIN_FREE_GB` (2) and `PIER_BUILD_CACHE_GB` (10).
Code builds require Docker Buildx.

### Instance backup and restore

In Settings → Instance backup, download an encrypted `.pierbak` file
or set up a local folder, a cron schedule and retention. The passphrase has at
least 16 characters; keep it off the server. The backup includes a
consistent snapshot of `pier.db` and the effective credentials key, including when
the key comes from `PIER_SECRET_KEY`. The schedule's passphrase is stored encrypted
in the database. Database dumps, volumes, application files and proxy certificates
need their own backups. Copy the local backups to another server.

Stop Pier and restore with the binary, using the host's data directory:

```sh
pier restore --file /backup/instance.pierbak --passphrase-file /secure/passphrase --data-dir /data/pier
```

The command refuses a directory locked by Pier, verifies the file's authentication
and the SQLite integrity before replacing files. The old files get the suffix
`.before-restore-*`. Keep those copies until you have verified the restore.
When restoring without `PIER_SECRET_KEY`, the restored key is used; remove an old
override or keep exactly the key that produced the backup. Also stop old versions
of Pier that do not use the `pier.lock` file yet.

The Dashboard checklist suggests `https://github.com/heroku/node-js-getting-started`
for the first deploy with Nixpacks: a Node.js/Express application with `npm start`,
`PORT` and listening on all interfaces. Use internal port 3000 and set
`PORT=3000`. The checklist can be dismissed item by item; the backup notice in the
frame stays until an instance backup exists.
