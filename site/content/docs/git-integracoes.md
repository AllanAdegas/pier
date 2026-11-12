## Compose do Git

Em **Novo recurso**, escolha **Compose do Git**. Informe repositório, branch ou tag fixa, diretório base e caminho do compose. Para acesso SSH privado, selecione uma chave autorizada no repositório.

Na aba **Git** da stack, confira o último commit e configure deploy automático, caminhos observados e webhook assinado. Uma tag fixa ignora pushes. Um push durante um deploy pode ser recusado com conflito, sem entrar numa fila: confira o commit implantado e tente novamente.

O Pier verifica caminhos do compose, includes e extends dentro do checkout. Se detectar novos poderes sobre o host, o deploy para e mostra riscos pendentes. Revise o diff antes de aceitar acesso privilegiado, socket Docker, rede, portas ou pastas do host. Aceitar um risco permite executá-lo; não o torna seguro.

O checkout é substituído a cada deploy. Dados gravados em pastas do repositório podem ser perdidos: use volumes nomeados e [backup](/docs/backup/). Builds de Compose usam fila e verificação de disco, mas não recebem os mesmos limites de CPU e memória dos builds de aplicações.

## GitHub App

Em **Configurações**, a integração permite cadastrar uma GitHub App própria, listar instalações e repositórios e associar um repositório a um recurso existente.

1. Crie a App no GitHub com leitura de conteúdo, escrita de commit statuses e pull requests (metadados têm leitura implícita).
2. Instale somente nos repositórios necessários. Não conceda escrita em Actions, secrets ou workflows.
3. Configure webhook HTTPS em `/webhooks/git-provider/github`, com eventos `installation` e `installation_repositories`, e segredo aleatório de pelo menos 32 caracteres.
4. No Pier, informe App ID, chave privada RSA PEM e o mesmo segredo. Após salvar, os campos de credenciais são limpos.
5. Liste instalações e repositórios e selecione o recurso pelo tipo e ID.

A integração usa `api.github.com`; GitHub Enterprise não é suportado. Não confunda GitHub App com [SSO](/docs/equipe/): ela autoriza acesso a repositórios, não o login de pessoas.

### Integração ainda em validação

As notas registram o adaptador de clone e reporte de preview, mas deixam a ligação ao deploy a cargo da integração. Selecionar um repositório não é, sozinho, prova de deploy privado funcionando. Mantenha o método de credencial já validado na sua instalação até confirmar clone e deploy reais. Reportes de status/comentário são de melhor esforço, sem garantia de repetição após falhas; confira os logs no Pier.

## Registry e múltiplos destinos

Há implementação de cadastro de registry e configuração de placement, mas as notas de multi-server registram pendência de integração ao engine. Esta documentação não oferece um procedimento de produção para build remoto ou deploy em múltiplos destinos. Salvar placement não comprova que o deploy passou a usá-lo.

O fluxo documentado continua sendo uma aplicação em um servidor. Réplicas locais não equivalem a alta disponibilidade entre máquinas. Não há promessa de transação distribuída, recuperação automática de rollout interrompido, volumes compartilhados, banco em HA ou configuração automática de DNS/balanceador externo.
