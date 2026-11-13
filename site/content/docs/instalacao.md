Este guia instala a versão beta **{{version}}** num servidor Linux. No fim você terá o painel no ar, com sua conta de administrador e, se quiser, um domínio com HTTPS.

## O que você precisa

| Item | Mínimo | Recomendado |
|---|---|---|
| Servidor | Linux 64 bits (x86_64 ou ARM64) | Ubuntu 24.04 LTS ou Debian 12 |
| Memória | 1 GB | 2 GB ou mais se for construir código no servidor |
| Disco | 20 GB | 40 GB ou mais; o Pier avisa abaixo de 10 GB livres |
| Docker | Docker Engine 24 com o plugin compose 2.20 | Versão suportada pelo projeto, com Buildx |
| Acesso | SSH com um usuário que use `sudo` | — |
| Domínio | Opcional | Um subdomínio para o painel, como `pier.seudominio.com` |

Sozinho, o Pier ocupa pouco: cerca de 16 MB de RAM parado. O que consome memória são os builds e as aplicações que você publicar.

> **Dá para testar no seu computador?** Sim. Com Docker Desktop, os mesmos comandos podem ser usados em ambiente local. No macOS, o padrão é `~/.pier`; em Linux, `/data/pier`. Mas webhooks de push não chegam a um computador sem endereço público, e nada roda com ele desligado. Para uso real, instale num servidor.

## 1. Instale o Docker

Se o servidor ainda não tem Docker, o script oficial instala tudo, inclusive os plugins compose e Buildx:

```sh
curl -fsSL https://get.docker.com | sudo sh
```

Confira se as três peças respondem:

```sh
docker --version
docker compose version
docker buildx version
```

Para usar o Docker sem `sudo`, adicione seu usuário ao grupo `docker` e entre de novo no SSH:

```sh
sudo usermod -aG docker "$USER"
```

> O grupo `docker` dá acesso equivalente ao root na máquina. Só adicione usuários em quem você confia.

## 2. Libere as portas

No firewall do servidor (e no painel do provedor da VPS, se houver), deixe abertas:

| Porta | Para quê |
|---|---|
| 22 | SSH |
| 80 | HTTP e validação dos certificados |
| 443 | HTTPS das suas aplicações e do painel |

**Não abra a porta 8700.** O painel escuta só em `127.0.0.1` e deve ser acessado por um túnel SSH ou, depois, pelo domínio com HTTPS.

No Ubuntu, com o `ufw`:

```sh
sudo ufw allow OpenSSH
sudo ufw allow 80/tcp
sudo ufw allow 443/tcp
sudo ufw enable
```

## 3. Instale o Pier

O Pier guarda os dados em `/data/pier`. Crie a pasta com permissão para o seu usuário:

```sh
sudo mkdir -p /data/pier
sudo chown "$USER" /data/pier
```

Baixe a versão beta e rode o instalador:

```sh
git clone --branch {{tag}} --depth 1 {{repo}}.git
cd pier
./install.sh
```

### Se o download da imagem falhar

O repositório e a imagem do GHCR são públicos: o clone e o `docker pull` funcionam sem login. Se o pull falhar (rede restrita, registro bloqueado), construa a imagem no próprio checkout:

```sh
PIER_BUILD=1 ./install.sh
```

Use essa mesma opção nas reinstalações enquanto depender de build local. Não coloque tokens no `.env` nem na URL do repositório.

O script:

1. confere o Docker;
2. cria o arquivo `.env` com as configurações;
3. baixa `ghcr.io/allanadegas/pier:{{version}}` (não faz build por padrão);
4. sobe o container `pier`;
5. espera o painel responder.

No fim, ele mostra algo assim:

```text
==> Pier no ar: http://127.0.0.1:8700
    Código de configuração: K7QM-2XPA-9DTE
    Use-o para criar a conta de administrador na primeira visita.
```

Guarde o **código de configuração**. Sem ele ninguém consegue criar a primeira conta, nem quem encontrar o painel exposto por engano.

## 4. Abra o painel e crie sua conta

O painel está em `127.0.0.1:8700` **do servidor**. Do seu computador, abra um túnel SSH:

