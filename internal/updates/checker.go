// Package updates tells the panel when a newer Pier release exists. It asks
// the public GitHub releases API at most once per interval, keeps the answer
// in memory and sends nothing that identifies the instance. It never changes
// the installation: upgrading is a command the operator runs (see the
// banner in the interface).
package updates

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	// DefaultURL is the "latest release" endpoint of the Pier repository.
	DefaultURL = "https://api.github.com/repos/AllanAdegas/pier/releases/latest"
	// DefaultInterval is the shortest time between two requests to GitHub.
	DefaultInterval = 12 * time.Hour
	// DefaultTimeout bounds one request.
	DefaultTimeout = 5 * time.Second
	// UserAgent is generic on purpose: no version, no host, no instance id.
	UserAgent = "pier-update-check"

	maxResponseBytes  = 1 << 20
	maxNotesRunes     = 600
	releasePagePrefix = "https://github.com/AllanAdegas/pier/"
)

// Status is what the API returns about updates.
type Status struct {
	// Enabled is false when the operator opted out; nothing else is set then
	// besides Current.
	Enabled         bool       `json:"enabled"`
	Current         string     `json:"current"`
	Latest          string     `json:"latest,omitempty"`
	UpdateAvailable bool       `json:"update_available"`
	URL             string     `json:"url,omitempty"`
	Notes           string     `json:"notes,omitempty"`
	CheckedAt       *time.Time `json:"checked_at"`
}

// Options configures a Checker. Zero values take the defaults.
type Options struct {
	Enabled  bool
	Current  string
	URL      string
	Interval time.Duration
	Timeout  time.Duration
	Client   *http.Client
	Now      func() time.Time
}

// Checker caches the latest release. It is safe for concurrent use.
type Checker struct {
	options Options
	current release

	mu        sync.Mutex
	attempted time.Time
	status    Status
}

// New validates the options. An invalid Current is an error (fail fast): a
// panel that does not know its own version cannot compare.
func New(options Options) (*Checker, error) {
	current, err := parseRelease(options.Current)
	if err != nil {
		return nil, fmt.Errorf("updates: current version: %w", err)
	}
	if options.URL == "" {
		options.URL = DefaultURL
	}
	if options.Interval <= 0 {
		options.Interval = DefaultInterval
	}
	if options.Timeout <= 0 {
		options.Timeout = DefaultTimeout
	}
	if options.Client == nil {
		options.Client = &http.Client{}
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	checker := &Checker{options: options, current: current}
	checker.status = Status{Enabled: options.Enabled, Current: options.Current}
	return checker, nil
}

// EnabledByEnvironment reads PIER_UPDATE_CHECK: only "0" opts out.
func EnabledByEnvironment(value string) bool {
	return strings.TrimSpace(value) != "0"
}

// Status returns the cached answer, asking GitHub first when the last
// attempt is older than the interval. A failed attempt keeps the previous
// answer and still counts as an attempt, so GitHub is never hit more often
// than once per interval; a client that disconnects does not cancel it. A nil Checker reports the check as disabled.
func (c *Checker) Status(ctx context.Context) Status {
	if c == nil {
		return Status{}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.options.Enabled && c.due() {
		c.attempted = c.options.Now()
		if latest, err := c.fetch(context.WithoutCancel(ctx)); err == nil {
			c.status = c.describe(latest)
		}
	}
	return c.status
}

func (c *Checker) due() bool {
	return c.attempted.IsZero() || c.options.Now().Sub(c.attempted) >= c.options.Interval
}

type githubRelease struct {
	TagName    string `json:"tag_name"`
	HTMLURL    string `json:"html_url"`
	Body       string `json:"body"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
}

func (c *Checker) fetch(ctx context.Context) (githubRelease, error) {
	ctx, cancel := context.WithTimeout(ctx, c.options.Timeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.options.URL, nil)
	if err != nil {
		return githubRelease{}, err
	}
	request.Header.Set("User-Agent", UserAgent)
	request.Header.Set("Accept", "application/vnd.github+json")
	response, err := c.options.Client.Do(request)
	if err != nil {
		return githubRelease{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return githubRelease{}, fmt.Errorf("updates: GitHub answered %d", response.StatusCode)
	}
	var latest githubRelease
	if err := json.NewDecoder(io.LimitReader(response.Body, maxResponseBytes)).Decode(&latest); err != nil {
		return githubRelease{}, fmt.Errorf("updates: reading the release: %w", err)
	}
	if latest.Draft || latest.Prerelease {
		return githubRelease{}, errors.New("updates: latest release is not stable")
	}
	return latest, nil
}

func (c *Checker) describe(latest githubRelease) Status {
	parsed, err := parseRelease(latest.TagName)
	if err != nil {
		return c.status
	}
	checkedAt := c.options.Now().UTC()
	return Status{
		Enabled:         true,
		Current:         c.options.Current,
		Latest:          strings.TrimPrefix(latest.TagName, "v"),
		UpdateAvailable: parsed.newerThan(c.current),
		URL:             releaseURL(latest),
		Notes:           excerpt(latest.Body),
		CheckedAt:       &checkedAt,
	}
}

// releaseURL trusts the page address only when it points at the Pier
// repository; otherwise it builds it from the (validated) tag.
func releaseURL(latest githubRelease) string {
	if strings.HasPrefix(latest.HTMLURL, releasePagePrefix) {
		return latest.HTMLURL
	}
	return releasePagePrefix + "releases/tag/" + latest.TagName
}

func excerpt(notes string) string {
	notes = strings.TrimSpace(strings.ToValidUTF8(notes, ""))
	if utf8.RuneCountInString(notes) <= maxNotesRunes {
		return notes
	}
	runes := []rune(notes)
	return strings.TrimSpace(string(runes[:maxNotesRunes])) + "…"
}
