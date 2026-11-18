// Package secretboxtest writes "v1:" ciphertexts the way Pier did before 0.8,
// so tests can build legacy databases and backups. It is an independent
// implementation of the old format, never used by production code.
package secretboxtest

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"testing"
)

// SealV1 encrypts plaintext with the legacy format: AES-256-GCM keyed by a
// single SHA-256 of the passphrase, no associated data.
func SealV1(t testing.TB, passphrase, plaintext string) string {
	t.Helper()
	key := sha256.Sum256([]byte(passphrase))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		t.Fatal(err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		t.Fatal(err)
	}
	return "v1:" + base64.StdEncoding.EncodeToString(aead.Seal(nonce, nonce, []byte(plaintext), nil))
}
