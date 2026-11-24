package indexnow

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pier/internal/platform/apperr"
	"pier/internal/platform/executor"
	"pier/internal/platform/sqlite"
	"pier/internal/server"
)

type fakeResources map[string]Resource

func (f fakeResources) Resource(_ context.Context, kind, id string) (Resource, error) {
	resource, found := f[kind+"/"+id]
	if !found {
		return Resource{}, ErrResourceGone
	}
	return resource, nil
}

func (f fakeResources) List(context.Context) ([]ResourceRef, error) {
	refs := []ResourceRef{}
	for key := range f {
		kind, id, _ := strings.Cut(key, "/")
		refs = append(refs, ResourceRef{Kind: kind, ID: id, Name: id})
	}
	return refs, nil
}

type fakeProxies struct {
	servers map[string]server.Server
	ex      map[string]*executor.Fake
}

func (f fakeProxies) Get(_ context.Context, id string) (server.Server, error) {
	return f.servers[id], nil
}

func (f fakeProxies) Executor(_ context.Context, id string) (executor.Executor, server.Server, error) {
	return f.ex[id], f.servers[id], nil
}

type fixture struct {
	service   *Service
	resources fakeResources
	web       *fakeWeb
	local     *executor.Fake
	remote    *executor.Fake
	panelURL  string
}

const routeFile = "/srv/pier/proxy/dynamic/" + RouteFileName

func newFixture(t *testing.T) *fixture {
	t.Helper()
	db, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "pier.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	f := &fixture{
		resources: fakeResources{
			"applications/site": {Domains: []string{"https://asigas.com", "http://legacy.com"}, ServerIDs: []string{"local"}, Proxied: true},
			"stacks/blog":       {Domains: []string{"https://blog.example.com"}, ServerIDs: []string{"vps"}, Proxied: true},
		},
		web: newFakeWeb(), local: executor.NewFake(), remote: executor.NewFake(),
	}
	proxies := fakeProxies{
		servers: map[string]server.Server{
			"local": {ID: "local", Kind: server.KindLocal, DataDir: "/srv/pier"},
			"vps":   {ID: "vps", Kind: server.KindSSH, DataDir: "/srv/pier"},
		},
		ex: map[string]*executor.Fake{"local": f.local, "vps": f.remote},
	}
	publicURL := func(context.Context) string { return f.panelURL }
	f.service = NewService(NewStore(db), f.resources, proxies, publicURL, f.web.client())
	return f
}

func (f *fixture) enable(t *testing.T, kind, id, host string) Overview {
	t.Helper()
	overview, err := f.service.SetEnabled(context.Background(), kind, id, host, true)
	if err != nil {
		t.Fatal(err)
	}
	return overview
}

// publish makes the site serve its key and a sitemap, as the proxy would.
func (f *fixture) publish(t *testing.T, overview Overview) {
	t.Helper()
	location := overview.Hosts[0].KeyLocation
	key := strings.TrimSuffix(location[strings.LastIndex(location, "/")+1:], ".txt")
	f.web.pages["asigas.com/"+key+".txt"] = key + "\n"
	f.web.pages["asigas.com/sitemap.xml"] = urlset
}

// deployLog returns every line the notifier wrote after a deployment.
func (f *fixture) deployLog(kind, id string) []string {
	lines := []string{}
	f.service.Notifier(kind).Deployed(context.Background(), id, func(line string) { lines = append(lines, line) })
	return lines
}

// deploy returns only the IndexNow lines of a deployment.
func (f *fixture) deploy(kind, id string) []string {
	lines := []string{}
	for _, line := range f.deployLog(kind, id) {
		if strings.Contains(line, "IndexNow") {
			lines = append(lines, line)
		}
	}
	return lines
}

func TestTurningNotificationOnServesTheKeyThroughTheProxy(t *testing.T) {
	f := newFixture(t)
	overview := f.enable(t, KindApplication, "site", "asigas.com")
	if !overview.Available || len(overview.Hosts) != 2 || !overview.Hosts[0].Enabled || !overview.Hosts[0].Eligible {
		t.Fatalf("overview = %+v", overview)
	}
	if legacy := overview.Hosts[1]; legacy.Eligible || legacy.Enabled || !strings.Contains(legacy.Reason, "https://") {
		t.Errorf("an http host is listed as never indexable: %+v", legacy)
	}
	location := overview.Hosts[0].KeyLocation
	if !strings.HasPrefix(location, "https://asigas.com/") || !strings.HasSuffix(location, ".txt") {
		t.Errorf("key location = %q", location)
	}
	written := string(f.local.Files[routeFile])
	if !strings.Contains(written, "Host(`asigas.com`)") || !strings.Contains(written, PanelService) {
		t.Errorf("route file = %s", written)
	}
	key := strings.TrimSuffix(location[strings.LastIndex(location, "/")+1:], ".txt")
	if got, err := f.service.Key(context.Background(), key+".txt"); err != nil || got != key {
		t.Errorf("the panel must answer with the key: %q %v", got, err)
	}
}

