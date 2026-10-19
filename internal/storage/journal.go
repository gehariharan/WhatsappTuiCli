package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Change journal operations (op_version 1). Entries carry identifiers only;
// consumers fetch current content by message ref under read.messages.
//
//   - message.created: a new original was stored, or a placeholder created by
//     an earlier edit or revoke gained its original.
//   - message.updated: an edit was applied (possibly before the original).
//   - message.revoked: a revoke turned the message into a tombstone (possibly
//     before the original arrived).
//   - message.expired: a disappearing message's content was removed.
//   - message.purged: the local copy was removed by a human (purge of a chat,
//     one entry per removed message, or a local clean of one message).
//   - capture.enabled / capture.disabled: the chat's capture preference changed.
//
// Ops are change notifications, not state: a consumer re-reads the message by
// ref and deletes its replica when the archive no longer returns it.
const (
	OpMessageCreated  = "message.created"
	OpMessageUpdated  = "message.updated"
	OpMessageRevoked  = "message.revoked"
	OpMessageExpired  = "message.expired"
	OpMessagePurged   = "message.purged"
	OpCaptureEnabled  = "capture.enabled"
	OpCaptureDisabled = "capture.disabled"
)

// JournalRetention is how long journal entries are kept. Expire prunes older
// entries and raises the low-water mark.
const JournalRetention = 30 * 24 * time.Hour

// ErrCursorExpired reports a journal position that can no longer be served:
// it is behind the retention low-water mark, ahead of the journal (an archive
// restored from a backup), or from another archive epoch. Consumers must
// resnapshot.
var ErrCursorExpired = errors.New("change cursor expired")

// ErrCheckpointBackwards reports an attempt to move a checkpoint to an
// earlier sequence. Checkpoints are monotonic within an epoch.
var ErrCheckpointBackwards = errors.New("checkpoint cannot move backwards")

// Change is one journal entry.
type Change struct {
	Sequence    int64
	AccountID   int64
	ChatID      string
	MessageID   string
	Op          string
	OpVersion   int
	CommittedAt time.Time
}

// ChangePage is one bounded read of the journal for an account. Next is the
// position to continue after: the last scanned sequence, or the requested
// position when the page is empty.
type ChangePage struct {
	Epoch    string
	LowWater int64
	Changes  []Change
	Next     int64
	HasMore  bool
}

// JournalInfo describes the archive's journal.
type JournalInfo struct {
	Epoch    string
	LowWater int64
	Head     int64 // highest sequence ever assigned
}

// Checkpoint is a consumer's committed journal position.
type Checkpoint struct {
	Consumer  string
	Sequence  int64
	Epoch     string
	UpdatedAt time.Time
}

