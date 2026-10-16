-- A stack may run the compose file of a folder of the server instead of one
-- kept by Pier; "compose" is then empty.
ALTER TABLE stacks ADD COLUMN directory    TEXT NOT NULL DEFAULT '';
ALTER TABLE stacks ADD COLUMN compose_file TEXT NOT NULL DEFAULT '';
ALTER TABLE stacks ADD COLUMN project_name TEXT NOT NULL DEFAULT '';
