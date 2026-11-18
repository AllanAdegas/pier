[English](CONTRIBUTING.md) | Português

# Contribuindo com o Pier

Obrigado por querer ajudar. O Pier é software livre sob [Apache-2.0](LICENSE) e foi pensado para continuar andando **sem depender de uma pessoa**: o CI decide se uma mudança é segura, o release-please publica as versões e o Dependabot cuida das dependências. Veja como as decisões são tomadas em [GOVERNANCE.md](GOVERNANCE.pt-BR.md). Ao participar, você concorda com o [Código de Conduta](CODE_OF_CONDUCT.pt-BR.md).

## Antes de começar

- **Bug ou ideia:** abra uma issue pelos formulários (bug, funcionalidade). Dúvidas de uso vão para as **Discussions**, não para issues.
- **Vulnerabilidade:** nunca em issue pública. Veja [SECURITY.md](SECURITY.pt-BR.md).
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
make docpairs     # só a verificação dos pares inglês/português
make install-test # Docker de verdade: instalação limpa, atualização a partir da última release, rollback (constrói a imagem, baixa a antiga; alguns minutos)
```

`make install-test` roda `tools/installtest/` no seu Docker local com containers, rede e dados chamados `pier-v08-installtest-*`, e remove tudo no fim, também em caso de falha. No CI, o workflow `Install test` o executa no Ubuntu 24.04 (instalação limpa, atualização a partir da release publicada, rollback) e roda instalação limpa, atualização e rollback num container Debian 12 amd64, quando `install.sh`, `compose*.yaml`, o `Dockerfile`, as migrations ou os scripts mudam, toda semana e sob demanda. O job Debian usa `docker run --network host` explícito e o Docker do runner pelo socket em vez de um daemon aninhado, então prova o userland do Debian (dash, CLI do Docker instalada por apt), não o kernel nem o engine Docker de um host Debian. Nunca precisa de segredos.

Regras:

- Todo comportamento novo vem com teste. Backend: `_test.go` com os fakes existentes (`internal/platform/executor`, SQLite temporário, `httptest`). Nenhum teste fala com a rede, com a nuvem ou com o GitHub de verdade.
- Todo texto visível existe em português e em inglês (veja [Idiomas / Languages](#idiomas--languages)); os testes de catálogo e de pares falham se faltar algo.
- Identificadores, arquivos e comentários em inglês.
- Segurança primeiro: toda rota declara a permissão que exige (`read`, `deploy`, `write`), valida a entrada e nunca devolve nem registra segredos.
- Dependências novas: só licenças compatíveis com Apache-2.0 (MIT, BSD, Apache-2.0, ISC), versão exata, registradas em `THIRD_PARTY_NOTICES.md`.

## Idiomas / Languages

O Pier existe em português (pt-BR) **e** em inglês: interface do painel, site, documentação, instalador e CLI. Toda mudança que cria ou altera texto visível a quem usa o Pier entrega os dois idiomas **no mesmo PR**. O português é o idioma-fonte no código (`t("Texto em português")`); o inglês vem de catálogos e de arquivos pareados.

| O que | Onde vive | O que o teste exige |
|---|---|---|
| Interface do painel | Português no código (`t("...")`); inglês em `web/src/i18n/en*.ts` | `cd web && deno task test`: falha se faltar ou sobrar uma entrada |
| Mensagens da API, notificações e e-mails | Português no código; inglês em `internal/platform/i18n/english*.go` | `go test ./internal/platform/i18n/...` |
| Site e guias | `site/content/docs/<slug-pt>.md` e `site/content/docs/en/<slug-en>.md`; títulos em `site/src/pages.ts` | `cd site && deno task test`: mesmas páginas, mesmos títulos de seção, mesmos blocos de código |
| README, CONTRIBUTING, CODE_OF_CONDUCT, GOVERNANCE, SECURITY, RELEASE_NOTES | `NOME.md` (inglês, principal) e `NOME.pt-BR.md` (português), com link para o outro na primeira linha | `go test ./tools/docpairs/...`: falha se faltar o par, o link, ou se o inglês tiver texto em português |
| Instalador | `install.sh`: cada mensagem tem uma versão `pt` e uma `en` | `go test ./tools/docpairs/...` |
| CLI `pierctl` | repositório [pier-cli](https://github.com/AllanAdegas/pier-cli): português no código, inglês no catálogo | teste de completude do catálogo, no pier-cli |

`CHANGELOG.md` (gerado pelo release-please) e `THIRD_PARTY_NOTICES.md` não são traduzidos.

**Como adicionar uma página de documentação:**

1. Escreva `site/content/docs/<slug-pt>.md` e `site/content/docs/en/<slug-en>.md`. Use os mesmos marcadores (`{{version}}`, `{{tag}}`, `{{repo}}`, `{{cliRepo}}`), os mesmos blocos de código (comandos não se traduzem; só comentários e texto) e a mesma estrutura de títulos `##`/`###`, na mesma ordem.
2. Registre as duas em `site/src/pages.ts` (título e descrição em cada idioma).
3. Nos links internos, a página em inglês aponta para `/en/docs/<slug-en>/#ancora-em-ingles`; a âncora é o título em inglês, em minúsculas e com hífens.
4. Rótulos de botões, abas e campos citados devem ser idênticos ao texto da interface em cada idioma (procure o texto em `web/src/i18n/en*.ts`).
5. Rode `cd site && deno task test`.

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
