ALTER TABLE deployments ADD COLUMN hints TEXT NOT NULL DEFAULT '[]';
ALTER TABLE deployments ADD COLUMN retired_logs TEXT NOT NULL DEFAULT '[]';
ALTER TABLE deployments ADD COLUMN suggested_port INTEGER NOT NULL DEFAULT 0;

-- Existing task output predates redaction and must only be returned to write callers.
ALTER TABLE task_executions ADD COLUMN output_masked INTEGER NOT NULL DEFAULT 0;
