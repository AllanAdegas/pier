Uma aplicação é um programa seu rodando num container: um site, uma API, um worker. O Pier constrói, publica e atualiza a aplicação sem derrubar a versão que está no ar.

## De onde vem o código

| Origem | Quando usar |
|---|---|
| **Aplicação do Git** | O código está num repositório. O Pier clona e constrói a cada deploy |
| **Imagem Docker** | Você já publica uma imagem num registry, como Docker Hub ou GHCR |
| **Pasta local** | Enviar a pasta do seu computador com `pierctl up`, sem passar pelo Git. Veja [pierctl](/docs/pierctl/#enviar-a-pasta-local) |
| **Promoção** | Levar para produção a mesma imagem que foi testada em staging |

Repositórios públicos usam o endereço `https://`. Para os privados há duas opções:

- **Deploy key (recomendado):** endereço `git@host:org/repo.git` e uma chave SSH gerada em **Segurança**.
- **Token HTTPS:** o token nunca fica no contexto de build.

## Como a imagem é construída

| Build pack | O que faz |
|---|---|
| **Dockerfile** | Usa o `Dockerfile` do repositório. Você pode indicar outro caminho e um diretório base |
| **Nixpacks** | Detecta a linguagem e gera a imagem sozinho. Exige o [Nixpacks](https://nixpacks.com) instalado no servidor |
| **Site estático** | Serve os arquivos com nginx na porta 80. Com **SPA** ligado, rotas desconhecidas devolvem o `index.html` |
| **Imagem Docker** | Sem build: só baixa a imagem |

Builds de código exigem o Docker Buildx. Cada servidor faz um build por vez, com limite de CPU e memória. Os limites ficam no `.env` do Pier e estão descritos na [instalação](/docs/instalacao/#configuracoes-do-env).

## Como um deploy acontece

1. **Implantar** coloca o deploy na fila. Se já houver um esperando, o Pier reaproveita esse.
2. O código é clonado e a imagem é construída.
3. O **comando de release** roda, se houver.
4. A nova versão sobe **ao lado** da atual.
5. Quando a nova fica pronta, o proxy passa a mandar o tráfego para ela, e a anterior é parada com calma.

A nova versão conta como pronta assim:

- **com health check:** quando o Docker a marca como saudável;
- **sem health check:** quando passa 6 segundos rodando sem reiniciar.

Se ela não ficar pronta, é descartada e **a anterior continua no ar**. O log mostra o motivo e, quando reconhece o erro, uma explicação com a correção. Exemplos: falta de memória, script de início ausente, disco cheio, porta errada.

Deploys da mesma aplicação nunca rodam em paralelo. De aplicações diferentes, rodam até `PIER_DEPLOY_CONCURRENCY` ao mesmo tempo (padrão 2).

## Porta, domínio e rede

- **Porta da aplicação:** a porta em que o programa escuta **dentro** do container. O Pier sugere a porta declarada na imagem. Depois do deploy, ele avisa se o programa escuta em outra porta ou só em `127.0.0.1`.
- **Domínios:** um ou vários, com `https://` para ter certificado automático. Antes de pedir o certificado, o Pier confere se o DNS aponta para o servidor. Se não aponta, publica em HTTP e diz qual registro criar.
- **Portas publicadas no host:** só quando algo de fora precisa acessar uma porta direta, como um servidor de jogo. Com elas, o deploy tem uma pequena indisponibilidade, porque duas versões não podem ocupar a mesma porta.
- **Rede interna:** recursos do mesmo servidor se enxergam pelo nome. Uma aplicação sem domínio só é acessível por eles.

## Variáveis de ambiente

Na aba **Variáveis**, cada variável pode ser:

| Tipo | Disponível | Observação |
|---|---|---|
| Runtime (padrão) | Na aplicação rodando | Os valores aparecem mascarados no log de deploy |
| Build | Durante o build, como `--build-arg` | Fica visível no histórico da imagem. Não use para segredos |
| Segredo de build | Durante o build, como *secret* do BuildKit, e também na aplicação rodando | Não fica nas camadas da imagem. No Dockerfile: `RUN --mount=type=secret,id=NOME`. Não copie o valor para um arquivo da imagem |

Você pode importar um arquivo `.env` inteiro. Mudanças valem no **próximo** deploy: o que está rodando não muda sozinho.

### Referências a bancos

Em vez de copiar a senha do banco, use uma referência. O Pier troca pelo valor atual a cada deploy:

```sh
DATABASE_URL=${{principal.url}}
DB_HOST=${{principal.host}}
DB_PORT=${{principal.port}}
DB_USER=${{principal.user}}
DB_PASSWORD=${{principal.password}}
DB_NAME=${{principal.database}}
```

`principal` é o nome (ou o id) do banco. O botão **Conectar a um banco** monta a referência para você.

## Health check, limites e volumes

- **Health check:** um caminho HTTP, como `/healthz`, ou um comando próprio. Vem desligado porque muitas imagens não têm `curl` nem `wget`. Ligue para ter deploys mais seguros.
- **Limites:** CPU (`0.5` = meio núcleo) e memória (`512m`, `1g`). Sem limite, a aplicação pode usar a máquina inteira.
- **Volumes:** pastas que sobrevivem aos deploys, para uploads e arquivos gerados. Os volumes não entram no backup automático. Veja [Backup](/docs/backup/).

## Comando de release

Um comando que roda **na imagem nova, antes dela receber tráfego**. É o lugar das migrations do banco:

```sh
npm run migrate
```

Se o comando falhar, o deploy para e a versão anterior continua no ar. Ele roda também no primeiro deploy.

Também há comandos de pré-deploy, que rodam no container que já está no ar, e de pós-deploy. Use-os para tarefas como avisar outro sistema ou limpar um cache.

## Processos e réplicas

O mesmo repositório pode rodar mais de um processo:

- `web`, que recebe o domínio;
- `worker`, sem domínio, por exemplo `bundle exec sidekiq` ou `node worker.js`.

Cada processo tem um comando, até 10 réplicas e limites próprios. Num deploy, **todas** as réplicas novas precisam ficar prontas antes de a versão anterior sair. As réplicas rodam no mesmo servidor.

## Rollback, cancelamento e logs

- **Rollback:** na aba **Deploys**, escolha um deploy concluído e relance. Não há build, e a imagem antiga volta em segundos, se ainda existir no servidor.
- **Cancelar:** um deploy na fila não começa; um em andamento é interrompido, e o que estava no ar continua.
- **Logs:** a aba **Logs** mostra o container ao vivo. Ao trocar de versão, as últimas 1.000 linhas da versão anterior ficam guardadas, com segredos mascarados.

## Deploy automático

Na aba **Automação**:

- **Webhook de push** para GitHub, GitLab e Gitea ou Forgejo. Dispara só para a branch configurada.
- **Deploy por tag:** com um padrão como `v*`, só tags que casam disparam deploy. Bom para publicar apenas versões marcadas.
- **Caminhos observados (monorepo):** com `apps/api/**`, um push só implanta se mudar algo nessa pasta.
- **Tarefas agendadas:** comandos que rodam no container em horários definidos por cron. Exemplo: `0 3 * * *` para todo dia às 3h.

## Promover para produção

**Promover** leva a imagem exata, identificada pelo digest, que está rodando numa aplicação, por exemplo `api-staging`, para outra, como `api`. A imagem não é reconstruída: produção recebe o mesmo artefato testado, com as **variáveis da aplicação de destino**.

A promoção funciona entre aplicações do mesmo servidor e respeita o congelamento do ambiente de destino.

```sh
pierctl promote api-staging --to api --wait
```

## Configuração no repositório

Um arquivo `pier.yaml` na raiz do código pode definir porta, build, health check, comando de release, limites e processos. Ele vale para cada deploy. Veja [pier.yaml e apply](/docs/pier-yaml/).
