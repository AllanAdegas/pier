## Browser terminal

Use **Terminal** on the application, the database, the stack container or the Docker explorer of the server. For stacks, choose the container you want. The image must have `/bin/sh`; the automatic selection prefers bash when it is available.

It is an interactive session with the privileges of the container user, and it has access to the container's files, network and variables. Privileged containers, or containers with the Docker socket, can give control of the host. Use it only with people you trust. The terminal needs a browser session with write capability; API tokens never open a terminal, not even write tokens.

Close it with the button in the modal. Escape is sent to the shell. The session ends after 15 minutes of inactivity or after eight hours; there is no reconnection, sharing or recording of commands. The audit log records opening and closing, without a transcript: do not use it as a complete record of what was run. Revoking the session ends the terminal at the next revalidation, within about 30 seconds.

## Server maintenance

In **Server operations**, check **Pause new deployments on this server** and save. Before that, check whether there are operations already in progress. Maintenance does not stop existing containers and does not block reads or restores; it is not a shutdown of the machine. When you finish, uncheck it and save, then validate a deploy.

## Safe cleanup

1. Use **Preview cleanup** and read the report.
2. Check the minimum age (24 to 8760 hours) and the candidate objects.
3. Type the server name and use **Run safe cleanup** only after reviewing.
4. To automate it, turn on the schedule, set the cron expression and save the policy; follow the results.

Only disposable objects with `pier.managed=true`, `pier.cleanup=true` and `pier.type=scratch` are included, and only if they respect the age. Containers must be stopped and images must have no tag. Volumes and backups are never included. The preview does not reserve the Docker state: check the result of the run. Do not replace this flow with `docker system prune --volumes`; volumes can hold the data of your services.


## Install Docker over SSH

The **Install Docker** flow of the server starts a remote operation and lets you follow its state. Use it on a supported machine, with a privileged SSH credential already prepared; it asks for confirmation before changing the host. Do not run it as an attempt to repair an installation that has important services, unless you have a backup and a maintenance window. Afterwards, check Docker, Compose, Buildx and the server validation.

## Create machines in the cloud

The backend offers provisioning for **Hetzner Cloud** and **DigitalOcean**. There is no generic AWS provisioner. Register an API credential with the smallest scope the provider accepts, look at the region/size/image catalog and review the billing before creating. Pier generates/uses the SSH key that is needed, waits for SSH and can install Docker; then it registers the server.

The flow is still being integrated in this beta, including the interface. The provisioning routes are `/api/v1/cloud/credentials` and `/api/v1/cloud/machines`; registering credentials and destroying a machine need a panel session and the matching permissions. Destroying a machine deletes the infrastructure at the provider, not only the local record. A failure in registration or installation does not guarantee that the resource stopped being billed: check the provider's console before you repeat or end the operation.

No real machines were created to validate this delivery; API tests use simulated services. Do not use production as the first test.

## Log shipping

Log drains forward the logs of a project, application or stack to **HTTP JSON** or **Loki**. It is not generic OTLP support. Configure the destination, scope, labels and token; with a token, the URL must use HTTPS. Do not put credentials in the URL. The destination receives data from the selected workloads: review the privacy, retention and access of the external service.

HTTP shipping publishes an array of records with timestamp, message and labels. Loki uses the matching push endpoint. There is a destination test in the API `/api/v1/log-drains/{id}/test`; sending a test does not prove continuous collection from all containers. The configuration interface is still being integrated in this beta.

The queue is limited and kept in memory. A slow destination, repeated failures, too many lines or a Pier restart can lose logs; there is no exactly-once or persistent delivery guarantee. Masking uses the secrets that Pier knows; it does not detect all sensitive data written by the application. Use your own audit/storage solution when you cannot lose events.

### Log collection limits

Shipping is best effort, with limited buffers. Complete records above 1 MiB and excess over the budget per collection (1 MiB or 5,000 records) are discarded and counted in **Dropped**. Normal lines after a large record are still eligible. Read failures do not advance the cursor, but final failures at the destination can lose records. The cursor is kept in memory; restarting Pier does not recover the earlier history. Rotation and records with identical timestamps can also cause losses. There is no guarantee of complete or exactly-once delivery.
