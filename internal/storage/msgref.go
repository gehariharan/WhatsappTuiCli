package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/gehariharan/wasync/internal/msgref"
)

// ErrMessageNotFound reports that a message is not (or no longer) in the
// account's visible archive. It wraps sql.ErrNoRows.
var ErrMessageNotFound = fmt.Errorf("message is not in this account's archive: %w", sql.ErrNoRows)

// MessageByRef resolves a stable message reference within one account. A
// reference minted for another account is reported as not found and never
// resolves another account's data. Lookups go through archived_messages, so
// expired content and mutation-only rows are never returned; revoked messages
// return the same tombstone shape as Messages.
func (s *Store) MessageByRef(ctx context.Context, accountID int64, accountJID string, r msgref.Ref) (Message, error) {
	if accountJID == "" || r.Account != accountJID {
		return Message{}, ErrMessageNotFound
	}
	var m Message
	var ts, received int64
	var edited, deleted, expiry sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT m.id,m.group_id,m.message_id,m.sender_id,m.sender_alt,m.timestamp,m.message_type,m.text_content,m.is_from_me,m.reply_to_message_id,m.media_metadata,m.source,m.edited_at,m.deleted_at,m.expires_at,m.received_at
 FROM archived_messages m JOIN accounts a ON a.id=m.account_id
 WHERE m.account_id=? AND a.jid=? AND m.group_id=? AND m.message_id=?`, accountID, accountJID, r.Chat, r.Message).
		Scan(&m.ID, &m.GroupID, &m.MessageID, &m.SenderID, &m.SenderAlt, &ts, &m.Type, &m.Text, &m.FromMe, &m.ReplyTo, &m.Media, &m.Source, &edited, &deleted, &expiry, &received)
	if errors.Is(err, sql.ErrNoRows) {
		return Message{}, ErrMessageNotFound
	}
	if err != nil {
		return Message{}, err
	}
	m.Timestamp = time.UnixMilli(ts).UTC()
	m.ReceivedAt = time.UnixMilli(received).UTC()
	m.EditedAt = timePointer(edited)
	m.DeletedAt = timePointer(deleted)
	m.ExpiresAt = timePointer(expiry)
	return m, nil
}
