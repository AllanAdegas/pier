// Package instance backs up Pier's control database and encryption key.
package instance

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/scrypt"
	"golang.org/x/sys/unix"

	"pier/internal/platform/apperr"
	"pier/internal/platform/secretbox"
	"pier/internal/platform/sqlite"
)

const maxArchiveBytes = 512 << 20
const magic = "PIERBAK1"

// Lock must be held for the entire lifetime of serve or restore. The lock file
// is never unlinked: otherwise another process could lock a different inode.
func Lock(dataDir string) (func(), error) {
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return nil, err
	}
	fd, err := unix.Open(filepath.Join(dataDir, "pier.lock"), unix.O_CREAT|unix.O_RDWR|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return nil, err
	}
	if err = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		unix.Close(fd)
		return nil, apperr.Conflict("O diretório de dados está em uso. Pare o Pier antes de restaurar.")
	}
	return func() { _ = unix.Flock(fd, unix.LOCK_UN); _ = unix.Close(fd) }, nil
}

type Service struct {
	DB      *sql.DB
	DataDir string
	Key     string
	Box     *secretbox.Box
	mu      sync.Mutex
}

type Status struct {
	LastBackupAt *time.Time `json:"last_backup_at"`
}

func (s *Service) Status(ctx context.Context) (Status, error) {
	var value string
	err := s.DB.QueryRowContext(ctx, `SELECT value FROM settings WHERE key='instance_last_backup_at'`).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return Status{}, nil
	}
	if err != nil {
		return Status{}, err
	}
	t := sqlite.ParseTime(value)
	return Status{LastBackupAt: &t}, nil
}

func validPassphrase(passphrase string) error {
	if len(passphrase) < 16 || len(passphrase) > 1024 {
		return apperr.Invalid("Use uma frase-senha de 16 a 1024 caracteres para o backup.")
	}
	return nil
}

func seal(plain []byte, passphrase string) ([]byte, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	key, err := scrypt.Key([]byte(passphrase), salt, 32768, 8, 1, 32)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return nil, err
	}
	header := append(append([]byte(magic), salt...), nonce...)
	return aead.Seal(header, nonce, plain, header), nil
}
func openArchive(encrypted []byte, passphrase string) ([]byte, error) {
	const headerLen = 8 + 16 + 12
	if len(encrypted) < headerLen+16 || string(encrypted[:8]) != magic {
		return nil, apperr.Invalid("Formato de backup inválido ou versão não suportada.")
	}
	key, err := scrypt.Key([]byte(passphrase), encrypted[8:24], 32768, 8, 1, 32)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	plain, err := aead.Open(nil, encrypted[24:headerLen], encrypted[headerLen:], encrypted[:headerLen])
	if err != nil {
		return nil, apperr.Invalid("Frase-senha incorreta ou backup corrompido.")
	}
	return plain, nil
}

// Create uses VACUUM INTO for a consistent snapshot, including committed WAL
// data, while the live database continues serving requests.
func (s *Service) Create(ctx context.Context, passphrase string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := validPassphrase(passphrase); err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp(s.DataDir, ".instance-backup-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	snapshot := filepath.Join(dir, "pier.db")
	if _, err = s.DB.ExecContext(ctx, `VACUUM INTO ?`, snapshot); err != nil {
		return nil, err
	}
	dbBytes, err := readBounded(snapshot)
	if err != nil {
		return nil, err
	}
	if len(s.Key) < 16 {
		return nil, fmt.Errorf("instance encryption key unavailable")
	}
	var packed bytes.Buffer
	gz := gzip.NewWriter(&packed)
	tw := tar.NewWriter(gz)
	for _, file := range []struct {
		name string
		data []byte
	}{{"pier.db", dbBytes}, {"secret.key", []byte(s.Key + "\n")}} {
		if err = tw.WriteHeader(&tar.Header{Name: file.name, Mode: 0600, Size: int64(len(file.data)), Typeflag: tar.TypeReg}); err != nil {
			return nil, err
		}
		if _, err = tw.Write(file.data); err != nil {
			return nil, err
		}
	}
	if err = tw.Close(); err != nil {
		return nil, err
	}
	if err = gz.Close(); err != nil {
		return nil, err
	}
	return seal(packed.Bytes(), passphrase)
}

// MarkComplete is separate so scheduled backups record success only after the
// archive has been durably written to its destination.
func (s *Service) MarkComplete(ctx context.Context) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO settings(key,value) VALUES('instance_last_backup_at',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, sqlite.FormatTime(time.Now()))
	return err
}

