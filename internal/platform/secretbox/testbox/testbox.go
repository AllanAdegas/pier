// Package testbox gives tests one shared secretbox.Box. Building a Box runs
// the production Argon2id derivation (64 MiB, 3 passes), which is slow under
// -race when every fixture repeats it; the AEAD inside a Box is safe for
// concurrent use, so tests whose subject is service behaviour share this one.
// Tests of key derivation, salts or wrong keys build their own Box with
// secretbox.New or secretbox.NewWithSalt. Never import it from production code.
package testbox

import (
	"sync"
	"testing"

	"pier/internal/platform/secretbox"
)

// Passphrase is the passphrase of the shared Box, for tests that need to
// build a second Box with the same key (process salt) or a legacy fixture.
const Passphrase = "shared-test-passphrase-for-secretbox"

var (
	once   sync.Once
	shared *secretbox.Box
	err    error
)

// Box returns the shared Box, deriving its key on the first call only.
func Box(t testing.TB) *secretbox.Box {
	t.Helper()
	once.Do(func() { shared, err = secretbox.New(Passphrase) })
	if err != nil {
		t.Fatalf("testbox: %v", err)
	}
	return shared
}
