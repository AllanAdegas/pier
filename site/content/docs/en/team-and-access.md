Access combines the person's role and the token's capability. Having a `write` token does not raise the user's role. The interface hides unavailable actions, and the backend must refuse the same operation when it is called directly.

## Roles and scopes

| Role | Operations allowed by the role |
|---|---|
| Read only (`viewer`) | View authorized resources |
| Developer (`developer`) | View and deploy |
| Administrator (`admin`) | Configure resources and manage access within the limit of their own role |
| Owner (`owner`) | Also manage owners and reserved operations |

A **project** role applies only to that project. An **instance** role applies to all projects and to global resources, such as servers and settings. **Projects only** means the account has no global role.

When you upgrade an installation older than 0.6, **all existing accounts get the instance `owner` role**, keeping the administrative access they already had, including through tokens. Review and reduce these roles after you upgrade; do not assume the migration restricted old users. In a new installation, the first account is an owner. The instance and each project keep at least one owner; a person does not change their own role through this flow.

Instance owners set each person's **Instance role** in **Security › Users**. With **Projects only**, the person sees only the projects they are a member of. Grant global access only when needed. A project is not kernel isolation: resources that allow running code, privileged Docker or a terminal require trust in the operator.

## Project invitations

On the project page, click **Team** to open **Team and permissions**:

1. Enter the e-mail of an existing account and choose the smallest role that is needed.
2. Click **Create invitation** and copy the token, shown once. Send it through a private channel.
3. The person signs in with the account of that e-mail and pastes the token in **Security › Your account › Accept invitation**.
4. Check members and invitations. Invitations expire in 24 hours, are single-use and can be revoked.

The invitation does not create an account and does not silently replace the role of an existing member. Revoking the invitation prevents its use; to take away access that was already granted, remove the member or the matching role. Also review global access.

## OpenID Connect SSO

SSO authenticates accounts that were linked beforehand. An equal e-mail does not create a link and there is no automatic sign-up through SSO. Keep a local account with [recovery prepared](/en/docs/account-security/) so you do not lose access when you disable or change the provider.

The operator sets the variables `PIER_OIDC_ISSUERS` (exact HTTPS issuers separated by spaces) and `PIER_OIDC_CALLBACK` (exact HTTPS return URL) at installation. Pass them to the Pier process/container; having them in the shell is not enough if Compose does not forward them. The callback ends in `/api/v1/access/oidc/callback`.

In **Security › Users › OpenID Connect SSO**, the owner enters the allowed issuer (listed in `PIER_OIDC_ISSUERS`), Client ID, Client secret (entered on every save) and the callback equal to `PIER_OIDC_CALLBACK`. Under **Authorized identities**, the owner authorizes the exact issuer + subject pair for an existing **Pier user**. **Test sign-in with SSO** starts the sign-in to check the setup; **Disable SSO** asks for confirmation. HTTPS is required, including for the cookie that ties the return to the browser.

Optionally, set up the groups claim and mappings, one `group=role` line (`admin`, `developer` or `viewer`). Check the default role before saving: an instance role widens access to all projects. SSO never grants or removes `owner`. Without a mapping, keep the roles in Pier. To remove an identity, use **Revoke SSO identity** and review the account's sessions.

## Check before opening access

The 0.6 integration is in QA validation. Test with different accounts: read, deploy, write, trying to reach another project, global operations, terminal and MCP. Implemented services and screens are not the same as an external audit or as isolation between untrusted workloads.