func TestOnlyEligibleHostsOfTheResourceCanBeTurnedOn(t *testing.T) {
	f := newFixture(t)
	for _, host := range []string{"legacy.com", "blog.example.com", "unknown.com"} {
		_, err := f.service.SetEnabled(context.Background(), KindApplication, "site", host, true)
		if e, ok := apperr.As(err); !ok || e.Kind != apperr.KindInvalid {
			t.Errorf("%s: want invalid, got %v", host, err)
		}
	}
}

func TestARemoteServerNeedsThePublicURLOfThePanel(t *testing.T) {
	f := newFixture(t)
	overview, err := f.service.Overview(context.Background(), KindStack, "blog")
	if err != nil || overview.Available || !strings.Contains(overview.Reason, "URL pública") {
		t.Fatalf("overview = %+v, %v", overview, err)
	}
	if _, err := f.service.SetEnabled(context.Background(), KindStack, "blog", "blog.example.com", true); err == nil {
		t.Errorf("turning on without a way to the panel must be refused")
	}
	f.panelURL = "https://pier.example.com"
	f.enable(t, KindStack, "blog", "blog.example.com")
	if written := string(f.remote.Files[routeFile]); !strings.Contains(written, "https://pier.example.com") {
		t.Errorf("the remote proxy must reach the public URL: %s", written)
	}
}

func TestAHostNotifiesFromOneResourceOnly(t *testing.T) {
	f := newFixture(t)
	f.resources["stacks/copy"] = Resource{Domains: []string{"https://asigas.com"}, ServerIDs: []string{"local"}, Proxied: true}
	f.enable(t, KindApplication, "site", "asigas.com")
	_, err := f.service.SetEnabled(context.Background(), KindStack, "copy", "asigas.com", true)
	if e, ok := apperr.As(err); !ok || e.Kind != apperr.KindConflict {
		t.Fatalf("want conflict, got %v", err)
	}
	// Once the application no longer has the domain, the stack takes it over.
	f.resources["applications/site"] = Resource{Domains: []string{}, ServerIDs: []string{"local"}, Proxied: true}
	f.enable(t, KindStack, "copy", "asigas.com")
}

func TestTurningOffOrLosingTheDomainRemovesTheRoute(t *testing.T) {
	f := newFixture(t)
	overview := f.enable(t, KindApplication, "site", "asigas.com")
	key := strings.TrimSuffix(overview.Hosts[0].KeyLocation[len("https://asigas.com/"):], ".txt")
	if _, err := f.service.SetEnabled(context.Background(), KindApplication, "site", "asigas.com", false); err != nil {
		t.Fatal(err)
	}
	if !f.local.Ran("rm -f -- " + routeFile) {
		t.Errorf("without sites the route file must be removed")
	}
	if _, err := f.service.Key(context.Background(), key+".txt"); err == nil {
		t.Errorf("a key turned off must not be served")
	}
	overview = f.enable(t, KindApplication, "site", "asigas.com")
	key = strings.TrimSuffix(overview.Hosts[0].KeyLocation[len("https://asigas.com/"):], ".txt")
	delete(f.resources, "applications/site")
	if _, err := f.service.Key(context.Background(), key+".txt"); err == nil {
		t.Errorf("the key of a removed resource must not be served")
	}
}

func TestADeploymentSubmitsTheSitemapAndThenOnlyWhatChanged(t *testing.T) {
	f := newFixture(t)
	f.publish(t, f.enable(t, KindApplication, "site", "asigas.com"))
	lines := f.deploy(KindApplication, "site")
	if len(lines) != 1 || !strings.Contains(lines[0], "2 URL(s)") || !strings.Contains(lines[0], "aceito (202)") {
		t.Fatalf("log = %v", lines)
	}
	if sent := f.web.sent(); len(sent) != 1 || len(sent[0].URLList) != 2 {
		t.Fatalf("sent = %+v", sent)
	}
	f.service.now = func() time.Time { return time.Date(2026, 11, 24, 0, 0, 0, 0, time.UTC) }
	lines = f.deploy(KindApplication, "site")
	if len(lines) != 1 || !strings.Contains(lines[0], "nenhuma página mudou") || len(f.web.sent()) != 1 {
		t.Errorf("an unchanged sitemap must not be resent: %v", lines)
	}
	state, err := f.service.SubmitNow(context.Background(), KindApplication, "site", "asigas.com")
	if err != nil || state.Last.URLCount != 2 || len(f.web.sent()) != 2 {
		t.Errorf("Notify now sends everything: %+v %v", state.Last, err)
	}
}

