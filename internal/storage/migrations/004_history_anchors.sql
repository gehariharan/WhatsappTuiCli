-- One metadata-only starting point per group, including disabled groups.
-- No message body, sender identity, or media is retained here.
CREATE TABLE history_anchors (
 account_id INTEGER NOT NULL REFERENCES accounts(id),
 group_id TEXT NOT NULL,
 message_id TEXT NOT NULL,
 timestamp INTEGER NOT NULL,
 is_from_me INTEGER NOT NULL,
 PRIMARY KEY(account_id,group_id)
);
