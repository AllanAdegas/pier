package server

import (
	"context"
	"net"
	"net/netip"
	"strings"
	"testing"
)

func domainLookup(records map[string][]string) lookupAddresses {
	return func(_ context.Context, network, host string) ([]netip.Addr, error) {
		if network != "ip" {
			panic("A and AAAA must both be queried")
		}
		values, ok := records[host]
		if !ok {
			return nil, &net.DNSError{Name: host, IsNotFound: true}
		}
		result := []netip.Addr{}
		for _, value := range values {
			result = append(result, netip.MustParseAddr(value))
		}
		return result, nil
	}
}
func TestDomainCheckMatchesAllAAndAAAARecords(t *testing.T) {
	records := map[string][]string{"server.example.com": {"192.0.2.10", "2001:db8::10"}, "app.example.com": {"2001:db8::10", "192.0.2.10", "192.0.2.10"}}
	result, err := compareDomain(context.Background(), "app.example.com", "server.example.com", domainLookup(records))
	if err != nil || !result.OK || len(result.Addresses) != 2 || len(result.Expected) != 2 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	for _, record := range []string{"app.example.com. IN A 192.0.2.10", "app.example.com. IN AAAA 2001:db8::10"} {
		if !strings.Contains(result.RecordHint, record) {
			t.Fatalf("missing %s in %s", record, result.RecordHint)
		}
	}
}
func TestDomainCheckRejectsWrongAAAAEvenWhenAMatches(t *testing.T) {
	lookup := domainLookup(map[string][]string{"app.example.com": {"192.0.2.10", "2001:db8::99"}})
	result, err := compareDomain(context.Background(), "app.example.com", "192.0.2.10", lookup)
	if err != nil || result.OK {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}
func TestMissingDNSHasARecordHint(t *testing.T) {
	result, err := compareDomain(context.Background(), "app.example.com", "192.0.2.10", domainLookup(nil))
	if err != nil || result.OK || len(result.Addresses) != 0 || !strings.Contains(result.RecordHint, "IN A 192.0.2.10") {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}
func TestDomainInputRejectsURLsWildcardsPortsAndPaths(t *testing.T) {
	for _, input := range []string{"https://app.example.com", "app.example.com/path", "app.example.com:443", "*.example.com", "app.example.com?x", "app.example.com\nother", ""} {
		if _, err := checkedDomain(input); err == nil {
			t.Errorf("accepted %q", input)
		}
	}
	if got, err := checkedDomain(" App.Example.com. "); err != nil || got != "app.example.com" {
		t.Fatalf("got=%q err=%v", got, err)
	}
}
