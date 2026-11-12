package main

import "strings"

func specialResponses(op, responses M, method, path string) {
	if strings.HasSuffix(path, "/deploy-archive") {
		op["requestBody"] = M{"required": true, "description": "gzip tar archive, maximum 200 MiB; safe extraction rejects traversal/symlinks.", "content": M{"application/gzip": M{"schema": M{"type": "string", "format": "binary"}}}}
	}
	if method == "get" && strings.HasSuffix(path, "/download") || method == "post" && path == "/api/v1/instance/backups" {
		responses["200"] = M{"description": "Backup archive; keep it private.", "content": M{"application/octet-stream": M{"schema": M{"type": "string", "format": "binary"}}}}
	}
	if strings.HasSuffix(path, "/stream") || (method == "get" && strings.HasSuffix(path, "/logs") && !strings.Contains(path, "proxy")) {
		responses["200"] = M{"description": "Server-sent event stream; client must handle disconnect/reconnect.", "content": M{"text/event-stream": M{"schema": M{"type": "string"}}}}
	}
	if path == "/api/v1/terminal" {
		responses["101"] = M{"description": "WebSocket upgrade with short-lived session-bound ticket. Origin is checked; session, resource permissions and expiry are revalidated."}
		delete(responses, "200")
		op["security"] = []M{{"cookieAuth": []string{}}}
		op["x-pier-session-only"] = true
	}
	if strings.HasSuffix(path, "/oidc/start") || strings.HasSuffix(path, "/oidc/callback") {
		responses["302"] = M{"description": "OIDC redirect; state/PKCE/nonce/browser binding enforced, callback URL configured server-side."}
		delete(responses, "200")
	}
	if strings.Contains(path, "/webhooks/") {
		op["description"] = "Provider signature/token verification is required even though no bearer/session is used. GitHub/Gitea use HMAC and GitLab uses the configured secret token. Replay/lifecycle rules depend on the event."
	}
}
