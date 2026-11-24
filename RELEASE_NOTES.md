English | [Português](RELEASE_NOTES.pt-BR.md)

# Pier 0.6.0 — beta

Self-hosted Apache-2.0 panel for small teams. This line remains in beta: the features and limits below should guide adoption. The local controls went through three rounds of QA with a bounded scope. See the limits below; artifact availability and public installation require verification after publication.

- TOTP/recovery authentication, password reset, sessions and OIDC SSO; instance/project roles and invitations.
- Previews isolated by identity, with forks off by default; Git stacks and GitHub App/provider integrations.
- Web terminal with a session and access revalidation; metrics history, alerts and log shipping.
- Backup of volumes and key-value engines, with availability recovery after interruption and a journal in migration 0024.
- Private registry, build server and multiple targets by digest; sequential rollout and compensation. External DNS/LB, no shared volumes or automatic rollout resume.
- Railpack, shared variables, definition cloning, automatic domains and runtime options; maintenance, restricted cleanup, provisioning and remote installation with confirmation.
- Expanded CLI, OpenAPI contract derived from 296 operations and a website with 24 guides. Automation of tests/releases/dependencies reduces recurring tasks, without removing the need for human review.
- 0.7 adds the English version: the panel, the website, the documentation, the installer and the CLI now ship in English and Portuguese (pt-BR).

## Upgrade and limits

**Search engines (after 0.9.0):** nothing Pier publishes is indexed unless marked. After the upgrade, the next deployment of each resource adds `X-Robots-Tag: noindex` to every domain not marked under **Settings › Search engines** or in the **Index in search engines** section of the resource. Mark the sites that must stay in search engines **before** redeploying them. The panel always answers with noindex.

Back up the control database and the key, and test the restore before upgrading. Migrations have no down. The 0.6 line changes access control: pre-existing users keep the instance owner role for compatibility; review the grants explicitly. Development databases that applied intermediate versions of the 0.6 migrations must be recreated; they are not a released upgrade path.

Dangerous binds/options require the instance owner and an explicit policy to write configuration, including clones. Previews do not inherit host privileges, but fork code is still untrusted code on the server; this is not a kernel sandbox. Fixed host ports imply an interruption during the switch; compensation does not undo external migrations/data. Metrics add up complete targets; overwritten legacy history is not recoverable. Log drains are best effort with accounted losses and an in-memory cursor.

Local evidence includes Linux/arm64 on Docker Desktop, fakes/SQLite/httptest and bounded rehearsals. It does not equal operation across all cloud providers, registries, architectures or the whole catalog in production. The GitHub/GHCR artifacts and the website should only be announced as available after real publication and verification.
