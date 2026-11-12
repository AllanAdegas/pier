ALTER TABLE servers ADD COLUMN proxy_token TEXT NOT NULL DEFAULT '';
UPDATE servers SET proxy_token = lower(hex(randomblob(32))) WHERE proxy_token = '';
ALTER TABLE servers ADD COLUMN validation_checks TEXT NOT NULL DEFAULT '[]';
