// Package secretbox encrypts secrets at rest with AES-256-GCM.
//
// Two formats exist. "v2:" (current) uses a key derived once, at startup,
// with Argon2id from the passphrase and a per-installation salt. "v1:"
// (before 0.8) used a single SHA-256 of the passphrase; it is only read, and
// UpgradeLegacy rewrites the v1 ciphertexts of the registered fields as v2.
package secretbox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync"
)

const (
	currentPrefix = "v2:"
	legacyPrefix  = "v1:"
	// MinPassphraseLength is the shortest passphrase New accepts.
	MinPassphraseLength = 16
)

// currentAssociatedData binds v2 ciphertexts to their format.
var currentAssociatedData = []byte("pier-secretbox-v2")

// ErrMalformed is returned when a ciphertext was not produced by this package.
var ErrMalformed = errors.New("secretbox: malformed ciphertext")

// Box seals and opens secrets. Both keys are derived when the Box is built,
// never per secret.
type Box struct {
	current cipher.AEAD
	legacy  cipher.AEAD
}

// NewWithSalt derives the keys from the passphrase and the installation salt
// (see LoadOrCreateSalt). This is the constructor production code uses.
func NewWithSalt(passphrase string, salt []byte) (*Box, error) {
	if len(passphrase) < MinPassphraseLength {
		return nil, fmt.Errorf("secretbox: passphrase must have at least %d characters", MinPassphraseLength)
	}
	if err := validateSalt(salt); err != nil {
		return nil, err
	}
	current, err := newAEAD(deriveCurrentKey([]byte(passphrase), salt))
	if err != nil {
		return nil, err
	}
	legacy, err := newAEAD(legacyKey([]byte(passphrase)))
	if err != nil {
		return nil, err
	}
	return &Box{current: current, legacy: legacy}, nil
}

var (
	processSaltOnce sync.Once
	processSalt     []byte
)

// New derives the keys with a random salt that lives only as long as the
// process: boxes built with the same passphrase in one process interoperate,
// but nothing they seal can be opened after a restart. Tests and short-lived
// tools use it; the server wires NewWithSalt with the stored salt.
func New(passphrase string) (*Box, error) {
	processSaltOnce.Do(func() {
		processSalt = make([]byte, SaltLength)
		if _, err := rand.Read(processSalt); err != nil {
			panic(fmt.Sprintf("secretbox: random salt: %v", err))
		}
	})
	return NewWithSalt(passphrase, processSalt)
}

func newAEAD(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("secretbox: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("secretbox: %w", err)
	}
	return aead, nil
}

// Seal encrypts plaintext in the current format. Empty input stays empty so
// optional fields are cheap.
func (b *Box) Seal(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	nonce := make([]byte, b.current.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("secretbox: %w", err)
	}
	sealed := b.current.Seal(nonce, nonce, []byte(plaintext), currentAssociatedData)
	return currentPrefix + base64.StdEncoding.EncodeToString(sealed), nil
}

// Open decrypts a value produced by Seal, in the current or the legacy format.
func (b *Box) Open(ciphertext string) (string, error) {
	if ciphertext == "" {
		return "", nil
	}
	if encoded, found := strings.CutPrefix(ciphertext, currentPrefix); found {
		return openWith(b.current, encoded, currentAssociatedData)
	}
	if encoded, found := strings.CutPrefix(ciphertext, legacyPrefix); found {
		return openWith(b.legacy, encoded, nil)
	}
	return "", ErrMalformed
}

func openWith(aead cipher.AEAD, encoded string, associatedData []byte) (string, error) {
	sealed, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(sealed) < aead.NonceSize()+aead.Overhead() {
		return "", ErrMalformed
	}
	nonce, body := sealed[:aead.NonceSize()], sealed[aead.NonceSize():]
	plaintext, err := aead.Open(nil, nonce, body, associatedData)
	if err != nil {
		return "", fmt.Errorf("secretbox: decrypt failed (wrong key?): %w", err)
	}
	return string(plaintext), nil
}

// IsLegacy reports whether a ciphertext uses the "v1:" format.
func IsLegacy(ciphertext string) bool { return strings.HasPrefix(ciphertext, legacyPrefix) }

// reseal rewrites a legacy ciphertext in the current format.
func (b *Box) reseal(legacyCiphertext string) (string, error) {
	plaintext, err := b.Open(legacyCiphertext)
	if err != nil {
		return "", err
	}
	return b.Seal(plaintext)
}
