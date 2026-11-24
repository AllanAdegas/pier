package indexnow

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakeWeb answers requests by host, so no test touches the network.
type fakeWeb struct {
	mu        sync.Mutex
	pages     map[string]string // "host/path" → body
	status    map[string]int    // "host/path" → status other than 200/404
	submitted []submission
	reply     int  // status of the IndexNow endpoint
	down      bool // the IndexNow endpoint is unreachable
}

func newFakeWeb() *fakeWeb {
	return &fakeWeb{pages: map[string]string{}, status: map[string]int{}, reply: 202}
}

func (w *fakeWeb) client() *http.Client { return &http.Client{Transport: w} }

func (w *fakeWeb) RoundTrip(request *http.Request) (*http.Response, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	recorder := httptest.NewRecorder()
	where := request.URL.Host + request.URL.Path
	switch {
	case request.URL.Host == "api.indexnow.org" && w.down:
		return nil, errors.New("connection refused")
	case request.URL.Host == "api.indexnow.org":
		var body submission
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			return nil, err
		}
		w.submitted = append(w.submitted, body)
		recorder.WriteHeader(w.reply)
	case w.status[where] != 0:
		recorder.WriteHeader(w.status[where])
	case w.pages[where] != "":
		recorder.WriteString(w.pages[where])
	default:
		recorder.WriteHeader(http.StatusNotFound)
	}
	return recorder.Result(), nil
}

func (w *fakeWeb) sent() []submission {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]submission(nil), w.submitted...)
}

const urlset = `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <url><loc>https://asigas.com/</loc><lastmod>2026-11-20</lastmod></url>
  <url><loc>https://asigas.com/sobre</loc><lastmod>2026-11-22T10:00:00Z</lastmod></url>
  <url><loc>https://other.com/elsewhere</loc></url>
  <url><loc>http://asigas.com/plain</loc></url>
</urlset>`

func TestTheSitemapIsFoundThroughRobotsTxt(t *testing.T) {
	web := newFakeWeb()
	web.pages["asigas.com/robots.txt"] = "User-agent: *\nSitemap: https://asigas.com/maps/pages.xml\nsitemap: https://evil.com/s.xml\n"
	web.pages["asigas.com/maps/pages.xml"] = urlset
	found, err := discover(context.Background(), fetcher{client: web.client()}, "asigas.com")
	if err != nil {
		t.Fatal(err)
	}
	if len(found.Entries) != 2 || found.Entries[1].Loc != "https://asigas.com/sobre" || found.Entries[1].Modified == nil {
		t.Errorf("only https pages of the host are kept: %+v", found.Entries)
	}
	if strings.Join(found.Sources, ",") != "https://asigas.com/maps/pages.xml" {
		t.Errorf("sources = %v", found.Sources)
	}
}

func TestWithoutRobotsTxtTheSitemapIsReadFromItsUsualPlace(t *testing.T) {
	web := newFakeWeb()
	web.pages["asigas.com/sitemap.xml"] = urlset
	found, err := discover(context.Background(), fetcher{client: web.client()}, "asigas.com")
	if err != nil || len(found.Entries) != 2 {
		t.Fatalf("entries %+v, err %v", found.Entries, err)
	}
	if _, err := discover(context.Background(), fetcher{client: newFakeWeb().client()}, "asigas.com"); !errors.Is(err, errNoSitemap) {
		t.Errorf("a site without sitemap must say so, got %v", err)
	}
}

func TestASitemapIndexIsFollowedOneLevelAndMayBeCompressed(t *testing.T) {
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	writer.Write([]byte(urlset))
	writer.Close()
	web := newFakeWeb()
	web.pages["asigas.com/sitemap.xml"] = `<sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
		<sitemap><loc>https://asigas.com/pages.xml.gz</loc></sitemap>
		<sitemap><loc>https://asigas.com/nested.xml</loc></sitemap>
		<sitemap><loc>https://other.com/s.xml</loc></sitemap></sitemapindex>`
	web.pages["asigas.com/pages.xml.gz"] = compressed.String()
	web.pages["asigas.com/nested.xml"] = `<sitemapindex><sitemap><loc>https://asigas.com/deeper.xml</loc></sitemap></sitemapindex>`
	web.pages["asigas.com/deeper.xml"] = `<urlset><url><loc>https://asigas.com/too-deep</loc></url></urlset>`
	found, err := discover(context.Background(), fetcher{client: web.client()}, "asigas.com")
	if err != nil {
		t.Fatal(err)
	}
	if len(found.Entries) != 2 {
		t.Errorf("one level of index, compressed sitemap read: %+v", found.Entries)
	}
}

func TestABrokenOrHugeSitemapIsReported(t *testing.T) {
	web := newFakeWeb()
	web.pages["asigas.com/sitemap.xml"] = "<urlset><url><loc>"
	if _, err := discover(context.Background(), fetcher{client: web.client()}, "asigas.com"); err == nil {
		t.Errorf("a broken sitemap must be an error")
	}
	web.pages["asigas.com/sitemap.xml"] = "<html>not a sitemap</html>"
	if _, err := discover(context.Background(), fetcher{client: web.client()}, "asigas.com"); err == nil {
		t.Errorf("an HTML page is not a sitemap")
	}
	web.pages["asigas.com/sitemap.xml"] = strings.Repeat(" ", sitemapLimit+1)
	if _, err := discover(context.Background(), fetcher{client: web.client()}, "asigas.com"); !errors.Is(err, errTooLarge) {
		t.Errorf("a sitemap over the limit must be refused, got %v", err)
	}
}

func TestAGzipBombIsCutAtTheLimit(t *testing.T) {
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	writer.Write(bytes.Repeat([]byte{' '}, sitemapLimit+1))
	writer.Close()
	if _, err := gunzipIfNeeded(compressed.Bytes(), sitemapLimit); !errors.Is(err, errTooLarge) {
		t.Errorf("the expanded size must be limited, got %v", err)
	}
}

func TestURLsAreSubmittedInBatchesWithTheKeyLocation(t *testing.T) {
	web := newFakeWeb()
	urls := make([]string, batchSize+1)
	for index := range urls {
		urls[index] = "https://asigas.com/p"
	}
	status, err := submitter{client: web.client(), endpoint: Endpoint}.submit(context.Background(), asigas, urls)
	if err != nil || status != 202 {
		t.Fatalf("status %d, err %v", status, err)
	}
	sent := web.sent()
	if len(sent) != 2 || len(sent[0].URLList) != batchSize || len(sent[1].URLList) != 1 {
		t.Fatalf("batches = %d", len(sent))
	}
	if sent[0].Host != "asigas.com" || sent[0].Key != asigas.Key || sent[0].KeyLocation != "https://asigas.com/"+asigas.Key+".txt" {
		t.Errorf("submission = %+v", sent[0])
	}
}
