## Terminal do navegador

Use **Terminal** na aplicação, banco, container da stack ou explorador Docker do servidor. Para stacks, escolha o container desejado. A imagem precisa ter `/bin/sh`; a seleção automática prefere bash quando disponível.

É uma sessão interativa com os privilégios do usuário do container e acesso aos seus arquivos, rede e variáveis. Containers privilegiados ou com socket Docker podem dar controle do host. Use apenas com pessoas confiáveis. O terminal exige sessão do navegador com capacidade de escrita; tokens de API, mesmo de escrita, não abrem terminal.

Feche pelo botão do modal. Escape é enviado ao shell. Inatividade de 15 minutos ou duração de oito horas encerra a sessão; não há reconexão, compartilhamento ou gravação de comandos. A auditoria registra abertura e fechamento, sem transcrição: não use isso como registro completo do que foi executado. Revogar a sessão encerra o terminal na próxima revalidação, em até cerca de 30 segundos.

## Manutenção do servidor

Em **Operações do servidor**, marque **Pausar novos deploys neste servidor** e salve. Confira antes se há operações já em andamento. A manutenção não para containers existentes nem bloqueia leitura e restauração; não é desligamento da máquina. Ao terminar, desmarque e salve, então valide um deploy.

## Limpeza segura

1. Use **Simular limpeza segura** e leia o relatório.
2. Confira a idade mínima (24 a 8760 horas) e os objetos candidatos.
3. Digite o nome do servidor e use **Executar limpeza segura** somente após revisar.
4. Para automatizar, ative a agenda, defina cron e salve a política; acompanhe os resultados.

Só entram objetos descartáveis com `pier.managed=true`, `pier.cleanup=true` e `pier.type=scratch`, respeitando a idade. Containers precisam estar parados e imagens sem tag. Volumes e backups nunca entram. A simulação não reserva o estado do Docker: confira o resultado da execução. Não substitua esse fluxo por `docker system prune --volumes`; volumes podem conter os dados dos seus serviços.


## Instalar Docker por SSH

O fluxo **Instalar Docker** do servidor inicia uma operação remota e permite acompanhar o estado. Use em uma máquina suportada e com credencial SSH privilegiada preparada; exige confirmação antes de mudar o host. Não execute como tentativa de reparar uma instalação com serviços importantes sem backup e janela de manutenção. Depois confira Docker, Compose, Buildx e a validação do servidor.

## Criar máquinas na nuvem

O backend oferece provisionamento para **Hetzner Cloud** e **DigitalOcean**. Não há provisionador AWS genérico. Registre uma credencial de API com o menor escopo aceito pelo provedor, consulte o catálogo de região/tamanho/imagem e revise a cobrança antes de criar. O Pier gera/acessa a chave SSH necessária, espera SSH e pode instalar Docker; depois registra o servidor.

O fluxo continua em integração nesta beta, inclusive a interface. As rotas de provisionamento são `/api/v1/cloud/credentials` e `/api/v1/cloud/machines`; cadastro de credenciais e destruição de máquina exigem sessão do painel e as permissões correspondentes. Destruir uma máquina apaga a infraestrutura no provedor, não apenas o registro local. Falha no cadastro ou instalação não garante que o recurso deixou de ser cobrado: confira o painel do provedor antes de repetir ou encerrar a operação.

Não foram criadas máquinas reais para validar esta entrega; testes de API usam serviços simulados. Não use produção como primeiro teste.

## Envio de logs

Log drains encaminham logs de um projeto, aplicação ou stack para **HTTP JSON** ou **Loki**. Não é suporte genérico a OTLP. Configure o destino, escopo, rótulos e token; com token, a URL precisa usar HTTPS. Não coloque credenciais na URL. O destino recebe dados das cargas selecionadas: revise privacidade, retenção e acesso do serviço externo.

O envio HTTP publica um array de registros com timestamp, mensagem e labels. Loki usa o endpoint de push correspondente. Há teste do destino na API `/api/v1/log-drains/{id}/test`; enviar um teste não comprova coleta contínua de todos os containers. A interface de configuração ainda está em integração nesta beta.

A fila é limitada e fica em memória. Lentidão do destino, repetidas falhas, excesso de linhas ou reinício do Pier podem perder logs; não há garantia de entrega única nem persistente. O mascaramento usa os segredos conhecidos pelo Pier, não detecta todo dado sensível escrito pela aplicação. Use uma solução própria de auditoria/armazenamento quando não puder perder eventos.
