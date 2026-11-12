A configuração de build define a imagem; a configuração de execução define o container. Mudanças normalmente só chegam ao runtime no próximo deploy. Teste o artefato antes de promovê-lo.

## Dockerfile, Railpack e comandos

O build pack **Dockerfile** usa o arquivo e diretório base configurados. Para um Dockerfile com vários estágios, informe **Estágio do Dockerfile** (`build.target` no `pier.yaml`); vazio usa o estágio final.

**Railpack** detecta a aplicação, prepara o plano e o constrói por BuildKit. O Pier usa uma versão fixada e verifica o download do binário antes de executar. O servidor precisa alcançar os downloads e registries utilizados. Comandos personalizados de instalação, build e início continuam sendo código que você autoriza executar; não são um sandbox. Nixpacks continua disponível para projetos que já o usam. Teste compatibilidade antes de mudar o pack de uma aplicação existente.

## Segredos de build

Marque **Segredo de build** para transportar o valor por BuildKit, em vez de argumento comum de build. No Dockerfile, leia por `RUN --mount=type=secret,id=NOME`.

Isso protege o transporte, mas não impede seu comando de copiar ou imprimir o segredo. A flag também não significa “apenas build”: as variáveis da aplicação vão ao runtime. Não use um segredo duradouro se o código construído não for confiável. Variáveis somente de runtime não precisam entrar no build.

## Reinício e encerramento

A aplicação aceita as políticas `no`, `always`, `unless-stopped` e `on-failure`. Sem configuração, usa `unless-stopped`. O prazo de encerramento gracioso aceita de 1 a 300 segundos; zero usa 30 segundos para novos containers. Vale para web e workers.

Ao aposentar uma versão, o Pier respeita o prazo que foi salvo no container antigo. Containers antigos sem esse valor usam o padrão do Docker até serem reimplantados. Não prometa que um processo encerra corretamente só por aumentar o prazo: ele precisa tratar SIGTERM e terminar requisições/tarefas.

## Variáveis compartilhadas

Uma variável pode pertencer ao projeto ou a um ambiente. A aplicação a referencia pelo nome:

```text
${{project.API_URL}}
${{environment.LOG_LEVEL}}
```

Cadastre o valor no escopo correto e use a referência como valor da variável da aplicação. Segredos compartilhados ficam cifrados e não são devolvidos em texto na listagem. Uma alteração marca as aplicações dependentes para redeploy; o Pier não publica todas automaticamente. Revise o impacto antes de mudar ou remover uma variável compartilhada. Referências a bancos, como `${{principal.url}}`, continuam sendo um mecanismo separado.

## Clonar para outro ambiente

A clonagem copia a **definição** de uma aplicação, banco ou stack para outro ambiente. Não copia dados de volumes nem conteúdo do banco, e não implanta a cópia automaticamente. Revise segredos, domínio, portas, servidor e credenciais da origem antes do primeiro deploy; copiar a configuração não cria dados de staging seguros. Riscos aceitos de uma Git stack exigem revisão na cópia.

## Domínio automático

A política da instância pode gerar domínio para aplicações sem domínio. Sem domínio base próprio, usa IPv4 público + `sslip.io`, somente HTTP. Isso é útil em testes, não é um substituto de HTTPS para dados sensíveis.

Com um domínio base próprio, configure DNS curinga para o servidor antes de ativar HTTPS. O Pier não cria registros no seu provedor DNS. Remover um domínio automático impede que ele seja recolocado sem uma nova solicitação. Em vários destinos, prepare DNS/balanceamento externo como no [guia multisservidor](/docs/multisservidor/).

## Opções avançadas do Docker

A configuração pode definir hosts extras, capabilities, ulimits, memória compartilhada e labels permitidos. Prefixos reservados `pier.`, `traefik.` e `com.docker.` não podem ser usados para alterar o controle do painel.

Modo privilegiado, rede/PID do host, devices sensíveis e acesso ao socket Docker exigem a política de risco da instância, administrada por proprietário. Ativar essa política não torna a opção segura. Rede do host não aceita domínios ou portas publicadas pelo Pier. Use somente quando souber quais privilégios o processo ganha no servidor.

Os campos avançados e suas telas ainda passam pela integração e QA desta beta. Não copie opções arbitrárias de outro Compose: confira validação, política e configuração salva. Veja também [pier.yaml](/docs/pier-yaml/) e [segurança](/docs/seguranca/).
