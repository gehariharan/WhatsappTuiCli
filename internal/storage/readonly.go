package storage

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"time"
)

// OpenReadOnly opens an existing archive without migrations, chmods, or a
// capture lock. Keep normal WAL coordination enabled so active commits are seen.
func OpenReadOnly(ctx context.Context, path string) (*Store, error) {
	for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
		info, err := os.Lstat(path + suffix)
		if suffix != "" && os.IsNotExist(err) {
			continue
		}
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("archive not found; start wasync first or specify --data-dir")
		}
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
			return nil, fmt.Errorf("archive and sidecars must be regular private files (0600)")
		}
	}
	u := url.URL{Scheme: "file", Path: path}
	q := u.Query()
	q.Set("mode", "ro")
	q.Set("_query_only", "true")
	q.Set("_busy_timeout", "5000")
	u.RawQuery = q.Encode()
	db, err := sql.Open("sqlite3", u.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	var version int
	if err = db.PingContext(ctx); err == nil {
		err = db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version)
	}
	if err == nil && version != 3 {
		err = fmt.Errorf("archive schema %d is incompatible with query schema 3; run the matching wasync version and start the TUI to migrate older archives", version)
	}
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("open read-only archive: %w", err)
	}
	return &Store{db: db}, nil
}

// ArchiveSnapshot reports durable observations, never a live connectivity claim.
type ArchiveSnapshot struct {
	AccountID            int64      `json:"account_id"`
	Connection           string     `json:"connection"`
	GroupCount           int        `json:"group_count"`
	EnabledGroups        int        `json:"enabled_groups"`
	MessageCount         int64      `json:"archived_messages"`
	LastDiscovery        *time.Time `json:"last_discovery_at"`
	LastReceived         *time.Time `json:"last_received_at"`
	LastHistory          *time.Time `json:"last_history_sync_at"`
	LastCaptureHeartbeat *time.Time `json:"last_capture_heartbeat_at"`
	LastCaptureFinished  *time.Time `json:"last_capture_finished_at"`
	LastRunState         string     `json:"last_capture_run_state"`
	PotentialGaps        int64      `json:"potential_account_gaps"`
	HistoryCompleteness  string     `json:"history_completeness"`
}

func (s *Store) ArchiveStatus(ctx context.Context, account int64) (ArchiveSnapshot, error) {
	status := ArchiveSnapshot{AccountID: account, Connection: "unknown", HistoryCompleteness: "unknown", LastRunState: "not_observed"}
	var discovery, received, history, heartbeat, finished sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT
 (SELECT last_discovery_at FROM accounts WHERE id=?),
 (SELECT MAX(last_received_at) FROM sync_state WHERE account_id=?),
 (SELECT MAX(last_history_sync_at) FROM sync_state WHERE account_id=?),
 (SELECT heartbeat_at FROM capture_runs WHERE account_id=? ORDER BY id DESC LIMIT 1),
 (SELECT finished_at FROM capture_runs WHERE account_id=? ORDER BY id DESC LIMIT 1),
 (SELECT COUNT(*) FROM sync_gaps WHERE account_id=?)`, account, account, account, account, account, account).Scan(&discovery, &received, &history, &heartbeat, &finished, &status.PotentialGaps)
	if err != nil {
		return status, err
	}
	status.LastDiscovery = timePointer(discovery)
	status.LastReceived = timePointer(received)
	status.LastHistory = timePointer(history)
	status.LastCaptureHeartbeat = timePointer(heartbeat)
	status.LastCaptureFinished = timePointer(finished)
	if heartbeat.Valid {
		status.LastRunState = "unfinished"
	}
	if finished.Valid {
		status.LastRunState = "finished"
	}
	return status, nil
}
