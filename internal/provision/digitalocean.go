package provision

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"pier/internal/platform/apperr"
)

// DigitalOceanAPI is the public DigitalOcean API.
const DigitalOceanAPI = "https://api.digitalocean.com/v2"

// DigitalOcean is a DigitalOcean droplets client (docs.digitalocean.com/reference/api).
type DigitalOcean struct{ api *apiClient }

// NewDigitalOcean creates a client; a nil http.Client uses Pier's defaults.
func NewDigitalOcean(base, token string, client *http.Client) *DigitalOcean {
	return &DigitalOcean{api: newAPIClient(base, token, client, digitalOceanError)}
}

func digitalOceanError(body []byte) string {
	var answer struct{ Message string }
	if json.Unmarshal(body, &answer) != nil {
		return ""
	}
	return answer.Message
}

type droplet struct {
	ID       int64  `json:"id"`
	Status   string `json:"status"`
	Networks struct {
		V4 []struct {
			IPAddress string `json:"ip_address"`
			Type      string `json:"type"`
		} `json:"v4"`
	} `json:"networks"`
}

func (d droplet) instance() Instance {
	instance := Instance{ExternalID: idString(d.ID), Status: d.Status, Running: d.Status == "active"}
	for _, network := range d.Networks.V4 {
		if network.Type == "public" {
			instance.IPv4 = network.IPAddress
		}
	}
	return instance
}

// Catalog implements Provider.
func (d *DigitalOcean) Catalog(ctx context.Context) (Catalog, error) {
	var regions struct {
		Regions []struct {
			Slug, Name string
			Available  bool
		} `json:"regions"`
	}
	if err := d.api.do(ctx, http.MethodGet, "/regions?per_page=200", nil, &regions); err != nil {
		return Catalog{}, notFoundAsUnavailable(err)
	}
	var sizes struct {
		Sizes []struct {
			Slug         string  `json:"slug"`
			Memory       int     `json:"memory"`
			VCPUs        int     `json:"vcpus"`
			Disk         int     `json:"disk"`
			PriceMonthly float64 `json:"price_monthly"`
			Available    bool    `json:"available"`
		} `json:"sizes"`
	}
	if err := d.api.do(ctx, http.MethodGet, "/sizes?per_page=200", nil, &sizes); err != nil {
		return Catalog{}, notFoundAsUnavailable(err)
	}
	var images struct {
		Images []struct {
			Slug, Name, Distribution string
		} `json:"images"`
	}
	if err := d.api.do(ctx, http.MethodGet, "/images?type=distribution&per_page=200", nil, &images); err != nil {
		return Catalog{}, notFoundAsUnavailable(err)
	}
	catalog := Catalog{Regions: []Option{}, Sizes: []Option{}, Images: []Option{}}
	for _, region := range regions.Regions {
		if region.Available {
			catalog.Regions = append(catalog.Regions, Option{ID: region.Slug, Name: region.Name})
		}
	}
	for _, size := range sizes.Sizes {
		if size.Available {
			detail := fmt.Sprintf("%d vCPU · %d MB RAM · %d GB · US$ %.2f", size.VCPUs, size.Memory, size.Disk, size.PriceMonthly)
			catalog.Sizes = append(catalog.Sizes, Option{ID: size.Slug, Name: size.Slug, Detail: detail})
		}
	}
	for _, image := range images.Images {
		if image.Slug != "" && supportedImage(image.Distribution+" "+image.Slug) {
			catalog.Images = append(catalog.Images, Option{ID: image.Slug, Name: image.Distribution + " " + image.Name})
		}
	}
	return catalog, nil
}

// EnsureKey implements Provider: DigitalOcean addresses keys by fingerprint.
func (d *DigitalOcean) EnsureKey(ctx context.Context, name, publicKey string) (string, error) {
	fingerprint, err := legacyFingerprint(publicKey)
	if err != nil {
		return "", err
	}
	var answer struct {
		SSHKey struct {
			ID int64 `json:"id"`
		} `json:"ssh_key"`
	}
	err = d.api.do(ctx, http.MethodGet, "/account/keys/"+url.PathEscape(fingerprint), nil, &answer)
	if err == nil && answer.SSHKey.ID != 0 {
		return idString(answer.SSHKey.ID), nil
	}
	if err != nil && !errors.Is(err, errNotFound) {
		return "", err
	}
	body := map[string]string{"name": name, "public_key": publicKey}
	if err := d.api.do(ctx, http.MethodPost, "/account/keys", body, &answer); err != nil {
		return "", notFoundAsUnavailable(err)
	}
	return idString(answer.SSHKey.ID), nil
}

// Create implements Provider.
func (d *DigitalOcean) Create(ctx context.Context, request CreateRequest) (Instance, error) {
	keyID, err := strconv.ParseInt(request.KeyRef, 10, 64)
	if err != nil {
		return Instance{}, fmt.Errorf("digitalocean key reference %q: %w", request.KeyRef, err)
	}
	body := map[string]any{
		"name": request.Name, "region": request.Region, "size": request.Size, "image": request.Image,
		"ssh_keys": []int64{keyID}, "user_data": request.UserData, "ipv6": false, "monitoring": false,
		"tags": []string{"pier"},
	}
	var answer struct {
		Droplet droplet `json:"droplet"`
	}
	if err := d.api.do(ctx, http.MethodPost, "/droplets", body, &answer); err != nil {
		return Instance{}, notFoundAsUnavailable(err)
	}
	if answer.Droplet.ID == 0 {
		return Instance{}, apperr.Unavailable("Resposta inesperada do provedor.")
	}
	return answer.Droplet.instance(), nil
}

// Get implements Provider.
func (d *DigitalOcean) Get(ctx context.Context, externalID string) (Instance, error) {
	if _, err := strconv.ParseInt(externalID, 10, 64); err != nil {
		return Instance{}, apperr.Invalid("Identificador de servidor inválido no provedor.")
	}
	var answer struct {
		Droplet droplet `json:"droplet"`
	}
	if err := d.api.do(ctx, http.MethodGet, "/droplets/"+externalID, nil, &answer); err != nil {
		return Instance{}, err
	}
	return answer.Droplet.instance(), nil
}

// Delete implements Provider.
func (d *DigitalOcean) Delete(ctx context.Context, externalID string) error {
	if _, err := strconv.ParseInt(externalID, 10, 64); err != nil {
		return apperr.Invalid("Identificador de servidor inválido no provedor.")
	}
	err := d.api.do(ctx, http.MethodDelete, "/droplets/"+externalID, nil, nil)
	if errors.Is(err, errNotFound) {
		return nil
	}
	return err
}