func TestSubmissionProblemsAreWarningsInTheLog(t *testing.T) {
	cases := map[string]func(f *fixture){
		"não respondeu com a chave": func(f *fixture) { f.web.pages = map[string]string{"asigas.com/sitemap.xml": urlset} },
		"nenhum sitemap":            func(f *fixture) { delete(f.web.pages, "asigas.com/sitemap.xml") },
		"chave recusada (403)":      func(f *fixture) { f.web.reply = 403 },
		"envios demais (429)":       func(f *fixture) { f.web.reply = 429 },
		"envio recusado (400)":      func(f *fixture) { f.web.reply = 400 },
		"URLs recusadas (422)":      func(f *fixture) { f.web.reply = 422 },
		"resposta inesperada (500)": func(f *fixture) { f.web.reply = 500 },
		"não chegou ao IndexNow":    func(f *fixture) { f.web.down = true },
	}
	for want, breakIt := range cases {
		f := newFixture(t)
		f.publish(t, f.enable(t, KindApplication, "site", "asigas.com"))
		breakIt(f)
		lines := f.deploy(KindApplication, "site")
		if len(lines) != 1 || !strings.HasPrefix(lines[0], "AVISO: IndexNow:") || !strings.Contains(lines[0], want) {
			t.Errorf("%s: log = %v", want, lines)
		}
		overview, _ := f.service.Overview(context.Background(), KindApplication, "site")
		if last := overview.Hosts[0].Last; last == nil || !last.Problem || last.SubmittedAt != nil {
			t.Errorf("%s: the problem must be recorded: %+v", want, last)
		}
	}
}

// Nothing is indexed unless marked: a deployment says which hosts answer
// noindex and where to mark them.
func TestADeploymentListsTheHostsOutOfSearchEngines(t *testing.T) {
	f := newFixture(t)
	lines := f.deployLog(KindApplication, "site")
	if len(lines) != 1 || !strings.HasPrefix(lines[0], "Fora dos buscadores (noindex): asigas.com, legacy.com.") {
		t.Fatalf("log = %v", lines)
	}
	f.publish(t, f.enable(t, KindApplication, "site", "asigas.com"))
	lines = f.deployLog(KindApplication, "site")
	if !strings.HasPrefix(lines[0], "Fora dos buscadores (noindex): legacy.com.") {
		t.Errorf("a marked host is not out of search engines: %v", lines)
	}
	if lines := f.deployLog(KindStack, "missing"); len(lines) != 0 {
		t.Errorf("log = %v", lines)
	}
}

func TestOnlyMarkedHostsAreIndexable(t *testing.T) {
	f := newFixture(t)
	notifier := f.service.Notifier(KindApplication)
	if hosts := notifier.IndexedHosts(context.Background(), "site"); len(hosts) != 0 {
		t.Errorf("nothing is indexable before marking: %v", hosts)
	}
	f.enable(t, KindApplication, "site", "asigas.com")
	if hosts := notifier.IndexedHosts(context.Background(), "site"); !hosts["asigas.com"] || len(hosts) != 1 {
		t.Errorf("indexed = %v", hosts)
	}
	if hosts := notifier.IndexedHosts(context.Background(), "missing"); len(hosts) != 0 {
		t.Errorf("an unknown resource has nothing indexable: %v", hosts)
	}
}

func TestAHostWithOnlyPathRoutesCanNeverBeIndexed(t *testing.T) {
	f := newFixture(t)
	f.resources["applications/api"] = Resource{Domains: []string{"https://api.example.com/v1"}, ServerIDs: []string{"local"}, Proxied: true}
	overview, err := f.service.Overview(context.Background(), KindApplication, "api")
	if err != nil || len(overview.Hosts) != 1 || overview.Hosts[0].Eligible || !strings.Contains(overview.Hosts[0].Reason, "sem caminho") {
		t.Fatalf("overview = %+v, %v", overview, err)
	}
}

func TestTheInstanceViewListsTheHostsOfEveryResource(t *testing.T) {
	f := newFixture(t)
	f.resources["stacks/internal"] = Resource{Domains: []string{}, ServerIDs: []string{"local"}, Proxied: true}
	f.enable(t, KindApplication, "site", "asigas.com")
	all, err := f.service.All(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]ResourceOverview{}
	for _, resource := range all {
		byID[resource.ResourceID] = resource
	}
	if len(all) != 2 || len(byID["site"].Hosts) != 2 || !byID["site"].Hosts[0].Enabled || byID["blog"].ResourceKind != KindStack {
		t.Errorf("resources without domains are left out, the others listed: %+v", all)
	}
}
