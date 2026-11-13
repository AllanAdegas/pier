English | [Português](SECURITY.pt-BR.md)

# Security policy

## Supported versions

We fix vulnerabilities only in the **latest minor version** (for example, while 0.6.x is the newest, only 0.6.x receives fixes). Pier is in beta (0.x): to get a fix, upgrade to the latest version. The panel notifies you when there is a new one (turn this off with `PIER_UPDATE_CHECK=0`).

| Version | Support |
|---|---|
| Latest minor version (`latest`) | Yes |
| Earlier ones | No |

## How to report a vulnerability

**Do not open a public issue.** Use GitHub's private reporting:

1. Open the repository's **Security** tab and click **Report a vulnerability** (or go straight to `https://github.com/AllanAdegas/pier/security/advisories/new`).
2. Describe the impact, the steps to reproduce, the affected version and, if you have one, a suggested fix.

Do not include real production secrets in the report.

## What to expect

- Acknowledgement within 7 days and an initial assessment within 14 days.
- The fix ships in a new version (automatic release), together with a *security advisory* crediting the reporter, if you wish.
- We ask for coordinated disclosure: wait for the fixed version before publishing details.

## Scope

The code in this repository is in scope: panel (API, interface, MCP), installer and image. Out of scope: vulnerabilities in the third-party images that Pier deploys, in Docker or in the server's operating system, and findings that require administrator access already granted (every Pier user is an administrator; a `write` token is equivalent to root on the servers, as documented).

## Supply chain

All CI Actions are pinned by SHA, CI runs `govulncheck`, `gitleaks` and CodeQL, and the published images carry an SBOM and a provenance attestation.
