CREATE TABLE registries (
 id TEXT PRIMARY KEY, host TEXT NOT NULL, username TEXT NOT NULL, credential TEXT NOT NULL
);
CREATE TABLE application_placements (
 application_id TEXT PRIMARY KEY REFERENCES applications(id) ON DELETE CASCADE,
 build_server_id TEXT NOT NULL REFERENCES servers(id),
 registry_id TEXT NOT NULL REFERENCES registries(id),
 repository TEXT NOT NULL, destinations TEXT NOT NULL
);
CREATE TABLE placement_status (
 application_id TEXT NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
 server_id TEXT NOT NULL REFERENCES servers(id),
 image TEXT NOT NULL DEFAULT '', previous_image TEXT NOT NULL DEFAULT '',
 status TEXT NOT NULL, PRIMARY KEY(application_id, server_id)
);
