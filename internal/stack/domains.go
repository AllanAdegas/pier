package stack

import (
	"context"

	"pier/internal/compose"
	"pier/internal/server"
)

// DomainChecker tells whether a domain already resolves to a server, so no
// certificate is requested for a domain that cannot pass validation yet:
// repeated failures hit the certificate authority's rate limits.
type DomainChecker interface {
	CheckDomain(ctx context.Context, serverID, domain string) (server.DomainCheck, error)
}

// SetDomainChecker installs the DNS check run before HTTPS routes are published.
func (s *Service) SetDomainChecker(checker DomainChecker) { s.domains = checker }

// withPendingCertificates records on the run which HTTPS hosts do not point
// to the server yet. Those are published over HTTP only and the operation log
// says which record to create; the next deployment after DNS is fixed asks
// for the certificate. A failed check keeps the route as configured.
func (s *Service) withPendingCertificates(ctx context.Context, run *stackRun) {
	if s.domains == nil || run.target.Proxy.Entry().Edge {
		return
	}
	pending := map[string]bool{}
	checked := map[string]bool{}
	for _, route := range run.stack.Routes {
		parsed, err := compose.ParseRoute(route.Domain)
		if err != nil || parsed.Scheme != "https" || checked[parsed.Host] {
			continue
		}
		checked[parsed.Host] = true
		result, err := s.domains.CheckDomain(ctx, run.stack.ServerID, parsed.Host)
		if err != nil {
			run.operation.Say("Não foi possível verificar o DNS de %s; o certificado HTTPS será solicitado normalmente.", parsed.Host)
			continue
		}
		if !result.OK {
			pending[parsed.Host] = true
			run.operation.Say("AVISO: o domínio %s ainda não aponta para este servidor: publicado só em HTTP, sem pedir "+
				"certificado. %s Depois que o DNS propagar, faça um novo deploy: o certificado HTTPS será emitido nele.",
				parsed.Host, result.RecordHint)
		}
	}
	run.stack.pendingHosts = pending
}
