-- Container registries: one credential per host, sealed with secretbox.
CREATE TABLE registries (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL,
    host       TEXT NOT NULL UNIQUE,
    username   TEXT NOT NULL,
    credential TEXT NOT NULL,
    created_at TEXT NOT NULL
);

-- Where an application is built and which servers run it. No row (or only
-- the application's own server and no build server) is a single-server
-- application, deployed exactly as before.
CREATE TABLE application_placements (
    application_id  TEXT PRIMARY KEY REFERENCES applications(id) ON DELETE CASCADE,
    build_server_id TEXT REFERENCES servers(id) ON DELETE SET NULL,
    registry_id     TEXT REFERENCES registries(id),
    repository      TEXT NOT NULL DEFAULT '',
    destinations    TEXT NOT NULL DEFAULT '[]',
    updated_at      TEXT NOT NULL
);

-- Last known rollout state of an application on each server.
CREATE TABLE placement_status (
    application_id TEXT NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    server_id      TEXT NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    deployment_id  TEXT NOT NULL DEFAULT '',
    image          TEXT NOT NULL DEFAULT '',
    previous_image TEXT NOT NULL DEFAULT '',
    status         TEXT NOT NULL,
    message        TEXT NOT NULL DEFAULT '',
    updated_at     TEXT NOT NULL,
    PRIMARY KEY (application_id, server_id)
);
