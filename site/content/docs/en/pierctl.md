`pierctl` is the Pier command line. Everything the panel does fits in a single binary, good for the terminal, for scripts and for CI pipelines. It talks only to the Pier API: it needs neither Docker nor SSH on your machine.

## Install

Download the binary for version {{version}} for your system (Linux or macOS, x86_64 or ARM64):

```sh
os=$(uname -s | tr '[:upper:]' '[:lower:]')
arch=$(uname -m); case "$arch" in x86_64) arch=amd64 ;; aarch64|arm64) arch=arm64 ;; esac
curl -fsSL -o pierctl "{{cliRepo}}/releases/download/{{tag}}/pierctl-{{version}}-$os-$arch"
chmod +x pierctl
sudo mv pierctl /usr/local/bin/
pierctl version
```

Each release includes a `SHA256SUMS` file to check the download:

```sh
curl -fsSL -O "{{cliRepo}}/releases/download/{{tag}}/SHA256SUMS"
shasum -a 256 -c SHA256SUMS --ignore-missing
```

To build from source, with Go 1.27.4 or newer:

```sh
git clone --branch {{tag}} --depth 1 {{cliRepo}}.git
cd pier-cli && make build      # produces bin/pierctl
```

## Connect to an instance

Create a token in **Security › API tokens** and save the instance as a context:

```sh
printf %s "$TOKEN" | pierctl login https://pier.seudominio.com --token-stdin
pierctl status
```

The context lives in `~/.config/pierctl/config.yaml`, readable only by your user. In CI, use environment variables instead of a file:

```sh
export PIER_URL=https://pier.seudominio.com
export PIER_TOKEN=...          # pipeline secret
pierctl app deploy api --wait
```

The order of precedence is: flags (`--url`, `--token`, `--context`), then variables (`PIER_URL`, `PIER_TOKEN`, `PIER_CONTEXT`), then the saved context.

## Language

`pierctl` speaks English and Portuguese: help, tables, messages, confirmations and errors. The choice follows this order:

1. `--lang pt` or `--lang en`;
2. the `PIERCTL_LANG` variable;
3. `LC_ALL`, then `LC_MESSAGES`, then `LANG`: a value starting with `pt` (such as `pt_BR.UTF-8`) means Portuguese, anything else means English;
4. with nothing set, English.

```sh
export PIERCTL_LANG=pt     # always in Portuguese
pierctl --lang en status   # only this command in English
```

The CLI sends the language to Pier (`Accept-Language`), so errors that come from the server arrive in the same language. In `-o json` and `-o yaml` the field names do not change with the language, nor do the values of `apply`'s `action` field (`criado`, `atualizado`, `sem mudanças`, `removido`), which scripts read. Only the hidden-secret marker follows the language (`[hidden]` / `[oculto]`).

## Commands

| Group | What it is for |
|---|---|
| `login`, `context`, `version`, `status` | Instances and health |
| `project`, `env` | Projects and environments |
| `server` | Servers: validate, metrics, proxy and folders (`files`) |
| `app`, `deployment` | Applications: create, configure, variables, deploy, rollback and logs |
| `db` | Databases: create, connection URL, backup and download |
| `stack` | Stacks: catalog, your own compose file or a folder on the server |
| `docker` | Plain Docker: `ps`, `run`, `exec`, `logs`, images, volumes and networks |
| `cluster`, `kube` | Kubernetes, optional |
| `apply` | Infrastructure described in YAML |
| `link`, `up`, `promote` | Local folder and promotion between applications |
| `instance` | Instance backup |
| `mcp` | MCP bridge over stdio for AI agents |
| `api` | Any API route, already authenticated |

`pierctl <group> --help` lists the subcommands, with examples.

## Examples

### Create and deploy an application

```sh
pierctl app create api --project loja --repo https://github.com/acme/api --port 3000 \
    --domain https://api.exemplo.com --deploy --follow
pierctl app set api health.enabled=true health.path=/healthz limits.memory=512m
pierctl app vars import api -f producao.env
pierctl app rollback api --wait
```

### Upload the local folder

Publish the code on your machine without going through Git:

```sh
pierctl link api       # writes the application id to .pier/app
pierctl up             # uploads the current folder and follows the log
pierctl up ./src --app api
```

