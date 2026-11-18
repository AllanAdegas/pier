-- Durable journal of the last multi-server rollout of each application. It is
-- written before any server changes, so a restart in the middle of a rollout
-- can resume it or put the previous version back, never guess.
CREATE TABLE placement_rollouts (
    application_id       TEXT PRIMARY KEY REFERENCES applications(id) ON DELETE CASCADE,
    -- deployment that started the rollout
    deployment_id        TEXT NOT NULL,
    -- digest every server runs at the end; empty until it is pinned
    image                TEXT NOT NULL DEFAULT '',
    -- preparing | releasing | compensating | interrupted | completed | failed | rolled_back
    state                TEXT NOT NULL,
    -- phase cut by the restart (releasing or compensating) while interrupted
    interrupted_in       TEXT NOT NULL DEFAULT '',
    -- last resume or rollback deployment queued for an interrupted rollout
    action_deployment_id TEXT NOT NULL DEFAULT '',
    -- automatic actions taken at startup; at most one per rollout
    automatic_recoveries INTEGER NOT NULL DEFAULT 0,
    message              TEXT NOT NULL DEFAULT '',
    started_at           TEXT NOT NULL,
    updated_at           TEXT NOT NULL
);

-- One row per server of the rollout, in release order.
CREATE TABLE placement_rollout_servers (
    application_id TEXT NOT NULL REFERENCES placement_rollouts(application_id) ON DELETE CASCADE,
    -- Keep historical targets even if a server is removed: recovery must fail closed.
    server_id      TEXT NOT NULL,
    position       INTEGER NOT NULL,
    -- what the server ran before this rollout touched it
    previous_image TEXT NOT NULL DEFAULT '',
    -- pending | releasing | released | failed | restored | restore_failed
    state          TEXT NOT NULL,
    updated_at     TEXT NOT NULL,
    PRIMARY KEY (application_id, server_id)
);

-- Startup looks up the rollouts left unfinished.
CREATE INDEX placement_rollouts_state ON placement_rollouts(state);