```sh
ssh -L 8700:127.0.0.1:8700 usuario@IP-DO-SERVIDOR
```

Com o túnel aberto, acesse [http://localhost:8700](http://localhost:8700) no navegador. Informe o código de configuração, seu nome, e-mail e uma senha de pelo menos 10 caracteres. Pronto: você é o administrador.

Se perdeu o código, ele também fica no log:

```sh
docker compose logs pier | grep setup_code
```

## 5. Coloque o painel num domínio com HTTPS

Recomendado assim que a conta existir. Você deixa de depender do túnel, e webhooks e alertas passam a ter um endereço público.

1. **DNS.** No provedor do seu domínio, crie um registro `A` de `pier.seudominio.com` apontando para o IP do servidor. Se o servidor tiver IPv6, crie também o `AAAA`. Se não tiver, não crie.
2. **Proxy.** No painel, abra **Infraestrutura › servidor local › Proxy**. Informe o e-mail para o Let's Encrypt e clique em **Iniciar**.
3. **Domínio do painel.** No servidor, edite o `.env` da pasta `pier` e descomente a linha:

   ```sh
   PIER_DOMAIN=pier.seudominio.com
   ```

   Depois rode o instalador de novo. Ele não apaga nada:

   ```sh
   ./install.sh
   ```

4. **URL pública.** Em **Configurações**, preencha **URL pública do Pier** com `https://pier.seudominio.com`. Isso liga o HSTS e corrige os endereços que o Pier mostra, como os de webhooks.

Em um ou dois minutos, `https://pier.seudominio.com` abre o painel com certificado válido. Se não abrir, veja [Problemas comuns](/docs/problemas-comuns/#o-certificado-nao-e-emitido).

## 6. Siga a lista do painel

A primeira tela do painel traz uma lista do que falta para a instância ficar segura e útil:

- **HTTPS do painel**: o passo 5 acima.
- **Backup da instância**: faça logo e guarde a frase-senha fora do servidor. Veja [Backup e restauração](/docs/backup/).
- **Canal de notificação**: Discord, Slack, Telegram, e-mail ou webhook, para receber os alertas.
- **Primeiro deploy**: siga os [primeiros passos](/docs/primeiros-passos/).

## Configurações do `.env`

O instalador cria o `.env` a partir do `.env.example`. As opções mais usadas:

| Variável | Padrão | Para quê |
|---|---|---|
| `PIER_DATA_DIR` | `/data/pier` | Onde ficam o banco do Pier, a chave e os arquivos do servidor local. Faça backup desta pasta |
| `PIER_BIND` / `PIER_PORT` | `127.0.0.1` / `8700` | Endereço e porta do painel no servidor |
| `PIER_DOMAIN` | vazio | Domínio do painel com HTTPS (passo 5) |
| `PIER_SECRET_KEY` | gerada em `secret.key` | Frase que cifra os segredos guardados. **Perdê-la torna os segredos ilegíveis** |
| `PIER_DEPLOY_CONCURRENCY` | `2` | Deploys simultâneos, de aplicações diferentes |
| `PIER_DEPLOY_TIMEOUT` | `1h` | Tempo máximo de um deploy |
| `PIER_BUILD_CPUS` / `PIER_BUILD_MEMORY` | `1` / `2g` | Limite de CPU e memória de cada build |
| `PIER_BUILD_MIN_FREE_GB` | `2` | Espaço livre mínimo para começar um build |
| `PIER_BUILD_CACHE_GB` | `10` | Tamanho máximo do cache de build |
| `TZ` | `UTC` | Fuso horário das tarefas e dos backups agendados. Exemplo: `America/Sao_Paulo` |

Depois de mudar o `.env`, rode `./install.sh` de novo para aplicar.

## Comandos do dia a dia

Rode na pasta `pier`, no servidor:

```sh
docker compose logs -f pier     # acompanhar o log
docker compose restart pier     # reiniciar o painel
docker compose down             # parar o painel
./install.sh                    # subir de novo ou aplicar mudanças do .env
```

Parar o painel **não** derruba o que ele publicou: aplicações, bancos e o proxy continuam rodando. Só deploys, backups agendados e alertas param até o painel voltar.
