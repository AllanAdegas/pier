## Compose from Git

In **New resource**, choose **Compose from Git**. Enter the repository, the branch or pinned tag, the base directory and the compose path. For private SSH access, select a key that is authorized in the repository.

On the stack's **Git** tab, check the latest commit and set up automatic deploy, watched paths and a signed webhook. A pinned tag ignores pushes. A push during a deploy can be refused with a conflict, without going into a queue: check the deployed commit and try again.

Pier checks compose paths, includes and extends inside the checkout. If it detects new powers over the host, the deploy stops and shows pending risks. Review the diff before accepting privileged access, the Docker socket, network, ports or host folders. Accepting a risk allows it to run; it does not make it safe.

The checkout is replaced on every deploy. Data written to repository folders can be lost: use named volumes and [backups](/en/docs/backups/). Compose builds use a queue and a disk check, but they do not get the same CPU and memory limits as application builds.

## GitHub App

In **Settings**, the integration lets you register your own GitHub App, list installations and repositories, and link a repository to an existing resource.

1. Create the App on GitHub with read access to contents and write access to commit statuses and pull requests (metadata has implicit read access).
2. Install it only on the repositories that are needed. Do not grant write access to Actions, secrets or workflows.
3. Set up an HTTPS webhook at `/webhooks/git-provider/github`, with the events `installation` and `installation_repositories`, and a random secret of at least 32 characters.
4. In Pier, enter the App ID, the RSA PEM private key and the same secret. After you save, the credential fields are cleared.
5. List the installations and repositories and select the resource by type and ID.

The integration defaults to `api.github.com`. A manually configured GitHub App can use a GitHub Enterprise API URL; the manifest flow remains specific to GitHub.com. Binding global Git credentials requires instance write access as well as write access to the resource. Project deployers can deploy an approved binding only while its source matches the bound repository. Authenticated clones refuse redirects and do not share credentials with other repositories, including submodules. Do not confuse a GitHub App with [SSO](/en/docs/team-and-access/): it authorizes access to repositories, not the sign-in of people.

### Link and follow

Use **Git integration** on the application or stack to choose an installed repository and click **Link repository**. The source must use the same HTTPS address that was selected. A link that is incompatible, suspended or without access is refused, without trying to clone publicly. **Unlink** goes back to the source's authentication method on the next deploy.

Linked applications receive pushes and pull requests through the App's webhook; do not also register the resource's individual webhook on GitHub. Keep the App's event subscriptions compatible with push and pull request. The tokens used to clone have read access to contents; the reporting uses separate write permissions. The tokens are temporary and must not be copied into application variables.

In applications, choose **Publish to the provider** to send commit statuses and PR comments. GitHub uses the App; GitLab, Gitea/Forgejo and Bitbucket Cloud use the token set up for the provider. Grant only the permissions needed on the repository. Stacks have a clone link, but not the same deploy reporting as applications. Reporting is best effort; a missing comment does not change the deploy result in Pier.

The deploy integration was exercised with simulated HTTP and test executors. This is not the same as validating permissions and webhooks against a real App: make a test PR before you depend on the reporting in production.

## Other providers

Applications and Git stacks show the webhook addresses and secrets in the automation area. Use the provider-specific address and check the signature/secret; events from a different branch or from a tag that is not selected must not publish the main application. Do not expose the secret in issues, screenshots or commands.

For builds on another machine, private registries and deploy to several destinations, follow [Registry and multiple servers](/en/docs/multi-server/).

For GitLab/Gitea/Forgejo instances on your own domain, choose the provider explicitly. The automatic suggestion recognizes only exact public hosts; names in the path, query or user part of the URL do not identify the provider.

## GitLab, Gitea and Bitbucket connections

In **Settings › Git**, open **Git connections**. Choose GitLab, Gitea / Forgejo or Bitbucket Cloud, enter the name, HTTPS address and **Access token**, then click **Save connection**. GitLab accepts subpaths; Bitbucket only accepts bitbucket.org, with an optional workspace. The token is validated and stored encrypted; responses never return it. To rotate it, fill **Access token** and click **Replace token** on the desired connection.

GitLab requires `api`. Gitea/Forgejo requires user read and repository write. Bitbucket requires repository read and webhook write; it accepts Bearer or `username:app-password`. Restrict credentials to the necessary repositories.

In **Git integration** on an existing application or stack, select **Git connection**, use **Search repositories**, and navigate with **Previous** and **Next**. The list shows the clone address and available branches. Configure that address and branch in the resource source before clicking **Link repository**. The link uses credentials only during cloning and refuses a different source.

Pier creates or updates one webhook per resource: `active` means success, `manual` means insufficient permission (configure the webhook in the automation area), and `failed` means another error. Linking again retries registration without duplicating the known webhook. Secret rotation updates the remote webhook. **Unlink** removes the webhook; temporary failures are retried in the background. Removing resources also schedules cleanup. **Remove connection** requires unlinking resources first.

Set the **Public URL** in **Settings** before you link a repository or rotate a managed webhook secret. The provider must be able to reach that address.

Linking a repository requires write access to both the instance and the resource project. Viewing or removing a link requires read or write access, respectively, to the resource project.

The same picker appears when creating applications and Git stacks. The selection is linked before the first deployment, provided the source address has not been changed.
