When a deploy fails, Pier tries to explain the error at the top of the log, with the suggested fix. This page gathers the most common cases, including those that happen outside a deploy.

## Installation and access

### I lost the setup code

It is in the container log:

```sh
docker compose logs pier | grep setup_code
```

The code is only valid until someone creates the administrator account.

### I cannot open the panel

The panel listens only on `127.0.0.1:8700` of the server, on purpose. From your computer, use a tunnel:

```sh
ssh -L 8700:127.0.0.1:8700 usuario@IP-DO-SERVIDOR
```

Then open `http://localhost:8700`. To access it without a tunnel, [put the panel on a domain with HTTPS](/en/docs/installation/#5-put-the-panel-on-a-domain-with-https).

### "Could not create /data/pier"

Your user cannot write to that folder:

```sh
sudo mkdir -p /data/pier && sudo chown "$USER" /data/pier
./install.sh
```

### "Docker is not responding"

The Docker service is stopped or your user does not have access to the socket:

```sh
sudo systemctl start docker
sudo usermod -aG docker "$USER"   # then sign out and back in to SSH
```

## Domains and certificates

### The certificate is not issued

Check, in this order:

1. **DNS:** `dig +short seu.dominio.com` must return the server's IP. An `AAAA` record with a wrong IPv6 address also prevents the certificate: delete it if the server has no IPv6.
2. **Ports:** 80 and 443 open in the server's firewall **and** in the VPS provider's panel.
3. **Proxy:** in **Infrastructure › server › Proxy**, it is running and has an email configured.
4. **New deploy:** after fixing the DNS, deploy again.

The proxy logs, on the same screen, show Let's Encrypt's response. Let's Encrypt limits attempts per domain. If you tried too many times, wait an hour.

### The proxy does not start: port in use

Another program is using port 80 or 443. The message says which one. If it is a Caddy, use the [Caddy in front](/en/docs/servers-and-domains/#i-already-have-caddy-on-the-server) mode. If it is an nginx or Apache installed on the system, stop and disable it:

```sh
sudo systemctl disable --now nginx    # or apache2
```

### The domain stopped responding after upgrading

Since 0.5.0, the proxy only routes resources deployed by the new version. Recreate the proxy and deploy the resources that have a domain again. See the [release notes](/en/docs/upgrading/#release-notes-for-0-5-0).

## Deploys

### The application never becomes ready: wrong port

The configured port is not the port the program listens on. Pier compares the two and shows the correct one, with a button to fix it. If the program reads `PORT`, set that variable to the same number.

### The application listens only on 127.0.0.1

Inside the container, `localhost` does not receive the proxy's traffic. Make the program listen on `0.0.0.0`:

| Language | How |
|---|---|
| Node.js | `app.listen(port, "0.0.0.0")` |
| Python | `uvicorn main:app --host 0.0.0.0` or `gunicorn -b 0.0.0.0:8000` |
| Go | `http.ListenAndServe(":8080", nil)` |
| Ruby | `rails server -b 0.0.0.0` |
| PHP | `php -S 0.0.0.0:8000` |
| Java (Spring) | `server.address=0.0.0.0` |

### Exited with code 137 (out of memory)

The container went over the memory limit or the machine ran out of memory. Raise the application's limit, reduce its usage or, if it happened during the build, raise `PIER_BUILD_MEMORY` in Pier's `.env`.

### "No space left on device"

The disk is full. In **Infrastructure › server › Maintenance**, clean up images and build cache. Also check the volumes and backups on the server. Pier refuses a build when there is less free space than `PIER_BUILD_MIN_FREE_GB`.

### "Nixpacks not found" or "Buildx not found"

- **Buildx** is required for code builds. Install the `docker-buildx-plugin` package.
- **Nixpacks** is only needed with the Nixpacks build pack. Install it on the server or use a Dockerfile.

### Private repository: "Permission denied (publickey)"

The deploy key is not registered in the repository, or the application uses the `https://` address instead of `git@…`. In **Security**, view the public key and register it as a *deploy key*.

### "Environment frozen"

The target environment has **Freeze deployments** turned on. Unfreeze it in **Projects**, with a reason, or deploy to another environment.

### "all predefined address pools have been fully subnetted"

The server's Docker ran out of IP ranges for new networks. Each stack uses at least one. Remove unused networks with `docker network prune`, or widen the ranges in `/etc/docker/daemon.json` and restart Docker:

```json
{ "default-address-pools": [{ "base": "10.200.0.0/16", "size": 24 }] }
```

## Servers

### "The server key changed"

The server was reinstalled or its IP moved to another machine. If you know the reason, reset the key in **Infrastructure › server › Maintenance**. If you do not know, investigate first: it may be another machine pretending to be your server.

### The webhook does not trigger a deploy

- Pier must be reachable from the internet, with the **Public URL** filled in under **Settings**.
- The pushed branch must be the application's branch.
- With **Watched paths**, the push must change a file inside them.
- With a tag pattern, only tags that match trigger a deploy. Branch pushes are ignored.

On the GitHub or GitLab webhooks page, Pier's response explains why nothing was done.

## Still having trouble?

Collect the deploy log and the output of `docker compose logs --tail 200 pier` and open an issue at [{{repo}}/issues]({{repo}}/issues). Before publishing, remove passwords, tokens and internal domains.
