-- State of the secretbox "v2:" format. salt is the per-installation Argon2id
-- salt (random, not secret); keeping it in the database means the instance
-- backup and a copied pier.db always carry the salt of their ciphertexts.
-- legacy_upgraded_at records when every "v1:" ciphertext was rewritten as v2.
CREATE TABLE secretbox_state (
    id                 INTEGER PRIMARY KEY CHECK (id = 1),
    salt               BLOB NOT NULL CHECK (length(salt) = 16),
    created_at         TEXT NOT NULL,
    legacy_upgraded_at TEXT
);
