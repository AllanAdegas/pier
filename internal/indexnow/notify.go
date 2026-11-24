package indexnow

import (
	"context"
	"errors"
	"strings"

	"pier/internal/platform/i18n"
)

// Notifier tells search engines about one kind of resource after a
// deployment. The application and stack packages depend on their own small
// interface, which it satisfies.
type Notifier struct {
	service *Service
	kind    string
}

// Notifier returns the notifier of a kind of resource.
func (s *Service) Notifier(kind string) Notifier { return Notifier{service: s, kind: kind} }

// Deployed runs after a successful deployment: it updates the proxies of the
// resource and submits the changed pages of each host with notification on.
// Every problem is reported as a warning; nothing here fails the deployment.
func (n Notifier) Deployed(ctx context.Context, id string, report func(string)) {
	s := n.service
	ctx, cancel := context.WithTimeout(ctx, deployBudget)
	defer cancel()
	resource, sites, err := s.load(ctx, n.kind, id)
	if err != nil || len(sites) == 0 {
		return
	}
	if reason := s.unavailable(ctx, resource); reason != "" {
		report(i18n.Instance().Sprintf("AVISO: IndexNow: %s", reason))
		return
	}
	if err := s.syncServers(ctx, resource.ServerIDs); err != nil {
		report(i18n.Instance().Sprintf("AVISO: IndexNow: o proxy não foi atualizado: %s", i18n.Localize(err, i18n.Instance())))
	}
	for _, site := range sites {
		outcome := s.notify(ctx, site, false)
		if outcome.Problem {
			report(i18n.Instance().Sprintf("AVISO: %s", outcome.Message))
			continue
		}
		report(outcome.Message)
	}
}

// notify submits the pages of a site and records the outcome. everything
// sends the whole sitemap; otherwise only what changed since the last
// accepted submission.
func (s *Service) notify(ctx context.Context, site Site, everything bool) Outcome {
	now := s.now()
	outcome := Outcome{AttemptedAt: &now, SubmittedAt: site.Last.SubmittedAt}
	language := i18n.Instance()
	urls, sources, err := s.collect(ctx, site, everything)
	switch {
	case err != nil:
		outcome.Message, outcome.Problem = describe(err, site, language), true
	case len(urls) == 0:
		outcome.Message = language.Text("IndexNow: nenhuma página mudou desde o último envio.")
	default:
		outcome.URLCount = len(urls)
		outcome.Status, err = s.submit.submit(ctx, site, urls)
		outcome.Message = submitted(outcome, err, sources, language)
		outcome.Problem = !outcome.Accepted()
		if outcome.Accepted() {
			outcome.SubmittedAt = &now
		}
	}
	_ = s.store.Record(context.WithoutCancel(ctx), site.ID, outcome)
	return outcome
}

// collect checks the key and returns the URLs to submit and the sitemaps
// they came from.
func (s *Service) collect(ctx context.Context, site Site, everything bool) ([]string, string, error) {
	if err := verifyKey(ctx, s.fetch, site); err != nil {
		return nil, "", keyError{cause: err}
	}
	found, err := discover(ctx, s.fetch, site.Host)
	if err != nil {
		return nil, "", err
	}
	since := site.Last.SubmittedAt
	if everything {
		since = nil
	}
	sources := strings.Join(found.Sources, ", ")
	if found.Truncated {
		sources = i18n.Instance().Sprintf("%s (só as primeiras %d páginas)", sources, maxURLs)
	}
	return pending(found.Entries, since), sources, nil
}

// keyError marks a failure to read the key back from the site.
type keyError struct{ cause error }

func (e keyError) Error() string { return e.cause.Error() }
func (e keyError) Unwrap() error { return e.cause }

func submitted(outcome Outcome, err error, sources string, language i18n.Language) string {
	if err != nil {
		return language.Sprintf("IndexNow: o envio não chegou ao IndexNow: %s", err)
	}
	switch outcome.Status {
	case 200, 202:
		return language.Sprintf("IndexNow: %d URL(s) de %s enviadas; aceito (%d).", outcome.URLCount, sources, outcome.Status)
	case 400:
		return language.Text("IndexNow: envio recusado (400): formato inválido.")
	case 403:
		return language.Text("IndexNow: chave recusada (403): o buscador não conseguiu ler o arquivo da chave.")
	case 422:
		return language.Text("IndexNow: URLs recusadas (422): não pertencem ao domínio ou à chave.")
	case 429:
		return language.Text("IndexNow: envios demais (429): o próximo deploy tenta de novo.")
	}
	return language.Sprintf("IndexNow: resposta inesperada (%d).", outcome.Status)
}

func describe(err error, site Site, language i18n.Language) string {
	var key keyError
	switch {
	case errors.Is(err, errNotPublic):
		return language.Sprintf("IndexNow: %s não resolve para um endereço público; nada foi enviado.", site.Host)
	case errors.As(err, &key):
		return language.Sprintf("IndexNow: %s não respondeu com a chave (%s). Confira o DNS e o proxy; nada foi enviado.",
			site.KeyLocation(), key.cause)
	case errors.Is(err, errNoSitemap):
		return language.Sprintf("IndexNow: nenhum sitemap encontrado em https://%s (robots.txt e /sitemap.xml).", site.Host)
	case errors.Is(err, errTooLarge):
		return language.Sprintf("IndexNow: sitemap acima do limite de leitura: %s", err)
	}
	return language.Sprintf("IndexNow: não foi possível ler o sitemap: %s", err)
}
