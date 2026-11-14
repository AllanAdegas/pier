package secretbox

import (
	"crypto/sha256"
	"fmt"

	"golang.org/x/crypto/argon2"
)

// Argon2id parameters of the "v2:" format: the second recommended option of
// RFC 9106 (section 4): 3 passes over 64 MiB with 4 lanes, 256-bit output.
// They are part of the format: changing any of them requires a new prefix.
const (
	argonTime    uint32 = 3
	argonMemory  uint32 = 64 * 1024 // KiB
	argonThreads uint8  = 4
	keyLength    uint32 = 32
	// SaltLength is the size of the per-installation salt in bytes.
	SaltLength = 16
)

// deriveCurrentKey is a variable so tests can count how many times the
// expensive derivation runs. Production code never reassigns it.
var deriveCurrentKey = func(passphrase, salt []byte) []byte {
	return argon2.IDKey(passphrase, salt, argonTime, argonMemory, argonThreads, keyLength)
}

// legacyKey reproduces the key of the "v1:" format. It exists only to read
// and upgrade ciphertexts written before 0.8; nothing new is sealed with it.
func legacyKey(passphrase []byte) []byte {
	sum := sha256.Sum256(passphrase)
	return sum[:]
}

func validateSalt(salt []byte) error {
	if len(salt) != SaltLength {
		return fmt.Errorf("secretbox: salt must have %d bytes, got %d", SaltLength, len(salt))
	}
	return nil
}
