# HTTP contract maintenance

From the `pier` repository root:

```sh
go run ./tools/openapi                 # regenerate api/openapi.json
go run ./tools/openapi -check          # reject contract drift; writes nothing
go test ./tools/openapi
go vet ./tools/openapi
```

`-out <path>` changes the artifact path. The generator uses only the Go standard
library and does not start Pier, access its data, or call external services.
The JSON document is OpenAPI 3.1. It is a repository artifact, not a new API route.

The route inventory comes from production `a.route`, `a.public` and explicit
`ServeMux.HandleFunc` registrations in `internal/httpapi`. Dynamic environment,
volume-backup, shared-variable and Git-provider registrations expand their
actual call sites or range values. An unknown route expression fails generation.
Tests exercise new route discovery, unknown-expression refusal, contract drift,
request/response types, protocol/security annotations, references and stable output.

Request DTOs come from `decode[T]` / `decodeOptional[T]`. Responses come from
`ok`, `created`, `accepted`, `noContent`, `writeJSON` and returned local HTTP
helpers, with local assignments and service return types resolved from the AST.
Go JSON tags determine property names and exclusions. Pointer nullability,
arrays/maps, typed string constants and base64 byte slices are represented.
Explicit protocol annotations cover archive upload/download, SSE, WebSocket and
OIDC redirects. Error responses describe common error classes, not a promise
that every endpoint can emit every class.

## Limits

This is an HTTP contract, not the MCP JSON-RPC protocol or frontend assets.
It inventories source registrations, including routes that require an enabled
optional service; it does not prove every registration is wired at runtime.
It does not replace endpoint integration tests or runtime validation.

Source inspection cannot prove every service validation constraint. JSON DTO
fields are therefore not marked required merely because a Go JSON tag lacks
`omitempty`; `x-json-always-serialized` records that serialization detail.
Body presence follows the decoder used. More restrictive field presence,
bounds, conditional requirements, RBAC cross-resource checks and business
rules remain service validations. No example contains credentials.

`x-schema-inference` explicitly identifies handler-specific success responses
(such as streams, redirects and archives); their protocols are separately
annotated. External Go scalar types not mapped by the generator carry a
`Go scalar` description instead of an invented structured schema. If new
helper patterns appear, extend inference and its tests instead of silently
assuming an object. Re-run generation after changes to models or registrations.
