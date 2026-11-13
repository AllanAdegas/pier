// Package provision creates servers at cloud providers (Hetzner Cloud and
// DigitalOcean) and registers them in Pier. It talks to each provider's REST
// API with net/http behind the small Provider interface; it never destroys a
// machine on its own.
package provision

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode"

	"pier/internal/platform/apperr"
	"pier/internal/platform/httpclient"
)

const (
	ProviderHetzner      = "hetzner"
	ProviderDigitalOcean = "digitalocean"

	requestTimeout   = 30 * time.Second
	maxResponseBytes = 8 << 20
	maxProviderText  = 200
)

// Option is one choice of a provider catalog (region, size or image).
type Option struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Detail string `json:"detail"`
}

// Catalog lists what a machine can be created with.
type Catalog struct {
	Regions []Option `json:"regions"`
	Sizes   []Option `json:"sizes"`
	Images  []Option `json:"images"`
}

// CreateRequest describes the machine to create at the provider.
type CreateRequest struct {
	Name, Region, Size, Image, UserData, KeyRef string
}

// Instance is the provider's view of a machine.
type Instance struct {
	ExternalID string
	Status     string
	Running    bool
	IPv4       string
}

// Provider is the part of a cloud API Pier uses.
type Provider interface {
	Catalog(ctx context.Context) (Catalog, error)
	// EnsureKey registers the public key at the provider (idempotent) and
	// returns its reference there.
	EnsureKey(ctx context.Context, name, publicKey string) (string, error)
	Create(ctx context.Context, request CreateRequest) (Instance, error)
	Get(ctx context.Context, externalID string) (Instance, error)
	// Delete destroys the machine; a machine already gone is not an error.
	Delete(ctx context.Context, externalID string) error
}

// ProviderFactory builds the client of one provider for a token.
type ProviderFactory func(token string) Provider

// errNotFound is a 404 from the provider.
var errNotFound = errors.New("not found at provider")

// apiClient is the JSON-over-HTTPS client both providers share.
type apiClient struct {
	base  string
	token string
	http  *http.Client
	// describe extracts the provider's error message from a response body.
	describe func(body []byte) string
}

func newAPIClient(base, token string, client *http.Client, describe func([]byte) string) *apiClient {
	if client == nil {
		client = &http.Client{Timeout: requestTimeout, CheckRedirect: httpclient.Never}
	}
	return &apiClient{base: strings.TrimRight(base, "/"), token: token, http: client, describe: describe}
}

// do sends a request and decodes the JSON answer into out (when not nil).
func (c *apiClient) do(ctx context.Context, method, path string, body, out any) error {
	var payload io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		payload = bytes.NewReader(encoded)
	}
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, method, c.base+path, payload)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+c.token)
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := c.http.Do(request)
	if err != nil {
		return apperr.Unavailable("Não foi possível contatar o provedor: %s", c.withoutToken(err.Error()))
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes))
	if err != nil {
		return apperr.Unavailable("Resposta incompleta do provedor.")
	}
	if err := c.failure(response.StatusCode, raw); err != nil {
		return err
	}
	if out == nil || len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return apperr.Unavailable("Resposta inesperada do provedor.")
	}
	return nil
}

func (c *apiClient) failure(status int, body []byte) error {
	switch {
	case status >= 200 && status < 300:
		return nil
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return apperr.Invalid("O provedor recusou o token. Confira se ele existe e tem permissão de leitura e escrita.")
	case status == http.StatusNotFound:
		return errNotFound
	case status == http.StatusTooManyRequests:
		return apperr.Unavailable("O provedor limitou as requisições. Tente de novo em instantes.")
	}
	return apperr.Unavailable("O provedor respondeu %d: %s", status, c.withoutToken(providerText(c.describe(body))))
}

// withoutToken guarantees the token never reaches a message or a log.
func (c *apiClient) withoutToken(text string) string {
	if c.token == "" {
		return text
	}
	return strings.ReplaceAll(text, c.token, "••••••")
}

// providerText keeps a provider message short and printable.
func providerText(text string) string {
	text = strings.Map(func(r rune) rune {
		if unicode.IsPrint(r) {
			return r
		}
		return ' '
	}, strings.TrimSpace(text))
	if len([]rune(text)) > maxProviderText {
		text = string([]rune(text)[:maxProviderText]) + "…"
	}
	if text == "" {
		return "-"
	}
	return text
}

// supportedImage keeps the images the Docker install assist supports.
func supportedImage(name string) bool {
	name = strings.ToLower(name)
	for _, family := range []string{"ubuntu", "debian", "fedora", "centos"} {
		if strings.Contains(name, family) {
			return true
		}
	}
	return false
}

func idString(id int64) string { return fmt.Sprintf("%d", id) }
