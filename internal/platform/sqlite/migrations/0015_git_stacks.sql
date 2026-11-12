-- Stacks from a Git repository: the fourth stack source. One row per stack;
-- the repository address (it may carry an HTTPS token) and the webhook secret
-- are sealed with the instance key.
CREATE TABLE stack_git_sources (
    stack_id            TEXT PRIMARY KEY REFERENCES stacks(id) ON DELETE CASCADE,
    repository          TEXT NOT NULL,
    branch              TEXT NOT NULL DEFAULT 'main',
    tag                 TEXT NOT NULL DEFAULT '',
    base_directory      TEXT NOT NULL DEFAULT '',
    compose_path        TEXT NOT NULL DEFAULT 'docker-compose.yaml',
    private_key_id      TEXT NOT NULL DEFAULT '',
    webhook_secret      TEXT NOT NULL,
    watch_paths         TEXT NOT NULL DEFAULT '[]',
    auto_deploy         INTEGER NOT NULL DEFAULT 1,
    accepted_risks      TEXT NOT NULL DEFAULT '[]',
    pending_risks       TEXT NOT NULL DEFAULT '[]',
    last_ref            TEXT NOT NULL DEFAULT '',
    last_commit         TEXT NOT NULL DEFAULT '',
    last_commit_message TEXT NOT NULL DEFAULT '',
    last_deployed_at    TEXT NOT NULL DEFAULT ''
);
