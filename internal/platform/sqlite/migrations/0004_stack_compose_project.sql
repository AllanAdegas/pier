-- The docker compose project a stack runs as, chosen once at creation as a
-- readable name ("n8n-k3j2xq") instead of the bare id. It never changes:
-- volumes and networks are named after it. Empty for stacks created before,
-- which keep running under their id.
ALTER TABLE stacks ADD COLUMN compose_project TEXT NOT NULL DEFAULT '';
