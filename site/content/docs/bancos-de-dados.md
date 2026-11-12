O Pier sobe um banco de dados em segundos, com senha forte gerada, dados em volume persistente e backup agendado. Você não escreve compose nem decora variáveis de cada imagem.

## Engines disponíveis

| Engine | Backup e restauração pelo Pier |
|---|---|
| PostgreSQL | Sim |
| MySQL | Sim |
| MariaDB | Sim |
| MongoDB | Sim |
| Redis | Não |
| KeyDB | Não |
| Dragonfly | Não |
| ClickHouse | Não |

Para as engines sem backup pelo Pier, use a persistência própria delas e faça cópia do volume por fora.

## Criar um banco

Em um projeto, clique em **Novo recurso**, escolha **Banco de dados** e depois a engine. Dê um nome e confirme. O Pier gera:

- o usuário `pier`;
- o banco `app`;
- uma senha de 32 caracteres, guardada cifrada.

## Conectar

- **De outro recurso do mesmo servidor:** use a URL interna mostrada na página do banco. Melhor ainda, use uma [referência](/docs/aplicacoes/#referencias-a-bancos) como `${{principal.url}}`. Ela acompanha a senha e o endereço atuais.
- **De fora do servidor:** defina uma **porta pública** (1024 a 65535). O banco passa a ser publicado no host e a página mostra a URL externa. Sem porta pública, nada fica exposto.

> Abrir um banco para a internet aumenta o risco. Prefira um túnel SSH (`ssh -L 5432:127.0.0.1:PORTA usuario@servidor`) ou libere a porta no firewall só para o seu IP.

Mudanças de imagem, porta e limites valem quando o banco reinicia. Usuário e senha não mudam depois de criados, porque o volume é inicializado com eles.

## Backups

Na página do banco:

- **Fazer backup agora** gera um dump em segundo plano. O tamanho, ou o erro, aparece na lista.
- **Agendamento:** uma expressão cron, como `0 3 * * *` para todo dia às 3h, e quantos backups manter. Os mais antigos são apagados.
- **Download:** qualquer backup concluído pode ser baixado pelo navegador.

Os arquivos ficam no próprio servidor do banco. Se o servidor se perder, os backups vão junto. Por isso, ligue a cópia externa.

### Cópia para S3

Configure um destino S3 compatível, com HTTPS: AWS S3, Cloudflare R2, Backblaze B2, MinIO, Wasabi e outros. Depois ligue a cópia em cada banco. Cada backup concluído é enviado ao destino. Quando a retenção apaga um backup, o objeto remoto também é apagado. As credenciais ficam cifradas e nunca aparecem em logs.

### Teste de restauração

Um backup que nunca foi restaurado é só uma esperança. Com o teste ligado, por padrão todo domingo às 4h, o Pier restaura o último backup num container descartável e faz uma consulta simples. Se falhar, você recebe um alerta.

## Restaurar

Escolha um backup concluído e clique em **Restaurar**. Você digita o nome do banco para confirmar e decide o destino:

- **No mesmo banco:** sobrescreve os dados atuais.
- **Num banco novo:** cria outro banco com o conteúdo do backup e credenciais novas. É o jeito seguro de conferir os dados antes de trocar.

Durante a restauração, o banco fica desconectado da rede interna e volta ao fim, mesmo se a restauração falhar. Pause antes as aplicações que escrevem nele.

## Remover

Remover um banco pergunta o que fazer com os dados. A mesma escolha vale para o volume **e** para os arquivos de backup:

- **Marcado:** apaga os dois.
- **Desmarcado:** mantém os dois no servidor.

Com um backup ou uma restauração em andamento, a remoção é recusada. Num [ambiente protegido](/docs/seguranca/#ambientes-protegidos-e-congelados), é preciso digitar também o nome do ambiente.
