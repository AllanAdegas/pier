// Package indexnow tells search engines about the pages of the sites Pier
// hosts. A host with the option on gets a key, served by the proxy of every
// server that publishes it, and its sitemap is submitted to IndexNow after
// each successful deployment. A failure is reported, never fatal.
package indexnow

import (
	"crypto/rand"
	"encoding/hex"
	"regexp"
	"strings"
	"time"

	"pier/internal/compose"
)

// Kinds of resources whose domains can notify search engines. They match
// the path segments of the API (/applications/{id}, /stacks/{id}).
const (
	KindApplication = "applications"
	KindStack       = "stacks"
)

// Site is a host with search engine notification turned on.
type Site struct {
	ID           string `json:"id"`
	Host         string `json:"host"`
	ResourceKind string `json:"resource_kind"`
	ResourceID   string `json:"resource_id"`
	Key          string `json:"key"`
	CreatedAt    time.Time
	Last         Outcome
}

// Outcome is what the last submission of a site achieved.
type Outcome struct {
	AttemptedAt *time.Time `json:"attempted_at"`
	// SubmittedAt is the last submission IndexNow accepted; later runs only
	// send pages changed after it.
	SubmittedAt *time.Time `json:"submitted_at"`
	// Status is the HTTP status of IndexNow, zero when nothing was sent.
	Status   int    `json:"status"`
	Message  string `json:"message"`
	URLCount int    `json:"url_count"`
	// Problem says the submission did not happen or was refused.
	Problem bool `json:"problem"`
}

// Accepted tells whether IndexNow took the URLs (200 or 202).
func (o Outcome) Accepted() bool { return o.Status == 200 || o.Status == 202 }

// KeyLocation is where search engines read the key of the site.
func (s Site) KeyLocation() string { return "https://" + s.Host + "/" + s.Key + ".txt" }

var keyPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

// newKey creates a key with 128 bits of entropy, within the [a-zA-Z0-9-]
// alphabet and the 8 to 128 characters IndexNow accepts.
func newKey() string {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		panic(err)
	}
	return hex.EncodeToString(buffer)
}

// keyFromFile extracts the key from a requested file name ("<key>.txt").
// The second result is false for anything that is not a key Pier creates.
func keyFromFile(name string) (string, bool) {
	key, found := strings.CutSuffix(name, ".txt")
	return key, found && keyPattern.MatchString(key)
}

// Eligible returns the hosts of a resource's domains that can notify search
// engines: https, without a path, each once, in the order they appear.
func Eligible(domains []string) []string {
	hosts := []string{}
	seen := map[string]bool{}
	for _, domain := range domains {
		route, err := compose.ParseRoute(domain)
		if err != nil || route.Scheme != "https" || route.Path != "" || seen[route.Host] {
			continue
		}
		seen[route.Host] = true
		hosts = append(hosts, route.Host)
	}
	return hosts
}
