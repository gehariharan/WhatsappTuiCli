package storage

import (
	"context"
	"fmt"
)

func ValidMediaPolicy(mode string) bool {
	return mode == "inherit" || mode == "auto" || mode == "manual"
}
func (s *Store) SetMediaPolicy(ctx context.Context, account int64, chat, mode string) error {
	if !ValidMediaPolicy(mode) {
		return fmt.Errorf("media policy must be inherit, auto or manual")
	}
	result, err := s.db.ExecContext(ctx, `INSERT INTO media_preferences(account_id,group_id,mode) SELECT account_id,group_id,? FROM groups WHERE account_id=? AND group_id=? ON CONFLICT(account_id,group_id) DO UPDATE SET mode=excluded.mode`, mode, account, chat)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n == 0 {
		return fmt.Errorf("chat is not in this account's archive")
	}
	return err
}
func (s *Store) MediaPolicy(ctx context.Context, account int64, chat string) (string, error) {
	var mode string
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE((SELECT mode FROM media_preferences WHERE account_id=g.account_id AND group_id=g.group_id),'inherit') FROM groups g WHERE g.account_id=? AND g.group_id=?`, account, chat).Scan(&mode)
	return mode, err
}

// ReferenceRetained deliberately ignores capture selection: disabling capture
// preserves already archived attachments, while deletion/expiry removes secrets.
func (s *Store) ReferenceRetained(ctx context.Context, account int64, chat, message string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM messages WHERE account_id=? AND group_id=? AND message_id=? AND has_original=1 AND deleted_at IS NULL AND edited_at IS NULL AND expires_at IS NULL`, account, chat, message).Scan(&n)
	return n > 0, err
}

func (s *Store) MediaReferenceAvailable(ctx context.Context, account int64, chat, message string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE messages SET media_metadata=json_set(CASE WHEN json_valid(media_metadata) THEN media_metadata ELSE '{}' END,'$.download_state','available') WHERE account_id=? AND group_id=? AND message_id=? AND deleted_at IS NULL AND edited_at IS NULL AND expires_at IS NULL AND COALESCE(json_extract(CASE WHEN json_valid(media_metadata) THEN media_metadata ELSE '{}' END,'$.download_state'),'') IN ('','invalid_metadata')`, account, chat, message)
	return err
}

func (s *Store) InterruptMediaDownloads(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `UPDATE messages SET media_metadata=json_set(media_metadata,'$.download_state','interrupted') WHERE json_valid(media_metadata) AND json_extract(CASE WHEN json_valid(media_metadata) THEN media_metadata ELSE '{}' END,'$.download_state') IN ('queued','downloading')`)
	return err
}

func (s *Store) ReleaseMediaDownload(ctx context.Context, account int64, chat, message string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE messages SET media_metadata=json_set(media_metadata,'$.download_state','available') WHERE account_id=? AND group_id=? AND message_id=? AND deleted_at IS NULL AND expires_at IS NULL AND json_valid(media_metadata) AND json_extract(CASE WHEN json_valid(media_metadata) THEN media_metadata ELSE '{}' END,'$.download_state') IN ('queued','downloading')`, account, chat, message)
	return err
}
