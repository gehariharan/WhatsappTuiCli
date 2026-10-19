package storage

import (
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"time"
	"whatsappsync/internal/localfs"
)

// OpenReadOnly opens an existing archive without migrations, chmods, or a
// capture lock. Keep normal WAL coordination enabled so active commits are seen.
func OpenReadOnly(ctx context.Context, path string) (*Store, error) {
	for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
		_, err := os.Lstat(path + suffix)
		if suffix != "" && os.IsNotExist(err) {
			continue
		}
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("archive not found; start wasync first or specify --data-dir")
		}
		if err != nil {
			return nil, err
		}
		if err = localfs.CheckPrivateFile(path + suffix); err != nil {
			return nil, err
		}
	}
	u := url.URL{Scheme: "file", Path: sqlitePath(path)}
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
	if err == nil && version != schemaVersion {
		readable := false
		if version > schemaVersion {
			var compat *schemaCompat
			if compat, err = readSchemaCompat(ctx, db); err == nil && compat != nil {
				readable = schemaVersion >= compat.MinReader
			}
		}
		if err == nil && !readable {
			err = &SchemaError{Archive: version, Supported: schemaVersion}
		}
	}
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("open read-only archive: %w", err)
	}
	return &Store{db: db}, nil
}

// SchemaError reports an archive whose schema differs from SchemaVersion.
type SchemaError struct{ Archive, Supported int }

func (e *SchemaError) Error() string {
	return fmt.Sprintf("archive schema %d is incompatible with query schema %d; run the matching wasync version and start the TUI to migrate older archives", e.Archive, e.Supported)
}

// ArchiveSchemaVersion reads PRAGMA user_version without migrating or creating
// files. exists is false when the archive file is absent. Without an active WAL
// the database header is authoritative, so it is read directly: SQLite would
// otherwise create -wal and -shm files even for a read-only connection.
func ArchiveSchemaVersion(ctx context.Context, path string) (version int, exists bool, err error) {
	if _, err = os.Lstat(path); os.IsNotExist(err) {
		return 0, false, nil
	} else if err != nil {
		return 0, true, err
	}
	if err = localfs.CheckPrivateFile(path); err != nil {
		return 0, true, err
	}
	_, walErr := os.Lstat(path + "-wal")
	_, shmErr := os.Lstat(path + "-shm")
	if walErr != nil || shmErr != nil {
		version, err = headerUserVersion(path)
		return version, true, err
	}
	u := url.URL{Scheme: "file", Path: sqlitePath(path)}
	q := u.Query()
	q.Set("mode", "ro")
	q.Set("_query_only", "true")
	q.Set("_busy_timeout", "5000")
	u.RawQuery = q.Encode()
	db, err := sql.Open("sqlite3", u.String())
	if err != nil {
		return 0, true, err
	}
	defer db.Close()
	if err = db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return 0, true, fmt.Errorf("read archive schema: %w", err)
	}
	return version, true, nil
}

func headerUserVersion(path string) (version int, result error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer func() { result = errors.Join(result, f.Close()) }()
	header := make([]byte, 100)
	if _, err = io.ReadFull(f, header); err != nil || string(header[:16]) != "SQLite format 3\x00" {
		return 0, fmt.Errorf("read archive schema: not a SQLite database")
	}
	return int(binary.BigEndian.Uint32(header[60:64])), nil
}

// ArchiveSnapshot reports durable observations, never a live connectivity claim.
type ArchiveSnapshot struct {
	AccountID            int64      `json:"account_id"`
	Connection           string     `json:"connection"`
	ChatCount            int        `json:"chat_count"`
	EnabledChats         int        `json:"enabled_chats"`
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
