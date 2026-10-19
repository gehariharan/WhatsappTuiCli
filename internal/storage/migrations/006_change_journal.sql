-- Change journal (ADR-001 D-4). Every archive mutation appends rows here in
-- the same transaction. Rows carry identifiers only, never message bodies.
CREATE TABLE IF NOT EXISTS change_journal(
 sequence INTEGER PRIMARY KEY AUTOINCREMENT,
 account_id INTEGER NOT NULL,
 chat_id TEXT NOT NULL,
 message_id TEXT NOT NULL DEFAULT '',
 op TEXT NOT NULL,
 op_version INTEGER NOT NULL DEFAULT 1,
 committed_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS change_journal_account_sequence ON change_journal(account_id,sequence);

-- epoch identifies this archive's journal; a recreated archive gets a new one
-- so stale cursors fail with cursor_expired. low_water is the highest pruned
-- sequence: cursors behind it can no longer be served.
CREATE TABLE IF NOT EXISTS journal_meta(
 id INTEGER PRIMARY KEY CHECK(id=1),
 epoch TEXT NOT NULL,
 low_water INTEGER NOT NULL DEFAULT 0
);
INSERT INTO journal_meta(id,epoch,low_water) VALUES(1,lower(hex(randomblob(16))),0) ON CONFLICT(id) DO NOTHING;

-- Named consumer checkpoints, scoped per account. Written only by the owner
-- through the API (scope observe); offline query reads them.
CREATE TABLE IF NOT EXISTS consumer_checkpoints(
 account_id INTEGER NOT NULL,
 consumer TEXT NOT NULL,
 sequence INTEGER NOT NULL,
 epoch TEXT NOT NULL,
 updated_at INTEGER NOT NULL,
 PRIMARY KEY(account_id,consumer)
);

-- Schema 5 readers stay correct (they ignore the journal); writers must be at
-- least schema 6 because every mutation has to append to the journal.
CREATE TABLE IF NOT EXISTS schema_compat(
 id INTEGER PRIMARY KEY CHECK(id=1),
 min_reader INTEGER NOT NULL,
 min_writer INTEGER NOT NULL
);
INSERT INTO schema_compat(id,min_reader,min_writer) VALUES(1,5,6)
 ON CONFLICT(id) DO UPDATE SET min_reader=excluded.min_reader,min_writer=excluded.min_writer;
