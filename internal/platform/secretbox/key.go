package secretbox

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"math"
	"os"
	"strings"
	"unicode"
)

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

// Thresholds of LooksWeak. The generated key (44 base64 characters, about
// 260 bits by this estimate) clears all of them; `openssl rand -hex 32` too.
const (
	weakBelowLength   = 24
	weakBelowDistinct = 10
	weakBelowBits     = 128
)

// LooksWeak is a heuristic for operator-chosen passphrases: short, few
// distinct characters, or a low entropy estimate (length times log2 of the
// size of the character classes used). It never inspects a dictionary and
// never rejects anything: callers only warn.
func LooksWeak(passphrase string) bool {
	runes := []rune(passphrase)
	if len(runes) < weakBelowLength || distinctCount(runes) < weakBelowDistinct {
		return true
	}
	return float64(len(runes))*math.Log2(float64(alphabetSize(runes))) < weakBelowBits
}

func distinctCount(runes []rune) int {
	seen := map[rune]struct{}{}
	for _, r := range runes {
		seen[r] = struct{}{}
	}
	return len(seen)
}

// alphabetSize sums the size of every character class the passphrase uses.
func alphabetSize(runes []rune) int {
	var lower, upper, digit, other bool
	for _, r := range runes {
		switch {
		case r >= 'a' && r <= 'z':
			lower = true
		case r >= 'A' && r <= 'Z':
			upper = true
		case unicode.IsDigit(r):
			digit = true
		default:
			other = true
		}
	}
	size := 0
	for _, class := range []struct {
		used bool
		size int
	}{{lower, 26}, {upper, 26}, {digit, 10}, {other, 33}} {
		if class.used {
			size += class.size
		}
	}
	return max(size, 2)
}