type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// journal appends one entry inside the caller's transaction.
func journal(ctx context.Context, tx execer, account int64, chat, message, op string, at int64) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO change_journal(account_id,chat_id,message_id,op,op_version,committed_at) VALUES(?,?,?,?,1,?)`, account, chat, message, op, at)
	if err != nil {
		return fmt.Errorf("append change journal: %w", err)
	}
	return nil
}

func journalInfo(ctx context.Context, q rowQuerier) (JournalInfo, error) {
	var info JournalInfo
	err := q.QueryRowContext(ctx, `SELECT epoch,low_water,COALESCE((SELECT seq FROM sqlite_sequence WHERE name='change_journal'),0) FROM journal_meta WHERE id=1`).Scan(&info.Epoch, &info.LowWater, &info.Head)
	if err != nil {
		return JournalInfo{}, fmt.Errorf("read change journal: %w", err)
	}
	if info.Head < info.LowWater {
		info.Head = info.LowWater
	}
	return info, nil
}

// Journal returns the archive's journal epoch, low-water mark and head.
func (s *Store) Journal(ctx context.Context) (JournalInfo, error) {
	return journalInfo(ctx, s.db)
}

// JournalEpoch returns the archive's journal epoch.
func (s *Store) JournalEpoch(ctx context.Context) (string, error) {
	info, err := s.Journal(ctx)
	return info.Epoch, err
}

// Changes returns up to limit (1 to 1000) journal entries of account with a
// sequence greater than after, in ascending order. A negative after reads from
// the low-water mark. After below the low-water mark or beyond the journal
// head returns ErrCursorExpired. Epoch and entries come from one snapshot.
func (s *Store) Changes(ctx context.Context, account int64, after int64, limit int) (ChangePage, error) {
	if limit < 1 || limit > 1000 {
		return ChangePage{}, fmt.Errorf("limit must be between 1 and 1000")
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return ChangePage{}, err
	}
	defer tx.Rollback()
	info, err := journalInfo(ctx, tx)
	if err != nil {
		return ChangePage{}, err
	}
	if after < 0 {
		after = info.LowWater
	}
	if after < info.LowWater || after > info.Head {
		return ChangePage{}, ErrCursorExpired
	}
	rows, err := tx.QueryContext(ctx, `SELECT sequence,chat_id,message_id,op,op_version,committed_at FROM change_journal WHERE account_id=? AND sequence>? ORDER BY sequence LIMIT ?`, account, after, limit+1)
	if err != nil {
		return ChangePage{}, err
	}
	defer rows.Close()
	page := ChangePage{Epoch: info.Epoch, LowWater: info.LowWater, Changes: make([]Change, 0), Next: after}
	for rows.Next() {
		c := Change{AccountID: account}
		var at int64
		if err = rows.Scan(&c.Sequence, &c.ChatID, &c.MessageID, &c.Op, &c.OpVersion, &at); err != nil {
			return ChangePage{}, err
		}
		c.CommittedAt = time.UnixMilli(at).UTC()
		page.Changes = append(page.Changes, c)
	}
	if err = rows.Err(); err != nil {
		return ChangePage{}, err
	}
	if len(page.Changes) > limit {
		page.Changes, page.HasMore = page.Changes[:limit], true
	}
	if n := len(page.Changes); n > 0 {
		page.Next = page.Changes[n-1].Sequence
	}
	return page, nil
}

// Checkpoint returns the consumer's checkpoint for account; found is false
// when the consumer never committed one.
func (s *Store) Checkpoint(ctx context.Context, account int64, consumer string) (cp Checkpoint, found bool, err error) {
	var updated int64
	err = s.db.QueryRowContext(ctx, `SELECT consumer,sequence,epoch,updated_at FROM consumer_checkpoints WHERE account_id=? AND consumer=?`, account, consumer).Scan(&cp.Consumer, &cp.Sequence, &cp.Epoch, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return Checkpoint{}, false, nil
	}
	if err != nil {
		return Checkpoint{}, false, err
	}
	cp.UpdatedAt = time.UnixMilli(updated).UTC()
	return cp, true, nil
}

// SetCheckpoint commits the consumer's position for account. The epoch must
// be the archive's current epoch and the sequence must lie between the
// low-water mark and the journal head (ErrCursorExpired otherwise).
// Checkpoints are monotonic: committing an earlier sequence than the stored
// one in the same epoch returns ErrCheckpointBackwards; committing the same
// sequence again is a no-op success. There is no reset; a consumer that
// resnapshots commits a newer cursor. A stored checkpoint from another epoch
// is replaced.
func (s *Store) SetCheckpoint(ctx context.Context, account int64, consumer, epoch string, sequence int64) (Checkpoint, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Checkpoint{}, err
	}
	defer tx.Rollback()
	info, err := journalInfo(ctx, tx)
	if err != nil {
		return Checkpoint{}, err
	}
	if epoch != info.Epoch || sequence < info.LowWater || sequence > info.Head {
		return Checkpoint{}, ErrCursorExpired
	}
	var stored int64
	var storedEpoch string
	err = tx.QueryRowContext(ctx, `SELECT sequence,epoch FROM consumer_checkpoints WHERE account_id=? AND consumer=?`, account, consumer).Scan(&stored, &storedEpoch)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Checkpoint{}, err
	}
	if err == nil && storedEpoch == epoch && sequence < stored {
		return Checkpoint{}, ErrCheckpointBackwards
	}
	now := time.Now().UnixMilli()
	if _, err = tx.ExecContext(ctx, `INSERT INTO consumer_checkpoints(account_id,consumer,sequence,epoch,updated_at) VALUES(?,?,?,?,?)
 ON CONFLICT(account_id,consumer) DO UPDATE SET sequence=excluded.sequence,epoch=excluded.epoch,updated_at=excluded.updated_at`, account, consumer, sequence, epoch, now); err != nil {
		return Checkpoint{}, err
	}
	if err = tx.Commit(); err != nil {
		return Checkpoint{}, err
	}
	return Checkpoint{Consumer: consumer, Sequence: sequence, Epoch: epoch, UpdatedAt: time.UnixMilli(now).UTC()}, nil
}

// pruneJournal removes entries committed before cutoff and raises the
// low-water mark to the highest removed sequence. Pruning by sequence keeps
// the journal contiguous above the low-water mark even if the clock moved.
func pruneJournal(ctx context.Context, tx *sql.Tx, cutoff int64) error {
	var highest sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT MAX(sequence) FROM change_journal WHERE committed_at<?`, cutoff).Scan(&highest); err != nil {
		return err
	}
	if !highest.Valid {
		return nil
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM change_journal WHERE sequence<=?`, highest.Int64); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `UPDATE journal_meta SET low_water=MAX(low_water,?) WHERE id=1`, highest.Int64)
	return err
}
