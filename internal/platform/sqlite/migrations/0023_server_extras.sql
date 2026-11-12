-- Server operations (maintenance mode, scheduled safe cleanup), cloud
-- provisioning and log drains.

-- No row means: deployments allowed, cleanup not scheduled.
CREATE TABLE server_operations (
    server_id       TEXT PRIMARY KEY REFERENCES servers(id) ON DELETE CASCADE,
    maintenance     INTEGER NOT NULL DEFAULT 0,
    cleanup_enabled INTEGER NOT NULL DEFAULT 0,
    cleanup_cron    TEXT NOT NULL DEFAULT '0 4 * * 0',
    min_age_hours   INTEGER NOT NULL DEFAULT 168
);

-- API token of a cloud account, sealed with secretbox. key_id is the Pier
-- SSH key injected into every machine created with this credential.
CREATE TABLE cloud_credentials (
    id         TEXT PRIMARY KEY,
    provider   TEXT NOT NULL CHECK (provider IN ('hetzner', 'digitalocean')),
    name       TEXT NOT NULL UNIQUE,
    token      TEXT NOT NULL,
    key_id     TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL
);

-- A machine Pier asked a provider to create. server_id is the Pier server
-- registered for it, once SSH answers.
CREATE TABLE cloud_machines (
    id             TEXT PRIMARY KEY,
    credential_id  TEXT NOT NULL REFERENCES cloud_credentials(id) ON DELETE RESTRICT,
    name           TEXT NOT NULL,
    region         TEXT NOT NULL,
    size           TEXT NOT NULL,
    image          TEXT NOT NULL,
    install_docker INTEGER NOT NULL DEFAULT 1,
    external_id    TEXT NOT NULL DEFAULT '',
    ipv4           TEXT NOT NULL DEFAULT '',
    server_id      TEXT REFERENCES servers(id) ON DELETE SET NULL,
    status         TEXT NOT NULL,
    error          TEXT NOT NULL DEFAULT '',
    created_at     TEXT NOT NULL
);
CREATE INDEX idx_cloud_machines_credential ON cloud_machines(credential_id);

-- Forwarding of container logs to an external sink. token is sealed;
-- labels is a JSON object; scope_type is project, application or stack.
CREATE TABLE log_drains (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL UNIQUE,
    kind       TEXT NOT NULL CHECK (kind IN ('http', 'loki')),
    url        TEXT NOT NULL,
    token      TEXT NOT NULL DEFAULT '',
    labels     TEXT NOT NULL DEFAULT '{}',
    scope_type TEXT NOT NULL CHECK (scope_type IN ('project', 'application', 'stack')),
    scope_id   TEXT NOT NULL,
    enabled    INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
