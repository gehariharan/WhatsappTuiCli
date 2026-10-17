package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

type HistoryAnchor struct {
	ID        string
	Timestamp time.Time
	FromMe    bool
}
type HistoryJob struct {
	GroupID, State, RequestID string
	Anchor                    HistoryAnchor
}

func (s *Store) QueueHistory(ctx context.Context, account int64, group string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var enabled bool
	if err = tx.QueryRowContext(ctx, "SELECT sync_enabled FROM groups WHERE account_id=? AND group_id=?", account, group).Scan(&enabled); err != nil {
		return err
	}
	if !enabled {
		return fmt.Errorf("enable capture for this group before requesting history")
	}
	var state string
	err = tx.QueryRowContext(ctx, "SELECT state FROM history_jobs WHERE account_id=? AND group_id=?", account, group).Scan(&state)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if state == "sending" || state == "waiting" {
		return fmt.Errorf("history request already pending; waiting for your phone")
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO history_jobs(account_id,group_id,updated_at) VALUES(?,?,?)
 ON CONFLICT(account_id,group_id) DO UPDATE SET state='queued',request_id='',requested_at=NULL,last_error='',updated_at=excluded.updated_at`, account, group, time.Now().UnixMilli())
	if err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) OldestHistoryAnchor(ctx context.Context, account int64, group string) (HistoryAnchor, error) {
	var anchor HistoryAnchor
	var stamp int64
	err := s.db.QueryRowContext(ctx, "SELECT message_id,timestamp,is_from_me FROM messages WHERE account_id=? AND group_id=? AND has_original=1 AND timestamp>0 ORDER BY timestamp,id LIMIT 1", account, group).Scan(&anchor.ID, &stamp, &anchor.FromMe)
	if err == sql.ErrNoRows {
		return anchor, nil
	}
	if err != nil {
		return anchor, err
	}
	anchor.Timestamp = time.UnixMilli(stamp)
	return anchor, nil
}
func (s *Store) PendingHistory(ctx context.Context, account int64) (*HistoryJob, error) {
	return s.historyJob(ctx, account, true)
}
func (s *Store) NextHistory(ctx context.Context, account int64) (*HistoryJob, error) {
	return s.historyJob(ctx, account, false)
}
func (s *Store) historyJob(ctx context.Context, account int64, pending bool) (*HistoryJob, error) {
	filter := "h.state IN ('queued','waiting_anchor')"
	if pending {
		filter = "h.state IN ('sending','waiting')"
	}
	var job HistoryJob
	var stamp int64
	err := s.db.QueryRowContext(ctx, `SELECT h.group_id,h.state,h.request_id,h.cursor_id,h.cursor_timestamp,h.cursor_from_me
 FROM history_jobs h JOIN groups g ON g.account_id=h.account_id AND g.group_id=h.group_id
 WHERE h.account_id=? AND g.sync_enabled=1 AND `+filter+" ORDER BY h.updated_at,h.group_id LIMIT 1", account).Scan(&job.GroupID, &job.State, &job.RequestID, &job.Anchor.ID, &stamp, &job.Anchor.FromMe)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	job.Anchor.Timestamp = time.UnixMilli(stamp)
	return &job, nil
}
func (s *Store) WaitForHistoryAnchor(ctx context.Context, account int64, group string) error {
	_, err := s.db.ExecContext(ctx, "UPDATE history_jobs SET state='waiting_anchor',updated_at=? WHERE account_id=? AND group_id=? AND state IN ('queued','waiting_anchor')", time.Now().UnixMilli(), account, group)
	return err
}
func (s *Store) StartHistoryRequest(ctx context.Context, account int64, group, request string, anchor HistoryAnchor) (bool, error) {
	r, err := s.db.ExecContext(ctx, `UPDATE history_jobs SET state='sending',request_id=?,cursor_id=?,cursor_timestamp=?,cursor_from_me=?,requested_at=?,updated_at=?,last_error=''
 WHERE account_id=? AND group_id=? AND state IN ('queued','waiting_anchor')
 AND EXISTS(SELECT 1 FROM groups WHERE account_id=? AND group_id=? AND sync_enabled=1)`, request, anchor.ID, anchor.Timestamp.UnixMilli(), anchor.FromMe, time.Now().UnixMilli(), time.Now().UnixMilli(), account, group, account, group)
	if err != nil {
		return false, err
	}
	n, err := r.RowsAffected()
	return n == 1, err
}
func (s *Store) HistoryRequestSent(ctx context.Context, account int64, request string, sendErr error) error {
	state, message := "waiting", ""
	if sendErr != nil {
		state = "failed"
		message = "Request could not be sent. Check the connection, then press h to retry."
	}
	_, err := s.db.ExecContext(ctx, "UPDATE history_jobs SET state=?,last_error=?,updated_at=? WHERE account_id=? AND request_id=? AND state='sending'", state, message, time.Now().UnixMilli(), account, request)
	return err
}
func (s *Store) TimeoutHistoryRequests(ctx context.Context, account int64, now time.Time) error {
	_, err := s.db.ExecContext(ctx, "UPDATE history_jobs SET state='timed_out',last_error='No history response within two minutes; h retries. Coverage remains unknown.',updated_at=? WHERE account_id=? AND state IN ('sending','waiting') AND requested_at<?", now.UnixMilli(), account, now.Add(-2*time.Minute).UnixMilli())
	return err
}

// FinishHistoryRequest advances only when a response provides an older cursor.
// An empty/repeated page stops the attempt without asserting complete history.
func (s *Store) FinishHistoryRequest(ctx context.Context, account int64, job HistoryJob, oldest HistoryAnchor, returned int) error {
	state, message := "no_progress", "No older messages returned. This does not prove the archive is complete."
	advanced := oldest.ID != "" && (oldest.Timestamp.Before(job.Anchor.Timestamp) || oldest.Timestamp.Equal(job.Anchor.Timestamp) && oldest.ID != job.Anchor.ID)
	anchor := job.Anchor
	if advanced {
		state = "queued"
		message = ""
		anchor = oldest
	}
	_, err := s.db.ExecContext(ctx, `UPDATE history_jobs SET state=?,cursor_id=?,cursor_timestamp=?,cursor_from_me=?,batch_messages=?,batches_received=batches_received+1,last_error=?,updated_at=?
 WHERE account_id=? AND group_id=? AND request_id=? AND state IN ('sending','waiting')
 AND EXISTS(SELECT 1 FROM groups WHERE account_id=? AND group_id=? AND sync_enabled=1)`, state, anchor.ID, anchor.Timestamp.UnixMilli(), anchor.FromMe, returned, message, time.Now().UnixMilli(), account, job.GroupID, job.RequestID, account, job.GroupID)
	return err
}

// SaveHistoryAnchors retains only cursor metadata, never message content.
func (s *Store) SaveHistoryAnchors(ctx context.Context, account int64, anchors map[string]HistoryAnchor) error {
	if len(anchors) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for group, anchor := range anchors {
		if anchor.ID == "" || anchor.Timestamp.Unix() <= 0 {
			continue
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO history_anchors(account_id,group_id,message_id,timestamp,is_from_me) VALUES(?,?,?,?,?)
   ON CONFLICT(account_id,group_id) DO UPDATE SET message_id=excluded.message_id,timestamp=excluded.timestamp,is_from_me=excluded.is_from_me
   WHERE excluded.timestamp>history_anchors.timestamp`, account, group, anchor.ID, anchor.Timestamp.UnixMilli(), anchor.FromMe)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) KnownHistoryAnchor(ctx context.Context, account int64, group string) (HistoryAnchor, error) {
	var anchor HistoryAnchor
	var stamp int64
	err := s.db.QueryRowContext(ctx, "SELECT message_id,timestamp,is_from_me FROM history_anchors WHERE account_id=? AND group_id=?", account, group).Scan(&anchor.ID, &stamp, &anchor.FromMe)
	if err == sql.ErrNoRows {
		return anchor, nil
	}
	if err != nil {
		return anchor, err
	}
	anchor.Timestamp = time.UnixMilli(stamp)
	return anchor, nil
}
