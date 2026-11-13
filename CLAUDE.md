# Pier: notes for AI coding agents and humans

Pier is a self-hosted dashboard for deploying applications, databases and compose stacks to your own
servers (Go backend, SolidJS interface, static documentation site). Read [README.md](README.md) first and
[CONTRIBUTING.md](CONTRIBUTING.md) for the full rules.

## Layout

- `cmd/pier/`: entry point and dependency wiring.
- `internal/`: one Go package per context (application, database, stack, server, engine, cluster, mcp, ...).
  `internal/platform/i18n/` holds the English catalog for API messages.
- `web/`: SolidJS panel. Texts are in `web/src/i18n/` (Portuguese in code, English in `en*.ts`).
- `site/`: documentation website (Deno). Portuguese pages in `site/content/docs/<pt-slug>.md`, English pages in
  `site/content/docs/en/<en-slug>.md`, titles in `site/src/pages.ts`.
- `tools/`: repository checks (`tools/openapi` keeps `api/openapi.json` in sync, `tools/docpairs` checks
  the language pairs).
- Root documents: `README`, `CONTRIBUTING`, `CODE_OF_CONDUCT`, `GOVERNANCE`, `SECURITY`, `RELEASE_NOTES`,
  each as `NAME.md` (English) and `NAME.pt-BR.md` (Portuguese). `install.sh` is the Docker installer.
- The `pierctl` CLI lives in a separate repository, `pier-cli`.

## The language rule

Every user-facing text added or changed must exist in English and in Portuguese (pt-BR) **in the same
change**: panel UI and API messages, website pages, documentation, installer and CLI messages, README and
the community files. Portuguese is the source language in code (`t("Texto em português")`) with an English
catalog entry; documents exist as pairs. When a document quotes a button, tab or field, use the exact label
from the panel in that language (search `web/src/i18n/en*.ts`). CI fails when a pair or a catalog entry is
missing. Details: the "Languages / Idiomas" section of [CONTRIBUTING.md](CONTRIBUTING.md).

## Rules

- Never deploy to production, and never touch a production server, from an agent session.
- Commit style: [Conventional Commits](https://www.conventionalcommits.org/) (`feat(api): ...`,
  `fix(deploy): ...`, `docs: ...`). The PR title becomes the squash commit. Never edit the version or
  `CHANGELOG.md` by hand; release-please owns them.
- Tests are required: every new behavior ships with a test, and no test talks to the real network, a cloud
  or GitHub. Run `make test`; CI also runs `go test -race ./...`, the web and site tests.
- Security first: each route declares the permission it needs (`read`, `deploy`, `write`), validates its
  input and never returns or logs secrets.
- New dependencies need an Apache-2.0-compatible license, an exact version and an entry in
  `THIRD_PARTY_NOTICES.md`.
- Identifiers, files and code comments are in English.
