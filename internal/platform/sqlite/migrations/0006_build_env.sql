-- Build secrets are independent of plain build arguments.
ALTER TABLE env_vars ADD COLUMN secret_build INTEGER NOT NULL DEFAULT 0;
