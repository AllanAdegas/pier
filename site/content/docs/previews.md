Previews são cópias temporárias de uma aplicação para revisar pull requests (ou merge requests). Ative na aba **Previews** da aplicação; ficam desligados por padrão.

## Configurar

1. Configure o webhook assinado do repositório (GitHub, GitLab ou Gitea/Forgejo).
2. Ative previews e defina um modelo de domínio com `{number}` no nome do host, por exemplo `pr-{number}.preview.exemplo.com`. `{branch}` é opcional.
3. Configure DNS e o proxy para esses domínios. O modelo sozinho não cria DNS.
4. Defina o máximo e o prazo de expiração: padrão de cinco previews e sete dias; limites de vinte e noventa dias.
5. Abra um PR de teste e acompanhe o deploy e seus logs na aba. Novos commits atualizam o preview pelo SHA recebido no webhook.

Cada preview tem identidade, domínio, imagens e volumes próprios. Portas do host, bind mounts, dispositivos e capabilities adicionadas são descartados; privileged e namespaces de host não são herdados, inclusive depois de sobreposições do repositório. A política Docker vigente é revalidada antes do build, e revogar a política bloqueia novos previews da configuração antes autorizada. Isso separa recursos da aplicação principal, mas o código ainda é construído no Docker do servidor.

## Variáveis e forks

PRs do próprio repositório herdam variáveis da aplicação, com sobreposições específicas de preview. Revise quais segredos esse código pode ler.

Forks ficam desativados por padrão. Ativá-los executa código de terceiros; não é um sandbox de segurança. Forks recebem somente variáveis específicas de preview, que também podem conter segredos se você os cadastrar. Use credenciais descartáveis e dados de teste; nunca coloque chaves de produção nessas variáveis. O isolamento de volumes não protege o host contra todo código malicioso de build.

## Encerrar e limpar

Fechar ou mesclar o PR remove o preview, inclusive com previews depois desativados ou ambiente congelado. A limpeza por prazo roda a cada hora. Ao atingir o máximo, novos previews são recusados; reduzi-lo permite remover os mais antigos. Também é possível remover ou reimplantar pela aba.

Congelar o ambiente bloqueia novos deploys; a limpeza continua permitida. Se o provedor não permitir buscar o SHA de um fork, o deploy falha. Confira resíduos quando houver falha de limpeza no servidor. Veja também [GitHub App](/docs/git-integracoes/).
