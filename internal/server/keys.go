package server

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	"pier/internal/platform/apperr"
	"pier/internal/platform/idgen"
	"pier/internal/platform/secretbox"
	"pier/internal/platform/sqlite"
)

// KeyService manages SSH private keys.
type KeyService struct {
	store *Store
	box   *secretbox.Box
}

// NewKeyService creates the service.
func NewKeyService(store *Store, box *secretbox.Box) *KeyService {
	return &KeyService{store: store, box: box}
}

// List returns every key without private material.
func (k *KeyService) List(ctx context.Context) ([]PrivateKey, error) { return k.store.listKeys(ctx) }

// Generate creates a new ed25519 key pair.
func (k *KeyService) Generate(ctx context.Context, name, description string) (PrivateKey, error) {
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return PrivateKey{}, err
	}
	block, err := ssh.MarshalPrivateKey(private, "pier")
	if err != nil {
		return PrivateKey{}, err
	}
	return k.Import(ctx, name, description, string(pem.EncodeToMemory(block)))
}

// Import stores a key supplied by the user after checking it parses.
func (k *KeyService) Import(ctx context.Context, name, description, privateKey string) (PrivateKey, error) {
	name = strings.TrimSpace(name)
	privateKey = strings.TrimSpace(privateKey) + "\n"
	fields := apperr.Fields{}
	if name == "" {
		fields.Add("name", "Informe um nome para a chave.")
	}
	signer, err := ssh.ParsePrivateKey([]byte(privateKey))
	if err != nil {
		fields.Add("private_key", "Chave privada inválida ou protegida por senha (não suportado).")
	}
	if err := fields.Err(); err != nil {
		return PrivateKey{}, err
	}
	sealed, err := k.box.Seal(privateKey)
	if err != nil {
		return PrivateKey{}, err
	}
	key := PrivateKey{
		ID:          idgen.New(),
		Name:        name,
		Description: strings.TrimSpace(description),
		PublicKey:   strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey()))) + " pier",
		Fingerprint: ssh.FingerprintSHA256(signer.PublicKey()),
		CreatedAt:   time.Now(),
	}
	if err := k.store.insertKey(ctx, key, sealed); err != nil {
		return PrivateKey{}, err
	}
	return key, nil
}

// Delete removes a key that no server depends on.
func (k *KeyService) Delete(ctx context.Context, id string) error {
	inUse, err := k.store.countServersUsingKey(ctx, id)
	if err != nil {
		return err
	}
	if inUse > 0 {
		return apperr.Conflict("A chave está em uso por %d servidor(es).", inUse)
	}
	if err := k.store.deleteKey(ctx, id); err != nil {
		return keyNotFound(err)
	}
	return nil
}

// Material returns the decrypted private key for internal use (SSH, git).
func (k *KeyService) Material(ctx context.Context, id string) (string, error) {
	sealed, err := k.store.sealedKey(ctx, id)
	if err != nil {
		return "", keyNotFound(err)
	}
	return k.box.Open(sealed)
}

func keyNotFound(err error) error {
	if errors.Is(err, sqlite.ErrNotFound) {
		return apperr.NotFound("Chave privada")
	}
	return err
}
