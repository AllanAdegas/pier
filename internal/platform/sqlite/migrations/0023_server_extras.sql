CREATE TABLE server_operations (
 server_id TEXT PRIMARY KEY REFERENCES servers(id) ON DELETE CASCADE,
 maintenance INTEGER NOT NULL DEFAULT 0,
 cleanup_enabled INTEGER NOT NULL DEFAULT 0,
 cleanup_cron TEXT NOT NULL DEFAULT '0 4 * * 0',
 min_age_hours INTEGER NOT NULL DEFAULT 168
);
