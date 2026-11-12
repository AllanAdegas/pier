ALTER TABLE backup_executions ADD COLUMN remote_key TEXT NOT NULL DEFAULT '';
CREATE TABLE database_operations (
 id TEXT PRIMARY KEY,
 database_id TEXT NOT NULL REFERENCES databases(id) ON DELETE CASCADE,
 backup_id TEXT NOT NULL REFERENCES backup_executions(id) ON DELETE CASCADE,
 kind TEXT NOT NULL,
 status TEXT NOT NULL,
 message TEXT NOT NULL DEFAULT '',
 logs TEXT NOT NULL DEFAULT '',
 started_at TEXT NOT NULL,
 finished_at TEXT
);
CREATE INDEX idx_database_operations ON database_operations(database_id, started_at DESC);
