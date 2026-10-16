package secretbox

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSealOpenRoundTrip(t *testing.T) {
	box, err := New("a-long-enough-passphrase")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	for _, plaintext := range []string{"s3cr3t", "multi\nline\nPEM", "ünïcödé", ""} {
		sealed, err := box.Seal(plaintext)
		if err != nil {
			t.Fatalf("Seal: %v", err)
		}
		if plaintext != "" && sealed == plaintext {
			t.Errorf("Seal(%q) returned the plaintext", plaintext)
		}
		opened, err := box.Open(sealed)
		if err != nil || opened != plaintext {
			t.Errorf("Open(Seal(%q)) = %q, %v", plaintext, opened, err)
		}
	}
}

func TestSealIsRandomized(t *testing.T) {
	box, _ := New("a-long-enough-passphrase")
	first, _ := box.Seal("same")
	second, _ := box.Seal("same")
	if first == second {
		t.Error("sealing the same value twice must not produce the same ciphertext")
	}
}

func TestOpenRejectsWrongKeyAndTampering(t *testing.T) {
	box, _ := New("a-long-enough-passphrase")
	other, _ := New("another-long-passphrase!")
	sealed, _ := box.Seal("secret")

	if _, err := other.Open(sealed); err == nil {
		t.Error("a different key must not open the ciphertext")
	}
	tampered := sealed[:len(sealed)-2] + "AA"
	if _, err := box.Open(tampered); err == nil {
		t.Error("tampered ciphertext must be rejected")
	}
	if _, err := box.Open("plain text"); err != ErrMalformed {
		t.Errorf("unsealed input must be ErrMalformed, got %v", err)
	}
}

func TestNewRejectsShortPassphrase(t *testing.T) {
	if _, err := New("short"); err == nil {
		t.Error("a short passphrase must be rejected")
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
	again, _ := LoadOrCreateKey("", path)
	if again != created {
		t.Error("the generated key must be stable across restarts")
	}
	if explicit, _ := LoadOrCreateKey("from-environment", path); explicit != "from-environment" {
		t.Error("an explicit key must take precedence over the file")
	}
}
