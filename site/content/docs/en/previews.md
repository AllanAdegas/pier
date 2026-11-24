Previews are temporary copies of an application, used to review pull requests (or merge requests). Turn them on in the **Previews** tab of the application; they are off by default.

## Configure

1. Configure the signed webhook of the repository (GitHub, GitLab or Gitea/Forgejo).
2. Turn on previews and set a domain template with `{number}` in the host name, for example `pr-{number}.preview.exemplo.com`. `{branch}` is optional.
3. Configure DNS and the proxy for these domains. The template alone does not create DNS.
4. Set the maximum and the expiry period: the default is five previews and seven days; the limits are twenty and ninety days.
5. Open a test PR and follow the deploy and its logs in the tab. New commits update the preview by the SHA received in the webhook.

Each preview has its own identity, domain, images and volumes. Host ports, bind mounts, devices and added capabilities are dropped; privileged mode and host namespaces are not inherited, even after repository overrides. The current Docker policy is revalidated before the build, and revoking the policy blocks new previews of the configuration that was authorized before. This separates resources from the main application, but the code is still built in the server's Docker.

Previews never show up in search engines: every answer carries `X-Robots-Tag: noindex, nofollow`, even when the production domain is marked in **Index in search engines**.

## Variables and forks

PRs from the repository itself inherit the application variables, with preview-specific overrides. Review which secrets this code can read.

Forks are off by default. Turning them on runs third-party code; it is not a security sandbox. Forks only receive preview-specific variables, which can also hold secrets if you register them. Use disposable credentials and test data; never put production keys in these variables. Volume isolation does not protect the host against all malicious build code.

## Close and clean up

Closing or merging the PR removes the preview, even with previews turned off later or a frozen environment. The cleanup by expiry runs every hour. When the maximum is reached, new previews are refused; lowering it lets the oldest ones be removed. You can also remove or redeploy from the tab.

Freezing the environment blocks new deploys; cleanup is still allowed. If the provider does not allow fetching the SHA of a fork, the deploy fails. Check for leftovers when a cleanup fails on the server. See also [GitHub App](/en/docs/git-integrations/).
