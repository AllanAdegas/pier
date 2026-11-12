A `pierctl` é a linha de comando do Pier. Tudo o que o painel faz cabe num binário único, bom para o terminal, para scripts e para pipelines de CI. Ela fala só com a API do Pier: não precisa de Docker nem de SSH na sua máquina.

## Instalar

Baixe o binário da versão {{version}} para o seu sistema (Linux ou macOS, x86_64 ou ARM64):

```sh
os=$(uname -s | tr '[:upper:]' '[:lower:]')
arch=$(uname -m); case "$arch" in x86_64) arch=amd64 ;; aarch64|arm64) arch=arm64 ;; esac
curl -fsSL -o pierctl "{{cliRepo}}/releases/download/{{tag}}/pierctl-{{version}}-$os-$arch"
chmod +x pierctl
sudo mv pierctl /usr/local/bin/
pierctl version
```

Cada release traz um arquivo `SHA256SUMS` para conferir o download:

```sh
curl -fsSL -O "{{cliRepo}}/releases/download/{{tag}}/SHA256SUMS"
shasum -a 256 -c SHA256SUMS --ignore-missing
```

Para compilar a partir do código, com Go 1.27.4 ou mais novo:

```sh
git clone --branch {{tag}} --depth 1 {{cliRepo}}.git
cd pier-cli && make build      # gera bin/pierctl
```

## Conectar a uma instância

Crie um token em **Segurança › Tokens de API** e salve a instância como um contexto:

```sh
printf %s "$TOKEN" | pierctl login https://pier.seudominio.com --token-stdin
pierctl status
```

O contexto fica em `~/.config/pierctl/config.yaml`, com permissão só para o seu usuário. Em CI, use variáveis de ambiente em vez de arquivo:

```sh
export PIER_URL=https://pier.seudominio.com
export PIER_TOKEN=...          # segredo do pipeline
pierctl app deploy api --wait
```

A ordem de precedência é: flags (`--url`, `--token`, `--context`), depois variáveis (`PIER_URL`, `PIER_TOKEN`, `PIER_CONTEXT`), depois o contexto salvo.

## Comandos

| Grupo | Para quê |
|---|---|
| `login`, `context`, `version`, `status` | Instâncias e saúde |
| `project`, `env` | Projetos e ambientes |
| `server` | Servidores: validar, métricas, proxy e pastas (`files`) |
| `app`, `deployment` | Aplicações: criar, configurar, variáveis, deploy, rollback e logs |
| `db` | Bancos: criar, URL de conexão, backup e download |
| `stack` | Stacks: catálogo, compose próprio ou pasta do servidor |
| `docker` | Docker puro: `ps`, `run`, `exec`, `logs`, imagens, volumes e redes |
| `cluster`, `kube` | Kubernetes, opcional |
| `apply` | Infraestrutura descrita em YAML |
| `link`, `up`, `promote` | Pasta local e promoção entre aplicações |
| `instance` | Backup da instância |
| `mcp` | Ponte MCP por stdio para agentes de IA |
| `api` | Qualquer rota da API, já autenticada |

`pierctl <grupo> --help` lista os subcomandos, com exemplos.

## Exemplos

### Criar e implantar uma aplicação

```sh
pierctl app create api --project loja --repo https://github.com/acme/api --port 3000 \
    --domain https://api.exemplo.com --deploy --follow
pierctl app set api health.enabled=true health.path=/healthz limits.memory=512m
pierctl app vars import api -f producao.env
pierctl app rollback api --wait
```

### Enviar a pasta local

Publique o código da sua máquina sem passar pelo Git:

```sh
pierctl link api       # grava o id da aplicação em .pier/app
pierctl up             # envia a pasta atual e acompanha o log
pierctl up ./src --app api
```

O `up` respeita o `.dockerignore` e o `.gitignore`, nunca envia `.git` nem `.pier` e aceita até 200 MiB.

### Promover para produção

```sh
pierctl promote api-staging --to api --wait
```

### Bancos e stacks

```sh
pierctl db create principal --project loja --engine postgresql
pierctl stack templates wiki                  # busca no catálogo
pierctl stack create wiki --project docs --template docmost \
    --route docmost:3000=https://wiki.exemplo.com --deploy --wait
```

## Feita para automação

| Convenção | Como funciona |
|---|---|
| Saída | `-o table` (padrão), `json`, `yaml` ou `name` (um id por linha) |
| stdout e stderr | Dados em stdout; progresso, logs e erros em stderr. `-o json` é sempre JSON puro |
| Códigos de saída | `0` sucesso · `1` erro · `2` uso incorreto · `3` a operação falhou · `4` tempo esgotado |
| Esperar | `--wait` espera a operação terminar, `--follow` mostra o log, `--timeout` limita a espera |
| Destrutivo | `rm`, `prune` e afins exigem `--yes`. Nunca há pergunta interativa |
| Nomes | Recursos aceitam id ou nome. Um nome ambíguo é erro, nunca um palpite |

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
      - name: Instalar a pierctl
        run: |
          curl -fsSL -o pierctl "{{cliRepo}}/releases/download/{{tag}}/pierctl-{{version}}-linux-amd64"
          chmod +x pierctl && sudo mv pierctl /usr/local/bin/
      - name: Implantar
        env:
          PIER_URL: ${{ secrets.PIER_URL }}
          PIER_TOKEN: ${{ secrets.PIER_TOKEN }}
        run: pierctl app deploy api --wait --timeout 15m
```

Use um token com permissão `deploy`: ele implanta, mas não remove nada nem lê segredos.
