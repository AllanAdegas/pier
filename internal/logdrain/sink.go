package logdrain

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"pier/internal/platform/httpclient"
)

const (
	sinkTimeout   = 10 * time.Second
	lokiPushPath  = "/loki/api/v1/push"
	maxErrorBytes = 512
)

// Record is one log line with its labels.
type Record struct {
	Time    time.Time         `json:"timestamp"`
	Message string            `json:"message"`
	Labels  map[string]string `json:"labels"`
}

// Sink delivers batches of records to an external system.
type Sink interface {
	Send(ctx context.Context, records []Record) error
}

// newSink builds the sink of a drain with its opened token.
func newSink(drain Drain, token string, client *http.Client) Sink {
	if client == nil {
		client = &http.Client{Timeout: sinkTimeout, CheckRedirect: httpclient.SameOrigin}
	}
	if drain.Kind == KindLoki {
		return &lokiSink{endpoint: lokiEndpoint(drain.URL), token: token, client: client}
	}
	return &httpSink{endpoint: drain.URL, token: token, client: client}
}

// httpSink posts a JSON array of records, with an optional bearer token.
type httpSink struct {
	endpoint, token string
	client          *http.Client
}

func (s *httpSink) Send(ctx context.Context, records []Record) error {
	body, err := json.Marshal(records)
	if err != nil {
		return err
	}
	return post(ctx, s.client, s.endpoint, body, func(request *http.Request) {
		if s.token != "" {
			request.Header.Set("Authorization", "Bearer "+s.token)
		}
	}, s.token)
}

// lokiSink uses the Loki push API (JSON), grouping records into streams by
// label set. A token "user:password" is sent as basic auth (Grafana Cloud),
// any other token as a bearer token.
type lokiSink struct {
	endpoint, token string
	client          *http.Client
}

type lokiStream struct {
	Stream map[string]string `json:"stream"`
	Values [][2]string       `json:"values"`
}

func (s *lokiSink) Send(ctx context.Context, records []Record) error {
	streams := map[string]*lokiStream{}
	keys := []string{}
	for _, record := range records {
		key := labelKey(record.Labels)
		stream, found := streams[key]
		if !found {
			stream = &lokiStream{Stream: record.Labels, Values: [][2]string{}}
			streams[key] = stream
			keys = append(keys, key)
		}
		stream.Values = append(stream.Values, [2]string{strconv.FormatInt(record.Time.UnixNano(), 10), record.Message})
	}
	payload := struct {
		Streams []*lokiStream `json:"streams"`
	}{}
	for _, key := range keys {
		payload.Streams = append(payload.Streams, streams[key])
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return post(ctx, s.client, s.endpoint, body, func(request *http.Request) {
		if user, password, basic := strings.Cut(s.token, ":"); basic {
			request.SetBasicAuth(user, password)
		} else if s.token != "" {
			request.Header.Set("Authorization", "Bearer "+s.token)
		}
	}, s.token)
}

func lokiEndpoint(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Path != "" && parsed.Path != "/") {
		return raw
	}
	parsed.Path = lokiPushPath
	return parsed.String()
}

func labelKey(labels map[string]string) string {
	names := make([]string, 0, len(labels))
	for name := range labels {
		names = append(names, name)
	}
	sort.Strings(names)
	var key strings.Builder
	for _, name := range names {
		key.WriteString(name + "\x00" + labels[name] + "\x00")
	}
	return key.String()
}

// post sends a JSON body; errors never contain the token.
func post(ctx context.Context, client *http.Client, endpoint string, body []byte, authorize func(*http.Request), token string) error {
	ctx, cancel := context.WithTimeout(ctx, sinkTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "pier-logdrain")
	authorize(request)
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("%s", hideToken(err.Error(), token))
	}
	defer response.Body.Close()
	answer, _ := io.ReadAll(io.LimitReader(response.Body, maxErrorBytes))
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return fmt.Errorf("HTTP %d: %s", response.StatusCode, hideToken(printable(string(answer)), token))
	}
	return nil
}

func hideToken(text, token string) string {
	if token == "" {
		return text
	}
	text = strings.ReplaceAll(text, token, "••••••")
	if _, password, basic := strings.Cut(token, ":"); basic && password != "" {
		text = strings.ReplaceAll(text, password, "••••••")
	}
	return text
}

func printable(text string) string {
	return strings.TrimSpace(strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, text))
}
