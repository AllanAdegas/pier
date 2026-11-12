Uma stack é um conjunto de containers descrito por um `docker-compose.yaml`. Use uma stack para ferramentas prontas, como n8n, Plausible, WordPress ou Supabase, ou quando o seu projeto já tem um compose.

## Três formas de criar

Em um projeto, clique em **Novo recurso** e escolha uma das três:

| Opção | Quando usar |
|---|---|
| **Serviço do catálogo** | Mais de 350 serviços prontos. O Pier gera senhas e chaves e pergunta só o necessário, como o domínio |
| **Compose próprio** | Você cola um `docker-compose.yaml` no painel |
| **Pasta do servidor** | O compose já está numa pasta do servidor, com o `.env` e os arquivos de segredo ao lado |

## Catálogo

Busque pelo nome ou navegue por categoria. Cada serviço mostra o que pede: domínio, e-mail, opções.

- Todos os serviços passam pela validação do `docker compose`. Os marcados como **verificados** também foram implantados e conferidos de verdade. Os outros avisam que ainda não foram testados num deploy real: teste antes de usar em produção.
- Alguns serviços pedem poderes especiais sobre o servidor: acesso ao socket do Docker, modo privilegiado, rede ou processos do host, portas do host, pastas do host. O formulário explica cada poder e pede que você confirme antes de criar.
- Serviços que criam a conta de administrador sozinhos recebem uma senha gerada pelo Pier. Quando não há um jeito seguro de fazer isso, o serviço fica privado até você concluir a configuração dele.

## Compose próprio

Cole o compose e defina, para cada serviço que deve ter domínio, a porta e o endereço. O Pier gera valores para variáveis com nomes especiais, para você não deixar senhas no arquivo:

| Variável | O que recebe |
|---|---|
| `PIER_PASSWORD_X` | Uma senha forte |
| `PIER_USER_X` | Um nome de usuário |
| `PIER_SECRET_X` | Um segredo longo |
| `PIER_HEX_X` | Um valor hexadecimal |
| `PIER_BASE64_X` | Um valor em base64 |
| `PIER_URL_<SERVIÇO>` | O endereço público do serviço com domínio |
| `PIER_HOST_<SERVIÇO>` | O host público desse serviço |

Troque `X` por um nome seu. Exemplo: `PIER_PASSWORD_DB` vira uma senha e continua a mesma em todos os deploys.

```yaml
services:
  app:
    image: ghcr.io/acme/app:2.1.0
    environment:
      DATABASE_URL: postgres://app:${PIER_PASSWORD_DB}@db:5432/app
      PUBLIC_URL: ${PIER_URL_APP}
  db:
    image: postgres:17-alpine
    environment:
      POSTGRES_USER: app
      POSTGRES_PASSWORD: ${PIER_PASSWORD_DB}
    volumes:
      - data:/var/lib/postgresql/data
volumes:
  data:
```

Imagens com labels `traefik.*` ou `pier.*` são recusadas. Quem cuida do roteamento é o Pier.

## Pasta do servidor

Aponte uma pasta da máquina, por exemplo `/srv/loja`, que já tenha o compose. O Pier usa os arquivos **onde estão**:

- o `.env`, os `env_file` e os `secrets` não são copiados para o painel;
- caminhos relativos continuam funcionando;
- para mudar algo, edite o compose na pasta e implante de novo.

Se o projeto já rodava à mão, informe o nome do projeto compose. Assim o Pier assume os mesmos volumes, sem perder dados.

> Se o Pier roda num container, a pasta precisa estar montada nele no mesmo caminho. No `compose.yaml` do Pier, acrescente uma linha em `volumes`, como `- /srv/stacks:/srv/stacks`, e rode `./install.sh` de novo.

## Operar uma stack

Na página da stack:

- **Implantar** aplica o compose. Serviços sem mudança não são recriados.
- **Logs** e o estado de cada serviço ficam lado a lado.
- `pierctl stack check <nome>` aponta variáveis obrigatórias que faltam, antes do deploy.
- Os logs dos containers têm rotação automática, para não encher o disco.
