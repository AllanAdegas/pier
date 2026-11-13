# Pier 0.6.0 — candidato, aguardando QA final

Painel self-hosted Apache-2.0 para equipes pequenas. Esta linha continua em beta: os recursos e limites abaixo devem orientar a adoção. Publicação depende da QA final; este arquivo não representa uma release já publicada.

- Autenticação TOTP/recovery, redefinição de senha, sessões e SSO OIDC; papéis de instância/projeto e convites.
- Previews isolados por identidade, com forks desligados por padrão; Git stacks e integrações GitHub App/provedores.
- Terminal web com sessão e revalidação de acesso; histórico de métricas, alertas e envio de logs.
- Backup de volumes e engines key-value, com recuperação de disponibilidade após interrupção e journal na migration0024.
- Registry privado, build server e destinos múltiplos por digest; rollout sequencial e compensação. DNS/LB externos, sem volumes compartilhados ou retomada automática de rollout.
- Railpack, variáveis compartilhadas, clonagem de definições, domínios automáticos e opções de runtime; manutenção, limpeza restrita, provisionamento e instalação remota com confirmação.
- CLI expandida, contrato OpenAPI derivado de296operações e site com24guias. Automação de testes/releases/dependências reduz tarefas recorrentes, sem dispensar revisão humana.

## Atualização e limites

Faça backup do banco de controle e da chave e teste a restauração antes de atualizar. Migrations não têm down. A linha0.6 altera o controle de acesso: usuários preexistentes preservam papel owner da instância por compatibilidade; revise explicitamente os grants. Bancos de desenvolvimento que aplicaram versões intermediárias das migrations0.6 precisam ser recriados; não são caminho de upgrade lançado.

Binds/opções perigosas exigem owner da instância e política explícita para gravar configuração, inclusive clones. Previews não herdam privilégios de host, mas código de fork continua sendo código não confiável no servidor; não é sandbox de kernel. Portas fixas do host implicam interrupção na troca; compensação não desfaz migrações/dados externos. Métricas somam destinos completos; histórico legado sobrescrito não é recuperável. Log drains são best effort com perdas contabilizadas e cursor em memória.

Evidência local inclui Linux/arm64 no Docker Desktop, fakes/SQLite/httptest e ensaios delimitados. Não equivale a operação de todos os provedores cloud, registries, arquiteturas ou todo catálogo em produção. Os artefatos GitHub/GHCR e site só devem ser anunciados como disponíveis após publicação e verificação reais.
