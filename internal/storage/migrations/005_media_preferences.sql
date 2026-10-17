CREATE TABLE IF NOT EXISTS media_preferences (
 account_id INTEGER NOT NULL,
 group_id TEXT NOT NULL,
 mode TEXT NOT NULL CHECK(mode IN ('inherit','auto','manual')),
 PRIMARY KEY(account_id,group_id),
 FOREIGN KEY(account_id,group_id) REFERENCES groups(account_id,group_id) ON DELETE CASCADE
);
