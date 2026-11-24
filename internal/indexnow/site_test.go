package indexnow

import (
	"net/netip"
	"slices"
	"testing"
	"time"
)

func TestOnlyHTTPSDomainsWithoutPathCanNotify(t *testing.T) {
	got := Eligible([]string{"https://asigas.com", "http://old.example.com", "https://app.example.com/api",
		"www.asigas.com", "https://asigas.com/", "not a domain", ""})
	if want := []string{"asigas.com", "www.asigas.com"}; !slices.Equal(got, want) {
		t.Errorf("eligible = %v, want %v", got, want)
	}
}

func TestKeysAreRandomAndOnlyTheirFilesAreAccepted(t *testing.T) {
	first, second := newKey(), newKey()
	if first == second || !keyPattern.MatchString(first) {
		t.Fatalf("keys %q %q", first, second)
	}
	if key, ok := keyFromFile(first + ".txt"); !ok || key != first {
		t.Errorf("a key file must be accepted")
	}
	for _, name := range []string{first, "../" + first + ".txt", "ABCDEF0123456789ABCDEF0123456789.txt", "short.txt"} {
		if _, ok := keyFromFile(name); ok {
			t.Errorf("%q must not be a key file", name)
		}
	}
}

func TestOnlyPublicAddressesAreFetched(t *testing.T) {
	for address, public := range map[string]bool{
		"93.184.216.34": true, "2606:2800:220:1::1": true,
		"127.0.0.1": false, "10.0.0.5": false, "172.17.0.1": false, "192.168.1.1": false,
		"169.254.169.254": false, "100.64.0.1": false, "::1": false, "fd00::1": false, "fe80::1": false,
		"0.0.0.0": false, "::ffff:127.0.0.1": false,
	} {
		if got := isPublic(netip.MustParseAddr(address)); got != public {
			t.Errorf("%s public = %v, want %v", address, got, public)
		}
	}
	if err := refuseNonPublic("tcp", "10.0.0.5:443", nil); err == nil {
		t.Errorf("a private address must be refused at the socket")
	}
}

func TestOnlyPagesChangedSinceTheLastSubmissionAreSent(t *testing.T) {
	since := time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC)
	old, recent := since.Add(-time.Hour), since.Add(time.Hour)
	entries := []Entry{{Loc: "https://a.com/", Modified: &old}, {Loc: "https://a.com/new", Modified: &recent}}
	if got := pending(entries, nil); len(got) != 2 {
		t.Errorf("the first submission sends everything: %v", got)
	}
	if got := pending(entries, &since); !slices.Equal(got, []string{"https://a.com/new"}) {
		t.Errorf("changed only = %v", got)
	}
	if got := pending(append(entries, Entry{Loc: "https://a.com/x"}), &since); len(got) != 3 {
		t.Errorf("without lastmod on every page, everything goes: %v", got)
	}
	if got := pending(entries[:1], &since); len(got) != 0 {
		t.Errorf("nothing changed: %v", got)
	}
}
