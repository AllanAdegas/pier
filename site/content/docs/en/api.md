Pier's HTTP API uses the `/api/v1` prefix. The versioned contract is in [api/openapi.json]({{repo}}/blob/main/api/openapi.json). The `info.version` field is the version of the contract (`v1`), not of the server. Use the file from the **same tag** as the server, not the `main` of another version.

## Authentication and permissions

Integrations use `Authorization: Bearer TOKEN`. The user's role also limits what the token can do. A 403 response should not be worked around with IDs from another project or credentials from another account.

Operations that require a browser session, such as account security and identity management, do not accept a `write` token as a substitute. Cookie mutations require the `X-Pier-Client` header, in addition to the protections of the flow itself. Do not copy browser cookies into CI scripts.

## Responses and long operations

Check the method, status and body defined for each route. A `202` normally means the work was accepted, not that the deployment or backup has finished. Check the state or follow the logs until it completes. A release or readiness failure must be treated as a failure of the operation, even when a previous version keeps responding.

Errors have a code and a message; do not use the translated text as a permanent identifier. Do not log authentication bodies, tokens, passwords or backup files in your client's logs. The contract may need Docker-specific or provider-specific fields; check the coverage notes of the version before generating an SDK automatically.

For ordinary scripts, prefer [pierctl](/en/docs/pierctl/), which already handles context, output, confirmation and error codes. For AI agents, there is [MCP](/en/docs/ai-mcp/), with its own set of tools, not an unrestricted mirror of the whole API.
