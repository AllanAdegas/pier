-- Docker Swarm clusters created by Pier from registered servers. Join tokens
-- are never stored: they are read from a manager when a node joins.
CREATE TABLE swarm_clusters (
    id             TEXT PRIMARY KEY,
    name           TEXT NOT NULL,
    description    TEXT NOT NULL DEFAULT '',
    swarm_id       TEXT NOT NULL DEFAULT '',
    advertise_addr TEXT NOT NULL,
    created_at     TEXT NOT NULL
);

-- A server belongs to at most one swarm (a Docker engine joins one swarm).
CREATE TABLE swarm_nodes (
    server_id      TEXT PRIMARY KEY REFERENCES servers(id) ON DELETE RESTRICT,
    cluster_id     TEXT NOT NULL REFERENCES swarm_clusters(id) ON DELETE CASCADE,
    node_id        TEXT NOT NULL,
    role           TEXT NOT NULL CHECK (role IN ('manager', 'worker')),
    is_primary     INTEGER NOT NULL DEFAULT 0,
    advertise_addr TEXT NOT NULL DEFAULT '',
    joined_at      TEXT NOT NULL
);
CREATE INDEX idx_swarm_nodes_cluster ON swarm_nodes(cluster_id);

-- Applications deployed as Swarm services instead of containers on their
-- server (which still builds the image).
CREATE TABLE application_swarm (
    application_id TEXT PRIMARY KEY REFERENCES applications(id) ON DELETE CASCADE,
    cluster_id     TEXT NOT NULL REFERENCES swarm_clusters(id) ON DELETE RESTRICT,
    registry_id    TEXT REFERENCES registries(id),
    repository     TEXT NOT NULL DEFAULT '',
    deployed       INTEGER NOT NULL DEFAULT 0,
    updated_at     TEXT NOT NULL
);
CREATE INDEX idx_application_swarm_cluster ON application_swarm(cluster_id);
