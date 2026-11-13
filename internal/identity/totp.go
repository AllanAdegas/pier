package identity

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// TOTP parameters (RFC 6238 defaults, what every authenticator app expects).
const (
	totpPeriod      = 30 * time.Second
	totpDigits      = 6
	totpSecretBytes = 20 // 160 bits, the HMAC-SHA1 block recommended by RFC 4226.
	// totpSkew accepts the previous and the next step, for clock drift.
	totpSkew   = 1
	totpIssuer = "Pier"
)

var totpEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// newTOTPSecret returns a random secret in the base32 form apps import.
func newTOTPSecret() string {
	raw := make([]byte, totpSecretBytes)
	_, _ = rand.Read(raw) // crypto/rand never fails on supported platforms.
	return totpEncoding.EncodeToString(raw)
}

func decodeTOTPSecret(secret string) ([]byte, error) {
	return totpEncoding.DecodeString(strings.ToUpper(strings.TrimSpace(secret)))
}

// totpStep is the RFC 6238 time counter T for an instant.
func totpStep(at time.Time) int64 { return at.Unix() / int64(totpPeriod/time.Second) }

// hotp is RFC 4226: HMAC-SHA1 of the counter, dynamic truncation, modulo 10^digits.
func hotp(key []byte, counter int64, digits int) string {
	mac := hmac.New(sha1.New, key)
	var message [8]byte
	binary.BigEndian.PutUint64(message[:], uint64(counter))
	mac.Write(message[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	value := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff
	modulo := uint32(1)
	for range digits {
		modulo *= 10
	}
	return fmt.Sprintf("%0*d", digits, value%modulo)
}

// matchTOTP returns the step a code belongs to, within the drift window and
// strictly after lastStep (replay protection). ok is false otherwise.
func matchTOTP(key []byte, code string, now time.Time, lastStep int64) (step int64, ok bool) {
	code = normalizeOTP(code)
	if len(code) != totpDigits {
		return 0, false
	}
	current := totpStep(now)
	for offset := int64(-totpSkew); offset <= totpSkew; offset++ {
		candidate := current + offset
		if candidate <= lastStep {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(hotp(key, candidate, totpDigits)), []byte(code)) == 1 {
			return candidate, true
		}
	}
	return 0, false
}

func normalizeOTP(code string) string {
	return strings.NewReplacer(" ", "", "-", "").Replace(strings.TrimSpace(code))
}

// isNumericOTP tells a TOTP code from a recovery code.
func isNumericOTP(code string) bool {
	code = normalizeOTP(code)
	if len(code) != totpDigits {
		return false
	}
	for _, r := range code {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// otpauthURI is the Key Uri Format understood by authenticator apps.
func otpauthURI(account, secret string) string {
	query := url.Values{}
	query.Set("secret", secret)
	query.Set("issuer", totpIssuer)
	query.Set("algorithm", "SHA1")
	query.Set("digits", fmt.Sprint(totpDigits))
	query.Set("period", fmt.Sprint(int(totpPeriod/time.Second)))
	label := url.PathEscape(totpIssuer + ":" + account)
	return "otpauth://totp/" + label + "?" + query.Encode()
}
