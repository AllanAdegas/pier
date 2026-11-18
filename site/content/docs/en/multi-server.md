An application can build the image on one server and run it on several destinations. Use this flow when you have already prepared the servers, the registry and the traffic routing. It does not create a cluster or a global load balancer.

## Prepare the destinations

1. Register and validate each server by SSH in **Infrastructure**. Docker, Compose, Buildx on the build server and free space must be available.
2. Set up the proxy of each server and let each machine reach the registry over HTTPS. The architectures must be compatible with the image.
3. Prepare DNS or an external load balancer to send traffic to the destinations. Pier does not distribute traffic between servers.
4. On the application's **Destinations** tab, register the registry credential and test the access. Docker Hub, GHCR, GitLab and other OCI registries over HTTPS can be used. Do not put a password in the image URL.
5. Choose the build server, the registry, the image repository and the servers that run it. The application's original server stays among the destinations. Save and make a test deploy.

The credentials are stored encrypted in Pier. Each push and pull uses a private temporary Docker configuration, removed when it finishes. Administrative reads show metadata, without returning the password.

## What happens on deploy

The image is built once on the build server, published and identified by an immutable digest. All destinations download that digest before the update begins. Pier updates one server at a time, using each destination's own proxy labels and readiness checks. All replicas on that server must be ready.

The release command and the pre-deploy hook run only on the first server. This avoids running the same migration on every machine, but it does not make the migration reversible.

If a destination fails, it keeps the previous version; destinations that were already updated try to go back to the previous image, and the ones not yet reached are not changed. Check the result per server: compensation can fail and needs intervention. Start, stop, restart and removal act on all destinations. A destination removed from the configuration is cleaned up after the next successful deploy.

## Follow and promote

The **Destinations** tab shows rollout state, digest, previous version, containers, CPU, memory, logs and terminal per server. Rollback uses the application's rollback flow.

A promotion between servers can reuse the same digest that was already published, without rebuilding or resending the image. The destination application gets its own variables. The registry must keep the artifact available; set up retention to preserve the rollback versions.

## Limits that matter

- The rollout is **sequential**, not an atomic transaction. Servers can serve different versions for some time.
- An image rollback does not undo migrations or changes in external services.
- Volumes are not shared. Local uploads and databases need their own architecture; application replicas do not replicate data.
- Pier records rollout progress before changing a server. After a restart it attempts recovery once: resume while releasing, rollback while compensating. A freeze, maintenance or an ambiguous one-shot command leaves recovery for the operator. Use **Resume rollout** or **Roll back rollout** in **Destinations**. Verify database migrations before rolling back.
- Previews, scheduled tasks and the Logs tab still use the application's original server; use the destinations view for the rest. The metrics history adds up the configured destinations: [understand totals and gaps](/en/docs/alerts/#metrics-history).
- An insecure HTTP registry is not supported. There is no automatic provisioning of DNS or a load balancer.

The integration was exercised in tests with simulated executors and test HTTP servers. It was not validated against real registries or real multiple servers at this stage. Before using production, test private publishing, partial failure, rollback and recovery in your environment.
