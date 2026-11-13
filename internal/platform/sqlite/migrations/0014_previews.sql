-- Pull/merge request preview deployments.

-- Opt-in preview settings of an application. No row means previews are off.
CREATE TABLE application_preview_settings (
    application_id  TEXT PRIMARY KEY REFERENCES applications(id) ON DELETE CASCADE,
    enabled         INTEGER NOT NULL DEFAULT 0,
    domain_template TEXT NOT NULL DEFAULT '',
    max_previews    INTEGER NOT NULL DEFAULT 5,
    allow_forks     INTEGER NOT NULL DEFAULT 0,
    ttl_days        INTEGER NOT NULL DEFAULT 7,
    updated_at      TEXT NOT NULL
);

-- One preview per open pull request of an application. Its id is the docker
-- identity of the preview (pier.id label, compose project, image namespace),
-- never the application's, so nothing done to a preview reaches the main app.
CREATE TABLE application_previews (
    id                 TEXT PRIMARY KEY,
    application_id     TEXT NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    number             INTEGER NOT NULL,
    provider           TEXT NOT NULL,
    branch             TEXT NOT NULL,
    commit_sha         TEXT NOT NULL,
    title              TEXT NOT NULL DEFAULT '',
    author             TEXT NOT NULL DEFAULT '',
    fork               INTEGER NOT NULL DEFAULT 0,
    url                TEXT NOT NULL DEFAULT '',
    status             TEXT NOT NULL,
    image              TEXT NOT NULL DEFAULT '',
    last_deployment_id TEXT NOT NULL DEFAULT '',
    created_at         TEXT NOT NULL,
    updated_at         TEXT NOT NULL,
    -- Last open/update event or manual redeploy; the TTL counts from here.
    last_activity_at   TEXT NOT NULL,
    UNIQUE (application_id, number)
);

-- The janitor scans by activity to find previews past their TTL.
CREATE INDEX application_previews_activity ON application_previews (last_activity_at);

-- Preview-only variables reuse env_vars with their own owner type, so the
-- CHECK constraint is widened (SQLite needs the table rebuilt for that).
CREATE TABLE env_vars_v14 (
    id           TEXT PRIMARY KEY,
    owner_type   TEXT NOT NULL CHECK (owner_type IN ('application', 'database', 'stack', 'application_preview')),
    owner_id     TEXT NOT NULL,
    key          TEXT NOT NULL,
    value        TEXT NOT NULL,
    build_time   INTEGER NOT NULL DEFAULT 0,
    created_at   TEXT NOT NULL,
    secret_build INTEGER NOT NULL DEFAULT 0,
    UNIQUE (owner_type, owner_id, key)
);
INSERT INTO env_vars_v14 (id, owner_type, owner_id, key, value, build_time, created_at, secret_build)
    SELECT id, owner_type, owner_id, key, value, build_time, created_at, secret_build FROM env_vars;
DROP TABLE env_vars;
ALTER TABLE env_vars_v14 RENAME TO env_vars;
