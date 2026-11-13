Atualizar o Pier é trocar a versão do código e rodar o instalador de novo. Os dados ficam onde estão e as migrations do banco rodam sozinhas.

## Versões

O Pier segue o [versionamento semântico](https://semver.org/lang/pt-BR/). Cada versão beta é uma tag `vX.Y.Z` no repositório, com as notas em [Releases]({{repo}}/releases). A versão beta atual é a **{{version}}**.

Enquanto o número começar com `0.`, uma versão nova pode mudar comportamentos. Leia as notas antes de atualizar.

## Antes de atualizar

1. Faça um [backup da instância](/docs/backup/) e guarde fora do servidor.
2. Pare o painel com `docker compose stop pier` e copie o banco do Pier. Não copie um SQLite ativo sem coordenar seu WAL. As migrations não têm volta, então esta cópia é o seu caminho para voltar à versão anterior:

   ```sh
   . ./.env
   sudo cp "$PIER_DATA_DIR/pier.db" "$PIER_DATA_DIR/pier.db.antes-da-atualizacao"
   ```

3. Confira se não há deploys em andamento.

## Atualizar

Na pasta `pier`, no servidor, troque `vX.Y.Z` pela versão nova:

```sh
git fetch --depth 1 origin tag vX.Y.Z
git checkout vX.Y.Z
./install.sh
```

O instalador baixa a imagem GHCR da versão selecionada e recria o container. Se o download falhar, veja [o que fazer](/docs/instalacao/#se-o-download-da-imagem-falhar) ou construa o código localmente com `PIER_BUILD=1 ./install.sh`. O `.env` é mantido. O painel fica fora do ar por alguns segundos, mas as aplicações continuam respondendo.

Confira a versão em execução:

```sh
docker compose exec pier wget -q -O - http://127.0.0.1:8700/api/v1/version
```

## Voltar para a versão anterior

1. Pare o painel: `docker compose stop pier`.
2. Volte o banco copiado antes da atualização:

   ```sh
   . ./.env
   sudo cp "$PIER_DATA_DIR/pier.db.antes-da-atualizacao" "$PIER_DATA_DIR/pier.db"
   sudo rm -f "$PIER_DATA_DIR/pier.db-wal" "$PIER_DATA_DIR/pier.db-shm"
   ```

3. Volte o código e reinstale:

   ```sh
   git checkout vVERSAO-ANTERIOR
   ./install.sh
   ```

O que mudou entre a cópia e a volta, como um deploy novo, fica de fora.

## Notas da versão 0.5.0

Quem vem de uma instalação anterior à 0.5.0 precisa de três passos a mais. O roteamento ficou mais restrito: o proxy só encaminha para containers marcados pelo Pier com o token do servidor.

1. **Rede do painel.** O instalador cria a rede `pier-panel` sozinho. Se você subir o Pier sem o instalador, crie a rede antes:

   ```sh
   docker network inspect pier-panel >/dev/null 2>&1 || docker network create pier-panel
   ```

2. **Proxy.** Em **Infraestrutura › servidor local › Proxy**, recrie o proxy. Faça o mesmo nos outros servidores.
3. **Recursos com domínio.** Implante de novo cada aplicação e stack que tem domínio. Até o novo deploy, elas deixam de ser roteadas.

Outras novidades da 0.5.0: comando de release, segredos de build, promoção entre ambientes, deploy por tag, processos e réplicas, `pier.yaml`, `pierctl up`, restauração e teste de backups, backup da instância, alertas de execução e uptime, auditoria e ambientes protegidos.

## Atualizar a pierctl

Baixe o binário da versão nova, como na [instalação da pierctl](/docs/pierctl/#instalar), e substitua o antigo. A CLI e o servidor falam a mesma API (v1). Mantenha os dois na mesma versão para ter todos os comandos.
