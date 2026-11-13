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

## Idioma

A `pierctl` fala português e inglês: ajuda, tabelas, mensagens, confirmações e erros. A escolha segue esta ordem:

1. `--lang pt` ou `--lang en`;
2. a variável `PIERCTL_LANG`;
3. `LC_ALL`, depois `LC_MESSAGES`, depois `LANG`: um valor que começa com `pt` (como `pt_BR.UTF-8`) é português, qualquer outro é inglês;
4. sem nada definido, inglês.

```sh
export PIERCTL_LANG=pt     # sempre em português
pierctl --lang en status   # só este comando em inglês
```

A CLI envia o idioma ao Pier (`Accept-Language`), então os erros que vêm do servidor chegam no mesmo idioma. Em `-o json` e `-o yaml` os nomes dos campos não mudam com o idioma, nem os valores do campo `action` do `apply` (`criado`, `atualizado`, `sem mudanças`, `removido`), que scripts leem. Só o marcador de segredo oculto segue o idioma (`[oculto]` / `[hidden]`).

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
| Nomes | Comandos tradicionais resolvem id ou nome. Os grupos novos abaixo exigem IDs explícitos; confira o help. Um nome ambíguo é erro |

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

Use um token com capacidade `deploy` e papel compatível no projeto. Ele não deve ler segredos nem remover recursos principais; remoção de previews temporários faz parte da capacidade de deploy.


## Grupos novos da linha 0.6

Esses comandos exigem uma versão da CLI e do servidor que os inclua. Os argumentos de recurso abaixo são **IDs**, não nomes. Use `--help` no grupo para ver payloads e opções. Os grupos novos usam JSON como saída padrão. Operações assíncronas retornam o estado inicial; consulte o status antes de considerar sucesso e não presuma que aceitam `--wait`.

| Grupo | Para quê |
|---|---|
| `preview` | Listar, configurar, reimplantar/remover previews e manter variáveis exclusivas |
| `placement` e `registry` | Consultar/configurar destinos e cadastrar/verificar credenciais de registry |
| `git-stack` e `git-provider` | Fonte Git, webhooks, vínculo de repositório e retorno dos deploys |
| `volume-backup` | Volumes elegíveis, políticas, backups, download e restauração |
| `metrics-history` | Consultar séries por servidor, aplicação, banco ou stack |
| `server-operations` | Política de manutenção, simulação/execução de limpeza e instalação Docker |
| `log-drain` | Configurar e testar encaminhamento externo de logs |
| `access` | Consultar permissões efetivas e membros autorizados |
| `shared-var` | Variáveis compartilhadas de projeto/ambiente e aplicações dependentes |
| `clone-resource` | Copiar definição de aplicação, banco ou stack para outro ambiente |
| `auto-domain` | Consultar/gerar domínio automático e configurar sua política |
| `cloud` | Consultar provedores/máquinas e criar máquinas; criação exige `--yes` e gera cobrança |

Exemplos de leitura e operação com IDs:

```sh
pierctl preview ls APP_ID
pierctl placement servers APP_ID
pierctl registry ls
pierctl volume-backup app-ls APP_ID
pierctl metrics-history application APP_ID --range 1h
pierctl access me
```

Mutações desses grupos recebem JSON com `-f arquivo.json` ou `-f -` pela entrada padrão. Não coloque credenciais em argumentos de shell. Arquivos de entrada precisam de acesso restrito e remoção após o uso. Valores sensíveis ficam ocultos na saída por padrão; `--show-secrets` é uma escolha explícita que pode vazar no log do CI.

Remoções e restaurações exigem `--yes`, além da confirmação por nome que a API exigir no JSON. `volume-backup download BACKUP_ID -O arquivo.tar.gz` cria um arquivo privado novo e não sobrescreve arquivo existente. Proteja os dumps baixados: o arquivo de volumes não é cifrado como `.pierbak`.

### O que exige o painel

A CLI usa token de API e não envia cookies de sessão. Segurança da conta (2FA, códigos de recuperação e sessões), convites, alteração de papéis e SSO exigem sessão no painel. `pierctl account` explica essa distinção; não converte um token de escrita em sessão de navegador. A recuperação offline usa `pier admin`, não `pierctl`.

Terminal interativo do navegador e fluxos de credenciais de nuvem que exigem sessão também não devem ser contornados por comandos de token. Veja [conta](/docs/conta/), [equipe](/docs/equipe/) e [operações](/docs/operacoes/).
