package identity

import (
	"net/url"
	"strings"
	"testing"
	"time"
)

// rfc6238Key is the SHA-1 seed of RFC 6238, Appendix B.
var rfc6238Key = []byte("12345678901234567890")

func TestTOTPMatchesTheRFC6238Vectors(t *testing.T) {
	vectors := []struct {
		unix int64
		code string
	}{
		{59, "94287082"}, {1111111109, "07081804"}, {1111111111, "14050471"},
		{1234567890, "89005924"}, {2000000000, "69279037"}, {20000000000, "65353130"},
	}
	for _, vector := range vectors {
		step := totpStep(time.Unix(vector.unix, 0))
		if got := hotp(rfc6238Key, step, 8); got != vector.code {
			t.Errorf("T=%d: got %s, want %s", vector.unix, got, vector.code)
		}
		// Six digits are the last six of the eight-digit value.
		if got := hotp(rfc6238Key, step, totpDigits); got != vector.code[2:] {
			t.Errorf("T=%d six digits: got %s, want %s", vector.unix, got, vector.code[2:])
		}
	}
}

func TestTOTPAcceptsOneStepOfDriftAndNoMore(t *testing.T) {
	now := time.Unix(1234567890, 0)
	current := totpStep(now)
	for offset, want := range map[int64]bool{-2: false, -1: true, 0: true, 1: true, 2: false} {
		code := hotp(rfc6238Key, current+offset, totpDigits)
		step, ok := matchTOTP(rfc6238Key, code, now, 0)
		if ok != want || (ok && step != current+offset) {
			t.Errorf("offset %d: ok=%v step=%d, want ok=%v", offset, ok, step, want)
		}
	}
	if _, ok := matchTOTP(rfc6238Key, "12345", now, 0); ok {
		t.Error("a code with the wrong length must be rejected")
	}
}

func TestTOTPRejectsAStepAlreadyUsed(t *testing.T) {
	now := time.Unix(1234567890, 0)
	code := hotp(rfc6238Key, totpStep(now), totpDigits)
	step, ok := matchTOTP(rfc6238Key, code, now, 0)
	if !ok {
		t.Fatal("a fresh code must be accepted")
	}
	if _, ok := matchTOTP(rfc6238Key, code, now, step); ok {
		t.Error("the same code must not be accepted twice")
	}
	older := hotp(rfc6238Key, step-1, totpDigits)
	if _, ok := matchTOTP(rfc6238Key, older, now, step); ok {
		t.Error("a code older than the last accepted one must be rejected")
	}
}

func TestTOTPSecretRoundTripsAndTheURIFollowsTheKeyFormat(t *testing.T) {
	secret := newTOTPSecret()
	key, err := decodeTOTPSecret(strings.ToLower(secret))
	if err != nil || len(key) != totpSecretBytes {
		t.Fatalf("decode %q: %d bytes, %v", secret, len(key), err)
	}
	parsed, err := url.Parse(otpauthURI("ana@example.com", secret))
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	if parsed.Scheme != "otpauth" || parsed.Host != "totp" || parsed.Path != "/Pier:ana@example.com" ||
		query.Get("secret") != secret || query.Get("issuer") != "Pier" || query.Get("digits") != "6" ||
		query.Get("period") != "30" {
		t.Errorf("unexpected URI %s", parsed)
	}
}

func TestQRCodeIsASelfContainedSVG(t *testing.T) {
	image, err := qrSVG(otpauthURI("ana@example.com", newTOTPSecret()))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(image, "<svg ") || !strings.HasSuffix(image, "</svg>") || !strings.Contains(image, "<path ") {
		t.Fatalf("not an SVG: %.80s", image)
	}
	for _, forbidden := range []string{"<script", "href", "url(", "onload"} {
		if strings.Contains(image, forbidden) {
			t.Errorf("SVG must not contain %q", forbidden)
		}
	}
}
