With Pier installed, let's put an application online in a few minutes: first a ready-made image, with no build, and then your own code.

## Before you start

- The panel opens and you are signed in. See the [installation](/en/docs/installation/).
- To use a domain, the server's **proxy** must be started in **Infrastructure › local server › Proxy**.
- To get HTTPS, create an `A` record in DNS for the application's domain pointing to the server's IP. Example: `ola.seudominio.com`.

## 1. Create a project

In **Projects**, click **New project** and give it a lowercase name, such as `meu-site`. The project already comes with the `production` environment.

## 2. Publish a ready-made image

This first deploy uses a small image that only answers with information about the request. It proves that the server, the proxy and the DNS are right before your code gets involved.

1. In the project, click **New resource** and choose **Docker image**.
2. In **Image**, enter `traefik/whoami:v1.11`.
3. In **Application port**, enter `80`. The **Suggest image port** button finds the port for you.
4. In **Domain**, enter `https://ola.seudominio.com`.
5. Click **Create and deploy**.

The panel opens the live deploy log. When **Finished** appears, open the domain: the page shows the container name and the request headers.

> If the DNS does not point to the server yet, the deploy ends on HTTP and the log says which record to create. After the DNS propagates, click **Deploy** again to issue the certificate.

## 3. Publish your code

Repeat step 2, choosing **Git application** instead of **Docker image**:

| Field | What to enter |
|---|---|
| Repository | `https://github.com/voce/projeto` for public repositories, or `git@github.com:voce/projeto.git` for private ones |
| Branch | The branch to deploy. Example: `main` |
| How to build | **Dockerfile** if the project has one; **Nixpacks** to detect the language automatically; **Static site** for HTML, CSS and JS without a server |
| Application port | The port your program listens on inside the container. Example: `3000` |
| Domain | `https://app.seudominio.com` |

Two details avoid most problems on the first deploy:

- **Listen on `0.0.0.0`, not on `localhost`.** A program that listens only on `127.0.0.1` does not receive the proxy's traffic. Pier detects this and shows how to fix it in your language.
- **The form's port is the program's port.** If the program reads the `PORT` variable, set `PORT` to the same value in **Variables**.

For private repositories, use the `git@…` address, choose a **deploy key** and register the public key shown by Pier as a *deploy key* on GitHub, GitLab or Gitea.

> **An example that works:** `https://github.com/heroku/node-js-getting-started` with **Nixpacks**, port `3000` and the variable `PORT=3000`. Nixpacks must be installed on the server.

## 4. Connect a database

1. In the same project, click **New resource**, choose **Database**, then **PostgreSQL**, and name it `principal`.
2. In the application, open **Variables** and use **Connect to a database**. Or write the reference by hand:

   ```sh
   DATABASE_URL=${{principal.url}}
   ```

3. Click **Deploy**. On every deploy, Pier replaces the reference with the current connection URL. The password is never written in the application.

## 5. Automatic deploy on every push

In the application, open the **Automation** tab, copy the webhook URL and secret and register both in the repository:

- **GitHub:** Settings › Webhooks › Add webhook, with *Content type* `application/json`.
- **GitLab:** Settings › Webhooks.
- **Gitea or Forgejo:** Settings › Webhooks.

From then on, every push to the configured branch triggers a deploy. Pier must be reachable from the internet, preferably with the [panel on HTTPS](/en/docs/installation/#5-put-the-panel-on-a-domain-with-https).

## What now?

- If something went wrong, the failed deploy shows the explained error at the top of the log. See also [Troubleshooting](/en/docs/troubleshooting/).
- Learn what else can be configured in [Applications](/en/docs/applications/).
- Make the [instance backup](/en/docs/backups/) if you have not yet.
