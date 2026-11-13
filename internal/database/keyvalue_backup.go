package database

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"time"

	"pier/internal/docker"
	"pier/internal/platform/apperr"
	"pier/internal/platform/executor"
	"pier/internal/platform/i18n"
	"pier/internal/platform/shell"
)

// HelperImage is the pinned image of throwaway helper containers (volume
// archives, key-value restores). Never a moving tag.
const HelperImage = "alpine:3.22.2"

// keyValueSaveScript triggers an asynchronous save (BGSAVE) on a Redis
// protocol server, waits for it to finish and prints the RDB file. The
// password comes from REDISCLI_AUTH in the container environment, never from
// argv. Anything but a freshly started save fails loudly, so a stale dump.rdb
// is never mistaken for a new backup.
const keyValueSaveScript = `set -e
cli=%[1]s
reply=$($cli BGSAVE)
case "$reply" in "Background saving started"*) ;; *) echo "$reply" >&2; exit 1 ;; esac
i=0
while $cli INFO persistence | grep -q 'rdb_bgsave_in_progress:1'; do
  i=$((i+1)); [ "$i" -lt 3600 ] || { echo "save timed out" >&2; exit 1; }
  sleep 1
done
$cli INFO persistence | grep -q 'rdb_last_bgsave_status:ok' || { echo "save failed" >&2; exit 1; }
cat /data/dump.rdb`

// dragonflySaveScript uses SAVE RDB (Dragonfly snapshots without blocking
// reads and writes) into a single predictable file name, prints it and
// removes it.
const dragonflySaveScript = `set -e
redis-cli SAVE RDB pier-backup >&2
file=$(ls -t /data/pier-backup*.rdb | head -n 1)
cat "$file"
rm -f /data/pier-backup*.rdb`

// keyValueDump returns how the logical backup of a Redis-protocol engine is
// taken, or nil for any other engine. The result feeds the same pipeline as
// the SQL dumps (file on the server, retention, S3, download).
func keyValueDump(engine Engine) *dumpSpec {
	switch engine {
	case EngineRedis:
		return &dumpSpec{Script: fmt.Sprintf(keyValueSaveScript, "redis-cli"), Extension: "rdb"}
	case EngineKeyDB:
		return &dumpSpec{Script: fmt.Sprintf(keyValueSaveScript, "keydb-cli"), Extension: "rdb"}
	case EngineDragonfly:
		return &dumpSpec{Script: dragonflySaveScript, Extension: "rdb"}
	}
	return nil
}

func isKeyValue(engine Engine) bool { return keyValueDump(engine) != nil }

// keyValueRestoreScript replaces the dataset in the stopped container's data
// volume: it drops the append-only files and old snapshots (an AOF would take
// precedence over the RDB), copies the backup as dump.rdb and gives it to the
// owner of the data directory so the server user can read it.
const keyValueRestoreScript = `set -e
rm -rf /data/appendonlydir /data/appendonly.aof /data/dump*.rdb /data/dump*.dfs
cp "/backup/$1" /data/dump.rdb
owner=$(stat -c %u:%g /data)
chown "$owner" /data/dump.rdb
chmod 0660 /data/dump.rdb`

func (s *Service) restoreKeyValue(ctx context.Context, source, target Database, b BackupExecution, op Operation, log executor.LineSink) error {
	if op.Kind != "restore" || source.ID != target.ID {
		return apperr.Conflict("Esta engine só pode ser restaurada no próprio banco.")
	}
	ex, _, err := s.connector.Executor(ctx, source.ServerID)
	if err != nil {
		return err
	}
	if err := s.localBackup(ctx, b, ex); err != nil {
		return err
	}
	containers, err := docker.ListByResource(ctx, ex, target.ID)
	if err != nil {
		return err
	}
	running := runningContainers(containers)
	ids := make([]string, 0, len(running))
	for _, c := range running {
		ids = append(ids, c.ID)
	}
	journal, err := json.Marshal(ids)
	if err != nil {
		return err
	}
	result, err := s.store.db.ExecContext(ctx, "UPDATE database_operations SET resume_containers=? WHERE id=? AND status='running'", string(journal), op.ID)
	if err != nil {
		return err
	}
	if n, err := result.RowsAffected(); err != nil || n != 1 {
		return i18n.Errorf("não foi possível registrar os containers antes da parada")
	}
	log(i18n.Instance().Text("Parando o banco para substituir os dados. As conexões serão interrompidas."))
	restoreErr := docker.Stop(ctx, ex, running)
	if restoreErr == nil {
		restoreErr = s.replaceKeyValueData(ctx, ex, target, b, op)
	}
	startCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Minute)
	defer cancel()
	if err := docker.Start(startCtx, ex, running); err != nil {
		return i18n.Errorf("Não foi possível iniciar o banco novamente: %w (restauração: %v).", err, restoreErr)
	}
	if _, err := s.store.db.ExecContext(startCtx, "UPDATE database_operations SET resume_containers='[]' WHERE id=?", op.ID); err != nil {
		return err
	}
	if restoreErr != nil {
		return restoreErr
	}
	spec, _ := specFor(target.Engine)
	if len(running) > 0 {
		if _, err := ex.Run(startCtx, waitHealthy(running[0].ID, spec.Healthcheck)); err != nil {
			return i18n.Errorf("A verificação da restauração falhou: %w", err)
		}
	}
	log(i18n.Instance().Text("Restauração verificada."))
	return nil
}

func (s *Service) replaceKeyValueData(ctx context.Context, ex executor.Executor, target Database, b BackupExecution, op Operation) error {
	helper := "pier-restore-" + op.ID
	command := shell.Join("docker", "run", "--rm", "--name", helper, "--network", "none",
		"-v", target.ID+"_data:/data", "-v", path.Dir(b.FilePath)+":/backup:ro",
		HelperImage, "sh", "-c", keyValueRestoreScript, "sh", path.Base(b.FilePath))
	if _, err := ex.Run(ctx, command); err != nil {
		return i18n.Errorf("A restauração falhou: %w", err)
	}
	return nil
}

func runningContainers(containers []docker.Container) []docker.Container {
	running := []docker.Container{}
	for _, container := range containers {
		if container.State == "running" {
			running = append(running, container)
		}
	}
	return running
}

func waitHealthy(container, healthcheck string) string {
	return "i=0; until " + docker.Exec(container, healthcheck) + "; do i=$((i+1)); [ \"$i\" -lt 60 ] || exit 1; sleep 2; done"
}

// supportsRestoreTest reports whether the engine can be restored into a
// disposable container (SQL and MongoDB logical restores only).
func supportsRestoreTest(engine Engine) bool {
	_, _, ok := restoreScript(engine)
	return ok
}
