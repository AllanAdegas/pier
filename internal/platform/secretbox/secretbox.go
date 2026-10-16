// Package secretbox encrypts secrets at rest with AES-256-GCM.
package secretbox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strings"
)

const prefix = "v1:"

// ErrMalformed is returned when a ciphertext was not produced by this package.
var ErrMalformed = errors.New("secretbox: malformed ciphertext")

// Box seals and opens secrets with a single symmetric key.
type Box struct {
	aead cipher.AEAD
}

// New derives the AES key from an arbitrary-length passphrase.
func New(passphrase string) (*Box, error) {
	if len(passphrase) < 16 {
		return nil, errors.New("secretbox: passphrase must have at least 16 characters")
	}
	key := sha256.Sum256([]byte(passphrase))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, fmt.Errorf("secretbox: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("secretbox: %w", err)
	}
	return &Box{aead: aead}, nil
}

// LoadOrCreateKey returns the explicit passphrase when given; otherwise it
// reads the key file, generating it on first boot.
func LoadOrCreateKey(explicit, keyPath string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	existing, err := os.ReadFile(keyPath)
	if err == nil {
		return strings.TrimSpace(string(existing)), nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("read secret key: %w", err)
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate secret key: %w", err)
	}
	key := base64.StdEncoding.EncodeToString(raw)
	if err := os.WriteFile(keyPath, []byte(key+"\n"), 0o600); err != nil {
		return "", fmt.Errorf("persist secret key: %w", err)
	}
	return key, nil
}

// Seal encrypts plaintext. Empty input stays empty so optional fields are cheap.
func (b *Box) Seal(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("secretbox: %w", err)
	}
	sealed := b.aead.Seal(nonce, nonce, []byte(plaintext), nil)
	return prefix + base64.StdEncoding.EncodeToString(sealed), nil
}

// Open decrypts a value produced by Seal.
func (b *Box) Open(ciphertext string) (string, error) {
	if ciphertext == "" {
		return "", nil
	}
	encoded, found := strings.CutPrefix(ciphertext, prefix)
	if !found {
		return "", ErrMalformed
	}
	sealed, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(sealed) < b.aead.NonceSize() {
		return "", ErrMalformed
	}
	nonce, body := sealed[:b.aead.NonceSize()], sealed[b.aead.NonceSize():]
	plaintext, err := b.aead.Open(nil, nonce, body, nil)
	if err != nil {
		return "", fmt.Errorf("secretbox: decrypt failed (wrong key?): %w", err)
	}
	return string(plaintext), nil
}
