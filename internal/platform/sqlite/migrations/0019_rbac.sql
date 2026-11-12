-- Only the earliest existing account becomes instance owner. Others receive no
-- implicit access. Empty installations bootstrap through access.Bootstrap.
CREATE TABLE access_instance_roles (
 user_id TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
 role TEXT NOT NULL CHECK(role IN ('owner','admin','developer','viewer'))
);
INSERT INTO access_instance_roles SELECT id, 'owner' FROM users ORDER BY created_at, id LIMIT 1;
CREATE TABLE access_memberships (
 project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
 user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 role TEXT NOT NULL CHECK(role IN ('owner','admin','developer','viewer')),
 PRIMARY KEY(project_id,user_id)
);
CREATE TABLE access_invites (
 id TEXT PRIMARY KEY,
 project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
 email TEXT NOT NULL,
 role TEXT NOT NULL CHECK(role IN ('admin','developer','viewer')),
 token_hash TEXT NOT NULL UNIQUE,
 expires_at INTEGER NOT NULL,
 consumed_at INTEGER,
 revoked_at INTEGER
);
CREATE TABLE access_oidc_config (
 id INTEGER PRIMARY KEY CHECK(id=1), sealed TEXT NOT NULL
);
CREATE TABLE access_oidc_links (
 issuer TEXT NOT NULL, subject TEXT NOT NULL,
 user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 PRIMARY KEY(issuer,subject)
);
CREATE TABLE access_oidc_states (
 state_hash TEXT PRIMARY KEY, browser_hash TEXT NOT NULL,
 sealed TEXT NOT NULL, expires_at INTEGER NOT NULL
);
