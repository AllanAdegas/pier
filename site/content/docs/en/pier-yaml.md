There are two ways to describe configuration in a file, so you can keep it in version control together with your code:

- **`pier.yaml`** lives in the application repository and adjusts **how it is built and how it runs**, on every deploy.
- **`pierctl apply`** reads a file of yours and creates or updates **which resources exist**: applications, databases and stacks.

## pier.yaml in the repository

Put a `pier.yaml` (or `.pier.yaml`) in the root of the code, or in the configured base directory. On every deploy, Pier reads the file and overlays these options on the panel configuration, **for that deploy only**. What is saved in the panel does not change.

All keys are optional:

```yaml
build:
  pack: dockerfile            # dockerfile | nixpacks | railpack | static
  dockerfile: docker/Dockerfile.prod
  target: production          # Dockerfile stage, optional
  publish_directory: /dist    # static
  spa: true                   # static
  install_command: npm ci     # nixpacks
  build_command: npm run build
  start_command: node server.js
port: 8080
runtime:
  restart_policy: unless-stopped
  stop_grace_seconds: 30
health:
  enabled: true
  path: /healthz
  interval_seconds: 5
  timeout_seconds: 5
  retries: 10
  start_period_seconds: 5
release:
  command: npm run migrate
limits:
  cpus: "0.5"
  memory: 512m
processes:
  - name: web
    replicas: 2
  - name: worker
    command: node worker.js
    limits: { memory: 1g }
watch_paths: ["apps/api/**"]
```

### What does not go in the file

Domains, variables, secrets, server, environment and the source of the code are **not** accepted in `pier.yaml`, on purpose. The repository does not decide where the code goes and does not carry secrets. These options stay in the panel or in `apply`.

### Errors

The file is validated strictly. An unknown key, a wrong type or an invalid value **fails the deploy before the build**, with the line of the problem:

```text
pier.yaml, line 4: unknown field "prot"; accepted: …
```

The deploy log lists what came from the file. Example: `Configuration from pier.yaml applied to this deployment: port, release.command.`

> `pier.yaml` is not read for image applications, rollbacks and promotions, because there is no code to read. Also, its `watch_paths` does not filter webhooks, which run before the checkout. For monorepos, configure the watched paths in the panel too.

## Declarative infrastructure with apply

Describe the resources of one or more environments in a YAML file, with one document per resource:

```yaml
# infra.yaml
kind: Application
project: loja
name: api
config:
  source: { repository: https://github.com/acme/api, branch: main }
  network: { port: 3000, domains: [https://api.exemplo.com] }
env:
  LOG_LEVEL: info
  DATABASE_URL: ${{principal.url}}
---
kind: Database
project: loja
name: principal
engine: postgresql
```

```sh
pierctl apply -f infra.yaml --diff              # shows what would change
pierctl apply -f infra.yaml --deploy --wait     # applies and deploys
```

`apply` creates what is missing and updates what changed. Running it twice changes nothing the second time.

### Remove what left the file

With `--prune`, applications, databases and stacks that exist in Pier but are **not in the file** are also removed. This only applies to the environments that the file mentions. The CLI lists the candidates and only removes with `--yes`:

```sh
pierctl apply -f infra.yaml --diff --prune      # who would be removed
pierctl apply -f infra.yaml --prune --yes
```

In a [protected environment](/en/docs/security/#protected-and-frozen-environments), confirm the environment name. Repeat the flag for several environments:

```sh
pierctl apply -f infra.yaml --prune --yes --confirm-environment production
```

Variables are never removed by `--prune`.
