-- Exact IDs running before a key-value restore stops them; retained until restart succeeds.
ALTER TABLE database_operations ADD COLUMN resume_containers TEXT NOT NULL DEFAULT '[]';
