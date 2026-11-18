## Prepare a cluster

Open **Clusters**, then **Docker Swarm**. Initialize a cluster on a registered manager and add workers. Pier does not import existing clusters. Keep manager quorum and configure firewall access between nodes yourself. Never place a database on this application target.

## Select an application target

In the application's **Destinations** tab, enter the cluster identifier in **Swarm target ID**. Keep only the application's primary server selected as an individual destination. Select a build server if needed. Source builds require an image registry and repository. Credentials are stored through the existing encrypted registry service; deployment forwards private registry authentication to Swarm workers.

The existing application placement API accepts `swarm_cluster_id`. Its read and write abilities retain application/project scope. Deploy, archive upload, rollback and promotion continue through the existing deployment queue. The build runs once and publishes an immutable image digest. Image applications can use a digest from a public or configured private registry. Promotion uses the source digest and the target application's own environment.

## Environment and routing

Application variables and secrets use the existing encrypted environment store. Values are masked in deployment logs and failure messages. During release, a private temporary directory contains a mode-0600 stack file, removed after the attempt. Runtime variables remain visible to Docker administrators through service inspection: they are environment variables, not Docker secret mounts.

Domains reuse the application's route labels at service level, with the `pier-swarm` overlay network. Configure an external Traefik Swarm provider on a manager, attached to that network, with compatible entrypoints and certificate resolver. Point DNS at that proxy. Pier does not install or validate this Swarm proxy configuration; the ordinary per-server Docker provider alone is insufficient. No real DNS or proxy verification was performed.

## Rollout and recovery

The dispatcher serializes deployment per application. Each generated replica becomes one Swarm service, with start-first updates, health checks from application configuration and resource limits. Release waits for task convergence and a stabilization interval; a paused rollout or timeout fails the deployment. On an initial failure the new stack is removed; on an update failure previous service specifications are rolled back. Recovery errors are included in the deployment failure. A successful Docker command alone does not mark the deployment successful.

## Limitations

Volumes, host port mappings, hooks, release commands and extra Docker options are rejected. Existing process names and replica counts must remain fixed after the first release. Use another application for a different topology. Changing target after a release is blocked. Start/stop/restart require a new deployment; deletion removes the Swarm stack. Previews, terminal, metrics, container inventory and application logs still follow the existing single-server paths and are not Swarm-aware. Avoid those operations for Swarm applications. Process interruption can require manual reconciliation; recovery is not a durable transaction. Mixed worker architectures, external Traefik and live Docker rollouts have only fake-executor coverage, with no real server or registry integration validation.
