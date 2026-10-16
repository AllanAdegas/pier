CREATE TABLE users (
    id            TEXT PRIMARY KEY,
    email         TEXT NOT NULL UNIQUE,
    name          TEXT NOT NULL,
    password_hash TEXT NOT NULL,
    created_at    TEXT NOT NULL
);

CREATE TABLE sessions (
    token_hash TEXT PRIMARY KEY,
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at TEXT NOT NULL,
    created_at TEXT NOT NULL
);
CREATE INDEX idx_sessions_user ON sessions(user_id);

CREATE TABLE api_tokens (
    id           TEXT PRIMARY KEY,
    user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name         TEXT NOT NULL,
    token_hash   TEXT NOT NULL UNIQUE,
    abilities    TEXT NOT NULL,
    last_used_at TEXT,
    created_at   TEXT NOT NULL
);

CREATE TABLE settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

CREATE TABLE private_keys (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    private_key TEXT NOT NULL,
    public_key  TEXT NOT NULL,
    fingerprint TEXT NOT NULL,
    created_at  TEXT NOT NULL
);

CREATE TABLE servers (
    id              TEXT PRIMARY KEY,
    name            TEXT NOT NULL,
    description     TEXT NOT NULL DEFAULT '',
    kind            TEXT NOT NULL CHECK (kind IN ('local', 'ssh')),
    host            TEXT NOT NULL DEFAULT '',
    port            INTEGER NOT NULL DEFAULT 22,
    username        TEXT NOT NULL DEFAULT '',
    private_key_id  TEXT REFERENCES private_keys(id) ON DELETE RESTRICT,
    host_key        TEXT NOT NULL DEFAULT '',
    data_dir        TEXT NOT NULL,
    proxy           TEXT NOT NULL DEFAULT '{}',
    status          TEXT NOT NULL DEFAULT 'unknown',
    status_message  TEXT NOT NULL DEFAULT '',
    docker_version  TEXT NOT NULL DEFAULT '',
    last_checked_at TEXT,
    created_at      TEXT NOT NULL
);

CREATE TABLE projects (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    created_at  TEXT NOT NULL
);

CREATE TABLE environments (
    id         TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    created_at TEXT NOT NULL,
    UNIQUE (project_id, name)
);

CREATE TABLE applications (
    id             TEXT PRIMARY KEY,
    environment_id TEXT NOT NULL REFERENCES environments(id) ON DELETE RESTRICT,
    server_id      TEXT NOT NULL REFERENCES servers(id) ON DELETE RESTRICT,
    name           TEXT NOT NULL,
    description    TEXT NOT NULL DEFAULT '',
    config         TEXT NOT NULL,
    webhook_secret TEXT NOT NULL,
    status         TEXT NOT NULL DEFAULT 'idle',
    current_image  TEXT NOT NULL DEFAULT '',
    created_at     TEXT NOT NULL,
    updated_at     TEXT NOT NULL
);
CREATE INDEX idx_applications_environment ON applications(environment_id);
CREATE INDEX idx_applications_server ON applications(server_id);

CREATE TABLE env_vars (
    id         TEXT PRIMARY KEY,
    owner_type TEXT NOT NULL CHECK (owner_type IN ('application', 'database', 'stack')),
    owner_id   TEXT NOT NULL,
    key        TEXT NOT NULL,
    value      TEXT NOT NULL,
    build_time INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL,
    UNIQUE (owner_type, owner_id, key)
);

CREATE TABLE deployments (
    id             TEXT PRIMARY KEY,
    application_id TEXT NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    status         TEXT NOT NULL,
    trigger        TEXT NOT NULL,
    commit_sha     TEXT NOT NULL DEFAULT '',
    commit_message TEXT NOT NULL DEFAULT '',
    image          TEXT NOT NULL DEFAULT '',
    rollback_image TEXT NOT NULL DEFAULT '',
    error          TEXT NOT NULL DEFAULT '',
    created_at     TEXT NOT NULL,
    started_at     TEXT,
    finished_at    TEXT
);
CREATE INDEX idx_deployments_application ON deployments(application_id, created_at DESC);
CREATE INDEX idx_deployments_status ON deployments(status);

CREATE TABLE deployment_logs (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    deployment_id TEXT NOT NULL REFERENCES deployments(id) ON DELETE CASCADE,
    level         TEXT NOT NULL,
    line          TEXT NOT NULL,
    created_at    TEXT NOT NULL
);
CREATE INDEX idx_deployment_logs_deployment ON deployment_logs(deployment_id, id);

CREATE TABLE databases (
    id             TEXT PRIMARY KEY,
    environment_id TEXT NOT NULL REFERENCES environments(id) ON DELETE RESTRICT,
    server_id      TEXT NOT NULL REFERENCES servers(id) ON DELETE RESTRICT,
    name           TEXT NOT NULL,
    description    TEXT NOT NULL DEFAULT '',
    engine         TEXT NOT NULL,
    config         TEXT NOT NULL,
    credentials    TEXT NOT NULL,
    status         TEXT NOT NULL DEFAULT 'idle',
    created_at     TEXT NOT NULL,
    updated_at     TEXT NOT NULL
);
CREATE INDEX idx_databases_environment ON databases(environment_id);

CREATE TABLE backup_executions (
    id          TEXT PRIMARY KEY,
    database_id TEXT NOT NULL REFERENCES databases(id) ON DELETE CASCADE,
    trigger     TEXT NOT NULL,
    status      TEXT NOT NULL,
    file_path   TEXT NOT NULL DEFAULT '',
    size_bytes  INTEGER NOT NULL DEFAULT 0,
    message     TEXT NOT NULL DEFAULT '',
    started_at  TEXT NOT NULL,
    finished_at TEXT
);
CREATE INDEX idx_backup_executions_database ON backup_executions(database_id, started_at DESC);

CREATE TABLE stacks (
    id             TEXT PRIMARY KEY,
    environment_id TEXT NOT NULL REFERENCES environments(id) ON DELETE RESTRICT,
    server_id      TEXT NOT NULL REFERENCES servers(id) ON DELETE RESTRICT,
    name           TEXT NOT NULL,
    description    TEXT NOT NULL DEFAULT '',
    template       TEXT NOT NULL DEFAULT '',
    compose        TEXT NOT NULL,
    routes         TEXT NOT NULL DEFAULT '[]',
    status         TEXT NOT NULL DEFAULT 'idle',
    created_at     TEXT NOT NULL,
    updated_at     TEXT NOT NULL
);
CREATE INDEX idx_stacks_environment ON stacks(environment_id);

CREATE TABLE scheduled_tasks (
    id             TEXT PRIMARY KEY,
    application_id TEXT NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    name           TEXT NOT NULL,
    command        TEXT NOT NULL,
    cron           TEXT NOT NULL,
    enabled        INTEGER NOT NULL DEFAULT 1,
    created_at     TEXT NOT NULL
);

CREATE TABLE task_executions (
    id          TEXT PRIMARY KEY,
    task_id     TEXT NOT NULL REFERENCES scheduled_tasks(id) ON DELETE CASCADE,
    status      TEXT NOT NULL,
    output      TEXT NOT NULL DEFAULT '',
    started_at  TEXT NOT NULL,
    finished_at TEXT
);
CREATE INDEX idx_task_executions_task ON task_executions(task_id, started_at DESC);

CREATE TABLE notification_channels (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL,
    kind       TEXT NOT NULL,
    config     TEXT NOT NULL,
    events     TEXT NOT NULL,
    enabled    INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL
);
