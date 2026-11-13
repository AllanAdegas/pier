-- GitHub App of the instance: App ID, slug, private key and webhook secret,
-- sealed together with the secretbox. One row at most.
CREATE TABLE git_provider_config (id INTEGER PRIMARY KEY CHECK(id=1), encrypted TEXT NOT NULL);

-- Webhook deliveries already processed (GitHub redelivers with the same ID).
CREATE TABLE git_provider_deliveries (delivery TEXT PRIMARY KEY, received_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP);

-- Installations GitHub told us about; suspended or deleted ones mint no token.
CREATE TABLE git_provider_installations (id INTEGER PRIMARY KEY, suspended INTEGER NOT NULL DEFAULT 0);

-- Which installation and repository give credentials to an application or stack.
CREATE TABLE git_provider_selections (
    kind TEXT NOT NULL CHECK(kind IN ('application','stack')),
    resource_id TEXT NOT NULL,
    installation_id INTEGER NOT NULL,
    repository TEXT NOT NULL,
    PRIMARY KEY(kind, resource_id)
);
CREATE INDEX git_provider_selections_repository ON git_provider_selections(installation_id, repository COLLATE NOCASE);

-- Commit status and pull request feedback of a resource. GitLab, Gitea/Forgejo
-- and Bitbucket use a sealed access token, bound to the API origin it was
-- saved for (a source moved to another host never receives it); GitHub uses
-- the App selection.
CREATE TABLE git_provider_feedback (
    kind TEXT NOT NULL CHECK(kind IN ('application','stack')),
    resource_id TEXT NOT NULL,
    provider TEXT NOT NULL CHECK(provider IN ('none','github','gitlab','gitea','bitbucket')),
    encrypted_token TEXT NOT NULL DEFAULT '',
    api_origin TEXT NOT NULL DEFAULT '',
    commit_status INTEGER NOT NULL DEFAULT 1,
    pull_request_comments INTEGER NOT NULL DEFAULT 1,
    PRIMARY KEY(kind, resource_id)
);

CREATE TRIGGER git_provider_application_deleted AFTER DELETE ON applications
BEGIN
    DELETE FROM git_provider_selections WHERE kind = 'application' AND resource_id = OLD.id;
    DELETE FROM git_provider_feedback WHERE kind = 'application' AND resource_id = OLD.id;
END;

CREATE TRIGGER git_provider_stack_deleted AFTER DELETE ON stacks
BEGIN
    DELETE FROM git_provider_selections WHERE kind = 'stack' AND resource_id = OLD.id;
    DELETE FROM git_provider_feedback WHERE kind = 'stack' AND resource_id = OLD.id;
END;
