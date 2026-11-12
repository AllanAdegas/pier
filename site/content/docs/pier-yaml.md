Há dois jeitos de descrever configuração em arquivo, para versionar junto com o código:

- **`pier.yaml`** fica no repositório da aplicação e ajusta **como ela é construída e roda**, a cada deploy.
- **`pierctl apply`** lê um arquivo seu e cria ou atualiza **quais recursos existem**: aplicações, bancos e stacks.

## pier.yaml no repositório

Coloque um `pier.yaml` (ou `.pier.yaml`) na raiz do código, ou no diretório base configurado. A cada deploy, o Pier lê o arquivo e sobrepõe essas opções à configuração do painel, **só para aquele deploy**. O que está salvo no painel não muda.

Todas as chaves são opcionais:

```yaml
build:
  pack: dockerfile            # dockerfile | nixpacks | railpack | static
  dockerfile: docker/Dockerfile.prod
  target: production          # estágio de Dockerfile, opcional
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

### O que não vai no arquivo

Domínios, variáveis, segredos, servidor, ambiente e origem do código **não** são aceitos no `pier.yaml`, de propósito. O repositório não decide para onde o código vai nem carrega segredos. Essas opções ficam no painel ou no `apply`.

### Erros

O arquivo é validado com rigor. Uma chave desconhecida, um tipo errado ou um valor inválido **falham o deploy antes do build**, com a linha do problema:

```text
pier.yaml, linha 4: campo desconhecido "prot"; aceitos: …
```

O log do deploy lista o que veio do arquivo. Exemplo: `Configuração de pier.yaml aplicada a este deploy: port, release.command.`

> O `pier.yaml` não é lido em aplicações de imagem, rollbacks e promoções, porque não há código para ler. E o `watch_paths` dele não filtra webhooks, que rodam antes do checkout. Para monorepos, configure os caminhos observados também no painel.

## Infraestrutura declarativa com apply

Descreva os recursos de um ou mais ambientes num YAML, com um documento por recurso:

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
pierctl apply -f infra.yaml --diff              # mostra o que mudaria
pierctl apply -f infra.yaml --deploy --wait     # aplica e implanta
```

O `apply` cria o que falta e atualiza o que mudou. Rodar duas vezes não muda nada na segunda.

### Remover o que saiu do arquivo

Com `--prune`, aplicações, bancos e stacks que existem no Pier mas **não estão no arquivo** também são removidos. Isso vale só nos ambientes que o arquivo menciona. A CLI lista os candidatos e só remove com `--yes`:

```sh
pierctl apply -f infra.yaml --diff --prune      # quem seria removido
pierctl apply -f infra.yaml --prune --yes
```

Num [ambiente protegido](/docs/seguranca/#ambientes-protegidos-e-congelados), confirme o nome do ambiente. Repita a flag para vários ambientes:

```sh
pierctl apply -f infra.yaml --prune --yes --confirm-environment production
```

Variáveis nunca são removidas pelo `--prune`.
