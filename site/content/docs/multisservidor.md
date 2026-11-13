Uma aplicação pode construir a imagem em um servidor e executá-la em vários destinos. Use esse fluxo quando já tiver preparado os servidores, o registry e o encaminhamento de tráfego. Ele não cria um cluster nem um balanceador global.

## Preparar os destinos

1. Cadastre e valide cada servidor por SSH em **Infraestrutura**. Docker, Compose, Buildx no servidor de build e espaço livre precisam estar disponíveis.
2. Configure o proxy de cada servidor e permita que cada máquina alcance o registry por HTTPS. As arquiteturas precisam ser compatíveis com a imagem.
3. Prepare DNS ou um balanceador externo para enviar o tráfego aos destinos. O Pier não distribui tráfego entre servidores.
4. Na aba **Destinos** da aplicação, cadastre a credencial do registry e teste o acesso. Docker Hub, GHCR, GitLab e outros registries OCI via HTTPS podem ser usados. Não coloque senha na URL da imagem.
5. Escolha servidor de build, registry, repositório da imagem e os servidores de execução. O servidor original da aplicação permanece entre os destinos. Salve e faça um deploy de teste.

As credenciais ficam cifradas no Pier. Cada push e pull usa uma configuração Docker temporária privada, removida ao terminar. A leitura administrativa mostra metadados, sem devolver a senha.

## O que acontece no deploy

A imagem é construída uma vez no servidor de build, publicada e identificada por um digest imutável. Todos os destinos baixam esse digest antes de começar a atualização. O Pier atualiza um servidor por vez, usando as próprias labels de proxy e verificações de prontidão de cada destino. Todas as réplicas daquele servidor precisam ficar prontas.

O comando de release e o hook de pré-deploy rodam somente no primeiro servidor. Isso evita executar a mesma migration em todas as máquinas, mas não torna a migration reversível.

Se um destino falhar, ele preserva a versão anterior; os destinos já atualizados tentam voltar à imagem anterior e os ainda não alcançados não são alterados. Confira o resultado por servidor: compensação pode falhar e precisa de intervenção. Start, stop, restart e remoção atuam em todos os destinos. Um destino retirado da configuração é limpo após o próximo deploy bem-sucedido.

## Acompanhar e promover

A aba **Destinos** mostra estado do rollout, digest, versão anterior, containers, CPU, memória e logs por servidor. O rollback usa o fluxo de rollback da aplicação.

Uma promoção entre servidores pode reutilizar o mesmo digest já publicado, sem reconstruir ou reenviar a imagem. A aplicação de destino recebe suas próprias variáveis. O registry precisa manter o artefato disponível; configure retenção para preservar as versões de rollback.

## Limites que importam

- O rollout é **sequencial**, não uma transação atômica. Servidores podem atender versões diferentes por algum tempo.
- Rollback de imagem não desfaz migrations ou alterações em serviços externos.
- Volumes não são compartilhados. Uploads locais e bancos precisam de arquitetura própria; réplicas de aplicação não replicam dados.
- Não há retomada automática de rollout interrompido. Depois de reiniciar o Pier durante uma atualização, confira cada destino antes de tentar de novo.
- Previews, tarefas agendadas, terminal e aba Logs ainda usam o servidor original da aplicação; use a visão de destinos para os demais. O histórico de métricas agrega os destinos configurados: [entenda totais e lacunas](/docs/alertas/#historico-de-metricas).
- Registry HTTP inseguro não é suportado. Não há provisionamento automático de DNS ou balanceador.

A integração foi exercitada em testes com executores simulados e servidores HTTP de teste. Não foi validada contra registries ou múltiplos servidores reais nesta etapa. Antes de usar produção, teste publicação privada, falha parcial, rollback e recuperação no seu ambiente.
