<!-- O título do PR vira o commit (squash) e segue Conventional Commits: feat(api): ..., fix(deploy): ... -->

## O que muda e por quê

<!-- O problema, a solução e as decisões de desenho. Link para a issue: Closes #123 -->

## Como foi testado

<!-- Testes adicionados e o que você rodou: make test, go test -race ./... -->

## Checklist

- [ ] Título no formato Conventional Commits
- [ ] Testes novos para o comportamento novo; `make test` passa
- [ ] Rotas novas declaram a permissão (`read`/`deploy`/`write`), validam a entrada e não expõem segredos
- [ ] Textos da interface em português, com tradução no catálogo inglês
- [ ] Dependências novas têm licença compatível com Apache-2.0 e estão em `THIRD_PARTY_NOTICES.md`
- [ ] Não editei versão nem `CHANGELOG.md` (o release-please cuida)
