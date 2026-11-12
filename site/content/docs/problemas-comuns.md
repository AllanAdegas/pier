Quando um deploy falha, o Pier tenta explicar o erro no topo do log, com a correção sugerida. Esta página reúne os casos mais comuns, inclusive os que acontecem fora do deploy.

## Instalação e acesso

### Perdi o código de configuração

Ele fica no log do container:

```sh
docker compose logs pier | grep setup_code
```

O código só vale enquanto ninguém criou a conta de administrador.

### Não consigo abrir o painel

O painel escuta só em `127.0.0.1:8700` do servidor, de propósito. Do seu computador, use um túnel:

```sh
ssh -L 8700:127.0.0.1:8700 usuario@IP-DO-SERVIDOR
```

Depois abra `http://localhost:8700`. Para acessar sem túnel, [coloque o painel num domínio com HTTPS](/docs/instalacao/#5-coloque-o-painel-num-dominio-com-https).

### "Não foi possível criar /data/pier"

O seu usuário não pode escrever nessa pasta:

```sh
sudo mkdir -p /data/pier && sudo chown "$USER" /data/pier
./install.sh
```

### "O Docker não está respondendo"

O serviço do Docker está parado ou o seu usuário não tem acesso ao socket:

```sh
sudo systemctl start docker
sudo usermod -aG docker "$USER"   # depois saia e entre de novo no SSH
```

## Domínios e certificados

### O certificado não é emitido

Confira, nesta ordem:

1. **DNS:** `dig +short seu.dominio.com` precisa devolver o IP do servidor. Um registro `AAAA` com IPv6 errado também impede o certificado: apague-o se o servidor não tem IPv6.
2. **Portas:** 80 e 443 abertas no firewall do servidor **e** no painel do provedor da VPS.
3. **Proxy:** em **Infraestrutura › servidor › Proxy**, ele está em execução e tem um e-mail configurado.
4. **Novo deploy:** depois de corrigir o DNS, implante de novo.

Os logs do proxy, na mesma tela, mostram a resposta do Let's Encrypt. O Let's Encrypt limita as tentativas por domínio. Se insistiu muito, espere uma hora.

### O proxy não inicia: porta em uso

Outro programa ocupa a porta 80 ou 443. A mensagem diz qual. Se for um Caddy, use o modo [Caddy na frente](/docs/servidores-e-dominios/#ja-tenho-um-caddy-no-servidor). Se for um nginx ou Apache instalado no sistema, pare e desative:

```sh
sudo systemctl disable --now nginx    # ou apache2
```

### O domínio parou de responder depois de atualizar

Desde a 0.5.0, o proxy só roteia recursos implantados pela versão nova. Recrie o proxy e implante de novo os recursos com domínio. Veja as [notas da versão](/docs/atualizar/#notas-da-versao-0-5-0).

## Deploys

### A aplicação não fica pronta: porta errada

A porta configurada não é a porta em que o programa escuta. O Pier compara as duas e mostra a correta, com um botão para corrigir. Se o programa lê `PORT`, defina essa variável com o mesmo número.

### A aplicação escuta só em 127.0.0.1

Dentro do container, `localhost` não recebe o tráfego do proxy. Faça o programa escutar em `0.0.0.0`:

| Linguagem | Como |
|---|---|
| Node.js | `app.listen(port, "0.0.0.0")` |
| Python | `uvicorn main:app --host 0.0.0.0` ou `gunicorn -b 0.0.0.0:8000` |
| Go | `http.ListenAndServe(":8080", nil)` |
| Ruby | `rails server -b 0.0.0.0` |
| PHP | `php -S 0.0.0.0:8000` |
| Java (Spring) | `server.address=0.0.0.0` |

### Saiu com código 137 (falta de memória)

O container passou do limite de memória ou a máquina ficou sem memória. Aumente o limite da aplicação, reduza o consumo ou, se for no build, aumente `PIER_BUILD_MEMORY` no `.env` do Pier.

### "No space left on device"

O disco encheu. Em **Infraestrutura › servidor › Manutenção**, limpe imagens e cache de build. Confira também os volumes e os backups no servidor. O Pier recusa um build quando há menos espaço livre que `PIER_BUILD_MIN_FREE_GB`.

### "Nixpacks não encontrado" ou "Buildx não encontrado"

- **Buildx** é obrigatório para builds de código. Instale o pacote `docker-buildx-plugin`.
- **Nixpacks** só é necessário no build pack Nixpacks. Instale no servidor ou use um Dockerfile.

### Repositório privado: "Permission denied (publickey)"

A chave de deploy não está cadastrada no repositório, ou a aplicação usa o endereço `https://` em vez de `git@…`. Em **Segurança**, veja a chave pública e cadastre-a como *deploy key*.

### "Ambiente congelado"

O ambiente de destino está com **Congelar deploys** ligado. Descongele em **Projetos**, com uma justificativa, ou implante em outro ambiente.

### "all predefined address pools have been fully subnetted"

O Docker do servidor ficou sem faixas de IP para redes novas. Cada stack usa pelo menos uma. Remova as redes sem uso com `docker network prune`, ou amplie as faixas em `/etc/docker/daemon.json` e reinicie o Docker:

```json
{ "default-address-pools": [{ "base": "10.200.0.0/16", "size": 24 }] }
```

## Servidores

### "A chave do servidor mudou"

O servidor foi reinstalado ou trocou de IP para outra máquina. Se você sabe o motivo, redefina a chave em **Infraestrutura › servidor › Manutenção**. Se não sabe, investigue antes: pode ser outra máquina se passando pelo seu servidor.

### O webhook não dispara deploy

- O Pier precisa ser alcançável pela internet, com a **URL pública** preenchida em **Configurações**.
- A branch do push precisa ser a branch da aplicação.
- Com **caminhos observados**, o push precisa mudar algum arquivo dentro deles.
- Com padrão de tag, só tags que casam disparam deploy. Pushes de branch são ignorados.

Na página de webhooks do GitHub ou GitLab, a resposta do Pier explica por que nada foi feito.

## Ainda com problema?

Junte o log do deploy e a saída de `docker compose logs --tail 200 pier` e abra uma issue em [{{repo}}/issues]({{repo}}/issues). Antes de publicar, apague senhas, tokens e domínios internos.
