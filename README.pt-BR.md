[English](README.md) | Português

# Pier

[![Licença: Apache-2.0 + Commons Clause](https://img.shields.io/badge/licen%C3%A7a-Apache--2.0%20%2B%20Commons%20Clause-blue.svg)](LICENSE) [![CI](https://github.com/AllanAdegas/pier/actions/workflows/ci.yml/badge.svg)](https://github.com/AllanAdegas/pier/actions/workflows/ci.yml)

Painel self-hosted para subir projetos em servidores próprios: aplicações, bancos de dados e serviços compose, com domínio, HTTPS e deploy com verificação de prontidão e compensação de falhas. Escrito em Go e SolidJS.

Três níveis sobre a mesma infraestrutura — use o que o projeto pedir:

| Nível | O que se opera | Onde |
|---|---|---|
| Plataforma | Aplicações, bancos e stacks: "suba meu projeto" — com um catálogo de 362 serviços | Projetos |
| Docker puro | Todo container, imagem, volume e rede de um servidor, criado ou não pelo Pier | Infraestrutura › servidor › Docker |
| Kubernetes (opcional) | Clusters importados por kubeconfig ou um K3s de nó único criado pelo Pier | Infraestrutura › Clusters |

Os dois últimos têm o [Rancher](https://github.com/rancher/rancher) como referência. Para automação há a CLI `pierctl`, em [AllanAdegas/pier-cli](https://github.com/AllanAdegas/pier-cli).

### Monitoramento

Em **Infraestrutura › Monitoramento** (`/monitoring`), acompanhe cada servidor: CPU, RAM utilizada/disponível, disco do diretório de dados, swap, tempo ativo, carga média e containers. Clusters Kubernetes têm um resumo separado de nós, pods saudáveis e avisos. As leituras se atualizam a cada 15 segundos com a página visível; também há atualização manual.

O consumo do host requer Linux com `/proc` acessível. Falta de suporte ou falha de coleta aparece como indisponível, nunca como consumo zero. O painel mostra snapshots e histórico persistente. Aplicações com vários destinos têm totais de CPU e memória somente quando a coleta está completa; leituras parciais aparecem como lacunas. Portas fixas no host exigem interrupção durante a troca de versão.

> Em beta (0.x). Em produção numa VPS Linux desde 11/11/2026: painel, proxy e cinco stacks validados. Instalação limpa, backup/restauração, deploy/rollback, segredos de build, promoção e alertas de uptime também exercitados em Docker local Linux/arm64. Essas verificações não cobrem todos os fluxos nem todo o catálogo.

## Instalar com Docker

Só precisa do Docker com o plugin compose:

```sh
git clone --depth 1 https://github.com/AllanAdegas/pier.git
cd pier
./install.sh
```

O script:
- cria o `.env`, com os dados em `/data/pier` no Linux e em `~/.pier` no macOS;
- baixa a imagem publicada `ghcr.io/allanadegas/pier:<versão>` (amd64 e arm64), na versão do código clonado (a última release, se você clonou a `main`);
- sobe o container;
- espera o Pier responder;
- mostra o endereço e o **código de configuração** da primeira conta.

Para construir a imagem na própria máquina em vez de baixá-la: `PIER_BUILD=1 ./install.sh`. Para outra porta: `PIER_PORT=8790 ./install.sh`. Para parar: `docker compose down`; o que o Pier implantou continua rodando. O instalador fala português ou inglês: segue `LC_ALL`/`LC_MESSAGES`/`LANG`, ou force com `PIER_LANG=pt` ou `PIER_LANG=en`.

### Atualizar

O painel mostra um aviso quando existe uma versão nova, com os comandos. Eles são sempre estes (na pasta onde o Pier foi instalado; os dados são mantidos):

```sh
git fetch --depth 1 origin tag vX.Y.Z
git checkout vX.Y.Z
./install.sh
```

Não há atualização automática de dentro do painel, de propósito: para se reiniciar, o Pier precisaria controlar o próprio container pelo socket do Docker, que equivale a root no servidor. Um painel que se substitui sozinho por uma imagem baixada da rede ampliaria muito o estrago de uma conta ou de um release comprometido. Atualizar é um comando, executado por quem administra a máquina.

A verificação consulta a API pública de releases do GitHub no máximo a cada 12 horas, sem enviar nenhum dado da instância (User-Agent genérico `pier-update-check`). Para desligá-la: `PIER_UPDATE_CHECK=0` no `.env`.

## Rodar a partir do código

```sh
make run          # compila a interface e o binário, inicia em http://localhost:8700
```

Na primeira visita o painel pede para criar a conta de administrador, com o **código de configuração** que o Pier escreve no log ao iniciar (`setup_code=XXXX-XXXX-XXXX`; no Docker, `docker logs pier`). Sem o código ninguém reivindica a instância, mesmo que ela esteja exposta.

O binário escuta só em `127.0.0.1:8700`. Para acessá-lo de outra máquina, ponha um proxy com HTTPS na frente ou defina `PIER_ADDR=:8700` sabendo que o painel fica exposto.

### Rodar no seu computador ou no servidor

O Pier opera servidores por SSH, então pode rodar no seu computador e gerenciar uma VPS sem instalar nada nela além do Docker. Nesse modo:

- webhooks de push não chegam, porque o computador não tem endereço público;
- agendamentos, backups, monitor e notificações só rodam com o Pier aberto;
- fechar o Pier no meio de um deploy interrompe a orquestração;
- o estado fica nessa máquina: faça backup de `PIER_DATA_DIR`.

Para time, automação e deploy por push, rode o Pier no servidor. Cada servidor deve ter **um** Pier dono.

## IA e agentes (MCP)

O Pier expõe um servidor [MCP](https://modelcontextprotocol.io) em `/mcp`. Agentes como Claude Code, Claude Desktop e Cursor podem listar recursos, ler logs, implantar, reiniciar e criar stacks do catálogo. **Nenhuma ferramenta remove nada.**

- **Autenticação:** só por token de API (`Authorization: Bearer …`), nunca pela sessão do painel, e com as mesmas permissões `read`/`deploy`/`write`. A tela **Segurança › IA e agentes (MCP)** cria um token dedicado e gera a configuração.
- **Claude Code:** `claude mcp add --transport http pier http://127.0.0.1:8700/mcp --header "Authorization: Bearer <token>"`
- **Clientes só com stdio:** `PIER_URL=http://127.0.0.1:8700 PIER_TOKEN=<token> pier mcp`, ou `pierctl mcp` com o contexto da CLI.
- **Cuidado:** logs de deploy e de containers chegam ao agente e podem conter instruções de terceiros. Dê ao token só o que a IA precisa.

Requisitos para compilar: Go 1.27.4+ e Deno 2.x. Para gerenciar a própria máquina: Docker com o plugin compose.

O proxy de cada servidor usa as portas 80 e 443 por padrão; em uma máquina de desenvolvimento, troque-as em Infraestrutura › servidor › Proxy antes de iniciá-lo.

Atualizar é trocar o binário e reiniciar: as migrations do banco rodam sozinhas. Não há migration de volta — copie `pier.db` antes se quiser poder retornar à versão anterior.

## Stacks: três formas de subir um compose

Em um projeto, **Novo recurso** oferece:

- **Serviço do catálogo** — 359 serviços prontos, com busca e categorias. Senhas e chaves são geradas; o formulário pede o domínio e o que o serviço exigir. Todos são validados com `docker compose config`, mas só os marcados como verificados foram implantados de verdade: teste o que for usar.
- **Compose próprio** — cole um `docker-compose.yaml`. Variáveis `PIER_PASSWORD_X`, `PIER_USER_X`, `PIER_SECRET_X`, `PIER_HEX_X` e `PIER_BASE64_X` são geradas pelo Pier; `PIER_URL_<SERVIÇO>` e `PIER_HOST_<SERVIÇO>` recebem o endereço de cada serviço com domínio.
- **Pasta do servidor** — aponte uma pasta da máquina que já tem o compose. O Pier roda o arquivo de lá: o `.env`, os `env_file` e os arquivos de `secrets` são usados **onde estão**, sem cópia para o painel, e caminhos relativos continuam valendo. Edite o compose na pasta e implante de novo. Se o projeto já roda à mão, informe o nome do projeto compose para o Pier assumir os mesmos volumes.

Cada serviço é um arquivo em `internal/stack/templates/`. Depois de editar ou acrescentar um:

```sh
PIER_COMPOSE_CHECK=1 go test ./internal/stack -run AcceptedByDockerCompose   # valida tudo com o Docker
```

## Kubernetes, só se precisar

Nada no fluxo de aplicações depende de um cluster. Quando precisar, em Infraestrutura › Clusters:

- **Importar** um cluster existente colando o kubeconfig. As credenciais precisam estar embutidas (token ou certificado); um kubeconfig que depende de `gcloud`, `aws` ou outro plugin é recusado — crie uma ServiceAccount e use o token dela.
- **Criar um K3s** de nó único em um servidor. Ele roda como container **privilegiado** e publica a API e a porta HTTP dos Ingress em todas as interfaces: indicado para desenvolvimento e cargas pequenas, não para um servidor compartilhado.

O Pier nunca lê `~/.kube/config`: só usa o kubeconfig que você colar ou o que ele mesmo gerar.

## Permissões

Todo usuário é administrador. Um token de API pode ser `read`, `deploy` ou `write`; **`write` equivale a root nos servidores** (Docker puro, compose próprio, K3s privilegiado) e a tudo o que um kubeconfig importado permite.

## Contribuir

O Pier tem código-fonte disponível ([licença](#licença)) e é feito para seguir sem um mantenedor dedicado: o CI valida, os releases saem sozinhos e as dependências se atualizam por automação. Leia o [CONTRIBUTING.pt-BR.md](CONTRIBUTING.pt-BR.md), o [GOVERNANCE.pt-BR.md](GOVERNANCE.pt-BR.md) e o [Código de Conduta](CODE_OF_CONDUCT.pt-BR.md). Vulnerabilidades: [SECURITY.pt-BR.md](SECURITY.pt-BR.md).

## Desenvolver

```sh
make dev-api      # backend em 127.0.0.1:8700 (dados em ./.data)
make dev-web      # interface com recarga em :5173
make test         # go vet, testes Go e verificação de tipos da interface
```

## Configuração

| Variável | Padrão | Para quê |
|---|---|---|
| `PIER_ADDR` | `127.0.0.1:8700` | Endereço de escuta (a imagem Docker usa `:8700` e o compose publica em `127.0.0.1`) |
| `PIER_DATA_DIR` | `./data` | Banco SQLite, chave e arquivos do servidor local |
| `PIER_SECRET_KEY` | gerada em `<data>/secret.key` | Deriva a chave que cifra os segredos (SHA-256). Se definir a sua, use um valor aleatório longo, como `openssl rand -hex 32`: uma frase curta ou previsível enfraquece a cifra |
| `PIER_DEPLOY_CONCURRENCY` | `2` | Deploys simultâneos |
| `PIER_DEV` | — | `1` liga o log de depuração |
| `PIER_UPDATE_CHECK` | `1` | `0` desliga a consulta de novas versões (e qualquer requisição de saída do aviso) |
| `PIER_VERSION` | versão do código clonado | Só no Docker: versão da imagem `ghcr.io/allanadegas/pier`; o `install.sh` preenche |
| `PIER_LANG` | de `LC_ALL`/`LC_MESSAGES`/`LANG` | Só do `install.sh`: `pt` ou `en` força o idioma do instalador |

## Estrutura

```
cmd/pier/        ponto de entrada e ligação das dependências
internal/        um pacote por contexto (application, database, stack, server,
                 engine = Docker puro, cluster + kube = Kubernetes,
                 mcp = servidor MCP sobre a API, …)
web/             interface SolidJS
```

## Licença

Código-fonte disponível: [Apache License 2.0 com a Commons Clause](LICENSE). Use à vontade, inclusive comercialmente dentro da sua empresa; **vender** o Pier ou um produto ou serviço baseado nele, mesmo com poucas alterações, exige [licença comercial](COMMERCIAL.pt-BR.md). Versões até a 0.9.0 continuam sob Apache-2.0 pura. Avisos de terceiros em [NOTICE](NOTICE) e [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).

## Documentação

O guia de uso fica em [`site/`](site): o site de lançamento, com a documentação escrita para quem vai instalar e operar o Pier. `cd site && deno task serve` abre em `http://localhost:8080`. Versão publicada: [allanadegas.github.io/pier](https://allanadegas.github.io/pier/) (instalação: [guia de instalação](https://allanadegas.github.io/pier/docs/instalacao/); primeiros passos: [guia](https://allanadegas.github.io/pier/docs/primeiros-passos/)).

### Atualização de segurança: rede do painel

O painel usa exclusivamente a rede Docker `pier-panel`. O proxy gerenciado se
conecta a essa rede e às redes das aplicações. A publicação do painel vem do
provider de arquivos do Traefik: ao iniciar com `PIER_DOMAIN`, o Pier escreve
`<diretório do servidor local>/proxy/dynamic/pier-panel.yaml` com permissão 0600.
Esse arquivo aponta para `http://pier-panel:8700` e configura HTTPS/ACME; nenhuma
capacidade de roteamento ou token precisa entrar no compose. `PIER_DOMAIN` deve
ser apenas o domínio. Sem essa variável, o Pier remove sua rota de painel.

Ao atualizar uma instalação anterior, crie a rede (o `install.sh` faz isso):

```sh
docker network inspect pier-panel >/dev/null 2>&1 || docker network create pier-panel
docker compose -f compose.yaml -f compose.proxy.yaml up --detach --build --force-recreate
```

Depois, recrie também o proxy em Infraestrutura → servidor local → Proxy. O painel
e o proxy precisam ser recriados para abandonar a antiga rede compartilhada
`pier`. Aplicações e stacks existentes devem ser reimplantadas para receber os
labels que o novo filtro do proxy exige. Configure a URL pública HTTPS em
Configurações; isso ativa HSTS. O override HTTPS força cookies Secure. Fora dele,
`PIER_COOKIE_SECURE=1` permite a mesma proteção atrás de outro terminador TLS.

Os limites de build também atravessam o compose: `PIER_BUILD_CPUS` (padrão 1),
`PIER_BUILD_MEMORY` (2g), `PIER_BUILD_MIN_FREE_GB` (2) e `PIER_BUILD_CACHE_GB` (10).
Builds de código exigem Docker Buildx.

### Backup e restauração da instância

Em Configurações → Backup da instância, baixe um arquivo `.pierbak` criptografado
ou configure uma pasta local, agenda cron e retenção. A frase-senha tem pelo
menos 16 caracteres; guarde-a fora do servidor. O backup inclui um snapshot
consistente de `pier.db` e a chave efetiva das credenciais, inclusive quando a
chave vem de `PIER_SECRET_KEY`. A frase-senha da agenda fica cifrada no banco.
Os dumps de bancos, volumes, arquivos de aplicações e certificados dos proxies
precisam de backups próprios. Copie os backups locais para outro servidor.

Pare o Pier e restaure com o binário, usando o diretório de dados do host:

```sh
pier restore --file /backup/instance.pierbak --passphrase-file /seguro/frase-senha --data-dir /data/pier
```

O comando recusa um diretório bloqueado pelo Pier, verifica a autenticação do
arquivo e a integridade SQLite antes de substituir arquivos. Os arquivos antigos
recebem o sufixo `.before-restore-*`. Preserve essas cópias até verificar a
restauração. Ao restaurar sem `PIER_SECRET_KEY`, a chave restaurada é usada;
remova um override antigo ou mantenha exatamente a chave que originou o backup.
Pare também versões antigas do Pier que ainda não usam o arquivo `pier.lock`.

O checklist do Painel sugere `https://github.com/heroku/node-js-getting-started`
para o primeiro deploy com Nixpacks: aplicação Node.js/Express com `npm start`,
`PORT` e escuta em todas as interfaces. Use a porta interna 3000 e configure
`PORT=3000`. O checklist pode ser dispensado por item; o aviso de backup na
moldura permanece até existir um backup da instância.
