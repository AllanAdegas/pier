package server

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"sort"
	"strings"
	"time"

	"pier/internal/compose"
	"pier/internal/platform/apperr"
	"pier/internal/platform/i18n"
)

// DomainCheck compares every resolved A/AAAA record with the server address.
type DomainCheck struct {
	OK         bool     `json:"ok"`
	Addresses  []string `json:"addresses"`
	Expected   []string `json:"expected"`
	RecordHint string   `json:"record_hint"`
}
type lookupAddresses func(context.Context, string, string) ([]netip.Addr, error)

// CheckDomain checks DNS without asking a certificate authority for a certificate.
func (s *Service) CheckDomain(ctx context.Context, serverID, domain string) (DomainCheck, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	domain, err := checkedDomain(domain)
	if err != nil {
		return DomainCheck{}, err
	}
	srv, err := s.Get(ctx, serverID)
	if err != nil {
		return DomainCheck{}, err
	}
	host := srv.Host
	if host == "" {
		ex, _, err := s.connector.Executor(ctx, serverID)
		if err != nil {
			return DomainCheck{}, err
		}
		host, err = ex.Run(ctx, "curl -4 --fail --silent --show-error --connect-timeout 2 --max-time 5 https://api.ipify.org")
		if err != nil {
			return DomainCheck{}, apperr.Unavailable("Não foi possível descobrir o IP público do servidor: %s", describeFailure(err))
		}
		if _, err := netip.ParseAddr(strings.TrimSpace(host)); err != nil {
			return DomainCheck{}, apperr.Unavailable("O servidor não informou um IP público válido.")
		}
	}
	return compareDomain(ctx, domain, strings.TrimSpace(host), net.DefaultResolver.LookupNetIP)
}

func checkedDomain(domain string) (string, error) {
	domain = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(domain), "."))
	route, err := compose.ParseRoute("https://" + domain)
	if err != nil || route.Host != domain || route.Path != "" || strings.ContainsAny(domain, "/:*@?#") || !strings.Contains(domain, ".") {
		return "", apperr.Invalid("Informe um domínio válido, sem protocolo, porta ou caminho.")
	}
	return domain, nil
}
func compareDomain(ctx context.Context, domain, host string, lookup lookupAddresses) (DomainCheck, error) {
	result := DomainCheck{Addresses: []string{}, Expected: []string{}}
	var expected []netip.Addr
	if address, err := netip.ParseAddr(host); err == nil {
		expected = []netip.Addr{address}
	} else {
		expected, err = lookup(ctx, "ip", host)
		if err != nil {
			return result, apperr.Unavailable("Não foi possível resolver o endereço do servidor: %s", err)
		}
	}
	expectedSet := map[string]bool{}
	records := []string{}
	for _, address := range expected {
		address = address.Unmap()
		if expectedSet[address.String()] {
			continue
		}
		expectedSet[address.String()] = true
		result.Expected = append(result.Expected, address.String())
		kind := "AAAA"
		if address.Is4() {
			kind = "A"
		}
		records = append(records, domain+". IN "+kind+" "+address.String())
	}
	sort.Strings(result.Expected)
	sort.Strings(records)
	if len(expectedSet) == 0 {
		return result, apperr.Unavailable("O servidor não informou um IP público válido.")
	}
	result.RecordHint = i18n.Instance().Sprintf("Configure os registros DNS abaixo e remova os registros A/AAAA que apontam para outro servidor:\n%s", strings.Join(records, "\n"))
	addresses, err := lookup(ctx, "ip", domain)
	if err != nil {
		var dnsErr *net.DNSError
		if !errors.As(err, &dnsErr) || !dnsErr.IsNotFound {
			return result, apperr.Unavailable("Não foi possível consultar o DNS do domínio: %s", err)
		}
		return result, nil
	}
	result.OK = len(addresses) > 0
	seen := map[string]bool{}
	for _, address := range addresses {
		value := address.Unmap().String()
		if !seen[value] {
			result.Addresses = append(result.Addresses, value)
			seen[value] = true
		}
		if !expectedSet[value] {
			result.OK = false
		}
	}
	sort.Strings(result.Addresses)
	return result, nil
}
