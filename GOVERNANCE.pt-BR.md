[English](GOVERNANCE.md) | Português

# Governança

O Pier foi desenhado para **continuar funcionando sem um mantenedor dedicado**: onde outros projetos dependem de uma pessoa, aqui há automação. Este documento diz como as coisas acontecem.

## Princípios

1. **O CI é o primeiro revisor.** Nada entra em `main` sem os checks obrigatórios verdes: Go (`vet` + `test -race`), interface (tipos + testes), site, gitleaks, govulncheck, CodeQL e título de PR convencional.
2. **Releases são automáticos.** O release-please transforma Conventional Commits em versão, `CHANGELOG.md` e tag; a imagem e o site são publicados pelos workflows. Ninguém sobe versão à mão.
3. **Dependências se atualizam sozinhas.** O Dependabot abre PRs semanais agrupados; atualizações *patch* e *minor* entram por auto-merge quando o CI passa. Atualizações *major* esperam revisão humana.
4. **Decisões ficam escritas.** Discussões, issues e PRs são o registro; mudanças de desenho relevantes viram uma issue marcada `design` ou um documento no repositório.
5. **Fork é um direito, não uma ameaça.** A licença é Apache-2.0 e a imagem, o instalador e os workflows funcionam em qualquer fork (troque o dono em `ghcr.io/<dono>/pier` e nos workflows). Se o projeto parar, a comunidade pode continuar.

## Como uma mudança entra

| Tipo de PR | Regra de merge |
|---|---|
| Dependabot, *patch* ou *minor* | Auto-merge quando todos os checks obrigatórios passam |
| Dependabot, *major* | Revisão de uma pessoa com permissão de escrita |
| Release PR (release-please) | Merge por uma pessoa com escrita, ou automático se o repositório definir a variável `RELEASE_AUTOMERGE=true` |
| Demais PRs | Checks verdes **e** uma aprovação de quem tem permissão de escrita (que não seja o autor) |

Branch protection recomendada em `main`: exigir PR, checks obrigatórios, histórico linear (squash) e auto-merge permitido.

PRs sem resposta de revisão por 14 dias podem ser mesclados por qualquer pessoa com escrita, se o CI estiver verde e a mudança for pequena e coberta por testes.

## Papéis

- **Contribuidor:** qualquer pessoa que abre issue, discussão ou PR.
- **Mantenedor:** quem tem permissão de escrita. Revisa PRs, mescla, triagem de issues e conduz as releases quando preciso. Pode ser listado em um `CODEOWNERS` (opcional; só ative se houver pessoas suficientes, para não travar PRs esperando alguém ausente).
- **Dono do repositório:** administra as configurações do GitHub e as permissões; decide em último caso.

### Como virar mantenedor

Após **cerca de cinco PRs relevantes mesclados** (ou contribuição equivalente em revisão e triagem) ao longo de pelo menos dois meses, e conduta alinhada ao [Código de Conduta](CODE_OF_CONDUCT.pt-BR.md), qualquer mantenedor pode propor você numa issue de governança. Com a aprovação de outro mantenedor (ou do dono, se não houver outro), você recebe permissão de escrita. Mantenedores inativos por 12 meses voltam a ser contribuidores, sem prejuízo, e podem retornar quando quiserem.

## Segurança

Vulnerabilidades seguem o [SECURITY.md](SECURITY.pt-BR.md): relato privado pelo GitHub, correção em release automático.

## Mudanças neste documento

Por PR, com aprovação do dono e de pelo menos um mantenedor, se houver.
