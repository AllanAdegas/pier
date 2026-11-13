package database

import (
	"context"
	"encoding/json"
	"path"
	"pier/internal/docker"
	"pier/internal/platform/i18n"
	"pier/internal/platform/sqlite"
	"time"

	"pier/internal/platform/shell"
)

func (s *Service) recoverOperations(ctx context.Context) error {
	rows, err := s.store.db.QueryContext(ctx, `SELECT id,backup_id,database_id,kind FROM database_operations WHERE status='running' OR (status='failed' AND resume_containers!='[]')`)
	if err != nil {
		return err
	}
	type interrupted struct{ id, backup, database, kind string }
	var ops []interrupted
	for rows.Next() {
		var op interrupted
		if err := rows.Scan(&op.id, &op.backup, &op.database, &op.kind); err != nil {
			rows.Close()
			return err
		}
		ops = append(ops, op)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, op := range ops {
		recoveryCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Minute)
		err := s.recoverOperation(recoveryCtx, op.id, op.backup, op.database, op.kind)
		cancel()
		if err != nil {
			if _, saveErr := s.store.db.ExecContext(ctx, `UPDATE database_operations SET status='failed',message=?,finished_at=? WHERE id=?`, i18n.Instance().Sprintf("Interrompido pela reinicialização do Pier. A recuperação falhou: %s", err), sqlite.FormatTime(time.Now()), op.id); saveErr != nil {
				return saveErr
			}
		}
	}
	return nil
}
func (s *Service) recoverOperation(ctx context.Context, operationID, backupID, databaseID, kind string) error {

	b, err := s.store.getBackup(ctx, backupID)
	if err != nil {
		return err
	}
	db, err := s.Get(ctx, b.DatabaseID)
	if err != nil {
		return err
	}
	ex, server, err := s.connector.Executor(ctx, db.ServerID)
	if err != nil {
		return err
	}
	if kind == "restore" {
		containers, err := docker.ListByResource(ctx, ex, databaseID)
		if err != nil {
			return err
		}
		if container := firstRunning(containers); container != "" {
			check := shell.Join("docker", "inspect", "-f", `{{if index .NetworkSettings.Networks "pier"}}connected{{end}}`, container)
			command := check + " | grep -q connected || " + shell.Join("docker", "network", "connect", "--alias", databaseID, docker.Network, container)
			if _, err := ex.Run(ctx, command); err != nil {
				return err
			}
		}
	}
	if _, err := ex.Run(ctx, shell.Join("docker", "rm", "-fv", "pier-restore-"+operationID)); err != nil {
		// An absent disposable container is expected for ordinary restores.
		if _, inspectErr := ex.Run(ctx, shell.Join("docker", "inspect", "pier-restore-"+operationID)); inspectErr == nil {
			return err
		}
	}
	if _, err := ex.Run(ctx, shell.Join("rm", "-f", b.FilePath+".restore-"+operationID, path.Join(server.BackupDir(db.ID), ".restore-"+operationID))); err != nil {
		return err
	}
	if kind == "restore" && isKeyValue(db.Engine) {
		var journal string
		if err := s.store.db.QueryRowContext(ctx, "SELECT resume_containers FROM database_operations WHERE id=?", operationID).Scan(&journal); err != nil {
			return err
		}
		var ids []string
		if err := json.Unmarshal([]byte(journal), &ids); err != nil {
			return err
		}
		// Never rediscover a replacement container by resource name.
		containers := make([]docker.Container, 0, len(ids))
		for _, id := range ids {
			containers = append(containers, docker.Container{ID: id})
		}
		if err := docker.Start(ctx, ex, containers); err != nil {
			return i18n.Errorf("não foi possível iniciar o banco novamente: %w", err)
		}
		if _, err := s.store.db.ExecContext(ctx, "UPDATE database_operations SET resume_containers='[]' WHERE id=?", operationID); err != nil {
			return err
		}
	}
	return nil
}
