package indexnow

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"time"

	"pier/internal/compose"
	"pier/internal/platform/apperr"
	"pier/internal/platform/i18n"
)

// deployBudget bounds the notification at the end of one deployment.
const deployBudget = 60 * time.Second

// ErrResourceGone is returned by Resources for a resource that no longer exists.
var ErrResourceGone = errors.New("resource gone")

// Resource is what notification needs to know about an application or stack.
type Resource struct {
	Domains []string
	// ServerIDs are the servers whose proxy publishes the domains.
	ServerIDs []string
	// Proxied is false where Pier's proxy does not route the domains
	// (Swarm services).
	Proxied bool
}

// ResourceRef names an application or stack for the overview of all sites.
type ResourceRef struct {
	Kind string
	ID   string
	Name string
}

// Resources resolves the resources whose domains notify search engines.
type Resources interface {
	Resource(ctx context.Context, kind, id string) (Resource, error)
	// List returns every application and stack.
	List(ctx context.Context) ([]ResourceRef, error)
}

// Service manages the sites and their submissions.
type Service struct {
	store     *Store
	resources Resources
	proxies   Proxies
	locks     serverLocks
	publicURL func(context.Context) string
	fetch     fetcher
	submit    submitter
	now       func() time.Time
}

// NewService creates the service. client is the HTTP client of every
// request to the sites and to IndexNow (NewHTTPClient in production).
func NewService(store *Store, resources Resources, proxies Proxies, publicURL func(context.Context) string,
	client *http.Client) *Service {
	return &Service{store: store, resources: resources, proxies: proxies, publicURL: publicURL,
		fetch: fetcher{client: client}, submit: submitter{client: client, endpoint: Endpoint},
		now: func() time.Time { return time.Now().UTC() }}
}

// SetEndpoint points submissions at another IndexNow endpoint (tests).
func (s *Service) SetEndpoint(endpoint string) { s.submit.endpoint = endpoint }

// Overview is the notification state of a resource's domains.
type Overview struct {
	// Available is false when the resource cannot serve keys; Reason says why.
	Available bool        `json:"available"`
	Reason    string      `json:"reason"`
	Hosts     []HostState `json:"hosts"`
	// Warning reports a proxy that could not be updated after a change.
	Warning string `json:"warning"`
}

// HostState is one eligible host of a resource.
type HostState struct {
	Host string `json:"host"`
	// Eligible is false for a host that always answers noindex; Reason
	// says why (http only, or only routes with a path).
	Eligible    bool     `json:"eligible"`
	Reason      string   `json:"reason"`
	Enabled     bool     `json:"enabled"`
	KeyLocation string   `json:"key_location"`
	Last        *Outcome `json:"last"`
}

// Overview lists the eligible hosts of a resource and their state.
func (s *Service) Overview(ctx context.Context, kind, id string) (Overview, error) {
	resource, sites, err := s.load(ctx, kind, id)
	if err != nil {
		return Overview{}, err
	}
	return s.overview(ctx, resource, sites), nil
}

// ResourceOverview is the overview of one resource in the list of all.
type ResourceOverview struct {
	ResourceKind string `json:"resource_kind"`
	ResourceID   string `json:"resource_id"`
	ResourceName string `json:"resource_name"`
	Overview
}

// All returns the overview of every resource that publishes a domain, for
// the instance-wide view. It reads only the database.
func (s *Service) All(ctx context.Context) ([]ResourceOverview, error) {
	refs, err := s.resources.List(ctx)
	if err != nil {
		return nil, err
	}
	all := []ResourceOverview{}
	for _, ref := range refs {
		overview, err := s.Overview(ctx, ref.Kind, ref.ID)
		if err != nil {
			return nil, err
		}
		if len(overview.Hosts) > 0 {
			all = append(all, ResourceOverview{ResourceKind: ref.Kind, ResourceID: ref.ID, ResourceName: ref.Name,
				Overview: overview})
		}
	}
	return all, nil
}

// SetEnabled turns notification on or off for a host of a resource and
// updates the proxies that publish it.
func (s *Service) SetEnabled(ctx context.Context, kind, id, host string, enabled bool) (Overview, error) {
	resource, sites, err := s.load(ctx, kind, id)
	if err != nil {
		return Overview{}, err
	}
	if !slices.Contains(Eligible(resource.Domains), host) {
		return Overview{}, apperr.Invalid("Use um domínio https:// sem caminho deste recurso.")
	}
	current, found := siteOf(sites, host)
	switch {
	case enabled && !found:
		if reason := s.unavailable(ctx, resource); reason != "" {
			return Overview{}, apperr.Invalid("%s", reason)
		}
		site, err := s.claim(ctx, kind, id, host)
		if err != nil {
			return Overview{}, err
		}
		sites = append(sites, site)
	case !enabled && found:
		if err := s.store.Delete(ctx, current.ID); err != nil {
			return Overview{}, err
		}
		sites = slices.DeleteFunc(sites, func(site Site) bool { return site.ID == current.ID })
	}
	overview := s.overview(ctx, resource, sites)
	if err := s.syncServers(ctx, resource.ServerIDs); err != nil {
		overview.Warning = i18n.Localize(err, i18n.FromContext(ctx))
	}
	return overview, nil
}

// SubmitNow submits every page of a host's sitemap and returns the outcome.
func (s *Service) SubmitNow(ctx context.Context, kind, id, host string) (HostState, error) {
	_, sites, err := s.load(ctx, kind, id)
	if err != nil {
		return HostState{}, err
	}
	site, found := siteOf(sites, host)
	if !found {
		return HostState{}, apperr.NotFound("Domínio")
	}
	outcome := s.notify(ctx, site, true)
	return HostState{Host: site.Host, Enabled: true, KeyLocation: site.KeyLocation(), Last: &outcome}, nil
}

