-- Engine wave 2: deployment origin (archive upload, Git tag, promotion).
ALTER TABLE deployments ADD COLUMN source TEXT NOT NULL DEFAULT '';
ALTER TABLE deployments ADD COLUMN ref TEXT NOT NULL DEFAULT '';
ALTER TABLE deployments ADD COLUMN promoted_from TEXT NOT NULL DEFAULT '';
