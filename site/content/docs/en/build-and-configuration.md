The build configuration defines the image; the runtime configuration defines the container. Changes normally only reach the runtime on the next deploy. Test the artifact before promoting it.

## Dockerfile, Railpack and commands

The **Dockerfile** build pack uses the configured file and base directory. For a Dockerfile with several stages, fill in **Dockerfile stage** (`build.target` in `pier.yaml`); empty uses the final stage.

**Railpack** detects the application, prepares the plan and builds it with BuildKit. Pier uses a pinned version and verifies the download of the binary before running it. The server must be able to reach the downloads and registries used. Custom install, build and start commands are still code that you authorize to run; they are not a sandbox. Nixpacks is still available for projects that already use it. Test compatibility before changing the pack of an existing application.

## Build secrets

Check **Build secret** to carry the value through BuildKit, instead of a common build argument. In the Dockerfile, read it with `RUN --mount=type=secret,id=NAME`.

This protects the transport, but it does not stop your command from copying or printing the secret. The flag also does not mean "build only": the application variables go to the runtime. Do not use a long-lived secret if the code being built is not trusted. Runtime-only variables do not need to enter the build.

## Restart and shutdown

The application accepts the policies `no`, `always`, `unless-stopped` and `on-failure`. Without configuration, it uses `unless-stopped`. The graceful shutdown timeout accepts 1 to 300 seconds; zero uses 30 seconds for new containers. It applies to web and workers.

When retiring a version, Pier respects the timeout that was saved in the old container. Old containers without this value use the Docker default until they are redeployed. Do not assume that a process shuts down correctly just because you raised the timeout: it needs to handle SIGTERM and finish requests/tasks.

## Shared variables

A variable can belong to the project or to an environment. The application references it by name:

```text
${{project.API_URL}}
${{environment.LOG_LEVEL}}
```

Register the value in the right scope and use the reference as the value of the application variable. Shared secrets are kept encrypted and are not returned as plain text in the list. A change marks the dependent applications for redeploy; Pier does not publish all of them automatically. Review the impact before changing or removing a shared variable. Database references, such as `${{principal.url}}`, are still a separate mechanism.

## Clone to another environment

Cloning copies the **definition** of an application, database or stack to another environment. It does not copy volume data or database content, and it does not deploy the copy automatically. Review secrets, domain, ports, server and credentials from the source before the first deploy; copying the configuration does not create safe staging data. Accepted risks of a Git stack need review in the copy.

## Automatic domain

The instance policy can generate a domain for applications that have none. Without its own base domain, it uses public IPv4 + `sslip.io`, HTTP only. This is useful for tests; it is not a substitute for HTTPS for sensitive data.

With your own base domain, configure a wildcard DNS record to the server before turning on HTTPS. Pier does not create records at your DNS provider. Removing an automatic domain prevents it from being put back without a new request. With several targets, prepare external DNS/load balancing as in the [multi-server guide](/en/docs/multi-server/).

## Advanced Docker options

The configuration can define extra hosts, capabilities, ulimits, shared memory and allowed labels. The reserved prefixes `pier.`, `traefik.` and `com.docker.` cannot be used to change the panel's control.

Privileged mode, host network/PID, sensitive devices and access to the Docker socket need the instance risk policy, managed by an owner. Turning this policy on does not make the option safe. Host network does not accept domains or ports published by Pier. Use it only when you know which privileges the process gains on the server.

The advanced fields and their screens are still going through integration and QA in this beta. Do not copy arbitrary options from another Compose file: check the validation, the policy and the saved configuration. See also [pier.yaml](/en/docs/pier-yaml/) and [security](/en/docs/security/).
