package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"

	"github.com/gehariharan/wasync/internal/localfs"
)

// HealthCounts are archive facts for deep health checks.
type HealthCounts struct {
	SchemaVersion     int
	OpenGaps          int64
	HistoryJobsActive int64
}

// HealthCounts reads the schema version, potential gaps that have not been
// closed by a later connection, and history requests still in progress.
func (s *Store) HealthCounts(ctx context.Context) (HealthCounts, error) {
	var c HealthCounts
	if err := s.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&c.SchemaVersion); err != nil {
		return c, err
	}
	err := s.db.QueryRowContext(ctx, `SELECT
 (SELECT COUNT(*) FROM sync_gaps WHERE ended_at IS NULL),
 (SELECT COUNT(*) FROM history_jobs WHERE state IN ('queued','waiting_anchor','sending','waiting'))`).Scan(&c.OpenGaps, &c.HistoryJobsActive)
	return c, err
}

// ArchiveProbe describes an archive without migrating, locking or creating it.
type ArchiveProbe struct {
	Exists        bool
	SchemaVersion int
	// Readable reports whether read-only queries of this binary accept it.
	Readable bool
}

// ProbeArchive inspects the archive at path. It never creates the file or
// any SQLite sidecar, even when no owner process is running.
func ProbeArchive(ctx context.Context, path string) (ArchiveProbe, error) {
	var p ArchiveProbe
	db, exists, err := openProbe(path)
	p.Exists = exists
	if err != nil || db == nil {
		return p, err
	}
	defer db.Close()
	if err = db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&p.SchemaVersion); err != nil {
		return p, fmt.Errorf("read archive schema: %w", err)
	}
	switch {
	case p.SchemaVersion == schemaVersion:
		p.Readable = true
	case p.SchemaVersion > schemaVersion:
		compat, err := readSchemaCompat(ctx, db)
		if err != nil {
			return p, fmt.Errorf("read archive schema: %w", err)
		}
		p.Readable = compat != nil && schemaVersion >= compat.MinReader
	}
	return p, nil
}

// ProbeSession reports whether the WhatsApp device store at path holds a
// saved device, reading it read-only without connecting to WhatsApp or
// creating files. exists is false when no session store was ever created.
func ProbeSession(ctx context.Context, path string) (exists, paired bool, err error) {
	db, exists, err := openProbe(path)
	if err != nil || db == nil {
		return exists, false, err
	}
	defer db.Close()
	var tables int
	if err = db.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type='table' AND name='whatsmeow_device'").Scan(&tables); err != nil {
		return true, false, fmt.Errorf("read session store: %w", err)
	}
	if tables == 0 {
		return true, false, nil
	}
	var devices int
	if err = db.QueryRowContext(ctx, "SELECT count(*) FROM whatsmeow_device").Scan(&devices); err != nil {
		return true, false, fmt.Errorf("read session store: %w", err)
	}
	return true, devices > 0, nil
}

// openProbe opens path read-only. With both WAL sidecars present (an active
// writer) it shares them; without them the file is opened immutable, because
// SQLite would otherwise create -wal and -shm files. A lone sidecar is an
// unknown state and is reported as an error rather than guessed around.
func openProbe(path string) (*sql.DB, bool, error) {
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	} else if err != nil {
		return nil, true, err
	}
	if err := localfs.CheckPrivateFile(path); err != nil {
		return nil, true, err
	}
	present := 0
	for _, suffix := range []string{"-wal", "-shm"} {
		if _, err := os.Lstat(path + suffix); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return nil, true, err
		}
		if err := localfs.CheckPrivateFile(path + suffix); err != nil {
			return nil, true, err
		}
		present++
	}
	if present == 1 {
		return nil, true, fmt.Errorf("database has an incomplete write-ahead log; start wasync to recover it")
	}
	u := url.URL{Scheme: "file", Path: sqlitePath(path)}
	q := u.Query()
	q.Set("mode", "ro")
	q.Set("_query_only", "true")
	q.Set("_busy_timeout", "5000")
	if present == 0 {
		q.Set("immutable", "1")
	}
	u.RawQuery = q.Encode()
	db, err := sql.Open("sqlite3", u.String())
	if err != nil {
		return nil, true, err
	}
	db.SetMaxOpenConns(1)
	return db, true, nil
}
