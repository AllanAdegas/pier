-- Account security: TOTP second factor, recovery codes, password reset
-- tokens and session metadata (to list and revoke sessions).

CREATE TABLE user_totp (
    user_id    TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    -- secretbox-sealed base32 secret; the plaintext never touches the disk.
    secret     TEXT NOT NULL,
    enabled    INTEGER NOT NULL DEFAULT 0,
    -- Last accepted 30 s step: a code is accepted only for a later step,
    -- so the same code (or an older one) cannot be replayed.
    last_step  INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL,
    enabled_at TEXT
);

CREATE TABLE recovery_codes (
    user_id   TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    code_hash TEXT NOT NULL,
    PRIMARY KEY (user_id, code_hash)
);

CREATE TABLE password_resets (
    token_hash TEXT PRIMARY KEY,
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at TEXT NOT NULL,
    created_at TEXT NOT NULL
);
CREATE INDEX idx_password_resets_user ON password_resets(user_id);

ALTER TABLE sessions ADD COLUMN id TEXT NOT NULL DEFAULT '';
ALTER TABLE sessions ADD COLUMN last_seen_at TEXT NOT NULL DEFAULT '';
ALTER TABLE sessions ADD COLUMN ip TEXT NOT NULL DEFAULT '';
ALTER TABLE sessions ADD COLUMN user_agent TEXT NOT NULL DEFAULT '';
UPDATE sessions SET id = lower(hex(randomblob(16))), last_seen_at = created_at WHERE id = '';
CREATE UNIQUE INDEX idx_sessions_id ON sessions(id);
