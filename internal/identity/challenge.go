package identity

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strconv"
	"strings"
	"sync"
	"time"

	"pier/internal/platform/apperr"
	"pier/internal/platform/idgen"
)

const (
	// challengeTTL is how long the second step of a login may take.
	challengeTTL = 5 * time.Minute
	// challengeAttempts caps the codes tried against one challenge; the
	// handler also rate limits per address and per account.
	challengeAttempts = 5
	maxLedgerEntries  = 8192
)

// challenger issues and checks the signed proof that a password was right.
// It is not a session: it grants nothing but the right to send a code, it
// lives in memory (a restart only asks for the password again) and it is
// spent by the first successful code.
type challenger struct {
	key    []byte
	mu     sync.Mutex
	ledger map[string]*challengeUse
}

type challengeUse struct {
	attempts  int
	spent     bool
	expiresAt time.Time
}

// loginChallenge is the verified content of a challenge.
type loginChallenge struct {
	userID      string
	fingerprint string
	nonce       string
}

func newChallenger() *challenger {
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	return &challenger{key: key, ledger: map[string]*challengeUse{}}
}

// issue signs userID|expiry|nonce|fingerprint. The fingerprint of the
// password hash kills the challenge if the password changes meanwhile.
func (c *challenger) issue(userID, passwordHash string, now time.Time) string {
	payload := strings.Join([]string{userID, strconv.FormatInt(now.Add(challengeTTL).Unix(), 10),
		idgen.Token(12), passwordFingerprint(passwordHash)}, "|")
	encoded := base64.RawURLEncoding.EncodeToString([]byte(payload))
	return encoded + "." + c.sign(encoded)
}

func (c *challenger) sign(encoded string) string {
	mac := hmac.New(sha256.New, c.key)
	mac.Write([]byte(encoded))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func challengeExpired() error {
	return apperr.Unauthorized("A verificação expirou. Entre com a senha de novo.")
}

// verify checks signature and expiry; it does not spend the challenge.
func (c *challenger) verify(token string, now time.Time) (loginChallenge, error) {
	encoded, signature, found := strings.Cut(token, ".")
	if !found || len(token) > 512 || !hmac.Equal([]byte(signature), []byte(c.sign(encoded))) {
		return loginChallenge{}, challengeExpired()
	}
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return loginChallenge{}, challengeExpired()
	}
	parts := strings.Split(string(raw), "|")
	if len(parts) != 4 {
		return loginChallenge{}, challengeExpired()
	}
	expiresAt, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || !now.Before(time.Unix(expiresAt, 0)) {
		return loginChallenge{}, challengeExpired()
	}
	return loginChallenge{userID: parts[0], nonce: parts[2], fingerprint: parts[3]}, nil
}

// attempt registers one code tried against the challenge, refusing spent or
// exhausted ones. Memory stays bounded: expired entries are swept when the
// ledger grows, and a full ledger refuses (fails closed).
func (c *challenger) attempt(nonce string, now time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	use := c.ledger[nonce]
	if use == nil {
		if len(c.ledger) >= maxLedgerEntries/2 {
			c.sweep(now)
		}
		if len(c.ledger) >= maxLedgerEntries {
			return apperr.Unavailable("Muitas verificações em andamento. Tente de novo em alguns minutos.")
		}
		use = &challengeUse{expiresAt: now.Add(challengeTTL)}
		c.ledger[nonce] = use
	}
	if use.spent || use.attempts >= challengeAttempts {
		return challengeExpired()
	}
	use.attempts++
	return nil
}

// spend marks the challenge used; only the first caller gets true, so two
// codes racing on one challenge cannot open two sessions.
func (c *challenger) spend(nonce string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	use := c.ledger[nonce]
	if use == nil || use.spent {
		return false
	}
	use.spent = true
	return true
}

func (c *challenger) sweep(now time.Time) {
	for nonce, use := range c.ledger {
		if !now.Before(use.expiresAt) {
			delete(c.ledger, nonce)
		}
	}
}

func passwordFingerprint(passwordHash string) string {
	sum := sha256.Sum256([]byte(passwordHash))
	return hex.EncodeToString(sum[:8])
}
