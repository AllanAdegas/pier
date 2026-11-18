-- Provider connections: a GitLab (cloud or self-hosted), Gitea/Forgejo or
-- Bitbucket Cloud account reached through an access token. The token is
-- sealed with the secretbox and only ever sent to base_url (its API).
-- base_url never changes after creation: a new address is a new connection.
CREATE TABLE git_connections (
    id TEXT PRIMARY KEY,
    provider TEXT NOT NULL CHECK(provider IN ('gitlab','gitea','bitbucket')),
    name TEXT NOT NULL,
    base_url TEXT NOT NULL,
    workspace TEXT NOT NULL DEFAULT '',
    encrypted_token TEXT NOT NULL,
    account TEXT NOT NULL DEFAULT '',
    scopes TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

-- Which connection repository an application or stack deploys from, and the
-- webhook Pier created there. orphaned marks links of deleted resources:
-- the sweeper removes their webhook from the provider, then the row.
CREATE TABLE git_connection_links (
    kind TEXT NOT NULL CHECK(kind IN ('application','stack')),
    resource_id TEXT NOT NULL,
    connection_id TEXT NOT NULL REFERENCES git_connections(id) ON DELETE RESTRICT,
    repository TEXT NOT NULL,
    clone_url TEXT NOT NULL,
    webhook_id TEXT NOT NULL DEFAULT '',
    webhook_status TEXT NOT NULL DEFAULT 'none' CHECK(webhook_status IN ('none','active','manual','failed')),
    webhook_http_status INTEGER NOT NULL DEFAULT 0,
    orphaned INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY(kind, resource_id)
);
CREATE INDEX git_connection_links_connection ON git_connection_links(connection_id);
CREATE INDEX git_connection_links_orphaned ON git_connection_links(orphaned) WHERE orphaned = 1;

CREATE TRIGGER git_connection_application_deleted AFTER DELETE ON applications
BEGIN
    UPDATE git_connection_links SET orphaned = 1 WHERE kind = 'application' AND resource_id = OLD.id;
END;

CREATE TRIGGER git_connection_stack_deleted AFTER DELETE ON stacks
BEGIN
    UPDATE git_connection_links SET orphaned = 1 WHERE kind = 'stack' AND resource_id = OLD.id;
END;

-- Persist remote webhook intent before mutation for recoverable cleanup.
ALTER TABLE git_connection_links ADD COLUMN hook_url TEXT NOT NULL DEFAULT '';