func readBounded(name string) ([]byte, error) {
	f, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("expected regular file")
	}
	b, err := io.ReadAll(io.LimitReader(f, maxArchiveBytes+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxArchiveBytes {
		return nil, fmt.Errorf("instance backup exceeds 512 MiB")
	}
	return b, nil
}

// Restore verifies authentication and SQLite integrity before replacing any
// files. Existing database/key/WAL files are retained under a unique suffix.
func Restore(ctx context.Context, archive, passphrase, dataDir string) error {
	if err := validPassphrase(passphrase); err != nil {
		return err
	}
	unlock, err := Lock(dataDir)
	if err != nil {
		return err
	}
	defer unlock()
	encrypted, err := readBounded(archive)
	if err != nil {
		return err
	}
	plain, err := openArchive(encrypted, passphrase)
	if err != nil {
		return err
	}
	gz, err := gzip.NewReader(bytes.NewReader(plain))
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(io.LimitReader(gz, maxArchiveBytes+1))
	files := map[string][]byte{}
	var total int64
	for {
		h, e := tr.Next()
		if e == io.EOF {
			break
		}
		if e != nil {
			return e
		}
		if (h.Name != "pier.db" && h.Name != "secret.key") || h.Typeflag != tar.TypeReg || files[h.Name] != nil || h.Size < 0 {
			return apperr.Invalid("Conteúdo inválido no backup da instância.")
		}
		total += h.Size
		if total > maxArchiveBytes {
			return apperr.Invalid("Conteúdo inválido no backup da instância.")
		}
		b, e := io.ReadAll(io.LimitReader(tr, h.Size+1))
		if e != nil {
			return e
		}
		if int64(len(b)) != h.Size {
			return io.ErrUnexpectedEOF
		}
		files[h.Name] = b
	}
	if len(files) != 2 || len(strings.TrimSpace(string(files["secret.key"]))) < 16 {
		return apperr.Invalid("Conteúdo inválido no backup da instância.")
	}
	stage, err := os.MkdirTemp(dataDir, ".instance-restore-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	for name, b := range files {
		if err = os.WriteFile(filepath.Join(stage, name), b, 0600); err != nil {
			return err
		}
	}
	check, err := sql.Open("sqlite", filepath.Join(stage, "pier.db"))
	if err != nil {
		return err
	}
	var integrity string
	err = check.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&integrity)
	if err == nil && integrity == "ok" {
		var n int
		err = check.QueryRowContext(ctx, `SELECT COUNT(*) FROM settings`).Scan(&n)
	}
	closeErr := check.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if integrity != "ok" {
		return apperr.Invalid("O banco do backup falhou na verificação de integridade.")
	}
	suffix := ".before-restore-" + filepath.Base(stage)
	moved := []string{}
	rollback := func() {
		for _, name := range moved {
			_ = os.Rename(filepath.Join(dataDir, name)+suffix, filepath.Join(dataDir, name))
		}
	}
	for _, name := range []string{"pier.db", "pier.db-wal", "pier.db-shm", "secret.key"} {
		target := filepath.Join(dataDir, name)
		if _, err = os.Lstat(target); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			rollback()
			return err
		}
		if err = os.Rename(target, target+suffix); err != nil {
			rollback()
			return err
		}
		moved = append(moved, name)
	}
	for _, name := range []string{"pier.db", "secret.key"} {
		if err = os.Rename(filepath.Join(stage, name), filepath.Join(dataDir, name)); err != nil {
			for _, installed := range []string{"pier.db", "secret.key"} {
				_ = os.Remove(filepath.Join(dataDir, installed))
			}
			rollback()
			return err
		}
	}
	return nil
}
