package application

import (
	"context"

	"pier/internal/compose"
	"pier/internal/diagnose"
	"pier/internal/platform/i18n"
	"pier/internal/server"
)

// DomainChecker tells whether a domain already resolves to a server, so no
// certificate is requested for a domain that cannot pass validation yet:
// repeated failures hit the certificate authority's rate limits.
type DomainChecker interface {
	CheckDomain(ctx context.Context, serverID, domain string) (server.DomainCheck, error)
}

// SetDomainChecker installs the DNS check run before HTTPS routes are released.
func (d *Deployer) SetDomainChecker(checker DomainChecker) { d.domains = checker }

// pendingCertificates returns the HTTPS hosts that do not point to the server
// yet. They are published over HTTP only, and the deployment explains which
// record to create; the next deployment after DNS is fixed asks for the
// certificate. A failed check keeps the route as configured.
func (j *job) pendingCertificates(ctx context.Context) map[string]bool {
	if j.domains == nil || j.server.Proxy.Entry().Edge {
		return nil
	}
	routes, err := parseRoutes(j.app.Config.Network.Domains)
	if err != nil {
		return nil
	}
	pending := map[string]bool{}
	checked := map[string]bool{}
	for _, route := range routes {
		if route.Scheme != "https" || checked[route.Host] {
			continue
		}
		checked[route.Host] = true
		result, err := j.domains.CheckDomain(ctx, j.app.ServerID, route.Host)
		if err != nil {
			j.log.Info("Não foi possível verificar o DNS de %s; o certificado HTTPS será solicitado normalmente.", route.Host)
			continue
		}
		if !result.OK {
			pending[route.Host] = true
			j.reportPendingCertificate(route.Host, result.RecordHint)
		}
	}
	if len(pending) > 0 {
		_ = j.log.store.setDeploymentDetails(context.WithoutCancel(ctx), j.deployment)
	}
	return pending
}

func (j *job) reportPendingCertificate(host, recordHint string) {
	language := i18n.Instance()
	j.log.Info("O domínio %s ainda não aponta para este servidor: publicado só em HTTP, sem pedir certificado. %s", host, recordHint)
	j.deployment.Hints = append(j.deployment.Hints, diagnose.Hint{
		Code:  "dns-not-pointing",
		Title: language.Sprintf("O domínio %s ainda não aponta para este servidor", host),
		Fix: language.Sprintf("%s Depois que o DNS propagar, faça um novo deploy: o certificado HTTPS será emitido nele.",
			recordHint),
	})
}

// withoutPendingCertificates serves the pending hosts over plain HTTP.
func withoutPendingCertificates(routes []compose.Route, pending map[string]bool) []compose.Route {
	published := make([]compose.Route, len(routes))
	for index, route := range routes {
		if pending[route.Host] && route.Scheme == "https" {
			route.Scheme = "http"
		}
		published[index] = route
	}
	return published
}
