Pier manages the server where it is installed and, over SSH, as many other servers as you want. In front of the applications, each server has a proxy that takes care of domains and certificates.

## The local server

On first start, Pier registers the **local machine**, the server where it runs. You do not need to configure anything: it already appears in **Infrastructure**.

## Add another server

Pier does not install an agent on the server. It only needs SSH and Docker.

1. **Prepare the server.** Install Docker as in the [installation](/en/docs/installation/#1-install-docker) guide and open ports 22, 80 and 443.
2. **Create a key.** In **Security**, generate an SSH key (ed25519) or import one you already have. Keys with a passphrase are not accepted.
3. **Authorize the key.** On the new server, add the public key to the `~/.ssh/authorized_keys` file of a user who can use Docker:

   ```sh
   echo "ssh-ed25519 AAAA... pier" >> ~/.ssh/authorized_keys
   ```

4. **Register it.** In **Infrastructure › Add server**, enter the name, address, SSH port, user and the key. Click **Add and validate**.

The validation checks the connection, Docker and the compose plugin. It also creates the data directory and shows a list of checks, each with a suggested fix:

| Check | Expected |
|---|---|
| Docker | Version 24 or newer |
| Docker Compose | Version 2.20 or newer |
| Disk | At least 10 GB free for Docker |
| Ports 80 and 443 | Free, or used by the Pier proxy or by a Caddy |
| Log rotation | Configured in Docker, so logs do not fill the disk |
| Clock | At most 30 seconds of difference |

Only the version and the connectivity stop you from using the server. The rest are warnings.

> **Server key:** on first access, Pier saves the SSH fingerprint of the server. If it changes later, for example because the server was reinstalled, every connection is refused until you reset it in **Maintenance**. This prevents another server from posing as yours.

## Proxy, domains and HTTPS

Each server has a proxy (Traefik) that receives the traffic on ports 80 and 443 and delivers each domain to the right container. It does not start by itself:

1. Open **Infrastructure › server › Proxy**.
2. Enter the Let's Encrypt email. This is where notices about the certificates go.
3. Click **Start**.

For a domain to work with HTTPS:

1. **DNS:** create an `A` record from the domain to the IP of the server. Create an `AAAA` only if the server has working IPv6: a wrong `AAAA` prevents the certificate.
2. **Ports:** 80 and 443 open to the internet.
3. **Resource:** enter the domain with `https://` in the application or in the stack route and deploy.

Before asking for the certificate, Pier checks whether the DNS points to the server. If it does not, it publishes over HTTP and the log says exactly which record to create. After the DNS propagates, deploy again.

Certificates are renewed automatically. Pier warns you if a certificate is invalid or close to expiring: see [Alerts](/en/docs/alerts/).

### I already have Caddy on the server

If a Caddy already uses ports 80 and 443 to serve other sites, you do not need to remove it. In **Proxy**, choose the Caddy container. Pier keeps its own block in the Caddyfile, which forwards its domains to the Pier proxy. The rest of the file is not touched. The Caddyfile must be writable inside the Caddy container.

### Alternative ports

To test on a machine that already uses 80 and 443, change the proxy ports before starting it. In this case there is no automatic certificate, because Let's Encrypt requires the default ports.

## Resource usage and cleanup

- **Resource usage** shows CPU, memory, disk and the consumption of each managed container.
- **Maintenance** cleans images, stopped containers and build cache created **by Pier**. Nothing created by another tool is removed.

## Plain Docker

The **Docker** tab of each server shows everything that exists on it: containers, images, volumes and networks, whether created by Pier or not. You can see logs, run commands and run a standalone container. Containers managed by Pier can only be removed by the application, database or stack that owns them.
