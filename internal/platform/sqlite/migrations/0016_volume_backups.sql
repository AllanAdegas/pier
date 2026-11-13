-- Volume backups of applications and stacks (resource_type: application | stack).
-- There is no foreign key to the resource tables: one table per type.
CREATE TABLE volume_backup_policies (
    resource_type    TEXT NOT NULL,
    resource_id      TEXT NOT NULL,
    enabled          INTEGER NOT NULL DEFAULT 0,
    cron             TEXT NOT NULL DEFAULT '0 3 * * *',
    retention        INTEGER NOT NULL DEFAULT 7,
    s3               INTEGER NOT NULL DEFAULT 0,
    quiesce          INTEGER NOT NULL DEFAULT 0,
    include_external INTEGER NOT NULL DEFAULT 0,
    created_at       TEXT NOT NULL,
    updated_at       TEXT NOT NULL,
    PRIMARY KEY (resource_type, resource_id)
);

CREATE TABLE volume_backups (
    id            TEXT PRIMARY KEY,
    resource_type TEXT NOT NULL,
    resource_id   TEXT NOT NULL,
    server_id     TEXT NOT NULL DEFAULT '',
    trigger       TEXT NOT NULL,
    status        TEXT NOT NULL,
    file_path     TEXT NOT NULL DEFAULT '',
    size_bytes    INTEGER NOT NULL DEFAULT 0,
    mounts        TEXT NOT NULL DEFAULT '[]',
    stopped       TEXT NOT NULL DEFAULT '[]',
    remote_key    TEXT NOT NULL DEFAULT '',
    message       TEXT NOT NULL DEFAULT '',
    started_at    TEXT NOT NULL,
    finished_at   TEXT
);
CREATE INDEX idx_volume_backups ON volume_backups(resource_type, resource_id, started_at DESC);

CREATE TABLE volume_operations (
    id            TEXT PRIMARY KEY,
    resource_type TEXT NOT NULL,
    resource_id   TEXT NOT NULL,
    backup_id     TEXT NOT NULL REFERENCES volume_backups(id) ON DELETE CASCADE,
    kind          TEXT NOT NULL,
    status        TEXT NOT NULL,
    stopped       TEXT NOT NULL DEFAULT '[]',
    message       TEXT NOT NULL DEFAULT '',
    logs          TEXT NOT NULL DEFAULT '',
    started_at    TEXT NOT NULL,
    finished_at   TEXT
);
CREATE INDEX idx_volume_operations ON volume_operations(resource_type, resource_id, started_at DESC);

-- Deleting a resource stops its schedule. Archives (and their records) stay,
-- like the data volumes: destroying data is an explicit choice.
CREATE TRIGGER volume_policy_application_deleted AFTER DELETE ON applications
BEGIN
    DELETE FROM volume_backup_policies WHERE resource_type = 'application' AND resource_id = OLD.id;
END;
CREATE TRIGGER volume_policy_stack_deleted AFTER DELETE ON stacks
BEGIN
    DELETE FROM volume_backup_policies WHERE resource_type = 'stack' AND resource_id = OLD.id;
END;
