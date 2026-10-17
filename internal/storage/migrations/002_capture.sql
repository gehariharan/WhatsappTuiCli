ALTER TABLE groups ADD COLUMN last_message_at INTEGER;
CREATE TABLE messages (
 id INTEGER PRIMARY KEY,
 account_id INTEGER NOT NULL,
 group_id TEXT NOT NULL,
 message_id TEXT NOT NULL,
 sender_id TEXT NOT NULL DEFAULT '',
 sender_alt TEXT NOT NULL DEFAULT '',
 timestamp INTEGER NOT NULL DEFAULT 0,
 message_type TEXT NOT NULL DEFAULT 'unknown',
 text_content TEXT NOT NULL DEFAULT '',
 is_from_me INTEGER NOT NULL DEFAULT 0,
 reply_to_message_id TEXT NOT NULL DEFAULT '',
 media_metadata TEXT NOT NULL DEFAULT '',
 source TEXT NOT NULL,
 has_original INTEGER NOT NULL DEFAULT 0,
 edited_at INTEGER,
 deleted_at INTEGER,
 expires_at INTEGER,
 received_at INTEGER NOT NULL,
 UNIQUE(account_id,group_id,message_id),
 FOREIGN KEY(account_id,group_id) REFERENCES groups(account_id,group_id)
);
CREATE INDEX messages_group_time ON messages(account_id,group_id,timestamp DESC,id DESC);
CREATE INDEX messages_group_sender ON messages(account_id,group_id,sender_id,timestamp DESC);
CREATE INDEX messages_expiry ON messages(expires_at) WHERE expires_at IS NOT NULL;
CREATE VIRTUAL TABLE messages_fts USING fts5(text_content,content='messages',content_rowid='id',tokenize='unicode61');
CREATE TRIGGER messages_ai AFTER INSERT ON messages BEGIN
 INSERT INTO messages_fts(rowid,text_content) VALUES(new.id,new.text_content);
END;
CREATE TRIGGER messages_ad AFTER DELETE ON messages BEGIN
 INSERT INTO messages_fts(messages_fts,rowid,text_content) VALUES('delete',old.id,old.text_content);
END;
CREATE TRIGGER messages_au AFTER UPDATE OF text_content ON messages BEGIN
 INSERT INTO messages_fts(messages_fts,rowid,text_content) VALUES('delete',old.id,old.text_content);
 INSERT INTO messages_fts(rowid,text_content) VALUES(new.id,new.text_content);
END;
CREATE TABLE sync_state (
 account_id INTEGER NOT NULL,
 group_id TEXT NOT NULL,
 last_received_at INTEGER,
 last_history_sync_at INTEGER,
 history_state TEXT NOT NULL DEFAULT 'not_observed',
 last_error TEXT NOT NULL DEFAULT '',
 PRIMARY KEY(account_id,group_id),
 FOREIGN KEY(account_id,group_id) REFERENCES groups(account_id,group_id)
);
CREATE TABLE history_batches (
 id INTEGER PRIMARY KEY,
 account_id INTEGER NOT NULL REFERENCES accounts(id),
 sync_type TEXT NOT NULL,
 chunk_order INTEGER NOT NULL,
 reported_progress INTEGER NOT NULL,
 status TEXT NOT NULL,
 imported INTEGER NOT NULL DEFAULT 0,
 skipped INTEGER NOT NULL DEFAULT 0,
 failures INTEGER NOT NULL DEFAULT 0,
 started_at INTEGER NOT NULL,
 finished_at INTEGER
);
CREATE TABLE capture_runs (
 id INTEGER PRIMARY KEY,
 account_id INTEGER NOT NULL REFERENCES accounts(id),
 started_at INTEGER NOT NULL,
 heartbeat_at INTEGER NOT NULL,
 finished_at INTEGER
);
CREATE TABLE sync_gaps (
 id INTEGER PRIMARY KEY,
 account_id INTEGER NOT NULL REFERENCES accounts(id),
 started_at INTEGER NOT NULL,
 ended_at INTEGER,
 reason TEXT NOT NULL
);
CREATE VIEW archived_messages AS
 SELECT id,account_id,group_id,message_id,sender_id,sender_alt,timestamp,message_type,
 text_content,is_from_me,reply_to_message_id,media_metadata,edited_at,deleted_at,expires_at,received_at,source
 FROM messages WHERE has_original=1 AND (expires_at IS NULL OR expires_at>CAST(unixepoch('subsec')*1000 AS INTEGER));
