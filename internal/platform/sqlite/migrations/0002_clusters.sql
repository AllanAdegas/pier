CREATE TABLE clusters (
    id              TEXT PRIMARY KEY,
    name            TEXT NOT NULL,
    description     TEXT NOT NULL DEFAULT '',
    source          TEXT NOT NULL CHECK (source IN ('imported', 'k3s')),
    server_id       TEXT REFERENCES servers(id) ON DELETE RESTRICT,
    config          TEXT NOT NULL DEFAULT '{}',
    kubeconfig      TEXT NOT NULL DEFAULT '',
    endpoint        TEXT NOT NULL DEFAULT '',
    status          TEXT NOT NULL DEFAULT 'unknown',
    status_message  TEXT NOT NULL DEFAULT '',
    version         TEXT NOT NULL DEFAULT '',
    nodes           INTEGER NOT NULL DEFAULT 0,
    last_checked_at TEXT,
    created_at      TEXT NOT NULL
);
CREATE INDEX idx_clusters_server ON clusters(server_id);
