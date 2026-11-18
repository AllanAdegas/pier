package main

import (
	"context"
	"database/sql"
	"log/slog"

	"pier/internal/platform/i18n"
	"pier/internal/platform/secretbox"
	"pier/internal/sealedfields"
)

// openSecretBox derives the secretbox keys once and upgrades legacy "v1:"
// ciphertexts in the registered fields (see internal/sealedfields). A failed
// upgrade is logged and the start continues: v1 stays readable and the next
// start retries. Only counts are logged, never values.
func openSecretBox(ctx context.Context, db *sql.DB, key string, logger *slog.Logger) (*secretbox.Box, error) {
	if secretbox.LooksWeak(key) {
		logger.Warn(i18n.Instance().Text("a chave secreta (PIER_SECRET_KEY ou secret.key) parece fraca: use um valor longo e aleatório, como `openssl rand -hex 32`; alterar a chave exige cadastrar novamente todos os segredos"))
	}
	salt, err := secretbox.LoadOrCreateSalt(ctx, db)
	if err != nil {
		return nil, err
	}
	box, err := secretbox.NewWithSalt(key, salt)
	if err != nil {
		return nil, err
	}
	report, err := secretbox.UpgradeLegacy(ctx, db, box, sealedfields.All())
	switch {
	case err != nil:
		// SQLite trigger errors may contain row values; log only the fixed message.
		logger.Error(i18n.Instance().Text("secretbox: falha ao migrar segredos v1; dados preservados, nova tentativa na próxima inicialização"))
	case !report.AlreadyDone:
		logger.Info(i18n.Instance().Text("secretbox: segredos v1 migrados para v2"),
			"ciphertexts", report.Ciphertexts, "rows", report.Rows, "fields", report.Fields)
	}
	return box, nil
}
