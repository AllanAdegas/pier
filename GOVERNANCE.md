English | [Português](GOVERNANCE.pt-BR.md)

# Governance

Pier was designed to **keep working without a dedicated maintainer**: where other projects depend on a person, here there is automation. This document says how things happen.

## Principles

1. **CI is the first reviewer.** Nothing enters `main` without the required checks green: Go (`vet` + `test -race`), interface (types + tests), site, gitleaks, govulncheck, CodeQL and a conventional PR title.
2. **Releases are automatic.** release-please turns Conventional Commits into a version, `CHANGELOG.md` and a tag; the image and the site are published by the workflows. Nobody bumps versions by hand.
3. **Dependencies update themselves.** Dependabot opens grouped weekly PRs; *patch* and *minor* updates go in by auto-merge when CI passes. *Major* updates wait for human review.
4. **Decisions are written down.** Discussions, issues and PRs are the record; relevant design changes become an issue labeled `design` or a document in the repository.
5. **Forking is a right, not a threat.** The license is Apache-2.0 and the image, the installer and the workflows work in any fork (swap the owner in `ghcr.io/<owner>/pier` and in the workflows). If the project stops, the community can carry on.

## How a change gets in

| PR type | Merge rule |
|---|---|
| Dependabot, *patch* or *minor* | Auto-merge when all required checks pass |
| Dependabot, *major* | Review by a person with write permission |
| Release PR (release-please) | Merge by a person with write access, or automatic if the repository sets the variable `RELEASE_AUTOMERGE=true` |
| All other PRs | Green checks **and** one approval from someone with write permission (who is not the author) |

Recommended branch protection on `main`: require a PR, the required checks, linear history (squash) and allow auto-merge.

PRs with no review response for 14 days may be merged by anyone with write access, if CI is green and the change is small and covered by tests.

## Roles

- **Contributor:** anyone who opens an issue, discussion or PR.
- **Maintainer:** whoever has write permission. Reviews PRs, merges, triages issues and drives releases when needed. May be listed in a `CODEOWNERS` file (optional; only enable it if there are enough people, so PRs do not get stuck waiting for someone absent).
- **Repository owner:** administers the GitHub settings and permissions; decides as a last resort.

### How to become a maintainer

After **about five meaningful merged PRs** (or equivalent contribution in review and triage) over at least two months, and conduct aligned with the [Code of Conduct](CODE_OF_CONDUCT.md), any maintainer may nominate you in a governance issue. With the approval of another maintainer (or of the owner, if there is no other), you receive write permission. Maintainers inactive for 12 months return to being contributors, with no prejudice, and may come back whenever they want.

## Security

Vulnerabilities follow [SECURITY.md](SECURITY.md): private report through GitHub, fix in an automatic release.

## Changes to this document

By PR, with the approval of the owner and of at least one maintainer, if there is one.
