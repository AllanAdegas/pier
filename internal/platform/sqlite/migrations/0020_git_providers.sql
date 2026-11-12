CREATE TABLE git_provider_config (id INTEGER PRIMARY KEY CHECK(id=1), encrypted TEXT NOT NULL);
CREATE TABLE git_provider_deliveries (delivery TEXT PRIMARY KEY, received_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP);
CREATE TABLE git_provider_installations (id INTEGER PRIMARY KEY, suspended INTEGER NOT NULL DEFAULT 0);
CREATE TABLE git_provider_selections (kind TEXT NOT NULL CHECK(kind IN ('application','stack')), resource_id TEXT NOT NULL, installation_id INTEGER NOT NULL, repository TEXT NOT NULL, PRIMARY KEY(kind,resource_id));