`up` honors `.dockerignore` and `.gitignore`, never uploads `.git` or `.pier`, and accepts up to 200 MiB.

### Promote to production

```sh
pierctl promote api-staging --to api --wait
```

### Databases and stacks

```sh
pierctl db create principal --project loja --engine postgresql
pierctl stack templates wiki                  # search the catalog
pierctl stack create wiki --project docs --template docmost \
    --route docmost:3000=https://wiki.exemplo.com --deploy --wait
```

## Built for automation

| Convention | How it works |
|---|---|
| Output | `-o table` (default), `json`, `yaml` or `name` (one id per line) |
| stdout and stderr | Data on stdout; progress, logs and errors on stderr. `-o json` is always pure JSON |
| Exit codes | `0` success · `1` error · `2` incorrect usage · `3` the operation failed · `4` timed out |
| Waiting | `--wait` waits for the operation to finish, `--follow` shows the log, `--timeout` limits the wait |
| Destructive | `rm`, `prune` and the like require `--yes`. There is never an interactive question |
| Names | Traditional commands resolve an id or a name. The new groups below require explicit IDs; check the help. An ambiguous name is an error |

### GitHub Actions

```yaml
name: deploy
on:
  push:
    branches: [main]
jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - name: Install pierctl
        run: |
          curl -fsSL -o pierctl "{{cliRepo}}/releases/download/{{tag}}/pierctl-{{version}}-linux-amd64"
          chmod +x pierctl && sudo mv pierctl /usr/local/bin/
      - name: Deploy
        env:
          PIER_URL: ${{ secrets.PIER_URL }}
          PIER_TOKEN: ${{ secrets.PIER_TOKEN }}
        run: pierctl app deploy api --wait --timeout 15m
```

Use a token with the `deploy` capability and a compatible role in the project. It must not read secrets or remove core resources; removing temporary previews is part of the deploy capability.


## New groups in the 0.6 line

These commands need a CLI version and a server version that include them. The resource arguments below are **IDs**, not names. Use `--help` on the group to see payloads and options. The new groups use JSON as the default output. Asynchronous operations return the initial state; check the status before considering it a success, and do not assume they accept `--wait`.

| Group | What it is for |
|---|---|
| `preview` | List, configure, redeploy/remove previews and keep exclusive variables |
| `placement` and `registry` | Query/configure destinations and register/verify registry credentials |
| `git-stack` and `git-provider` | Git source, webhooks, repository link and deploy status reporting |
| `volume-backup` | Eligible volumes, policies, backups, download and restore |
| `metrics-history` | Query series by server, application, database or stack |
| `server-operations` | Maintenance policy, cleanup dry run/execution and Docker installation |
| `log-drain` | Configure and test external log forwarding |
| `access` | Query effective permissions and authorized members |
| `shared-var` | Shared project/environment variables and dependent applications |
| `clone-resource` | Copy an application, database or stack definition to another environment |
| `auto-domain` | Query/generate an automatic domain and configure its policy |
| `cloud` | Query providers/machines and create machines; creation requires `--yes` and is billed |

Read and operate examples with IDs:

```sh
pierctl preview ls APP_ID
pierctl placement servers APP_ID
pierctl registry ls
pierctl volume-backup app-ls APP_ID
pierctl metrics-history application APP_ID --range 1h
pierctl access me
```

Mutations in these groups take JSON with `-f file.json` or `-f -` on standard input. Do not put credentials in shell arguments. Input files need restricted access and must be removed after use. Sensitive values are hidden in the output by default; `--show-secrets` is an explicit choice that can leak into the CI log.

Removals and restores require `--yes`, in addition to the confirmation by name that the API requires in the JSON. `volume-backup download BACKUP_ID -O archive.tar.gz` creates a new private file and does not overwrite an existing file. Protect the downloaded dumps: the volumes file is not encrypted like `.pierbak`.

### What needs the panel

The CLI uses an API token and does not send session cookies. Account security (2FA, recovery codes and sessions), invitations, role changes and SSO need a session in the panel. `pierctl account` explains this distinction; it does not turn a write token into a browser session. Offline recovery uses `pier admin`, not `pierctl`.

The interactive browser terminal and cloud credential flows that need a session must not be worked around with token commands either. See [account security](/en/docs/account-security/), [team and access](/en/docs/team-and-access/) and [server operations](/en/docs/server-operations/).
