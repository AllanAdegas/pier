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

A integração usa `api.github.com` por padrão. Um GitHub App configurado manualmente pode usar uma URL de API do GitHub Enterprise; o fluxo de manifesto continua específico do GitHub.com. Vincular credenciais Git globais exige permissão de escrita na instância e no recurso. Pessoas com permissão de deploy no projeto podem implantar um vínculo aprovado somente enquanto a origem corresponder ao repositório vinculado. Clones autenticados recusam redirecionamentos e não compartilham credenciais com outros repositórios, inclusive submódulos. Não confunda GitHub App com [SSO](/docs/equipe/): ela autoriza acesso a repositórios, não o login de pessoas.

### Vincular e acompanhar

Use **Integração Git** na aplicação ou stack para escolher um repositório instalado e clicar em **Vincular repositório**. A origem precisa usar o mesmo endereço HTTPS selecionado. Um vínculo incompatível, suspenso ou sem acesso é recusado, sem tentar clonar publicamente. **Desvincular** volta ao método de autenticação da origem no próximo deploy.

Aplicações vinculadas recebem pushes e pull requests pelo webhook da App; não cadastre também o webhook individual do recurso no GitHub. Mantenha as inscrições de eventos da App compatíveis com push e pull request. Os tokens para clone têm leitura de conteúdo; o reporte usa permissões separadas de escrita. Os tokens são temporários e não devem ser copiados para variáveis da aplicação.

Em aplicações, escolha **Publicar no provedor** para enviar status de commit e comentários em PR. GitHub usa a App; GitLab, Gitea/Forgejo e Bitbucket Cloud usam o token configurado para o provedor. Conceda só as permissões necessárias no repositório. Stacks têm vínculo de clone, mas não o mesmo reporte de deploy de aplicações. O reporte é de melhor esforço; um comentário ausente não altera o resultado do deploy no Pier.

A integração ao deploy foi exercitada com HTTP simulado e executores de teste. Não equivale a validar permissões e webhooks contra uma App real: faça um PR de teste antes de depender do reporte em produção.

## Outros provedores

Aplicações e Git stacks mostram os endereços e segredos de webhook na área de automação. Use o endereço específico do provedor e valide a assinatura/segredo; eventos de uma branch diferente ou tag não selecionada não devem publicar a aplicação principal. Não exponha o segredo em issues, capturas ou comandos.

Para builds em outra máquina, registries privados e deploy em vários destinos, siga [Registry e múltiplos servidores](/docs/multisservidor/).

Para instâncias GitLab/Gitea/Forgejo em domínio próprio, escolha o provedor explicitamente. A sugestão automática reconhece apenas hosts públicos exatos; nomes no caminho, query ou usuário da URL não identificam o provedor.

## Conexões GitLab, Gitea e Bitbucket

Em **Configurações**, abra **Conexões Git**. Escolha GitLab, Gitea / Forgejo ou Bitbucket Cloud, informe o nome, endereço HTTPS e **Token de acesso**, e clique em **Salvar conexão**. GitLab aceita subcaminhos; Bitbucket aceita apenas bitbucket.org, com workspace opcional. O token é validado e armazenado criptografado; nunca volta na resposta. Para trocar o token, preencha **Token de acesso** e clique em **Substituir token** na conexão desejada.

GitLab precisa de `api`. Gitea/Forgejo precisa de leitura de usuário e escrita de repositório. Bitbucket precisa de leitura de repositório e escrita de webhook; aceita Bearer ou `usuario:senha-de-app`. Use credenciais restritas aos repositórios necessários.

Em **Integração Git** de uma aplicação ou stack existente, escolha **Conexão Git**, use **Buscar repositório** e navegue com **Anterior** e **Próxima**. A lista mostra o endereço de clone e as branches disponíveis. Configure esse endereço e a branch na origem do recurso antes de clicar em **Vincular repositório**. O vínculo usa credenciais somente durante o clone e recusa uma origem diferente.

O Pier cria ou atualiza um webhook por recurso: `active` indica sucesso, `manual` indica falta de permissão (configure o webhook na área de automação), e `failed` indica outro erro. Vincular novamente tenta registrar de novo sem duplicar o webhook conhecido. A rotação do segredo atualiza o webhook remoto. **Desvincular** remove o webhook; falhas temporárias são tentadas novamente em segundo plano. Remover recursos também agenda essa limpeza. **Remover conexão** exige desvincular os recursos primeiro.

As rotas globais de conexões exigem acesso de instância; a listagem de repositórios e branches exige escrita. Os vínculos exigem acesso ao projeto do recurso (leitura para consultar, escrita para alterar). Os testes usam HTTP simulado e SQLite temporário; nenhum provedor real foi validado.

O mesmo seletor aparece na criação de aplicações e Git stacks. A seleção é vinculada antes do primeiro deploy, desde que o endereço da origem não tenha sido alterado.
