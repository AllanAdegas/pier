Pier controls whole servers. It is worth understanding what each access allows and closing what does not need to stay open.

## What Pier can do

Pier uses the Docker socket of the local server and reaches the other servers by SSH. In practice, **whoever administers Pier administers the servers**. So:

- keep the panel behind HTTPS and do not open port 8700;
- use strong passwords, with 10 characters or more;
- give access only to people who would already have root access to the servers.

Access is controlled by [project and instance roles](/en/docs/team-and-access/). A token's capability is also limited by the user's current role. Accounts with privileged operations over Docker and servers must remain trusted.

## API tokens

In **Security › Keys and tokens**, section **API tokens**, create tokens for scripts, CI and AI. Each token has one permission:

| Permission | Can |
|---|---|
| `read` | View resources, states and logs. Does not see secrets |
| `deploy` | Everything `read` does, plus deploy, restart and operate |
| `write` | Configure within the scope allowed by the role. Authorized Docker/host operations can be equivalent to root |

Give each use the minimum it needs. A CI pipeline that only deploys needs `deploy`, not `write`. The token value appears only once, when it is created.

## Protected and frozen environments

In **Projects**, each environment has two protections:

- **Protect deletions:** to delete an application, a database, a stack, the environment or the project, you must type the **environment name**. It prevents deleting production thinking it was staging.
- **Freeze deployments:** no deploy enters the environment, by any path: panel, webhook, CLI, MCP, tag or promotion. Use it in windows when nothing can change.

To unfreeze, you must write a reason, which is recorded in the audit log.

In the CLI, a deletion in a protected environment requires `--confirm-environment <name>`.

## Audit log

**Security › Audit log** holds the record of who did what: sign-ins, deploys, configuration changes, deletions and actions by token or by AI. Sensitive data appears masked. The record keeps 90 days.

## Secrets

- Passwords, SSH keys, tokens and variables are stored **encrypted** in Pier's database.
- The key is in `secret.key`, in the data directory, or in `PIER_SECRET_KEY`. **Back it up:** the [instance backup](/en/docs/backups/) already includes it.
- Variable values appear masked in deploy logs.
- Private keys are never returned by the API.

## Good practices

- [ ] Panel on HTTPS, with the public URL in **Settings**.
- [ ] Port 8700 closed in the firewall.
- [ ] Instance backup scheduled and copied outside the server.
- [ ] Tokens with the lowest possible permission, one per use.
- [ ] Production with **Protect deletions** on.
- [ ] Databases without a public port, unless there is a real need.
- [ ] Catalog services that ask for special powers reviewed before creating.
- [ ] Pier and Docker up to date. See [Upgrading Pier](/en/docs/upgrading/).

## Report a vulnerability

Did you find a security flaw? Do not open a public issue. Write to the maintainer through the project's GitHub, at [{{repo}}]({{repo}}), describing the problem and how to reproduce it.
