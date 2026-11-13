// Package idgen generates identifiers and random secrets.
package idgen

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
)

const (
	letters      = "abcdefghijklmnopqrstuvwxyz"
	alphanumeric = "abcdefghijklmnopqrstuvwxyz0123456789"
	idLength     = 14
)

// New returns a DNS-safe identifier: lowercase, starts with a letter. It is
// used as container name, network alias and compose project name.
func New() string {
	id := make([]byte, idLength)
	id[0] = pick(letters)
	for i := 1; i < idLength; i++ {
		id[i] = pick(alphanumeric)
	}
	return string(id)
}

// Password returns a random alphanumeric secret of the given length.
func Password(length int) string {
	const charset = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789"
	out := make([]byte, boundedLength(length))
	for i := range out {
		out[i] = pick(charset)
	}
	return string(out)
}

// PasswordWithSymbols is Password with punctuation mixed in, for software
// that demands it. The symbols are safe inside URLs, shells and YAML.
func PasswordWithSymbols(length int) string {
	const charset = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789-_.~+*"
	out := make([]byte, boundedLength(length))
	for i := range out {
		out[i] = pick(charset)
	}
	return string(out)
}

// Token returns n random bytes hex-encoded.
func Token(n int) string { return hex.EncodeToString(randomBytes(n)) }

// Base64 returns n random bytes base64-encoded.
func Base64(n int) string { return base64.StdEncoding.EncodeToString(randomBytes(n)) }

// maxLength bounds every generated value: callers ask for at most 128.
const maxLength = 4096

// boundedLength fails fast on a programming error instead of allocating
// whatever size a caller passes.
func boundedLength(n int) int {
	if n < 0 || n > maxLength {
		panic("idgen: length out of range")
	}
	return n
}

func randomBytes(n int) []byte {
	buf := make([]byte, boundedLength(n))
	// crypto/rand.Read never fails on supported platforms (it panics instead).
	_, _ = rand.Read(buf)
	return buf
}

// pick selects one byte of charset without modulo bias.
// The arithmetic is done in int: with 64 or 128 characters, 256 - 256%n is
// 256, which does not fit a byte and would loop forever.
func pick(charset string) byte {
	limit := 256 - 256%len(charset)
	for {
		b := int(randomBytes(1)[0])
		if b < limit {
			return charset[b%len(charset)]
		}
	}
}
