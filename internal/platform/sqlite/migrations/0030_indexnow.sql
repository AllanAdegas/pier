-- Hosts that notify search engines (IndexNow) after each deployment. The
-- key is public by design: search engines read it at https://<host>/<key>.txt.
-- resource_kind/resource_id point to an application or a stack; rows of a
-- resource that no longer exists, or no longer has the host, are pruned by
-- the indexnow service.
CREATE TABLE indexnow_sites (
    id                TEXT PRIMARY KEY,
    host              TEXT NOT NULL UNIQUE,
    resource_kind     TEXT NOT NULL CHECK (resource_kind IN ('applications', 'stacks')),
    resource_id       TEXT NOT NULL,
    key               TEXT NOT NULL UNIQUE,
    created_at        TEXT NOT NULL,
    last_attempt_at   TEXT,
    last_submitted_at TEXT,
    last_status       INTEGER NOT NULL DEFAULT 0,
    last_message      TEXT NOT NULL DEFAULT '',
    last_url_count    INTEGER NOT NULL DEFAULT 0,
    last_problem      INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX idx_indexnow_sites_resource ON indexnow_sites(resource_kind, resource_id);
