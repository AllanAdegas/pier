<!-- The PR title becomes the commit (squash) and follows Conventional Commits: feat(api): ..., fix(deploy): ...
     O título do PR vira o commit (squash) e segue Conventional Commits. -->

## What changes and why / O que muda e por quê

<!-- The problem, the solution and the design decisions. Link the issue: Closes #123
     O problema, a solução e as decisões de desenho. -->

## How it was tested / Como foi testado

<!-- Tests added and what you ran: make test, go test -race ./...
     Testes adicionados e o que você rodou. -->

## Checklist

- [ ] Title in Conventional Commits format / Título no formato Conventional Commits
- [ ] New tests for the new behavior; `make test` passes / Testes novos para o comportamento novo; `make test` passa
- [ ] New routes declare the permission (`read`/`deploy`/`write`), validate input and do not expose secrets / Rotas novas declaram a permissão, validam a entrada e não expõem segredos
- [ ] Every user-facing text I changed exists in English and Portuguese (UI, docs, site, installer, CLI) / Todo texto visível que alterei existe em inglês e em português
- [ ] New dependencies have a license compatible with Apache-2.0 and are in `THIRD_PARTY_NOTICES.md` / Dependências novas têm licença compatível com Apache-2.0 e estão em `THIRD_PARTY_NOTICES.md`
- [ ] I did not edit the version or `CHANGELOG.md` (release-please handles it) / Não editei versão nem `CHANGELOG.md` (o release-please cuida)
