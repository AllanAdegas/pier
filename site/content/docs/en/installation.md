This guide installs the beta version **{{version}}** on a Linux server. At the end you will have the panel running, with your administrator account and, if you want, a domain with HTTPS.

## What you need

| Item | Minimum | Recommended |
|---|---|---|
| Server | 64-bit Linux (x86_64 or ARM64) | Ubuntu 24.04 LTS or Debian 12 |
| Memory | 1 GB | 2 GB or more if you build code on the server |
| Disk | 20 GB | 40 GB or more; Pier warns you below 10 GB free |
| Docker | Docker Engine 24 with the compose 2.20 plugin | A version supported by the project, with Buildx |
| Access | SSH with a user that can use `sudo` | — |
| Domain | Optional | A subdomain for the panel, such as `pier.seudominio.com` |

On its own, Pier uses little: about 16 MB of RAM when idle. What uses memory is the builds and the applications you publish.

> **Can you try it on your own computer?** Yes. With Docker Desktop, the same commands can be used in a local environment. On macOS the default is `~/.pier`; on Linux, `/data/pier`. But push webhooks cannot reach a computer without a public address, and nothing runs while it is turned off. For real use, install it on a server.

## 1. Install Docker

If the server does not have Docker yet, the official script installs everything, including the compose and Buildx plugins:

```sh
curl -fsSL https://get.docker.com | sudo sh
```

Check that all three pieces respond:

```sh
docker --version
docker compose version
docker buildx version
```

To use Docker without `sudo`, add your user to the `docker` group and sign in to SSH again:

```sh
sudo usermod -aG docker "$USER"
```

> The `docker` group gives access equivalent to root on the machine. Only add users you trust.

## 2. Open the ports

In the server firewall (and in the VPS provider's panel, if there is one), keep these open:

| Port | What it is for |
|---|---|
| 22 | SSH |
| 80 | HTTP and certificate validation |
| 443 | HTTPS for your applications and the panel |

**Do not open port 8700.** The panel listens only on `127.0.0.1` and should be reached through an SSH tunnel or, later, through the domain with HTTPS.

On Ubuntu, with `ufw`:

```sh
sudo ufw allow OpenSSH
sudo ufw allow 80/tcp
sudo ufw allow 443/tcp
sudo ufw enable
```

## 3. Install Pier

Pier keeps its data in `/data/pier`. Create the folder with permission for your user:

```sh
sudo mkdir -p /data/pier
sudo chown "$USER" /data/pier
```

Download the beta version and run the installer:

```sh
git clone --branch {{tag}} --depth 1 {{repo}}.git
cd pier
./install.sh
```

### If the image download fails

The repository and the GHCR image are public: the clone and `docker pull` work without signing in. If the pull fails (restricted network, blocked registry), build the image in the checkout itself:

```sh
PIER_BUILD=1 ./install.sh
```

Use this same option on reinstalls while you depend on a local build. Do not put tokens in the `.env` or in the repository URL.

The script:

1. checks Docker;
2. creates the `.env` file with the settings;
3. pulls `ghcr.io/allanadegas/pier:{{version}}` (it does not build by default);
4. starts the `pier` container;
5. waits for the panel to respond.

At the end, it shows something like this:

```text
==> Pier is up: http://127.0.0.1:8700
    Setup code: K7QM-2XPA-9DTE
    Use it to create the administrator account on your first visit.
```

Keep the **setup code**. Without it nobody can create the first account, not even someone who finds the panel exposed by mistake.

## 4. Open the panel and create your account

The panel is at `127.0.0.1:8700` **on the server**. From your computer, open an SSH tunnel:

```sh
ssh -L 8700:127.0.0.1:8700 usuario@IP-DO-SERVIDOR
```

With the tunnel open, go to [http://localhost:8700](http://localhost:8700) in your browser. Enter the setup code, your name, your email and a password of at least 10 characters. Done: you are the administrator.

If you lost the code, it is also in the log:

```sh
docker compose logs pier | grep setup_code
```

## 5. Put the panel on a domain with HTTPS

Recommended as soon as the account exists. You stop depending on the tunnel, and webhooks and alerts get a public address.

1. **DNS.** At your domain provider, create an `A` record for `pier.seudominio.com` pointing to the server IP. If the server has IPv6, also create the `AAAA` record. If it does not, do not create it.
2. **Proxy.** In the panel, open **Infrastructure › local server › Proxy**. Enter the email for Let's Encrypt and click **Start**.
3. **Panel domain.** On the server, edit the `.env` in the `pier` folder and uncomment the line:

   ```sh
   PIER_DOMAIN=pier.seudominio.com
   ```

   Then run the installer again. It does not delete anything:

   ```sh
   ./install.sh
   ```

4. **Public URL.** In **Settings**, fill in **Pier's public URL** with `https://pier.seudominio.com`. This turns on HSTS and fixes the addresses Pier shows, such as the webhook ones.

In a minute or two, `https://pier.seudominio.com` opens the panel with a valid certificate. If it does not open, see [Troubleshooting](/en/docs/troubleshooting/#the-certificate-is-not-issued).

## 6. Follow the panel checklist

The first screen of the panel has a list of what is missing for the instance to be safe and useful:

- **Enable HTTPS for the panel**: step 5 above.
- **Create an instance backup**: do it soon and keep the passphrase outside the server. See [Backups and restore](/en/docs/backups/).
- **Configure notifications**: Discord, Slack, Telegram, email or webhook, to receive alerts.
- **Make your first deployment**: follow the [getting started](/en/docs/getting-started/) guide.

## `.env` settings

The installer creates the `.env` from `.env.example`. The most used options:

| Variable | Default | What it is for |
|---|---|---|
| `PIER_DATA_DIR` | `/data/pier` | Where Pier's database, the key and the local server's files are kept. Back up this folder |
| `PIER_BIND` / `PIER_PORT` | `127.0.0.1` / `8700` | Address and port of the panel on the server |
| `PIER_DOMAIN` | empty | Panel domain with HTTPS (step 5) |
| `PIER_SECRET_KEY` | generated in `secret.key` | Passphrase that encrypts the stored secrets. **Losing it makes the secrets unreadable** |
| `PIER_DEPLOY_CONCURRENCY` | `2` | Simultaneous deployments, of different applications |
| `PIER_DEPLOY_TIMEOUT` | `1h` | Maximum time of a deployment |
| `PIER_BUILD_CPUS` / `PIER_BUILD_MEMORY` | `1` / `2g` | CPU and memory limit of each build |
| `PIER_BUILD_MIN_FREE_GB` | `2` | Minimum free space to start a build |
| `PIER_BUILD_CACHE_GB` | `10` | Maximum size of the build cache |
| `TZ` | `UTC` | Time zone for tasks and scheduled backups. Example: `America/Sao_Paulo` |

After changing the `.env`, run `./install.sh` again to apply it.

## Everyday commands

Run these in the `pier` folder, on the server:

```sh
docker compose logs -f pier     # follow the log
docker compose restart pier     # restart the panel
docker compose down             # stop the panel
./install.sh                    # start again or apply .env changes
```

Stopping the panel does **not** take down what it published: applications, databases and the proxy keep running. Only deployments, scheduled backups and alerts stop until the panel is back.
