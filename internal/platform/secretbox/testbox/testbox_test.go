package testbox

import (
	"testing"

	"pier/internal/platform/secretbox"
)

func TestBoxIsSharedAndInteroperatesWithItsPassphrase(t *testing.T) {
	first, second := Box(t), Box(t)
	if first != second {
		t.Fatal("Box must return the same instance on every call")
	}
	sealed, err := first.Seal("shared")
	if err != nil {
		t.Fatal(err)
	}
	// secretbox.New shares the process salt, so the same passphrase opens it.
	other, err := secretbox.New(Passphrase)
	if err != nil {
		t.Fatal(err)
	}
	if opened, err := other.Open(sealed); err != nil || opened != "shared" {
		t.Fatalf("Open = %q, %v", opened, err)
	}
}
