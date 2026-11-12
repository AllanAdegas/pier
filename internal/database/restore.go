package database

import (
	"context"
	"database/sql"
	"path"
	"strings"
	"time"

	"pier/internal/docker"
	"pier/internal/notify"
	"pier/internal/platform/apperr"
	"pier/internal/platform/executor"
	"pier/internal/platform/i18n"
	"pier/internal/platform/idgen"
	"pier/internal/platform/shell"
	"pier/internal/platform/sqlite"
)

type RestoreInput struct {
	Confirmation string `json:"confirmation"`
	NewName      string `json:"new_name"`
}

type Operation struct {
	ID         string     `json:"id"`
	DatabaseID string     `json:"database_id"`
	BackupID   string     `json:"backup_id"`
	Kind       string     `json:"kind"`
	Status     string     `json:"status"`
	Message    string     `json:"message"`
	Logs       string     `json:"logs"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at"`
}

func (s *Service) ListOperations(ctx context.Context, id string) ([]Operation, error) {
	if _, err := s.Get(ctx, id); err != nil {
		return nil, err
	}
	rows, err := s.store.db.QueryContext(ctx, `SELECT id,database_id,backup_id,kind,status,message,logs,started_at,finished_at FROM database_operations WHERE database_id=? ORDER BY started_at DESC LIMIT 50`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Operation{}
	for rows.Next() {
		var op Operation
		var start string
		var end sql.NullString
		if err := rows.Scan(&op.ID, &op.DatabaseID, &op.BackupID, &op.Kind, &op.Status, &op.Message, &op.Logs, &start, &end); err != nil {
			return nil, err
		}
		op.StartedAt = sqlite.ParseTime(start)
		op.FinishedAt = sqlite.ParseOptionalTime(end)
		result = append(result, op)
	}
	return result, rows.Err()
}

func (s *Service) StartRestore(ctx context.Context, backupID string, input RestoreInput) (Operation, error) {
	b, err := s.store.getBackup(ctx, backupID)
	if err != nil {
		return Operation{}, err
	}
	source, err := s.Get(ctx, b.DatabaseID)
	if err != nil {
		return Operation{}, err
	}
	if b.Status != BackupSucceeded {
		return Operation{}, apperr.Conflict("Selecione um backup concluído para restaurar.")
	}
	if _, _, ok := restoreScript(source.Engine); !ok && !isKeyValue(source.Engine) {
		return Operation{}, apperr.Conflict("Esta engine não suporta restauração.")
	}
	if input.Confirmation != source.Name {
		return Operation{}, apperr.Invalid("Digite o nome do banco para confirmar a restauração.")
	}
	release, err := s.acquire(source.ID)
	if err != nil {
		return Operation{}, err
	}
	defer func() {
		if release != nil {
			release()
		}
	}()
	if err := s.ensureIdle(ctx, source.ID); err != nil {
		return Operation{}, err
	}
	target := source
	if strings.TrimSpace(input.NewName) != "" {
		if isKeyValue(source.Engine) {
			return Operation{}, apperr.Conflict("Esta engine só pode ser restaurada no próprio banco.")
		}
		creds, err := s.store.credentials(ctx, source.ID)
		if err != nil {
			return Operation{}, err
		}
		target, err = s.Create(ctx, CreateInput{EnvironmentID: source.EnvironmentID, ServerID: source.ServerID, Name: input.NewName, Engine: source.Engine, Image: source.Config.Image, Username: creds.Username, Database: creds.Database})
		if err != nil {
			return Operation{}, err
		}
		if err := s.Start(ctx, target.ID); err != nil {
			return Operation{}, err
		}
	}
	reserved := release
	release = nil
	return s.startOperation(ctx, source, target, b, "restore", reserved)
}

func (s *Service) StartRestoreTest(ctx context.Context, id string) (Operation, error) {
	db, err := s.Get(ctx, id)
	if err != nil {
		return Operation{}, err
	}
	backups, err := s.ListBackups(ctx, id)
	if err != nil {
		return Operation{}, err
	}
	for _, b := range backups {
		if b.Status == BackupSucceeded {
			return s.startOperation(ctx, db, db, b, "restore_test")
		}
	}
	return Operation{}, apperr.Conflict("Faça um backup antes de testar a restauração.")
}

func (s *Service) startOperation(ctx context.Context, source, target Database, b BackupExecution, kind string, reserved ...func()) (Operation, error) {
	var release func()
	var err error
	if len(reserved) > 0 {
		release = reserved[0]
	} else {
		release, err = s.acquire(source.ID)
	}
	if err != nil {
		return Operation{}, err
	}
	started := false
	defer func() {
		if !started {
			release()
		}
	}()
	if err := s.ensureIdle(ctx, source.ID); err != nil {
		return Operation{}, err
	}
	releaseTarget := func() {}
	if source.ID != target.ID {
		releaseTarget, err = s.acquire(target.ID)
		if err != nil {
			return Operation{}, err
		}
		if err = s.ensureIdle(ctx, target.ID); err != nil {
			releaseTarget()
			return Operation{}, err
		}
	}
	op := Operation{ID: idgen.New(), DatabaseID: target.ID, BackupID: b.ID, Kind: kind, Status: "running", StartedAt: time.Now()}
	_, err = s.store.db.ExecContext(ctx, `INSERT INTO database_operations(id,database_id,backup_id,kind,status,started_at) VALUES(?,?,?,?,?,?)`, op.ID, op.DatabaseID, op.BackupID, op.Kind, op.Status, sqlite.FormatTime(op.StartedAt))
	if err != nil {
		releaseTarget()
		return Operation{}, err
	}
	started = true
	go func() { defer release(); defer releaseTarget(); s.runRestore(source, target, b, op) }()
	return op, nil
}

func restoreScript(engine Engine) (string, string, bool) {
	switch engine {
	case EnginePostgreSQL:
		return `PGPASSWORD="$POSTGRES_PASSWORD" pg_restore --clean --if-exists --no-owner --no-acl --exit-on-error -U "$POSTGRES_USER" -d "$POSTGRES_DB"`, `PGPASSWORD="$POSTGRES_PASSWORD" psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -v ON_ERROR_STOP=1 -c 'SELECT count(*) FROM information_schema.tables'`, true
	case EngineMySQL:
		return `MYSQL_PWD="$MYSQL_ROOT_PASSWORD" mysql -uroot "$MYSQL_DATABASE"`, `MYSQL_PWD="$MYSQL_ROOT_PASSWORD" mysql -uroot "$MYSQL_DATABASE" -e 'SHOW TABLES'`, true
	case EngineMariaDB:
		return `MYSQL_PWD="$MARIADB_ROOT_PASSWORD" mariadb -uroot "$MARIADB_DATABASE"`, `MYSQL_PWD="$MARIADB_ROOT_PASSWORD" mariadb -uroot "$MARIADB_DATABASE" -e 'SHOW TABLES'`, true
	case EngineMongoDB:
		return `mongorestore --archive --gzip --drop --stopOnError --nsInclude "$MONGO_INITDB_DATABASE.*" --username "$MONGO_INITDB_ROOT_USERNAME" --password "$MONGO_INITDB_ROOT_PASSWORD" --authenticationDatabase admin`, `mongosh --quiet --username "$MONGO_INITDB_ROOT_USERNAME" --password "$MONGO_INITDB_ROOT_PASSWORD" --authenticationDatabase admin --eval 'db.getCollectionNames().length' "$MONGO_INITDB_DATABASE"`, true
	}
	return "", "", false
}

func (s *Service) runRestore(source, target Database, b BackupExecution, op Operation) {
	ctx, cancel := context.WithTimeout(context.Background(), backupTimeout)
	defer cancel()
	var logs strings.Builder
	log := func(line string) {
		if logs.Len() < 64*1024 {
			logs.WriteString(line + "\n")
			_, _ = s.store.db.ExecContext(context.WithoutCancel(ctx), `UPDATE database_operations SET logs=? WHERE id=?`, logs.String(), op.ID)
		}
	}
	err := s.restore(ctx, source, target, b, op, log)
	status, message := "succeeded", ""
	if err != nil {
		status = "failed"
		message = i18n.Localize(err, i18n.Instance())
	}
	_, _ = s.store.db.ExecContext(context.WithoutCancel(ctx), `UPDATE database_operations SET status=?,message=?,logs=?,finished_at=? WHERE id=?`, status, message, logs.String(), sqlite.FormatTime(time.Now()), op.ID)
	if err != nil && op.Kind == "restore_test" {
		s.events.Publish(notify.Event{Type: notify.RestoreTestFailed, Resource: target.Name, Title: i18n.Instance().Sprintf("Teste de restauração falhou: %s", target.Name), Message: message})
	}
}

func (s *Service) restore(ctx context.Context, source, target Database, b BackupExecution, op Operation, log executor.LineSink) (restoreErr error) {
	if isKeyValue(target.Engine) {
		return s.restoreKeyValue(ctx, source, target, b, op, log)
	}
	script, query, ok := restoreScript(target.Engine)
	if !ok {
		return apperr.Conflict("Esta engine não suporta restauração.")
	}
	ex, server, err := s.connector.Executor(ctx, source.ServerID)
	if err != nil {
		return err
	}
	if err := s.localBackup(ctx, b, ex); err != nil {
		return err
	}
	spec, _ := specFor(target.Engine)
	container := ""
	if op.Kind == "restore_test" {
		container = "pier-restore-" + op.ID
		creds, err := s.store.credentials(ctx, source.ID)
		if err != nil {
			return err
		}
		env := []string{}
		for k, v := range spec.Environment(creds) {
			env = append(env, k+"="+v)
		}
		envPath := path.Join(server.BackupDir(source.ID), ".restore-"+op.ID)
		if err := ex.WriteFile(ctx, envPath, []byte(strings.Join(env, "\n")+"\n"), 0600); err != nil {
			return err
		}
		defer func() {
			c, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			_, _ = ex.Run(c, shell.Join("docker", "rm", "-fv", container))
			_, _ = ex.Run(c, shell.Join("rm", "-f", envPath))
		}()
		if _, err := ex.Run(ctx, shell.Join("docker", "run", "-d", "--name", container, "--network", "none", "--memory", "1g", "--cpus", "1", "--tmpfs", spec.DataPath, "--env-file", envPath, target.Config.Image)); err != nil {
			return err
		}
	} else {
		containers, err := docker.ListByResource(ctx, ex, target.ID)
		if err != nil {
			return err
		}
		container = firstRunning(containers)
		if container == "" {
			return apperr.Conflict("Inicie o banco antes de restaurar.")
		}
	}
	// Wait for initialization, including newly created restore targets.
	if _, err := ex.Run(ctx, "i=0; until "+docker.Exec(container, spec.Healthcheck)+"; do i=$((i+1)); [ \"$i\" -lt 60 ] || exit 1; sleep 2; done"); err != nil {
		return err
	}
	file := b.FilePath
	if spec.Dump.Compress {
		file = b.FilePath + ".restore-" + op.ID
		defer func() {
			c, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			_, _ = ex.Run(c, shell.Join("rm", "-f", file))
		}()
		if _, err := ex.Run(ctx, "umask 077 && "+shell.Join("gzip", "-dc", b.FilePath)+" > "+shell.Quote(file)); err != nil {
			return err
		}
	}
	if op.Kind == "restore" {
		if _, err := ex.Run(ctx, shell.Join("docker", "network", "disconnect", docker.Network, container)); err != nil {
			return err
		}
		defer func() {
			c, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			if _, err := ex.Run(c, shell.Join("docker", "network", "connect", "--alias", target.ID, docker.Network, container)); err != nil {
				restoreErr = i18n.Errorf("Não foi possível reconectar o banco: %w (restauração: %v).", err, restoreErr)
			}
		}()
	}
	// No pipe: the executor observes restore's status even if reading/decompression fails.
	log(i18n.Instance().Text("Restaurando backup. Interrompa as escritas das aplicações dependentes durante a restauração."))
	command := shell.Join("docker", "exec", "-i", container, "sh", "-c", script) + " < " + shell.Quote(file)
	// Tool output can contain credentials or data. Persist phase logs only.
	if _, err := ex.Run(ctx, command); err != nil {
		return i18n.Errorf("A restauração falhou: %w", err)
	}
	if _, err := ex.Run(ctx, docker.Exec(container, query)); err != nil {
		return i18n.Errorf("A verificação da restauração falhou: %w", err)
	}
	log(i18n.Instance().Text("Restauração verificada."))
	return nil
}

// RestoreTestSchedules lets the coordinator wire weekly restore checks into the scheduler.
func (s *Service) RestoreTestSchedules(ctx context.Context) ([]Schedule, error) {
	dbs, err := s.List(ctx, "")
	if err != nil {
		return nil, err
	}
	out := []Schedule{}
	for _, db := range dbs {
		if db.Config.Backup.RestoreTest {
			cron := db.Config.Backup.RestoreTestCron
			if cron == "" {
				cron = "0 4 * * 0"
			}
			out = append(out, Schedule{DatabaseID: db.ID, Cron: cron})
		}
	}
	return out, nil
}
