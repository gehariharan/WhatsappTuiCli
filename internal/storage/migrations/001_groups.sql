CREATE TABLE accounts (
 id INTEGER PRIMARY KEY,
 jid TEXT NOT NULL UNIQUE,
 created_at INTEGER NOT NULL,
 last_discovery_at INTEGER
);
CREATE TABLE app_state (
 id INTEGER PRIMARY KEY CHECK (id=1),
 account_id INTEGER NOT NULL REFERENCES accounts(id)
);
CREATE TABLE groups (
 account_id INTEGER NOT NULL REFERENCES accounts(id),
 group_id TEXT NOT NULL,
 name TEXT NOT NULL,
 sync_enabled INTEGER NOT NULL DEFAULT 0 CHECK (sync_enabled IN (0,1)),
 enabled_at INTEGER,
 created_at INTEGER NOT NULL,
 updated_at INTEGER NOT NULL,
 PRIMARY KEY (account_id,group_id)
);
