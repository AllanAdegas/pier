package indexnow

import (
	"bufio"
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"net/url"
	"strings"
	"time"
)

// Limits of what one submission reads from a site.
const (
	robotsLimit  = 512 << 10
	sitemapLimit = 10 << 20
	maxSitemaps  = 50
	maxURLs      = 50_000
)

// errNoSitemap is returned when the site publishes no sitemap at all.
var errNoSitemap = errors.New("no sitemap")

// Entry is a page listed in a sitemap.
type Entry struct {
	Loc      string
	Modified *time.Time
}

// discovery is what the sitemaps of a site list.
type discovery struct {
	Entries []Entry
	// Sources are the sitemaps read, for the log.
	Sources []string
	// Truncated says maxURLs was reached.
	Truncated bool
}

// sitemapDocument covers both <urlset> and <sitemapindex>.
type sitemapDocument struct {
	XMLName xml.Name
	URLs    []struct {
		Loc     string `xml:"loc"`
		LastMod string `xml:"lastmod"`
	} `xml:"url"`
	Sitemaps []struct {
		Loc string `xml:"loc"`
	} `xml:"sitemap"`
}

// discover reads the sitemaps of https://<host>: the Sitemap lines of its
// robots.txt or, without any, /sitemap.xml. A sitemap index is followed one
// level. Only pages of the same host are kept.
func discover(ctx context.Context, f fetcher, host string) (discovery, error) {
	queue := sitemapsFromRobots(ctx, f, host)
	fromRobots := len(queue) > 0
	if !fromRobots {
		queue = []string{"https://" + host + "/sitemap.xml"}
	}
	topLevel := len(queue)
	result := discovery{}
	seen := map[string]bool{}
	for index := 0; index < len(queue) && index < maxSitemaps; index++ {
		document, err := readSitemap(ctx, f, queue[index])
		if err != nil {
			if !fromRobots && index == 0 && isStatus(err, 404) {
				return discovery{}, errNoSitemap
			}
			return discovery{}, sitemapError{address: queue[index], cause: err}
		}
		result.Sources = append(result.Sources, queue[index])
		if document.XMLName.Local == "sitemapindex" {
			if index >= topLevel {
				continue // only one level of index is followed
			}
			for _, child := range document.Sitemaps {
				if loc := strings.TrimSpace(child.Loc); sameHost(loc, host) {
					queue = append(queue, loc)
				}
			}
			continue
		}
		for _, page := range document.URLs {
			loc := strings.TrimSpace(page.Loc)
			if !sameHost(loc, host) || seen[loc] {
				continue
			}
			if len(result.Entries) == maxURLs {
				result.Truncated = true
				return result, nil
			}
			seen[loc] = true
			result.Entries = append(result.Entries, Entry{Loc: loc, Modified: parseLastMod(page.LastMod)})
		}
	}
	return result, nil
}

// sitemapError names the sitemap that could not be read.
type sitemapError struct {
	address string
	cause   error
}

func (e sitemapError) Error() string { return e.address + ": " + e.cause.Error() }
func (e sitemapError) Unwrap() error { return e.cause }

func readSitemap(ctx context.Context, f fetcher, address string) (sitemapDocument, error) {
	body, err := f.get(ctx, address, sitemapLimit)
	if err != nil {
		return sitemapDocument{}, err
	}
	var document sitemapDocument
	if err := xml.Unmarshal(body, &document); err != nil {
		return sitemapDocument{}, err
	}
	if name := document.XMLName.Local; name != "urlset" && name != "sitemapindex" {
		return sitemapDocument{}, errors.New("not a sitemap: <" + name + ">")
	}
	return document, nil
}

// sitemapsFromRobots returns the Sitemap lines of robots.txt that point to
// the same host. A missing or unreadable robots.txt lists none.
func sitemapsFromRobots(ctx context.Context, f fetcher, host string) []string {
	body, err := f.get(ctx, "https://"+host+"/robots.txt", robotsLimit)
	if err != nil {
		return nil
	}
	sitemaps := []string{}
	scanner := bufio.NewScanner(bytes.NewReader(body))
	for scanner.Scan() {
		field, value, found := strings.Cut(scanner.Text(), ":")
		if !found || !strings.EqualFold(strings.TrimSpace(field), "sitemap") {
			continue
		}
		if address := strings.TrimSpace(value); sameHost(address, host) {
			sitemaps = append(sitemaps, address)
		}
	}
	return sitemaps
}

// sameHost tells whether an address is an https page of the host, on the
// standard port and without credentials.
func sameHost(address, host string) bool {
	parsed, err := url.Parse(address)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil {
		return false
	}
	port := parsed.Port()
	return strings.EqualFold(parsed.Hostname(), host) && (port == "" || port == "443")
}

func isStatus(err error, status int) bool {
	var target statusError
	return errors.As(err, &target) && target.status == status
}

// lastModLayouts are the W3C datetime forms sitemaps use.
var lastModLayouts = []string{time.RFC3339Nano, "2006-01-02T15:04Z07:00", "2006-01-02", "2006-01", "2006"}

func parseLastMod(value string) *time.Time {
	value = strings.TrimSpace(value)
	for _, layout := range lastModLayouts {
		if parsed, err := time.Parse(layout, value); err == nil {
			return &parsed
		}
	}
	return nil
}
