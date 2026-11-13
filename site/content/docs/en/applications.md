An application is a program of yours running in a container: a website, an API, a worker. Pier builds, publishes and updates the application without taking down the version that is live.

## Where the code comes from

| Source | When to use it |
|---|---|
| **Git application** | The code is in a repository. Pier clones it and builds it on every deployment |
| **Docker image** | You already publish an image to a registry, such as Docker Hub or GHCR |
| **Local folder (pierctl up)** | Send the folder from your computer with `pierctl up`, without going through Git. See [pierctl](/en/docs/pierctl/#upload-the-local-folder) |
| **Promotion** | Take to production the same image that was tested in staging |

Public repositories use the `https://` address. For private ones there are two options:

- **Deploy key (recommended):** a `git@host:org/repo.git` address and an SSH key generated in **Security**.
- **HTTPS token:** the token is never left in the build context.

## How the image is built

| Build pack | What it does |
|---|---|
| **Dockerfile** | Uses the repository's `Dockerfile`. You can set another path and a base directory |
| **Nixpacks** | Detects the language and generates the image by itself. Requires [Nixpacks](https://nixpacks.com) installed on the server |
| **Static site** | Serves the files with nginx on port 80. With **Single-page application** turned on, unknown routes return `index.html` |
| **Docker image** | No build: it only pulls the image |

Code builds require Docker Buildx. Each server runs one build at a time, with a CPU and memory limit. The limits are in Pier's `.env` and are described in the [installation](/en/docs/installation/#env-settings) guide.

## How a deployment happens

1. **Deploy** puts the deployment in the queue. If one is already waiting, Pier reuses it.
2. The code is cloned and the image is built.
3. The **release command** runs, if there is one.
4. The new version starts **alongside** the current one.
5. When the new one is ready, the proxy sends traffic to it, and the previous one is stopped gently.

The new version counts as ready like this:

- **with a health check:** when Docker marks it as healthy;
- **without a health check:** when it has run for 6 seconds without restarting.

If it does not become ready, it is discarded and the previous version is kept. With fixed host ports, the two versions cannot run at the same time: there is an interruption to free the port, and Pier tries to restart the original containers if the attempt fails. This compensation does not undo migrations or external changes and may need your intervention if Docker cannot start the previous version. The log shows the reason and, when it recognizes the error, an explanation with the fix. Examples: out of memory, missing start script, full disk, wrong port.

Deployments of the same application never run in parallel. Of different applications, up to `PIER_DEPLOY_CONCURRENCY` run at the same time (default 2).

## Port, domain and network

- **Application port:** the port the program listens on **inside** the container. Pier suggests the port declared in the image. After the deployment, it warns you if the program listens on another port or only on `127.0.0.1`.
- **Domains:** one or several, with `https://` to get an automatic certificate. Before asking for the certificate, Pier checks that DNS points to the server. If it does not, it publishes over HTTP and tells you which record to create.
- **Ports published on the host:** only when something outside needs to reach a port directly, such as a game server. With them, the deployment has a short downtime, because two versions cannot use the same port.
- **Internal network:** resources on the same server see each other by name. An application without a domain can only be reached through them.

## Environment variables

In the **Variables** tab, each variable can be:

| Type | Available | Note |
|---|---|---|
| Runtime (default) | In the running application | Values appear masked in the deployment log |
| Build | During the build, as `--build-arg` | Visible in the image history. Do not use for secrets |
| Build secret | During the build, as a BuildKit *secret*, and also in the running application | It does not stay in the image layers. In the Dockerfile: `RUN --mount=type=secret,id=NOME`. Do not copy the value into a file in the image |

You can import a whole `.env` file. Changes take effect on the **next** deployment: what is running does not change by itself.

### Database references

Instead of copying the database password, use a reference. Pier replaces it with the current value on every deployment:

```sh
DATABASE_URL=${{principal.url}}
DB_HOST=${{principal.host}}
DB_PORT=${{principal.port}}
DB_USER=${{principal.user}}
DB_PASSWORD=${{principal.password}}
DB_NAME=${{principal.database}}
```

`principal` is the name (or the id) of the database. The **Connect to a database** button builds the reference for you.

## Health check, limits and volumes

- **Health check:** an HTTP path, such as `/healthz`, or a command of your own. It is off by default because many images have neither `curl` nor `wget`. Turn it on for safer deployments.
- **Limits:** CPU (`0.5` = half a core) and memory (`512m`, `1g`). Without a limit, the application can use the whole machine.
- **Volumes:** folders that survive deployments, for uploads and generated files. Volumes are not part of the automatic backup. See [Backups](/en/docs/backups/).

## Release command

A command that runs **in the new image, before it receives traffic**. It is the place for database migrations:

```sh
npm run migrate
```

If the command fails, the deployment stops and the previous version stays live. It also runs on the first deployment.

There are also pre-deploy commands, which run in the container that is already live, and post-deploy commands. Use them for tasks such as notifying another system or clearing a cache.

## Processes and replicas

The same repository can run more than one process:

- `web`, which receives the domain;
- `worker`, without a domain, for example `bundle exec sidekiq` or `node worker.js`.

Each process has a command, up to 10 replicas and its own limits. In a deployment, **all** the new replicas must be ready before the previous version leaves. The replicas run on the same server.

## Rollback, cancellation and logs

- **Rollback:** in the **Deploys** tab, choose a finished deployment and run it again. There is no build, and the old image comes back in seconds, if it still exists on the server.
- **Cancel:** a queued deployment does not start; one in progress is interrupted, and what was live stays live.
- **Logs:** the **Logs** tab shows the container live. When the version changes, the last 1,000 lines of the previous version are kept, with secrets masked.

## Automatic deployment

In the **Automation** tab:

- **Push webhook** for GitHub, GitLab and Gitea or Forgejo. It fires only for the configured branch.
- **Deployment by tag:** with a pattern such as `v*`, only tags that match trigger a deployment. Good for publishing only tagged versions.
- **Watched paths (monorepo):** with `apps/api/**`, a push only deploys if something in that folder changed.
- **Scheduled tasks:** commands that run in the container at times set by cron. Example: `0 3 * * *` for every day at 3 a.m.

## Promote to production

**Promote** takes the exact image, identified by its digest, that is running in one application, for example `api-staging`, to another, such as `api`. The image is not rebuilt: production receives the same tested artifact, with the **variables of the destination application**.

Promotion works on the same server and, when there is an accessible published digest, between servers. It respects the freeze of the destination environment. See [multiple servers](/en/docs/multi-server/).

```sh
pierctl promote api-staging --to api --wait
```

## Configuration in the repository

A `pier.yaml` file at the root of the code can define port, build, health check, release command, limits and processes. It applies to every deployment. See [pier.yaml and apply](/en/docs/pier-yaml/).


## Advanced configuration and destinations

For Railpack, Dockerfile stage, restart, graceful shutdown, shared variables and cloning, follow [Build and configuration](/en/docs/build-and-configuration/). For a separate build server, a private registry and one application in several destinations, see [Registry and multiple servers](/en/docs/multi-server/).

### Host binds and privileges

Binds of server directories and dangerous options require an **instance** owner, in addition to the explicit Docker policy. Being only an administrator or owner of the project does not allow saving this configuration, even with the policy turned on. Prefer the resource's named volumes. The deploy permission for an already authorized configuration remains subject to the current policy.