// Key returns the key requested at /indexnow/<key>.txt, when it belongs to
// a host that still notifies. Any other request is not found, without
// telling a missing key from a disabled one.
func (s *Service) Key(ctx context.Context, file string) (string, error) {
	key, valid := keyFromFile(file)
	if !valid {
		return "", apperr.NotFound("Chave")
	}
	site, err := s.store.ByKey(ctx, key)
	if err != nil {
		return "", apperr.NotFound("Chave")
	}
	if _, sites, err := s.load(ctx, site.ResourceKind, site.ResourceID); err != nil || !hasSite(sites, site.ID) {
		return "", apperr.NotFound("Chave")
	}
	return site.Key, nil
}

// load resolves a resource and its sites, pruning the sites of hosts it no
// longer has.
func (s *Service) load(ctx context.Context, kind, id string) (Resource, []Site, error) {
	resource, err := s.resources.Resource(ctx, kind, id)
	if err != nil {
		if errors.Is(err, ErrResourceGone) {
			_ = s.pruneResource(ctx, kind, id)
			return Resource{}, nil, apperr.NotFound("Recurso")
		}
		return Resource{}, nil, err
	}
	sites, err := s.store.ByResource(ctx, kind, id)
	if err != nil {
		return Resource{}, nil, err
	}
	eligible := Eligible(resource.Domains)
	kept := []Site{}
	for _, site := range sites {
		if slices.Contains(eligible, site.Host) {
			kept = append(kept, site)
			continue
		}
		if err := s.store.Delete(ctx, site.ID); err != nil {
			return Resource{}, nil, err
		}
	}
	return resource, kept, nil
}

func (s *Service) pruneResource(ctx context.Context, kind, id string) error {
	sites, err := s.store.ByResource(ctx, kind, id)
	if err != nil {
		return err
	}
	for _, site := range sites {
		if err := s.store.Delete(ctx, site.ID); err != nil {
			return err
		}
	}
	return nil
}

// claim creates the site of a host, taking it over from a resource that no
// longer has it (loading the owner prunes its site); a host still published
// by another resource is a conflict.
func (s *Service) claim(ctx context.Context, kind, id, host string) (Site, error) {
	site, err := s.store.Create(ctx, kind, id, host)
	if !errors.Is(err, errHostTaken) {
		return site, err
	}
	owner, err := s.store.ByHost(ctx, host)
	if err != nil {
		return Site{}, err
	}
	if _, sites, err := s.load(ctx, owner.ResourceKind, owner.ResourceID); err == nil && hasSite(sites, owner.ID) {
		return Site{}, apperr.Conflict("Outro recurso já avisa os buscadores sobre %s.", host)
	}
	site, err = s.store.Create(ctx, kind, id, host)
	if errors.Is(err, errHostTaken) {
		return Site{}, apperr.Conflict("Outro recurso já avisa os buscadores sobre %s.", host)
	}
	return site, err
}

func (s *Service) overview(ctx context.Context, resource Resource, sites []Site) Overview {
	overview := Overview{Hosts: []HostState{}}
	overview.Reason = s.unavailable(ctx, resource)
	overview.Available = overview.Reason == ""
	eligible := Eligible(resource.Domains)
	for _, host := range routedHosts(resource.Domains) {
		state := HostState{Host: host, Eligible: slices.Contains(eligible, host)}
		if !state.Eligible {
			state.Reason = ineligibility(ctx, resource.Domains, host)
		}
		if site, found := siteOf(sites, host); found {
			last := site.Last
			state.Enabled, state.KeyLocation, state.Last = true, site.KeyLocation(), &last
		}
		overview.Hosts = append(overview.Hosts, state)
	}
	return overview
}

// unavailable explains why a resource cannot serve keys, or returns "".
func (s *Service) unavailable(ctx context.Context, resource Resource) string {
	if !resource.Proxied {
		return i18n.FromContext(ctx).Text("Disponível só para recursos publicados pelo proxy do Pier, fora do Swarm.")
	}
	reachable, err := s.reachable(ctx, resource.ServerIDs)
	if err != nil {
		return i18n.Localize(err, i18n.FromContext(ctx))
	}
	if !reachable {
		return i18n.FromContext(ctx).Text("Configure a URL pública do Pier em Configurações para avisar " +
			"buscadores a partir de outros servidores.")
	}
	return ""
}

func siteOf(sites []Site, host string) (Site, bool) {
	for _, site := range sites {
		if site.Host == host {
			return site, true
		}
	}
	return Site{}, false
}

func hasSite(sites []Site, id string) bool {
	return slices.ContainsFunc(sites, func(site Site) bool { return site.ID == id })
}

// ineligibility explains why a host can never be indexed.
func ineligibility(ctx context.Context, domains []string, host string) string {
	if slices.Contains(Eligible(rewriteScheme(domains, "https")), host) {
		return i18n.FromContext(ctx).Text("Só domínios https:// podem ser indexados.")
	}
	return i18n.FromContext(ctx).Text("Só um domínio com uma rota sem caminho pode ser indexado.")
}

// rewriteScheme forces a scheme on the domains, to tell an http-only host
// from one that only has routes with a path.
func rewriteScheme(domains []string, scheme string) []string {
	rewritten := make([]string, 0, len(domains))
	for _, domain := range domains {
		if route, err := compose.ParseRoute(domain); err == nil {
			route.Scheme = scheme
			rewritten = append(rewritten, route.String())
		}
	}
	return rewritten
}
