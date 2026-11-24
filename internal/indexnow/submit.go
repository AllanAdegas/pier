package indexnow

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
)

// Endpoint is the shared IndexNow endpoint: participants pass the URLs on to
// each other (Bing, Yandex, Seznam, Naver and others).
const Endpoint = "https://api.indexnow.org/indexnow"

// batchSize is the most URLs IndexNow takes in one request.
const batchSize = 10_000

// errKeyNotServed is returned when https://<host>/<key>.txt does not answer
// with the key, so search engines could not validate the submission.
var errKeyNotServed = errors.New("key not served")

// submitter posts URL lists to IndexNow.
type submitter struct {
	client   *http.Client
	endpoint string
}

type submission struct {
	Host        string   `json:"host"`
	Key         string   `json:"key"`
	KeyLocation string   `json:"keyLocation"`
	URLList     []string `json:"urlList"`
}

// submit sends the URLs in batches and returns the status of the last
// request; it stops at the first batch IndexNow does not accept.
func (s submitter) submit(ctx context.Context, site Site, urls []string) (int, error) {
	status := 0
	for start := 0; start < len(urls); start += batchSize {
		end := min(start+batchSize, len(urls))
		body, err := json.Marshal(submission{Host: site.Host, Key: site.Key, KeyLocation: site.KeyLocation(),
			URLList: urls[start:end]})
		if err != nil {
			return 0, err
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint, bytes.NewReader(body))
		if err != nil {
			return 0, err
		}
		request.Header.Set("Content-Type", "application/json; charset=utf-8")
		response, err := s.client.Do(request)
		if err != nil {
			return 0, err
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
		response.Body.Close()
		status = response.StatusCode
		if !(Outcome{Status: status}).Accepted() {
			return status, nil
		}
	}
	return status, nil
}

// verifyKey checks that the site serves its key where search engines read
// it, so a missing DNS record or route is reported before submitting.
func verifyKey(ctx context.Context, f fetcher, site Site) error {
	body, err := f.get(ctx, site.KeyLocation(), 1<<10)
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(body)) != site.Key {
		return errKeyNotServed
	}
	return nil
}

// pending selects the URLs to submit. Everything goes on the first
// submission (since is nil) or when any entry lacks <lastmod>, because then
// what changed is unknown; otherwise only pages modified after since.
func pending(entries []Entry, since *time.Time) []string {
	changedOnly := since != nil
	for _, entry := range entries {
		if entry.Modified == nil {
			changedOnly = false
			break
		}
	}
	urls := []string{}
	for _, entry := range entries {
		if !changedOnly || entry.Modified.After(*since) {
			urls = append(urls, entry.Loc)
		}
	}
	return urls
}
