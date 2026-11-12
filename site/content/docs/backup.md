Há dois tipos de backup, e você precisa dos dois:

- **Backup da instância:** o próprio Pier. Guarda contas, projetos, configurações, senhas cifradas e a chave que abre essas senhas.
- **Backup dos dados:** o que as suas aplicações guardam, como bancos e volumes.

## Backup da instância

Em **Configurações › Backup da instância**:

- **Baixar backup criptografado:** gera um arquivo `.pierbak`. Você escolhe uma frase-senha de pelo menos 16 caracteres.
- **Backup automático local:** escolha a pasta de destino no servidor, um horário em cron (por exemplo `0 2 * * *`) e quantos backups manter.

O arquivo contém uma cópia consistente do banco do Pier (`pier.db`) e a chave das credenciais. Ele é cifrado com AES-256-GCM, com chave derivada da frase-senha.

> **Guarde a frase-senha fora do servidor**, num gerenciador de senhas. Sem ela, o backup não abre. E copie os arquivos `.pierbak` para outra máquina: um backup no mesmo disco some junto com ele.

Pela CLI, o mesmo backup:

```sh
pierctl instance backup --out pier-$(date +%F).pierbak --passphrase-file ./frase-senha.txt
pierctl instance status        # quando foi o último backup
```

Até o primeiro backup existir, o painel mostra um aviso no topo.

### O que não entra

O backup da instância **não** inclui:

- os dados dos bancos, que têm backup próprio, descrito abaixo;
- os volumes das aplicações e das stacks;
- os certificados emitidos, que são pedidos de novo quando faltam.

## Restaurar a instância

A restauração é feita com o Pier **parado**, para não haver duas cópias escrevendo no mesmo banco. No servidor, na pasta `pier`:

1. Pare o painel:

   ```sh
   docker compose stop pier
   ```

2. Coloque o arquivo e a frase-senha numa pasta, por exemplo `/root/restauracao`. A frase-senha vai num arquivo de texto:

   ```sh
   sudo install -d -m 700 /root/restauracao
   sudo cp pier-2026-11-11.pierbak /root/restauracao/instancia.pierbak
   printf '%s' 'sua frase-senha' | sudo tee /root/restauracao/frase > /dev/null
   ```

3. Rode a restauração com a mesma imagem do Pier. O comando usa o diretório de dados do `.env`:

   ```sh
   . ./.env
   docker compose run --rm --no-deps \
     -v /root/restauracao:/restauracao:ro \
     pier restore --file /restauracao/instancia.pierbak \
       --passphrase-file /restauracao/frase --data-dir "$PIER_DATA_DIR"
   ```

4. Suba o painel e entre com a sua conta de sempre:

   ```sh
   docker compose up -d pier
   ```

Antes de trocar qualquer arquivo, o comando confere a frase-senha, a autenticidade do arquivo e a integridade do banco. Os arquivos antigos não são apagados: recebem o sufixo `.before-restore-…`. Guarde-os até confirmar que está tudo certo, e depois apague a pasta `/root/restauracao`.

Se o painel ainda estiver rodando, a restauração é recusada.

> Se você define `PIER_SECRET_KEY` no `.env`, ela precisa ser **exatamente** a da instância que gerou o backup. Sem `PIER_SECRET_KEY`, vale a chave que veio no backup.

### Mudar de servidor

Para levar o Pier para outra máquina:

1. Instale o Pier no servidor novo, sem criar conta.
2. Restaure o backup como acima.
3. Aponte o DNS para o novo IP.

As aplicações precisam ser implantadas de novo no servidor novo. Os dados dos bancos voltam pelos backups deles.

## Backup dos bancos

Cada banco PostgreSQL, MySQL, MariaDB ou MongoDB tem backup agendado, cópia para S3, teste de restauração semanal e restauração com um clique. Veja [Bancos de dados](/docs/bancos-de-dados/#backups).

## Volumes das aplicações

O Pier ainda não faz backup de volumes. Se uma aplicação guarda arquivos num volume, como uploads, copie a pasta do volume com uma ferramenta própria. Duas opções: uma tarefa agendada que envia os arquivos para um armazenamento externo, ou um serviço do catálogo de backup.

## Uma rotina que funciona

1. Backup da instância agendado todo dia, com cópia diária para fora do servidor.
2. Backup de cada banco agendado, com cópia para S3 e teste de restauração ligado.
3. Alertas de "backup não executado" chegando num canal que você lê.
4. Uma vez por trimestre, restaure de verdade num servidor de teste.
