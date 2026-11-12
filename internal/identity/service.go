package identity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"net/mail"
	"slices"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"

	"pier/internal/platform/apperr"
	"pier/internal/platform/idgen"
	"pier/internal/platform/sqlite"
)

const (
	minPasswordLength = 10
	// bcrypt silently truncates beyond 72 bytes, so longer input is rejected.
	maxPasswordLength = 72
	tokenPrefix       = "pier_"
)

// Service implements the identity use cases.
type Service struct {
	store      *Store
	sessionTTL time.Duration
	now        func() time.Time
	// setupCode must accompany the creation of the first user. Without it,
	// whoever reached a fresh instance first (it may be on the internet)
	// would own it. It lives in memory only and is logged at boot.
	setupMu   sync.Mutex
	setupCode string
}

// SetupRequest carries the first user and the code that proves access to
// the server's log.
type SetupRequest struct {
	Code     string `json:"setup_code"`
	Name     string `json:"name"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

// setupCodeLength gives 60 bits of entropy (base32), and setup attempts are
// rate limited like logins.
const setupCodeLength = 12

// NewService creates the service.
func NewService(store *Store, sessionTTL time.Duration) *Service {
	return &Service{store: store, sessionTTL: sessionTTL, now: time.Now}
}

// NeedsSetup reports whether the instance still has no user.
func (s *Service) NeedsSetup(ctx context.Context) (bool, error) {
	count, err := s.store.countUsers(ctx)
	return count == 0, err
}

// SetupCode returns the code that claims an instance without users, creating
// it on first use; it is empty once the instance is configured.
func (s *Service) SetupCode(ctx context.Context) (string, error) {
	needsSetup, err := s.NeedsSetup(ctx)
	if err != nil || !needsSetup {
		return "", err
	}
	s.setupMu.Lock()
	defer s.setupMu.Unlock()
	if s.setupCode == "" {
		s.setupCode = rand.Text()[:setupCodeLength]
	}
	return formatSetupCode(s.setupCode), nil
}

// Setup creates the first user. It fails once any user exists, and without
// the setup code. Concurrent attempts are serialized, so only one wins.
func (s *Service) Setup(ctx context.Context, request SetupRequest) (User, error) {
	s.setupMu.Lock()
	defer s.setupMu.Unlock()
	needsSetup, err := s.NeedsSetup(ctx)
	if err != nil {
		return User{}, err
	}
	if !needsSetup {
		return User{}, apperr.Conflict("A instância já foi configurada.")
	}
	given := []byte(normalizeSetupCode(request.Code))
	if s.setupCode == "" || subtle.ConstantTimeCompare(given, []byte(s.setupCode)) != 1 {
		fields := apperr.Fields{}
		fields.Add("setup_code", "Código incorreto. Ele aparece no log do Pier ao iniciar (ex.: docker logs pier).")
		return User{}, fields.Err()
	}
	user, err := s.CreateUser(ctx, request.Name, request.Email, request.Password)
	if err == nil {
		s.setupCode = ""
	}
	return user, err
}

// formatSetupCode groups the code in fours, easier to read from a log.
func formatSetupCode(code string) string {
	return code[0:4] + "-" + code[4:8] + "-" + code[8:12]
}

func normalizeSetupCode(code string) string {
	return strings.ToUpper(strings.NewReplacer("-", "", " ", "").Replace(strings.TrimSpace(code)))
}

// CreateUser registers a new operator.
func (s *Service) CreateUser(ctx context.Context, name, email, password string) (User, error) {
	name, email = strings.TrimSpace(name), normalizeEmail(email)
	fields := apperr.Fields{}
	validateProfile(fields, name, email)
	validatePassword(fields, password)
	if err := fields.Err(); err != nil {
		return User{}, err
	}
	if _, err := s.store.userByEmail(ctx, email); err == nil {
		return User{}, apperr.Conflict("Já existe um usuário com este e-mail.")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return User{}, err
	}
	user := User{ID: idgen.New(), Email: email, Name: name, CreatedAt: s.now()}
	if err := s.store.insertUser(ctx, user, string(hash)); err != nil {
		return User{}, err
	}
	return user, nil
}

// ListUsers returns every operator.
func (s *Service) ListUsers(ctx context.Context) ([]User, error) { return s.store.listUsers(ctx) }

// DeleteUser removes an operator; the last one cannot be removed.
func (s *Service) DeleteUser(ctx context.Context, actorID, targetID string) error {
	if actorID == targetID {
		return apperr.Conflict("Você não pode remover o próprio usuário.")
	}
	return s.store.deleteUser(ctx, targetID)

}

// UpdateProfile changes the caller's name and e-mail.
func (s *Service) UpdateProfile(ctx context.Context, userID, name, email string) (User, error) {
	name, email = strings.TrimSpace(name), normalizeEmail(email)
	fields := apperr.Fields{}
	validateProfile(fields, name, email)
	if err := fields.Err(); err != nil {
		return User{}, err
	}
	if existing, err := s.store.userByEmail(ctx, email); err == nil && existing.ID != userID {
		return User{}, apperr.Conflict("Já existe um usuário com este e-mail.")
	}
	if err := s.store.updateProfile(ctx, userID, name, email); err != nil {
		return User{}, notFoundAs(err, "Usuário")
	}
	record, err := s.store.userByID(ctx, userID)
	return record.User, err
}

// ChangePassword verifies the current password and invalidates other sessions.
func (s *Service) ChangePassword(ctx context.Context, userID, current, next string) error {
	record, err := s.store.userByID(ctx, userID)
	if err != nil {
		return notFoundAs(err, "Usuário")
	}
	if bcrypt.CompareHashAndPassword([]byte(record.PasswordHash), []byte(current)) != nil {
		return apperr.Invalid("A senha atual está incorreta.")
	}
	fields := apperr.Fields{}
	validatePassword(fields, next)
	if err := fields.Err(); err != nil {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(next), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	if err := s.store.updatePassword(ctx, userID, string(hash)); err != nil {
		return err
	}
	return s.store.deleteUserSessions(ctx, userID)
}

// Login checks credentials and opens a session, returning its secret token.
func (s *Service) Login(ctx context.Context, email, password string) (string, User, error) {
	const invalid = "E-mail ou senha inválidos."
	record, err := s.store.userByEmail(ctx, normalizeEmail(email))
	if errors.Is(err, sqlite.ErrNotFound) {
		// Spend the same time as a real comparison so timing does not reveal accounts.
		_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
		return "", User{}, apperr.Unauthorized(invalid)
	}
	if err != nil {
		return "", User{}, err
	}
	if bcrypt.CompareHashAndPassword([]byte(record.PasswordHash), []byte(password)) != nil {
		return "", User{}, apperr.Unauthorized(invalid)
	}
	token, err := s.OpenSession(ctx, record.ID)
	if err != nil {
		return "", User{}, err
	}
	// The password may have changed while it was being checked. A password
	// change updates the hash before it closes the sessions, so either that
	// deletion already sees this session or the hash read here is the new one.
	if current, err := s.store.userByID(ctx, record.ID); err != nil || current.PasswordHash != record.PasswordHash {
		_ = s.Logout(ctx, token)
		return "", User{}, apperr.Unauthorized(invalid)
	}
	return token, record.User, nil
}

// OpenSession creates a session for an already-verified user.
func (s *Service) OpenSession(ctx context.Context, userID string) (string, error) {
	now := s.now()
	_ = s.store.deleteExpiredSessions(ctx, now)
	token := idgen.Token(32)
	if err := s.store.insertSession(ctx, hashToken(token), userID, now.Add(s.sessionTTL)); err != nil {
		return "", err
	}
	return token, nil
}

// Logout closes a session.
func (s *Service) Logout(ctx context.Context, sessionToken string) error {
	return s.store.deleteSession(ctx, hashToken(sessionToken))
}

// AuthenticateSession resolves a browser session cookie.
func (s *Service) AuthenticateSession(ctx context.Context, sessionToken string) (Principal, error) {
	userID, err := s.store.sessionUserID(ctx, hashToken(sessionToken), s.now())
	if err != nil {
		return Principal{}, unauthorizedIfMissing(err)
	}
	record, err := s.store.userByID(ctx, userID)
	if err != nil {
		return Principal{}, unauthorizedIfMissing(err)
	}
	return Principal{User: record.User, Abilities: AllAbilities}, nil
}

// AuthenticateToken resolves an API bearer token.
func (s *Service) AuthenticateToken(ctx context.Context, bearer string) (Principal, error) {
	if !strings.HasPrefix(bearer, tokenPrefix) {
		return Principal{}, apperr.Unauthorized("Token de API inválido.")
	}
	token, err := s.store.tokenByHash(ctx, hashToken(bearer))
	if err != nil {
		return Principal{}, unauthorizedIfMissing(err)
	}
	record, err := s.store.userByID(ctx, token.UserID)
	if err != nil {
		return Principal{}, unauthorizedIfMissing(err)
	}
	_ = s.store.touchToken(ctx, token.ID, s.now())
	return Principal{User: record.User, Abilities: token.Abilities, ViaToken: true, TokenID: token.ID, TokenName: token.Name}, nil
}

// CreateToken issues an API token and returns the secret, shown only once.
func (s *Service) CreateToken(ctx context.Context, userID, name string, abilities []Ability) (APIToken, string, error) {
	name = strings.TrimSpace(name)
	fields := apperr.Fields{}
	if name == "" {
		fields.Add("name", "Informe um nome para identificar o token.")
	}
	if len(abilities) == 0 {
		fields.Add("abilities", "Selecione ao menos uma permissão.")
	}
	for _, ability := range abilities {
		if !slices.Contains(AllAbilities, ability) {
			fields.Add("abilities", "Permissão desconhecida: %s.", ability)
		}
	}
	if err := fields.Err(); err != nil {
		return APIToken{}, "", err
	}
	secret := tokenPrefix + idgen.Token(24)
	token := APIToken{ID: idgen.New(), UserID: userID, Name: name, Abilities: abilities, CreatedAt: s.now()}
	if err := s.store.insertToken(ctx, token, hashToken(secret)); err != nil {
		return APIToken{}, "", err
	}
	return token, secret, nil
}

// ListTokens returns every API token (without secrets).
func (s *Service) ListTokens(ctx context.Context) ([]APIToken, error) { return s.store.listTokens(ctx) }

// DeleteToken revokes an API token.
func (s *Service) DeleteToken(ctx context.Context, id string) error {
	return notFoundAs(s.store.deleteToken(ctx, id), "Token")
}

// dummyHash is a valid bcrypt hash of a random value, used to equalize timing.
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte(idgen.Token(16)), bcrypt.DefaultCost)

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func normalizeEmail(email string) string { return strings.ToLower(strings.TrimSpace(email)) }

func validateProfile(fields apperr.Fields, name, email string) {
	if name == "" {
		fields.Add("name", "Informe o nome.")
	}
	if address, err := mail.ParseAddress(email); len(email) > 320 || err != nil || address.Address != email {
		fields.Add("email", "Informe um e-mail válido.")
	}
}

func validatePassword(fields apperr.Fields, password string) {
	switch {
	case len(password) < minPasswordLength:
		fields.Add("password", "A senha precisa ter ao menos %d caracteres.", minPasswordLength)
	case len(password) > maxPasswordLength:
		fields.Add("password", "A senha pode ter no máximo %d caracteres.", maxPasswordLength)
	}
}

func notFoundAs(err error, what string) error {
	if errors.Is(err, sqlite.ErrNotFound) {
		return apperr.NotFound(what)
	}
	return err
}

func unauthorizedIfMissing(err error) error {
	if errors.Is(err, sqlite.ErrNotFound) {
		return apperr.Unauthorized("Sessão expirada ou credencial inválida.")
	}
	return err
}
