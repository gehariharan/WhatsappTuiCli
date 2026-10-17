package storage

import (
	"context"
	"time"
)

func (s *Store) StartCapture(ctx context.Context, account int64) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	now := time.Now().UnixMilli()
	// A stopped service, whether cleanly or abruptly, is a potential delivery gap.
	_, err = tx.ExecContext(ctx, `INSERT INTO sync_gaps(account_id,started_at,ended_at,reason)
 SELECT account_id,COALESCE(finished_at,heartbeat_at),?,'service was not observing' FROM capture_runs
 WHERE id=(SELECT MAX(id) FROM capture_runs WHERE account_id=?)`, now, account)
	if err != nil {
		return 0, err
	}
	_, err = tx.ExecContext(ctx, "UPDATE history_batches SET status='interrupted',finished_at=? WHERE account_id=? AND status='processing'", now, account)
	if err != nil {
		return 0, err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE history_jobs SET state='queued',request_id='',requested_at=NULL,last_error='',updated_at=? WHERE account_id=? AND state IN ('sending','waiting')", now, account); err != nil {
		return 0, err
	}
	r, err := tx.ExecContext(ctx, "INSERT INTO capture_runs(account_id,started_at,heartbeat_at) VALUES(?,?,?)", account, now, now)
	if err != nil {
		return 0, err
	}
	id, err := r.LastInsertId()
	if err != nil {
		return 0, err
	}
	return id, tx.Commit()
}
func (s *Store) Heartbeat(ctx context.Context, run int64) error {
	_, err := s.db.ExecContext(ctx, "UPDATE capture_runs SET heartbeat_at=? WHERE id=?", time.Now().UnixMilli(), run)
	return err
}
func (s *Store) EndCapture(ctx context.Context, run int64) error {
	_, err := s.db.ExecContext(ctx, "UPDATE capture_runs SET finished_at=?,heartbeat_at=? WHERE id=?", time.Now().UnixMilli(), time.Now().UnixMilli(), run)
	return err
}
func (s *Store) Gap(ctx context.Context, account int64, reason string) error {
	_, err := s.db.ExecContext(ctx, "INSERT INTO sync_gaps(account_id,started_at,reason) SELECT ?,?,? WHERE NOT EXISTS(SELECT 1 FROM sync_gaps WHERE account_id=? AND ended_at IS NULL)", account, time.Now().UnixMilli(), reason, account)
	return err
}
func (s *Store) Resume(ctx context.Context, account int64) error {
	_, err := s.db.ExecContext(ctx, "UPDATE sync_gaps SET ended_at=? WHERE account_id=? AND ended_at IS NULL", time.Now().UnixMilli(), account)
	return err
}
func (s *Store) BeginHistory(ctx context.Context, account int64, kind string, chunk, progress uint32) (int64, error) {
	r, err := s.db.ExecContext(ctx, "INSERT INTO history_batches(account_id,sync_type,chunk_order,reported_progress,status,started_at) VALUES(?,?,?,?,'processing',?)", account, kind, chunk, progress, time.Now().UnixMilli())
	if err != nil {
		return 0, err
	}
	return r.LastInsertId()
}
func (s *Store) EndHistory(ctx context.Context, id int64, stored, skipped, failures int) error {
	state := "observed"
	if failures > 0 {
		state = "partial_error"
	}
	_, err := s.db.ExecContext(ctx, "UPDATE history_batches SET status=?,imported=?,skipped=?,failures=?,finished_at=? WHERE id=?", state, stored, skipped, failures, time.Now().UnixMilli(), id)
	return err
}
