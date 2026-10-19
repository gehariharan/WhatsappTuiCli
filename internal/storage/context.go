package storage

import (
	"context"
	"database/sql"
	"slices"
	"time"
)

const messageColumns = `id,group_id,message_id,sender_id,sender_alt,timestamp,message_type,text_content,is_from_me,reply_to_message_id,media_metadata,source,edited_at,deleted_at,expires_at,received_at`

func scanMessage(rows *sql.Rows) (Message, error) {
	var m Message
	var ts, received int64
	var edited, deleted, expiry sql.NullInt64
	if err := rows.Scan(&m.ID, &m.GroupID, &m.MessageID, &m.SenderID, &m.SenderAlt, &ts, &m.Type, &m.Text, &m.FromMe, &m.ReplyTo, &m.Media, &m.Source, &edited, &deleted, &expiry, &received); err != nil {
		return Message{}, err
	}
	m.Timestamp, m.ReceivedAt = time.UnixMilli(ts).UTC(), time.UnixMilli(received).UTC()
	m.EditedAt, m.DeletedAt, m.ExpiresAt = timePointer(edited), timePointer(deleted), timePointer(expiry)
	return m, nil
}

// MessageContext returns up to before messages preceding anchor and up to
// after messages following it in the same chat, both ordered oldest to newest.
// Ordering is the keyset (timestamp, id), so messages sharing the anchor's
// timestamp are split deterministically by row id. Rows come from
// archived_messages: expired content and mutation-only rows never appear and
// revoked messages keep their tombstone shape.
func (s *Store) MessageContext(ctx context.Context, account int64, chat string, anchor Message, before, after int) (older, newer []Message, err error) {
	ts := anchor.Timestamp.UnixMilli()
	window := func(n int, cond, order string) ([]Message, error) {
		out := make([]Message, 0, max(n, 0))
		if n <= 0 {
			return out, nil
		}
		rows, err := s.db.QueryContext(ctx, `SELECT `+messageColumns+` FROM archived_messages WHERE account_id=? AND group_id=? AND `+cond+` ORDER BY timestamp `+order+`,id `+order+` LIMIT ?`, account, chat, ts, ts, anchor.ID, n)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			m, err := scanMessage(rows)
			if err != nil {
				return nil, err
			}
			out = append(out, m)
		}
		return out, rows.Err()
	}
	if older, err = window(before, "(timestamp<? OR (timestamp=? AND id<?))", "DESC"); err != nil {
		return nil, nil, err
	}
	slices.Reverse(older)
	if newer, err = window(after, "(timestamp>? OR (timestamp=? AND id>?))", "ASC"); err != nil {
		return nil, nil, err
	}
	return older, newer, nil
}
