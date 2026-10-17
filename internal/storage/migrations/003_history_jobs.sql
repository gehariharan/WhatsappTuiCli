CREATE TABLE history_jobs (
 account_id INTEGER NOT NULL,
 group_id TEXT NOT NULL,
 state TEXT NOT NULL DEFAULT 'queued',
 request_id TEXT NOT NULL DEFAULT '',
 cursor_id TEXT NOT NULL DEFAULT '',
 cursor_timestamp INTEGER NOT NULL DEFAULT 0,
 cursor_from_me INTEGER NOT NULL DEFAULT 0,
 requested_at INTEGER,
 updated_at INTEGER NOT NULL,
 batch_messages INTEGER NOT NULL DEFAULT 0,
 batches_received INTEGER NOT NULL DEFAULT 0,
 last_error TEXT NOT NULL DEFAULT '',
 PRIMARY KEY(account_id,group_id),
 FOREIGN KEY(account_id,group_id) REFERENCES groups(account_id,group_id)
);
INSERT INTO history_jobs(account_id,group_id,updated_at)
 SELECT account_id,group_id,CAST(unixepoch('subsec')*1000 AS INTEGER) FROM groups WHERE sync_enabled=1;
