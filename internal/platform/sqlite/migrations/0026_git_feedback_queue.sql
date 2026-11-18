-- Durable queue of the feedback Pier gives to Git providers (commit statuses,
-- GitHub check runs and pull request comments). One row per subject: a
-- deployment run on a commit ("run:<sha>:<context>") or one pull request
-- comment ("comment:<number>:<marker>"). A newer report replaces the payload
-- of its subject, so a retry never sends an older state after a newer one.
-- Payloads hold no credential: the token is resolved when the report is sent.
CREATE TABLE git_provider_reports (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    kind TEXT NOT NULL CHECK(kind IN ('application','stack')),
    resource_id TEXT NOT NULL,
    operation TEXT NOT NULL CHECK(operation IN ('run','comment')),
    subject TEXT NOT NULL,
    payload TEXT NOT NULL,
    -- Deployment (or stack run) the payload belongs to; a new run on the same
    -- subject starts a new GitHub check run.
    run_id TEXT NOT NULL DEFAULT '',
    -- Order of the phase within one run (queued < in progress < finished):
    -- a report of an earlier phase of the same run is ignored.
    phase_rank INTEGER NOT NULL DEFAULT 0,
    check_run_id INTEGER NOT NULL DEFAULT 0,
    -- Bumped on every replacement; a send only settles the revision it sent.
    revision INTEGER NOT NULL DEFAULT 1,
    state TEXT NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','delivered','dead')),
    attempts INTEGER NOT NULL DEFAULT 0,
    -- Unix milliseconds.
    next_attempt_at INTEGER NOT NULL,
    -- Provider name and HTTP status only, in the language of the instance.
    last_error TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    UNIQUE(kind, resource_id, subject)
);
CREATE INDEX git_provider_reports_due ON git_provider_reports(state, next_attempt_at);

CREATE TRIGGER git_provider_reports_application_deleted AFTER DELETE ON applications
BEGIN
    DELETE FROM git_provider_reports WHERE kind = 'application' AND resource_id = OLD.id;
END;

CREATE TRIGGER git_provider_reports_stack_deleted AFTER DELETE ON stacks
BEGIN
    DELETE FROM git_provider_reports WHERE kind = 'stack' AND resource_id = OLD.id;
END;
