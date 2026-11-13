-- Before 0.6 every account was a full administrator of the instance. Every
-- existing account becomes instance owner so upgrades keep working, existing
-- API tokens included; owners demote others afterwards. Empty installations
-- bootstrap through access.Bootstrap when the first account is created.
CREATE TABLE access_instance_roles (
 user_id TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
 role TEXT NOT NULL CHECK(role IN ('owner','admin','developer','viewer'))
);
INSERT INTO access_instance_roles SELECT id, 'owner' FROM users;
-- The instance never loses its last owner: not by demotion, not by deleting
-- the role row, not by deleting the account (the cascade fires this trigger).
CREATE TRIGGER access_keep_last_owner_update BEFORE UPDATE OF role ON access_instance_roles
WHEN OLD.role = 'owner' AND NEW.role <> 'owner'
 AND (SELECT COUNT(*) FROM access_instance_roles WHERE role = 'owner') <= 1
BEGIN SELECT RAISE(ABORT, 'access: last instance owner'); END;
CREATE TRIGGER access_keep_last_owner_delete BEFORE DELETE ON access_instance_roles
WHEN OLD.role = 'owner' AND (SELECT COUNT(*) FROM access_instance_roles WHERE role = 'owner') <= 1
BEGIN SELECT RAISE(ABORT, 'access: last instance owner'); END;
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
 invited_by TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
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
