package application

import (
	"net/http"
	"net/url"
	"strings"
)

// This file exposes the Git source rules (validation, push webhooks, watch
// paths) to other resources that deploy from a repository. They are thin
// wrappers: the rules live next to the application code that owns them.

// ValidRepository reports whether a repository address is accepted: HTTPS,
// SSH or scp-like; never a transport that runs local commands.
func ValidRepository(repository string) bool {
	if !repositoryPattern.MatchString(repository) {
		return false
	}
	if !strings.Contains(repository, "://") {
		return true
	}
	parsed, err := url.Parse(repository)
	return err == nil && parsed.Host != "" && parsed.Fragment == "" && parsed.RawQuery == ""
}

// UsesSSH reports whether the repository is reached over SSH (and so needs a deploy key).
func UsesSSH(repository string) bool { return !strings.HasPrefix(repository, "http") }

// ValidRef reports whether a branch or tag name is safe to hand to git.
func ValidRef(ref string) bool { return validRef(ref) }

// ValidCommit reports whether value is a full commit SHA.
func ValidCommit(value string) bool { return commitPattern.MatchString(value) }

// ValidWatchPattern reports whether a watch path glob is well formed.
func ValidWatchPattern(pattern string) bool { return validWatchPattern(pattern) }

// ValidRelativePath reports whether a path stays inside the repository.
func ValidRelativePath(value string) bool { return safeRelativePath(value) }

// WithoutCredentials hides the credentials of a repository address.
func WithoutCredentials(repository string) string { return withoutCredentials(repository) }

// VerifyWebhook authenticates a push webhook with the shared secret.
func VerifyWebhook(provider GitProvider, secret string, header http.Header, body []byte) bool {
	return verifyWebhook(provider, secret, header, body)
}

// IsPushEvent filters out pings and every event that is not a push.
func IsPushEvent(provider GitProvider, header http.Header) bool { return isPushEvent(provider, header) }

// Push is what a webhook says about the pushed ref.
type Push struct {
	Branch  string
	Tag     string
	Removed bool
}

// ParsePush reads the pushed ref of a webhook body.
func ParsePush(body []byte) (Push, bool) {
	event, ok := parsePush(body)
	if !ok {
		return Push{}, false
	}
	push := Push{Removed: event.removesRef()}
	push.Branch, _ = strings.CutPrefix(event.Ref, "refs/heads/")
	push.Tag, _ = strings.CutPrefix(event.Ref, "refs/tags/")
	if strings.HasPrefix(event.Ref, "refs/heads/") {
		push.Tag = ""
	}
	if strings.HasPrefix(event.Ref, "refs/tags/") {
		push.Branch = ""
	}
	return push, true
}

// WatchPathsChanged reports whether the push touched one of the patterns; an
// empty list, or a payload that cannot tell, counts as changed.
func WatchPathsChanged(patterns []string, body []byte) bool { return watchPathsChanged(patterns, body) }

// ConstantTimeEqual compares two secrets without leaking where they differ;
// an empty secret never matches.
func ConstantTimeEqual(a, b string) bool { return constantTimeEqual(a, b) }
