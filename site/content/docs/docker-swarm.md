## Prepare um cluster

Abra **Clusters**, depois **Docker Swarm**. Inicialize um cluster em um manager cadastrado e adicione workers. O Pier não importa clusters existentes. Preserve o quorum e configure o firewall entre os nós. Não coloque bancos de dados neste alvo de aplicações.

## Selecione um alvo de aplicação

Na aba **Destinos** da aplicação, informe o identificador do cluster em **ID do alvo Swarm**. Mantenha apenas o servidor principal selecionado como destino individual. Escolha outro servidor de build se necessário. Builds de código exigem registro de imagens e repositório. As credenciais usam o serviço criptografado de registries existente; o deploy encaminha autenticação privada aos workers Swarm.

A API existente de destinos aceita `swarm_cluster_id`. As habilidades de leitura e escrita mantêm o escopo da aplicação/projeto. Deploy, envio de arquivo, rollback e promoção usam a fila existente. O build roda uma vez e publica uma imagem por digest imutável. Aplicações de imagem podem usar digest público ou privado configurado. A promoção usa o digest da origem e o ambiente da aplicação de destino.

## Ambiente e roteamento

Variáveis e segredos usam o armazenamento criptografado existente. Os valores são mascarados nos logs e mensagens de falha. Durante o release, um diretório temporário privado contém um arquivo de stack com modo 0600, removido após a tentativa. Administradores Docker podem consultar as variáveis na especificação do serviço: são variáveis de ambiente, não montagens de Docker secrets.

Domínios reutilizam os labels de rotas da aplicação no serviço, com a rede overlay `pier-swarm`. Configure um Traefik externo com provider Swarm em um manager, conectado à rede, com entrypoints e resolvedor de certificados compatíveis. Aponte o DNS para esse proxy. O Pier não instala nem valida essa configuração; o provider Docker comum por servidor é insuficiente. Não houve validação real de DNS ou proxy.

## Rollout e recuperação

O dispatcher serializa o deploy por aplicação. Cada réplica gerada vira um serviço Swarm, com atualização start-first, health checks da aplicação e limites de recursos. O release aguarda convergência das tarefas e estabilização; rollout pausado ou timeout falha o deploy. Na primeira falha a stack nova é removida; em uma atualização as especificações anteriores são restauradas por rollback. Erros da recuperação aparecem na falha do deploy. Sucesso de comando Docker isolado não conclui o deploy.

## Limitações

Volumes, portas publicadas no host, hooks, comandos de release e opções Docker extras são recusados. Nomes de processos e quantidades de réplicas devem permanecer fixos após o primeiro release. Use outra aplicação para outra topologia. Mudar o alvo após um release é bloqueado. Iniciar/parar/reiniciar exigem novo deploy; excluir remove a stack Swarm. Previews, terminal, métricas, inventário de containers e logs da aplicação ainda usam os caminhos existentes de servidor único e não entendem Swarm. Evite essas operações em aplicações Swarm. Interrupção do processo pode exigir reconciliação manual; a recuperação não é uma transação durável. Arquiteturas diferentes nos workers, Traefik externo e rollouts Docker têm apenas cobertura com executor falso, sem validação de integração com servidor ou registry real.
