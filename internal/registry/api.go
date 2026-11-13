package registry

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"pier/internal/platform/apperr"
)

const (
	apiTimeout       = 15 * time.Second
	maxTokenResponse = 1 << 20
	maxRedirects     = 3
)

// manifestTypes are the manifests a digest may name: single image or index.
var manifestTypes = strings.Join([]string{
	"application/vnd.oci.image.index.v1+json",
	"application/vnd.oci.image.manifest.v1+json",
	"application/vnd.docker.distribution.manifest.list.v2+json",
	"application/vnd.docker.distribution.manifest.v2+json",
}, ", ")

// newHTTPClient talks only HTTPS, follows few redirects and none that leave
// HTTPS, so a registry cannot downgrade where the credential travels.
func newHTTPClient() *http.Client {
	return &http.Client{
		Timeout: apiTimeout,
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects || request.URL.Scheme != "https" {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}
}

var errRejected = errors.New("registry rejected the credential")

// Verify signs in to the registry API (Docker's /v2/ handshake, with the
// Bearer token flow when the registry asks for it) with the stored
// credential. It runs from the panel; servers are not involved.
func (s *Service) Verify(ctx context.Context, id string) error {
	found, err := s.credentialByID(ctx, id)
	if err != nil {
		return apperr.NotFound("Registro de imagens")
	}
	authorization, err := s.authorize(ctx, found, "")
	if err == nil {
		var status int
		status, err = s.send(ctx, http.MethodGet, apiURL(found.host, "/v2/"), authorization, "")
		if err == nil && status != http.StatusOK {
			err = errRejected
		}
	}
	switch {
	case errors.Is(err, errRejected):
		return apperr.Invalid("O registro %s recusou o usuário e a senha. Confira o token e as permissões de leitura e escrita.", found.host)
	case err != nil:
		return apperr.Unavailable("Não foi possível falar com o registro %s a partir do painel.", found.host)
	}
	return nil
}

// ManifestExists asks the registry whether a digest reference still exists.
// It is false only when the registry says so (404); anything inconclusive
// is an error, so callers can decide to go on.
func (s *Service) ManifestExists(ctx context.Context, image string) (bool, error) {
	host, repository, digest, ok := splitDigest(image)
	if !ok {
		return false, apperr.Invalid("Use uma imagem fixada por digest.")
	}
	found, stored, err := s.credentialForHost(ctx, host)
	if err != nil {
		return false, err
	}
	if !stored {
		found = credential{host: host}
	}
	authorization, err := s.authorize(ctx, found, "repository:"+repository+":pull")
	if err != nil {
		return false, err
	}
	status, err := s.send(ctx, http.MethodHead, apiURL(host, "/v2/"+repository+"/manifests/"+digest), authorization, manifestTypes)
	switch {
	case err != nil:
		return false, err
	case status == http.StatusOK:
		return true, nil
	case status == http.StatusNotFound:
		return false, nil
	}
	return false, errors.New("registry answered " + http.StatusText(status))
}

// authorize returns the Authorization header the registry wants for scope:
// none, Basic, or a Bearer token obtained from the realm it announces.
func (s *Service) authorize(ctx context.Context, found credential, scope string) (string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL(found.host, "/v2/"), nil)
	if err != nil {
		return "", err
	}
	response, err := s.client.Do(request)
	if err != nil {
		return "", err
	}
	drain(response)
	if response.StatusCode == http.StatusOK {
		return basic(found), nil
	}
	if response.StatusCode != http.StatusUnauthorized {
		return "", errors.New("registry ping answered " + http.StatusText(response.StatusCode))
	}
	scheme, params := parseChallenge(response.Header.Get("WWW-Authenticate"))
	switch scheme {
	case "basic":
		return basic(found), nil
	case "bearer":
		return s.token(ctx, found, params, scope)
	}
	return "", errors.New("unsupported registry authentication")
}

// token runs the Docker token flow against the realm announced by the
// registry. The realm must be HTTPS: the credential is sent there.
func (s *Service) token(ctx context.Context, found credential, params map[string]string, scope string) (string, error) {
	realm, err := url.Parse(params["realm"])
	if err != nil || realm.Scheme != "https" || realm.Host == "" {
		return "", errors.New("registry announced an invalid token realm")
	}
	query := realm.Query()
	if service := params["service"]; service != "" {
		query.Set("service", service)
	}
	if scope != "" {
		query.Set("scope", scope)
	}
	if found.username != "" {
		query.Set("account", found.username)
	}
	realm.RawQuery = query.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, realm.String(), nil)
	if err != nil {
		return "", err
	}
	if header := basic(found); header != "" {
		request.Header.Set("Authorization", header)
	}
	response, err := s.client.Do(request)
	if err != nil {
		return "", err
	}
	defer drain(response)
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		return "", errRejected
	}
	if response.StatusCode != http.StatusOK {
		return "", errors.New("token endpoint answered " + http.StatusText(response.StatusCode))
	}
	var body struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, maxTokenResponse)).Decode(&body); err != nil {
		return "", errors.New("token endpoint answered an invalid body")
	}
	token := body.Token
	if token == "" {
		token = body.AccessToken
	}
	if token == "" {
		return "", errRejected
	}
	return "Bearer " + token, nil
}

// send performs one API request and returns its status code; bodies are
// never read into errors, so nothing the registry says reaches a caller.
func (s *Service) send(ctx context.Context, method, target, authorization, accept string) (int, error) {
	request, err := http.NewRequestWithContext(ctx, method, target, nil)
	if err != nil {
		return 0, err
	}
	if authorization != "" {
		request.Header.Set("Authorization", authorization)
	}
	if accept != "" {
		request.Header.Set("Accept", accept)
	}
	response, err := s.client.Do(request)
	if err != nil {
		return 0, err
	}
	drain(response)
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		return response.StatusCode, errRejected
	}
	return response.StatusCode, nil
}

func basic(found credential) string {
	if found.username == "" {
		return ""
	}
	request := http.Request{Header: http.Header{}}
	request.SetBasicAuth(found.username, found.password)
	return request.Header.Get("Authorization")
}

func apiURL(host, path string) string { return "https://" + apiHost(host) + path }

func drain(response *http.Response) {
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxTokenResponse))
	_ = response.Body.Close()
}

// parseChallenge reads `Bearer realm="…",service="…",scope="…"`.
func parseChallenge(header string) (string, map[string]string) {
	scheme, rest, _ := strings.Cut(strings.TrimSpace(header), " ")
	params := map[string]string{}
	for rest != "" {
		var pair string
		rest = strings.TrimLeft(rest, " ,")
		key, value, found := strings.Cut(rest, "=")
		if !found {
			break
		}
		if strings.HasPrefix(value, `"`) {
			end := strings.Index(value[1:], `"`)
			if end < 0 {
				break
			}
			pair, rest = value[1:end+1], value[end+2:]
		} else {
			pair, rest, _ = strings.Cut(value, ",")
		}
		params[strings.ToLower(strings.TrimSpace(key))] = pair
	}
	return strings.ToLower(scheme), params
}
