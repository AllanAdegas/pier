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

## Interactive terminal

- `github.com/creack/pty` 1.1.24 — MIT, https://github.com/creack/pty
- `github.com/coder/websocket` 1.8.15 — ISC, https://github.com/coder/websocket
- `@xterm/xterm` 5.5.0 and `@xterm/addon-fit` 0.10.0 — MIT, https://github.com/xtermjs/xterm.js

## Authenticator QR codes

- `rsc.io/qr` v0.2.0 — BSD-3-Clause, https://pkg.go.dev/rsc.io/qr. Copyright (c) 2009 The Go Authors.

```text
Copyright (c) 2009 The Go Authors. All rights reserved.

Redistribution and use in source and binary forms, with or without
modification, are permitted provided that the following conditions are
met:

   * Redistributions of source code must retain the above copyright
notice, this list of conditions and the following disclaimer.
   * Redistributions in binary form must reproduce the above
copyright notice, this list of conditions and the following disclaimer
in the documentation and/or other materials provided with the
distribution.
   * Neither the name of Google Inc. nor the names of its
contributors may be used to endorse or promote products derived from
this software without specific prior written permission.

THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
"AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
(INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
```

## RBAC OIDC dependencies

- github.com/coreos/go-oidc/v3 v3.14.1 — Apache-2.0; https://github.com/coreos/go-oidc/blob/v3.14.1/LICENSE
- github.com/go-jose/go-jose/v4 v4.1.4 — Apache-2.0; https://github.com/go-jose/go-jose/blob/v4.1.4/LICENSE

## Railpack build pack (downloaded at build time, not bundled)

Pier does not embed Railpack. The `railpack` build pack downloads the pinned
release v0.43.0 to `<data_dir>/tools/railpack/v0.43.0/` on the server that
builds, verifying the archive and the binary against SHA-256 checksums in
`internal/application/railpack_install.go`, and builds through the BuildKit
frontend image `ghcr.io/railwayapp/railpack-frontend:v0.43.0` pinned by digest.

- Railpack v0.43.0 (CLI and BuildKit frontend) — MIT, https://github.com/railwayapp/railpack/blob/v0.43.0/LICENSE.
  Copyright (c) 2025 Railway Corp.
