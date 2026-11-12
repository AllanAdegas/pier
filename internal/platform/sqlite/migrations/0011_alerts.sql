CREATE TABLE alert_states (
 type TEXT NOT NULL, resource TEXT NOT NULL, active INTEGER NOT NULL DEFAULT 0,
 last_sent TEXT NOT NULL DEFAULT '', PRIMARY KEY(type, resource)
);
CREATE TABLE alert_silences (resource TEXT PRIMARY KEY, until_at TEXT NOT NULL);
CREATE TABLE alert_options (id INTEGER PRIMARY KEY CHECK(id=1), disk REAL NOT NULL DEFAULT 90,
 memory REAL NOT NULL DEFAULT 90, cpu REAL NOT NULL DEFAULT 95, minutes INTEGER NOT NULL DEFAULT 10);
INSERT INTO alert_options(id) VALUES(1);
CREATE TABLE uptime_checks (id TEXT PRIMARY KEY, resource TEXT NOT NULL, url TEXT NOT NULL,
 expected_status INTEGER NOT NULL DEFAULT 200, interval_seconds INTEGER NOT NULL DEFAULT 60,
 enabled INTEGER NOT NULL DEFAULT 1, last_checked TEXT NOT NULL DEFAULT '');
CREATE TABLE uptime_history (check_id TEXT NOT NULL REFERENCES uptime_checks(id) ON DELETE CASCADE,
 checked_at TEXT NOT NULL, status INTEGER NOT NULL, healthy INTEGER NOT NULL, PRIMARY KEY(check_id,checked_at));
CREATE INDEX uptime_history_time ON uptime_history(checked_at);
