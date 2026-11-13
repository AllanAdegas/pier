Pier is a panel you install on **your own** server to publish projects on it. You point it at a Git repository, a Docker image or a folder on your computer. Pier builds, starts, adds a domain and HTTPS, backs up the databases and warns you when something goes wrong.

It is a self-hosted alternative to services such as Heroku, Render and Vercel: projects run on a VPS you already pay for, with no monthly fee per application.

## Who it is for

- **People who are just starting** and want to put a website, an API or a bot online without learning Docker, reverse proxies and certificates.
- **Developers and small teams** who keep several applications on one or a few VPSs and want deployments without downtime, rollback and backups without putting it all together by hand.
- **People who automate**: there is an API, a CLI (`pierctl`), configuration in the repository (`pier.yaml`) and an MCP server for AI agents.

## What you can publish

| Type | Examples | Page |
|---|---|---|
| Application | Node, Python, Go, PHP, Ruby, Java APIs; static sites; React or Vue SPAs | [Applications](/en/docs/applications/) |
| Database | PostgreSQL, MySQL, MariaDB, MongoDB, Redis, KeyDB, Dragonfly, ClickHouse | [Databases](/en/docs/databases/) |
| Stack | More than 350 ready-made services (n8n, Plausible, Uptime Kuma, WordPress…) or your own `docker-compose.yaml` | [Stacks and catalog](/en/docs/stacks/) |

## How it works

Pier is a single program, written in Go, that runs in a Docker container. It keeps everything in its own SQLite database and operates the Docker of the servers:

- **On the server where it is installed**, through the Docker socket.
- **On other servers**, through SSH. You do not need to install anything on them besides Docker.

In front of the applications, each server has a proxy (Traefik) managed by Pier. It receives traffic on ports 80 and 443, forwards each domain to the right container and issues the Let's Encrypt certificates.

A deployment always follows the same path:

1. Pier downloads the code or the image.
2. It builds the image, if it is code.
3. It runs the release command, if there is one. Example: the database migrations.
4. It starts the new version **alongside** the current one and waits for it to be ready.
5. Only then does it switch the traffic and stop the previous version.

Failures before the switch keep the previous version. With multiple servers, the update is sequential and the compensation may need your intervention; migrations and external effects are not undone by the rollback.

## Organization

- **Project**: groups what belongs to the same product. Example: "Shop".
- **Environment**: splits a project into `production`, `staging` and others. An environment can be **protected** (to delete something, you must type its name) or **frozen** (it does not accept deployments).
- **Resource**: an application, a database or a stack, always inside an environment and running on a server.

## What it does not do yet

Pier is in **beta** (version {{version}}). Before you decide, know what is left out of this version:

- [Multiple destinations](/en/docs/multi-server/) use a sequential rollout. DNS/load balancer and shared data are up to you.
- [Previews per pull request](/en/docs/previews/) are optional and run code from the repository; forks need special care.
- There are [team and roles per project](/en/docs/team-and-access/); validate this version's authorization integration before opening access to a team.
- There is a [metrics history](/en/docs/alerts/#metrics-history); gaps do not mean zero usage.
- There are flows to install Docker over SSH and to provision Hetzner/DigitalOcean, still subject to validation in your environment. See [server operations](/en/docs/server-operations/).
- The catalog services are validated automatically, but only some were actually deployed. Test what you are going to use.

Kubernetes exists as an advanced option (import a cluster or create a K3s), but nothing in the common flow depends on it.

## Next step

Follow the [installation](/en/docs/installation/) guide: it takes about 10 minutes on a server with Docker.

## License and responsibility

The code uses Apache-2.0. The repository and the images are public. See [license and governance](/en/docs/community/). Automation reduces maintenance, but it does not replace human review.

The new guides follow the development of the 0.6 line. The published version {{version}} may not contain all these features until the matching release; check the notes of the tag you choose.
