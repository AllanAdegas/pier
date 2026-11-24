O Pier gerencia o servidor onde está instalado e, por SSH, quantos outros você quiser. Na frente das aplicações, cada servidor tem um proxy que cuida dos domínios e dos certificados.

## O servidor local

No primeiro início, o Pier cadastra a **máquina local**, o servidor onde ele roda. Você não precisa configurar nada: ela já aparece em **Infraestrutura**.

## Adicionar outro servidor

O Pier não instala agente no servidor. Ele só precisa de SSH e Docker.

1. **Prepare o servidor.** Instale o Docker como na [instalação](/docs/instalacao/#1-instale-o-docker) e libere as portas 22, 80 e 443.
2. **Crie uma chave.** Em **Segurança**, gere uma chave SSH (ed25519) ou importe uma que você já tem. Chaves com frase-senha não são aceitas.
3. **Autorize a chave.** No servidor novo, adicione a chave pública ao arquivo `~/.ssh/authorized_keys` de um usuário que possa usar o Docker:

   ```sh
   echo "ssh-ed25519 AAAA... pier" >> ~/.ssh/authorized_keys
   ```

4. **Cadastre.** Em **Infraestrutura › Adicionar servidor**, informe nome, endereço, porta SSH, usuário e a chave. Clique em **Adicionar e validar**.

A validação confere a conexão, o Docker e o plugin compose. Também cria o diretório de dados e mostra uma lista de checagens, cada uma com a correção sugerida:

| Checagem | Esperado |
|---|---|
| Docker | Versão 24 ou mais nova |
| Docker Compose | Versão 2.20 ou mais nova |
| Disco | Pelo menos 10 GB livres para o Docker |
| Portas 80 e 443 | Livres, ou ocupadas pelo proxy do Pier ou por um Caddy |
| Rotação de logs | Configurada no Docker, para os logs não encherem o disco |
| Relógio | No máximo 30 segundos de diferença |

Só a versão e a conectividade impedem o uso do servidor. O resto são avisos.

> **Chave do servidor:** no primeiro acesso, o Pier grava a impressão digital SSH do servidor. Se ela mudar depois, por exemplo porque o servidor foi reinstalado, toda conexão é recusada até você redefinir em **Manutenção**. Isso evita que outro servidor se passe pelo seu.

## Proxy, domínios e HTTPS

Cada servidor tem um proxy (Traefik) que recebe o tráfego nas portas 80 e 443 e entrega cada domínio ao container certo. Ele não sobe sozinho:

1. Abra **Infraestrutura › servidor › Proxy**.
2. Informe o e-mail do Let's Encrypt. É para onde vão os avisos sobre os certificados.
3. Clique em **Iniciar**.

Para um domínio funcionar com HTTPS:

1. **DNS:** crie um registro `A` do domínio para o IP do servidor. Crie um `AAAA` só se o servidor tiver IPv6 funcionando: um `AAAA` errado impede o certificado.
2. **Portas:** 80 e 443 abertas para a internet.
3. **Recurso:** informe o domínio com `https://` na aplicação ou na rota da stack e implante.

Antes de pedir o certificado, o Pier confere se o DNS aponta para o servidor. Se não aponta, publica em HTTP e o log diz exatamente qual registro criar. Depois que o DNS propagar, implante de novo.

Um domínio `https://` também responde em `http://` com um redirecionamento permanente (308) para o endereço HTTPS. Assim, os buscadores indexam só a versão HTTPS. Recursos implantados antes da versão com o 308 continuam com o redirecionamento temporário (307) até o próximo deploy.

Certificados são renovados sozinhos. O Pier avisa se um certificado estiver inválido ou perto de vencer: veja [Alertas](/docs/alertas/).

### Já tenho um Caddy no servidor

Se um Caddy já ocupa as portas 80 e 443 servindo outros sites, não precisa tirá-lo. Em **Proxy**, escolha o container do Caddy. O Pier mantém no Caddyfile um bloco próprio, que encaminha os domínios dele para o proxy do Pier. O resto do arquivo não é tocado. O Caddyfile precisa ser gravável dentro do container do Caddy.

### Portas alternativas

Para testar numa máquina que já usa 80 e 443, mude as portas do proxy antes de iniciá-lo. Nesse caso não há certificado automático, porque o Let's Encrypt exige as portas padrão.

## Uso de recursos e limpeza

- **Uso de recursos** mostra CPU, memória, disco e o consumo de cada container gerenciado.
- **Manutenção** limpa imagens, containers parados e cache de build criados **pelo Pier**. Nada que outra ferramenta criou é removido.

## Docker puro

A aba **Docker** de cada servidor mostra tudo o que existe nele: containers, imagens, volumes e redes, criados pelo Pier ou não. Dá para ver logs, executar comandos e rodar um container avulso. Containers gerenciados pelo Pier só podem ser removidos pela aplicação, banco ou stack dona.
