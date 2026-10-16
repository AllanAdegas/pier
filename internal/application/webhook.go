package application

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
)

// GitProvider identifies who is calling a push webhook.
type GitProvider string

const (
	ProviderGitHub GitProvider = "github"
	ProviderGitLab GitProvider = "gitlab"
	ProviderGitea  GitProvider = "gitea"
)

// WebhookResult tells the caller what the webhook did.
type WebhookResult struct {
	Deployed     bool   `json:"deployed"`
	DeploymentID string `json:"deployment_id,omitempty"`
	Reason       string `json:"reason"`
}

// pushEvent is the part of a push payload every provider shares.
type pushEvent struct {
	Ref string `json:"ref"`
}

// verifyWebhook authenticates the request using the application secret.
func verifyWebhook(provider GitProvider, secret string, header http.Header, body []byte) bool {
	switch provider {
	case ProviderGitHub:
		signature, found := strings.CutPrefix(header.Get("X-Hub-Signature-256"), "sha256=")
		return found && validHMAC(secret, body, signature)
	case ProviderGitea:
		return validHMAC(secret, body, header.Get("X-Gitea-Signature"))
	case ProviderGitLab:
		return constantTimeEqual(header.Get("X-Gitlab-Token"), secret)
	}
	return false
}

// isPushEvent filters out pings and other event types.
func isPushEvent(provider GitProvider, header http.Header) bool {
	switch provider {
	case ProviderGitHub:
		return header.Get("X-GitHub-Event") == "push"
	case ProviderGitea:
		return header.Get("X-Gitea-Event") == "push"
	case ProviderGitLab:
		return header.Get("X-Gitlab-Event") == "Push Hook"
	}
	return false
}

// pushedBranch extracts the branch name from "refs/heads/<branch>".
func pushedBranch(body []byte) (string, bool) {
	var event pushEvent
	if err := json.Unmarshal(body, &event); err != nil {
		return "", false
	}
	return strings.CutPrefix(event.Ref, "refs/heads/")
}

func validHMAC(secret string, body []byte, signatureHex string) bool {
	signature, err := hex.DecodeString(signatureHex)
	if err != nil || len(signature) == 0 {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hmac.Equal(mac.Sum(nil), signature)
}

func constantTimeEqual(a, b string) bool {
	return a != "" && subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
