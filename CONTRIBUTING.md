# Contribuindo com o Pier

Obrigado por querer ajudar. O Pier é software livre sob [Apache-2.0](LICENSE) e foi pensado para continuar andando **sem depender de uma pessoa**: o CI decide se uma mudança é segura, o release-please publica as versões e o Dependabot cuida das dependências. Veja como as decisões são tomadas em [GOVERNANCE.md](GOVERNANCE.md). Ao participar, você concorda com o [Código de Conduta](CODE_OF_CONDUCT.md).

*English summary at the end.*

## Antes de começar

- **Bug ou ideia:** abra uma issue pelos formulários (bug, funcionalidade). Dúvidas de uso vão para as **Discussions**, não para issues.
- **Vulnerabilidade:** nunca em issue pública. Veja [SECURITY.md](SECURITY.md).
- Mudanças grandes: abra uma issue ou discussão antes, para alinhar o desenho e evitar retrabalho.

## Ambiente de desenvolvimento

Requisitos: Go 1.27.4+, Deno 2.9.5+ e, para o que mexe com containers, Docker com o plugin compose.

```sh
git clone https://github.com/AllanAdegas/pier.git && cd pier
make dev-api      # backend em 127.0.0.1:8700, dados em ./.data
make dev-web      # interface com recarga em :5173 (em outro terminal)
make run          # ou: compila tudo num binário e inicia em :8700
```

O painel pede o código de configuração que aparece no log (`setup_code=...`) para criar a primeira conta.

## Testes

```sh
make test         # go vet, go test, typecheck e testes da interface
go test -race ./...                       # o mesmo que o CI roda
cd web && deno task typecheck && deno task test
cd site && deno task test
```

Regras:

- Todo comportamento novo vem com teste. Backend: `_test.go` com os fakes existentes (`internal/platform/executor`, SQLite temporário, `httptest`). Nenhum teste fala com a rede, com a nuvem ou com o GitHub de verdade.
- Textos da interface são em português e precisam de tradução no catálogo inglês (`web/src/i18n/`, `internal/platform/i18n/`); os testes de catálogo falham se faltar ou sobrar uma entrada.
- Identificadores, arquivos e comentários em inglês.
- Segurança primeiro: toda rota declara a permissão que exige (`read`, `deploy`, `write`), valida a entrada e nunca devolve nem registra segredos.
- Dependências novas: só licenças compatíveis com Apache-2.0 (MIT, BSD, Apache-2.0, ISC), versão exata, registradas em `THIRD_PARTY_NOTICES.md`.

## Commits e Pull Requests

O repositório usa [Conventional Commits](https://www.conventionalcommits.org/pt-br/). Como os merges são *squash*, **o título do PR vira o commit**, e o CI o valida:

```
feat(api): adiciona previews por pull request
fix(deploy): evita rollback duplicado
docs: explica o backup externo
```

Tipos: `feat`, `fix`, `perf`, `refactor`, `docs`, `test`, `build`, `ci`, `chore`, `revert`. Use `!` após o tipo (ou `BREAKING CHANGE:` no corpo) para quebra de compatibilidade.

Para ser aceito, um PR precisa: CI verde (Go, interface, site, gitleaks, govulncheck, CodeQL), título convencional e o formulário do PR preenchido.

## Como os releases acontecem (sem ninguém apertar botão)

1. Cada merge em `main` atualiza um PR de release (`chore(main): release X.Y.Z`) com o `CHANGELOG.md` e a nova versão em `internal/version/version.go`, calculados dos commits.
2. Quando esse PR é mesclado, o release-please cria a tag `vX.Y.Z` e a Release no GitHub.
3. O workflow publica a imagem multi-arquitetura `ghcr.io/allanadegas/pier:X.Y.Z` (e `latest`) com SBOM e atestado de procedência, e atualiza o site no GitHub Pages.
4. Quem usa o Pier vê o aviso de atualização no painel e roda o comando de upgrade mostrado nele.

Você nunca edita a versão nem o `CHANGELOG.md` à mão.

## English summary

Pier is Apache-2.0 and is designed to keep working without a dedicated maintainer: CI gates merges, release-please publishes releases, Dependabot updates dependencies (see [GOVERNANCE.md](GOVERNANCE.md)).

- **Setup:** Go 1.27.4+, Deno 2.9.5+, Docker (optional). `make dev-api` + `make dev-web`, or `make run`.
- **Tests:** `make test` (go vet, `go test`, web typecheck and tests); CI also runs `go test -race`, site tests, gitleaks, govulncheck and CodeQL. New behaviour needs tests; tests never touch real networks or clouds. UI text is Portuguese with an English catalog entry (catalog tests enforce it); code, comments and identifiers are English.
- **Commits:** Conventional Commits; PRs are squash-merged and the PR title is the commit message (checked by CI).
- **Releases:** automatic. Merging the release PR opened by release-please tags `vX.Y.Z`, publishes `ghcr.io/allanadegas/pier:X.Y.Z` (amd64 + arm64, SBOM, provenance) and the site. Never bump versions or edit `CHANGELOG.md` by hand.
- **Security issues:** report privately, see [SECURITY.md](SECURITY.md).
