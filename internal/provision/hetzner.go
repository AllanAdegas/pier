package provision

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"golang.org/x/crypto/ssh"

	"pier/internal/platform/apperr"
)

// HetznerAPI is the public Hetzner Cloud API.
const HetznerAPI = "https://api.hetzner.cloud/v1"

// Hetzner is a Hetzner Cloud client (docs.hetzner.cloud).
type Hetzner struct{ api *apiClient }

// NewHetzner creates a client; a nil http.Client uses Pier's defaults.
func NewHetzner(base, token string, client *http.Client) *Hetzner {
	return &Hetzner{api: newAPIClient(base, token, client, hetznerError)}
}

func hetznerError(body []byte) string {
	var answer struct {
		Error struct{ Code, Message string } `json:"error"`
	}
	if json.Unmarshal(body, &answer) != nil {
		return ""
	}
	return answer.Error.Message
}

type hetznerServer struct {
	ID        int64  `json:"id"`
	Status    string `json:"status"`
	PublicNet struct {
		IPv4 struct {
			IP string `json:"ip"`
		} `json:"ipv4"`
	} `json:"public_net"`
}

func (s hetznerServer) instance() Instance {
	return Instance{ExternalID: idString(s.ID), Status: s.Status, Running: s.Status == "running", IPv4: s.PublicNet.IPv4.IP}
}

// Catalog implements Provider.
func (h *Hetzner) Catalog(ctx context.Context) (Catalog, error) {
	var locations struct {
		Locations []struct{ Name, Description, City, Country string } `json:"locations"`
	}
	if err := h.api.do(ctx, http.MethodGet, "/locations?per_page=50", nil, &locations); err != nil {
		return Catalog{}, notFoundAsUnavailable(err)
	}
	var types struct {
		ServerTypes []struct {
			Name         string  `json:"name"`
			Cores        int     `json:"cores"`
			Memory       float64 `json:"memory"`
			Disk         int     `json:"disk"`
			Architecture string  `json:"architecture"`
			Deprecated   bool    `json:"deprecated"`
		} `json:"server_types"`
	}
	if err := h.api.do(ctx, http.MethodGet, "/server_types?per_page=50", nil, &types); err != nil {
		return Catalog{}, notFoundAsUnavailable(err)
	}
	var images struct {
		Images []struct {
			Name         string `json:"name"`
			Description  string `json:"description"`
			Architecture string `json:"architecture"`
		} `json:"images"`
	}
	if err := h.api.do(ctx, http.MethodGet, "/images?type=system&status=available&per_page=50", nil, &images); err != nil {
		return Catalog{}, notFoundAsUnavailable(err)
	}
	catalog := Catalog{Regions: []Option{}, Sizes: []Option{}, Images: []Option{}}
	for _, location := range locations.Locations {
		catalog.Regions = append(catalog.Regions, Option{ID: location.Name, Name: location.Description, Detail: location.City + ", " + location.Country})
	}
	for _, kind := range types.ServerTypes {
		if !kind.Deprecated {
			detail := fmt.Sprintf("%d vCPU · %g GB RAM · %d GB · %s", kind.Cores, kind.Memory, kind.Disk, kind.Architecture)
			catalog.Sizes = append(catalog.Sizes, Option{ID: kind.Name, Name: kind.Name, Detail: detail})
		}
	}
	for _, image := range images.Images {
		if image.Name != "" && supportedImage(image.Name) {
			catalog.Images = append(catalog.Images, Option{ID: image.Name, Name: image.Description, Detail: image.Architecture})
		}
	}
	return catalog, nil
}

// EnsureKey implements Provider: a key already uploaded is found by its
// MD5 fingerprint, which is how Hetzner identifies keys.
func (h *Hetzner) EnsureKey(ctx context.Context, name, publicKey string) (string, error) {
	fingerprint, err := legacyFingerprint(publicKey)
	if err != nil {
		return "", err
	}
	var found struct {
		SSHKeys []struct {
			ID int64 `json:"id"`
		} `json:"ssh_keys"`
	}
	if err := h.api.do(ctx, http.MethodGet, "/ssh_keys?fingerprint="+url.QueryEscape(fingerprint), nil, &found); err != nil {
		return "", notFoundAsUnavailable(err)
	}
	if len(found.SSHKeys) > 0 {
		return idString(found.SSHKeys[0].ID), nil
	}
	var created struct {
		SSHKey struct {
			ID int64 `json:"id"`
		} `json:"ssh_key"`
	}
	body := map[string]string{"name": name, "public_key": publicKey}
	if err := h.api.do(ctx, http.MethodPost, "/ssh_keys", body, &created); err != nil {
		return "", notFoundAsUnavailable(err)
	}
	return idString(created.SSHKey.ID), nil
}

// Create implements Provider.
func (h *Hetzner) Create(ctx context.Context, request CreateRequest) (Instance, error) {
	keyID, err := strconv.ParseInt(request.KeyRef, 10, 64)
	if err != nil {
		return Instance{}, fmt.Errorf("hetzner key reference %q: %w", request.KeyRef, err)
	}
	body := map[string]any{
		"name": request.Name, "server_type": request.Size, "location": request.Region, "image": request.Image,
		"ssh_keys": []int64{keyID}, "user_data": request.UserData, "start_after_create": true,
		"labels": map[string]string{"managed-by": "pier"},
	}
	var answer struct {
		Server hetznerServer `json:"server"`
	}
	if err := h.api.do(ctx, http.MethodPost, "/servers", body, &answer); err != nil {
		return Instance{}, notFoundAsUnavailable(err)
	}
	if answer.Server.ID == 0 {
		return Instance{}, apperr.Unavailable("Resposta inesperada do provedor.")
	}
	return answer.Server.instance(), nil
}

// Get implements Provider.
func (h *Hetzner) Get(ctx context.Context, externalID string) (Instance, error) {
	if _, err := strconv.ParseInt(externalID, 10, 64); err != nil {
		return Instance{}, apperr.Invalid("Identificador de servidor inválido no provedor.")
	}
	var answer struct {
		Server hetznerServer `json:"server"`
	}
	if err := h.api.do(ctx, http.MethodGet, "/servers/"+externalID, nil, &answer); err != nil {
		return Instance{}, err
	}
	return answer.Server.instance(), nil
}

// Delete implements Provider.
func (h *Hetzner) Delete(ctx context.Context, externalID string) error {
	if _, err := strconv.ParseInt(externalID, 10, 64); err != nil {
		return apperr.Invalid("Identificador de servidor inválido no provedor.")
	}
	err := h.api.do(ctx, http.MethodDelete, "/servers/"+externalID, nil, nil)
	if errors.Is(err, errNotFound) {
		return nil
	}
	return err
}

func legacyFingerprint(publicKey string) (string, error) {
	key, _, _, _, err := ssh.ParseAuthorizedKey([]byte(publicKey))
	if err != nil {
		return "", apperr.Invalid("Chave pública SSH inválida.")
	}
	return ssh.FingerprintLegacyMD5(key), nil
}

// notFoundAsUnavailable turns a 404 on a collection into a readable error.
func notFoundAsUnavailable(err error) error {
	if errors.Is(err, errNotFound) {
		return apperr.Unavailable("O provedor não reconheceu a requisição (404).")
	}
	return err
}
