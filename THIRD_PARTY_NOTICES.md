# Third-party notices

## Service catalog (`internal/stack/templates/`)

The catalog is written in Pier's own blueprint format (`internal/stack/blueprint.go`),
with databases, caches and object storage taken from Pier's component library
(`internal/stack/components.go`). The images the templates reference are
distributed by their own publishers, under their own licenses; Pier does not
redistribute them.

## Go and web dependencies

Go modules are listed in `go.mod` and web packages in `web/deno.lock`. Each is
distributed under its own license, included with its source.
