Com o Pier instalado, vamos colocar uma aplicação no ar em poucos minutos: primeiro uma imagem pronta, sem build, e depois o seu código.

## Antes de começar

- O painel abre e você está logado. Veja a [instalação](/docs/instalacao/).
- Para usar um domínio, o **proxy** do servidor precisa estar iniciado em **Infraestrutura › servidor local › Proxy**.
- Para ter HTTPS, crie no DNS um registro `A` do domínio da aplicação apontando para o IP do servidor. Exemplo: `ola.seudominio.com`.

## 1. Crie um projeto

Em **Projetos**, clique em **Novo projeto** e dê um nome em letras minúsculas, como `meu-site`. O projeto já vem com o ambiente `production`.

## 2. Publique uma imagem pronta

Este primeiro deploy usa uma imagem pequena que só responde com informações da requisição. Serve para provar que servidor, proxy e DNS estão certos antes de envolver o seu código.

1. No projeto, clique em **Novo recurso** e escolha **Imagem Docker**.
2. Em **Imagem**, informe `traefik/whoami:v1.11`.
3. Em **Porta da aplicação**, informe `80`. O botão **Sugerir porta da imagem** descobre a porta sozinho.
4. Em **Domínio**, informe `https://ola.seudominio.com`.
5. Clique em **Criar e implantar**.

O painel abre o log do deploy ao vivo. Quando aparecer **Concluído**, abra o domínio: a página mostra o nome do container e os cabeçalhos da requisição.

> Se o DNS ainda não apontar para o servidor, o deploy termina em HTTP e o log diz qual registro criar. Depois que o DNS propagar, clique em **Implantar** de novo para emitir o certificado.

## 3. Publique o seu código

Repita o passo 2 escolhendo **Aplicação do Git** em vez de **Imagem Docker**:

| Campo | O que informar |
|---|---|
| Repositório | `https://github.com/voce/projeto` para repositórios públicos, ou `git@github.com:voce/projeto.git` para privados |
| Branch | A branch que será implantada. Exemplo: `main` |
| Como construir | **Dockerfile** se o projeto tem um; **Nixpacks** para detectar a linguagem sozinho; **Site estático** para HTML, CSS e JS sem servidor |
| Porta da aplicação | A porta em que o seu programa escuta dentro do container. Exemplo: `3000` |
| Domínio | `https://app.seudominio.com` |

Dois detalhes evitam a maioria dos problemas no primeiro deploy:

- **Escute em `0.0.0.0`, não em `localhost`.** Um programa que escuta só em `127.0.0.1` não recebe o tráfego do proxy. O Pier detecta isso e mostra como corrigir na sua linguagem.
- **A porta do formulário é a porta do programa.** Se o programa lê a variável `PORT`, defina `PORT` com o mesmo valor em **Variáveis**.

Para repositórios privados, use o endereço `git@…`, escolha uma **chave de deploy** e cadastre a chave pública mostrada pelo Pier como *deploy key* no GitHub, GitLab ou Gitea.

> **Exemplo que funciona:** `https://github.com/heroku/node-js-getting-started` com **Nixpacks**, porta `3000` e a variável `PORT=3000`. O Nixpacks precisa estar instalado no servidor.

## 4. Conecte um banco

1. No mesmo projeto, clique em **Novo recurso**, escolha **Banco de dados**, depois **PostgreSQL**, e dê o nome `principal`.
2. Na aplicação, abra **Variáveis** e use **Conectar a um banco**. Ou escreva a referência à mão:

   ```sh
   DATABASE_URL=${{principal.url}}
   ```

3. Clique em **Implantar**. A cada deploy, o Pier troca a referência pela URL de conexão atual. A senha nunca fica escrita na aplicação.

## 5. Deploy automático a cada push

Na aplicação, abra a aba **Automação**, copie a URL e o segredo do webhook e cadastre os dois no repositório:

- **GitHub:** Settings › Webhooks › Add webhook, com *Content type* `application/json`.
- **GitLab:** Settings › Webhooks.
- **Gitea ou Forgejo:** Configurações › Webhooks.

A partir daí, cada push na branch configurada dispara um deploy. O Pier precisa ser alcançável pela internet, de preferência com o [painel em HTTPS](/docs/instalacao/#5-coloque-o-painel-num-dominio-com-https).

## E agora?

- Se algo deu errado, o deploy falho mostra o erro explicado no topo do log. Veja também [Problemas comuns](/docs/problemas-comuns/).
- Conheça o que mais dá para configurar em [Aplicações](/docs/aplicacoes/).
- Faça o [backup da instância](/docs/backup/) se ainda não fez.
