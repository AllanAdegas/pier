package secretbox

import (
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"pier/internal/platform/secretbox/secretboxtest"
)

const testPassphrase = "a-long-enough-passphrase"

func testSalt(fill byte) []byte {
	salt := make([]byte, SaltLength)
	for i := range salt {
		salt[i] = fill
	}
	return salt
}

func newTestBox(t *testing.T, passphrase string) *Box {
	t.Helper()
	box, err := NewWithSalt(passphrase, testSalt(7))
	if err != nil {
		t.Fatalf("NewWithSalt: %v", err)
	}
	return box
}

func TestSealOpenRoundTripUsesV2(t *testing.T) {
	box := newTestBox(t, testPassphrase)
	for _, plaintext := range []string{"s3cr3t", "multi\nline\nPEM", "ünïcödé", ""} {
		sealed, err := box.Seal(plaintext)
		if err != nil {
			t.Fatalf("Seal: %v", err)
		}
		if plaintext != "" && (!strings.HasPrefix(sealed, "v2:") || strings.Contains(sealed, plaintext)) {
			t.Errorf("Seal(%q) = %q, want an opaque v2 ciphertext", plaintext, sealed)
		}
		opened, err := box.Open(sealed)
		if err != nil || opened != plaintext {
			t.Errorf("Open(Seal(%q)) = %q, %v", plaintext, opened, err)
		}
	}
}

func TestOpenReadsLegacyV1(t *testing.T) {
	box := newTestBox(t, testPassphrase)
	legacy := secretboxtest.SealV1(t, testPassphrase, "old secret")
	if !IsLegacy(legacy) {
		t.Fatal("the fixture must be a v1 ciphertext")
	}
	if opened, err := box.Open(legacy); err != nil || opened != "old secret" {
		t.Fatalf("Open(v1) = %q, %v", opened, err)
	}
}

func TestSealIsRandomized(t *testing.T) {
	box := newTestBox(t, testPassphrase)
	first, _ := box.Seal("same")
	second, _ := box.Seal("same")
	if first == second {
		t.Error("sealing the same value twice must not produce the same ciphertext")
	}
}

func TestOpenRejectsWrongKeyInBothFormats(t *testing.T) {
	box := newTestBox(t, testPassphrase)
	other := newTestBox(t, "another-long-passphrase!")
	current, _ := box.Seal("secret")
	legacy := secretboxtest.SealV1(t, testPassphrase, "secret")
	for _, sealed := range []string{current, legacy} {
		if _, err := other.Open(sealed); err == nil {
			t.Errorf("a different key must not open %.3s ciphertexts", sealed)
		}
	}
}

func TestOpenRejectsAnotherInstallationSalt(t *testing.T) {
	sealed, _ := newTestBox(t, testPassphrase).Seal("secret")
	elsewhere, err := NewWithSalt(testPassphrase, testSalt(9))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := elsewhere.Open(sealed); err == nil {
		t.Error("the same passphrase with another salt must not open v2 ciphertexts")
	}
}

func TestOpenRejectsTamperingInBothFormats(t *testing.T) {
	box := newTestBox(t, testPassphrase)
	current, _ := box.Seal("secret")
	legacy := secretboxtest.SealV1(t, testPassphrase, "secret")
	for _, sealed := range []string{current, legacy} {
		tampered := sealed[:len(sealed)-4] + flip(sealed[len(sealed)-4:])
		if _, err := box.Open(tampered); err == nil {
			t.Errorf("tampered %.3s ciphertext must be rejected", sealed)
		}
	}
	downgraded := "v1:" + strings.TrimPrefix(current, "v2:")
	if _, err := box.Open(downgraded); err == nil {
		t.Error("a v2 body relabelled as v1 must be rejected")
	}
	for _, malformed := range []string{"plain text", "v2:not-base64!", "v2:AAAA", "v3:abcd"} {
		if _, err := box.Open(malformed); err != ErrMalformed {
			t.Errorf("Open(%q) error = %v, want ErrMalformed", malformed, err)
		}
	}
}

func flip(s string) string {
	if s[0] == 'A' {
		return "B" + s[1:]
	}
	return "A" + s[1:]
}

func TestKeyDerivationRunsOncePerBox(t *testing.T) {
	var calls atomic.Int32
	original := deriveCurrentKey
	deriveCurrentKey = func(passphrase, salt []byte) []byte {
		calls.Add(1)
		return original(passphrase, salt)
	}
	t.Cleanup(func() { deriveCurrentKey = original })

	box := newTestBox(t, testPassphrase)
	legacy := secretboxtest.SealV1(t, testPassphrase, "old")
	for range 50 {
		sealed, _ := box.Seal("value")
		if _, err := box.Open(sealed); err != nil {
			t.Fatal(err)
		}
		if _, err := box.reseal(legacy); err != nil {
			t.Fatal(err)
		}
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("Argon2id ran %d times, want exactly 1 (at construction)", got)
	}
}

func TestNewWithSaltValidatesInput(t *testing.T) {
	if _, err := NewWithSalt("short", testSalt(1)); err == nil {
		t.Error("a short passphrase must be rejected")
	}
	if _, err := NewWithSalt(testPassphrase, []byte("tiny")); err == nil {
		t.Error("a salt of the wrong size must be rejected")
	}
}

func TestNewSharesTheProcessSalt(t *testing.T) {
	first, err := New(testPassphrase)
	if err != nil {
		t.Fatal(err)
	}
	second, _ := New(testPassphrase)
	sealed, _ := first.Seal("secret")
	if opened, err := second.Open(sealed); err != nil || opened != "secret" {
		t.Errorf("boxes of one process must interoperate: %q, %v", opened, err)
	}
}

func TestLooksWeak(t *testing.T) {
	cases := map[string]bool{
		"passwordpassword12345678":                                         true,  // lowercase and digits, 24 chars: about 124 bits
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa":                     true,  // one distinct character
		"Summer2026!Summer2026!":                                           true,  // shorter than 24
		"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef": false, // openssl rand -hex 32
		"q1Zx8VbL0mN3pR7sT2uW5yA9cE4gH6jK8oP0iU2tY4=":                      false, // generated key
		"correct horse battery staple orbit lantern":                       false, // long diceware phrase
	}
	for passphrase, want := range cases {
		if got := LooksWeak(passphrase); got != want {
			t.Errorf("LooksWeak(%q) = %v, want %v", passphrase, got, want)
		}
	}
}

func TestLoadOrCreateKeyPersistsWithOwnerOnlyPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secret.key")
	created, err := LoadOrCreateKey("", path)
	if err != nil {
		t.Fatalf("LoadOrCreateKey: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("key file missing: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("key file mode = %o, want 600", info.Mode().Perm())
	}
	if LooksWeak(created) {
		t.Error("the generated key must never be reported as weak")
	}
	again, _ := LoadOrCreateKey("", path)
	if again != created {
		t.Error("the generated key must be stable across restarts")
	}
	if explicit, _ := LoadOrCreateKey("from-environment", path); explicit != "from-environment" {
		t.Error("an explicit key must take precedence over the file")
	}
}
