-- Shared variables of a project or of an environment. Applications use them
-- as ${{project.NAME}} / ${{environment.NAME}}. Values are sealed with
-- secretbox; secret ones are never returned by the API.
CREATE TABLE shared_variables (
    id             TEXT PRIMARY KEY,
    project_id     TEXT REFERENCES projects(id) ON DELETE CASCADE,
    environment_id TEXT REFERENCES environments(id) ON DELETE CASCADE,
    key            TEXT NOT NULL,
    value          TEXT NOT NULL,
    secret         INTEGER NOT NULL DEFAULT 0,
    created_at     TEXT NOT NULL,
    updated_at     TEXT NOT NULL,
    CHECK ((project_id IS NULL) <> (environment_id IS NULL))
);
CREATE UNIQUE INDEX shared_variables_project_key ON shared_variables (project_id, key) WHERE project_id IS NOT NULL;
CREATE UNIQUE INDEX shared_variables_environment_key ON shared_variables (environment_id, key) WHERE environment_id IS NOT NULL;

-- An application whose referenced shared variable changed after its last
-- successful deployment needs a redeploy. Nothing is redeployed automatically.
CREATE TABLE application_redeploy_marks (
    application_id TEXT PRIMARY KEY REFERENCES applications(id) ON DELETE CASCADE,
    reason         TEXT NOT NULL,
    marked_at      TEXT NOT NULL
);

-- The domain Pier generated for an application. removed = 1 when the user
-- took it out of the configuration: it is not added back on its own.
CREATE TABLE application_auto_domains (
    application_id TEXT PRIMARY KEY REFERENCES applications(id) ON DELETE CASCADE,
    domain         TEXT NOT NULL UNIQUE,
    removed        INTEGER NOT NULL DEFAULT 0,
    created_at     TEXT NOT NULL
);
