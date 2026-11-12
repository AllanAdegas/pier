package settings

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/url"
	"strings"

	"pier/internal/platform/apperr"
	"pier/internal/platform/secretbox"
)

// S3Settings is stored as one encrypted value, including access credentials.
type S3Settings struct {
	Endpoint  string `json:"endpoint"`
	Region    string `json:"region"`
	Bucket    string `json:"bucket"`
	Prefix    string `json:"prefix"`
	AccessKey string `json:"access_key"`
	SecretKey string `json:"secret_key"`
}

type S3Store struct {
	db  *sql.DB
	box *secretbox.Box
}

func NewS3Store(db *sql.DB, box *secretbox.Box) *S3Store { return &S3Store{db, box} }
func (s *S3Store) Get(ctx context.Context) (S3Settings, error) {
	var sealed string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key='backup_s3'`).Scan(&sealed)
	if err == sql.ErrNoRows {
		return S3Settings{}, nil
	}
	if err != nil {
		return S3Settings{}, err
	}
	raw, err := s.box.Open(sealed)
	if err != nil {
		return S3Settings{}, err
	}
	var value S3Settings
	err = json.Unmarshal([]byte(raw), &value)
	return value, err
}
func (s *S3Store) Update(ctx context.Context, value S3Settings) error {
	u, err := url.Parse(value.Endpoint)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return apperr.Invalid("Informe um endpoint S3 HTTPS válido.")
	}
	if value.Bucket == "" || strings.ContainsAny(value.Bucket, "/\\\n\r") || value.AccessKey == "" || value.SecretKey == "" {
		return apperr.Invalid("Informe bucket e credenciais S3.")
	}
	for _, v := range []string{value.Endpoint, value.Region, value.Bucket, value.Prefix, value.AccessKey, value.SecretKey} {
		if strings.ContainsAny(v, "\r\n\x00") {
			return apperr.Invalid("A configuração S3 não pode conter quebras de linha.")
		}
	}
	value.Prefix = strings.Trim(value.Prefix, "/")
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	sealed, err := s.box.Seal(string(raw))
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO settings(key,value) VALUES('backup_s3',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, sealed)
	return err
}
