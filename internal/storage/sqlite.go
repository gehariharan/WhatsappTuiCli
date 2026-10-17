// Package storage owns the local SQLite archive and its versioned migrations.
package storage

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	_ "github.com/mattn/go-sqlite3"
	"net/url"
	"path/filepath"
	"runtime"
	"time"
	"whatsappsync/internal/localfs"
)

//go:embed migrations/001_groups.sql
var migration1 string

//go:embed migrations/002_capture.sql
var migration2 string

//go:embed migrations/003_history_jobs.sql
var migration3 string

//go:embed migrations/004_history_anchors.sql
var migration4 string

//go:embed migrations/005_media_preferences.sql
var migration5 string

type Store struct{ db *sql.DB }
type Group struct {
	ID                   string
	Name                 string
	Enabled              bool
	MediaPolicy          string
	MessageCount         int64
	LastMessage          *time.Time
	HistoryState         string
	GapCount             int64
	HistoryRequest       string
	HistoryRequestError  string
	HistoryBatchMessages int
}
type Account struct {
	ID            int64
	JID           string
	LastDiscovery *time.Time
}

// DSN uses URL encoding so paths containing ?, #, or spaces are literal filenames.
func DSN(path string) string {
	u := url.URL{Scheme: "file", Path: sqlitePath(path)}
	q := u.Query()
	q.Set("_foreign_keys", "on")
	q.Set("_busy_timeout", "5000")
	q.Set("_journal_mode", "WAL")
	q.Set("_synchronous", "FULL")
	u.RawQuery = q.Encode()
	return u.String()
}

func Prepare(path string) error {
	if err := localfs.EnsureFile(path); err != nil {
		return err
	}
	return localfs.CheckSidecars(path)
}

func Open(ctx context.Context, path string) (*Store, error) {
	if err := Prepare(path); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite3", DSN(path))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err = db.PingContext(ctx); err == nil {
		err = s.migrate(ctx)
	}
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("open archive: %w", err)
	}
	return s, nil
}
func (s *Store) Close() error { return s.db.Close() }
func (s *Store) migrate(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var version int
	if err = tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	migrations := []string{migration1, migration2, migration3, migration4, migration5}
	if version > len(migrations) {
		return fmt.Errorf("archive schema %d is newer than supported schema %d", version, len(migrations))
	}
	for i := version; i < len(migrations); i++ {
		if _, err = tx.ExecContext(ctx, migrations[i]); err != nil {
			return fmt.Errorf("migration %d: %w", i+1, err)
		}
		if _, err = tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version=%d", i+1)); err != nil {
			return err
		}
	}
	return tx.Commit()
}
func (s *Store) UseAccount(ctx context.Context, jid string) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var id int64
	err = tx.QueryRowContext(ctx, `INSERT INTO accounts(jid,created_at) VALUES(?,?)
 ON CONFLICT(jid) DO UPDATE SET jid=excluded.jid RETURNING id`, jid, time.Now().UnixMilli()).Scan(&id)
	if err != nil {
		return 0, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO app_state(id,account_id) VALUES(1,?) ON CONFLICT(id) DO UPDATE SET account_id=excluded.account_id`, id)
	if err != nil {
		return 0, err
	}
	return id, tx.Commit()
}
func (s *Store) Account(ctx context.Context) (*Account, error) {
	var a Account
	var last sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT a.id,a.jid,a.last_discovery_at FROM accounts a JOIN app_state s ON s.account_id=a.id WHERE s.id=1`).Scan(&a.ID, &a.JID, &last)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if last.Valid {
		t := time.UnixMilli(last.Int64).UTC()
		a.LastDiscovery = &t
	}
	return &a, nil
}
func (s *Store) SaveGroups(ctx context.Context, account int64, groups []Group) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().UnixMilli()
	for _, g := range groups {
		_, err = tx.ExecContext(ctx, `INSERT INTO groups(account_id,group_id,name,created_at,updated_at) VALUES(?,?,?,?,?)
  ON CONFLICT(account_id,group_id) DO UPDATE SET name=excluded.name,updated_at=excluded.updated_at`, account, g.ID, g.Name, now, now)
		if err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, "UPDATE accounts SET last_discovery_at=? WHERE id=?", now, account); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) Groups(ctx context.Context, account int64) ([]Group, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT group_id,name,sync_enabled,COALESCE((SELECT mode FROM media_preferences p WHERE p.account_id=groups.account_id AND p.group_id=groups.group_id),'inherit') FROM groups WHERE account_id=? ORDER BY name COLLATE NOCASE,group_id`, account)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	groups := make([]Group, 0)
	for rows.Next() {
		var g Group
		if err = rows.Scan(&g.ID, &g.Name, &g.Enabled, &g.MediaPolicy); err != nil {
			return nil, err
		}
		groups = append(groups, g)
	}
	return groups, rows.Err()
}
func (s *Store) SetEnabled(ctx context.Context, account int64, ids []string, enabled bool) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, id := range ids {
		var wasEnabled bool
		if err = tx.QueryRowContext(ctx, "SELECT sync_enabled FROM groups WHERE account_id=? AND group_id=?", account, id).Scan(&wasEnabled); err != nil {
			return fmt.Errorf("group %q is not in the cached group list; refresh discovery first", id)
		}
		result, e := tx.ExecContext(ctx, `UPDATE groups SET sync_enabled=?,enabled_at=CASE WHEN ? AND sync_enabled=0 THEN ? ELSE enabled_at END,updated_at=? WHERE account_id=? AND group_id=?`, enabled, enabled, time.Now().UnixMilli(), time.Now().UnixMilli(), account, id)
		if e != nil {
			return e
		}
		n, e := result.RowsAffected()
		if e != nil {
			return e
		}
		if n == 0 {
			return fmt.Errorf("group %q is not in the cached group list; refresh discovery first", id)
		}
		if enabled && !wasEnabled {
			if _, err = tx.ExecContext(ctx, `INSERT INTO history_jobs(account_id,group_id,updated_at) VALUES(?,?,?)
   ON CONFLICT(account_id,group_id) DO UPDATE SET state='queued',request_id='',requested_at=NULL,last_error='',updated_at=excluded.updated_at`, account, id, time.Now().UnixMilli()); err != nil {
				return err
			}
		} else if !enabled {
			if _, err = tx.ExecContext(ctx, "UPDATE history_jobs SET state='disabled',request_id='',updated_at=? WHERE account_id=? AND group_id=?", time.Now().UnixMilli(), account, id); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

// Ping checks archive readiness without reading message content.
func (s *Store) Ping(ctx context.Context) error { return s.db.PingContext(ctx) }

// SQLite file URIs require a slash before Windows drive letters.
func sqlitePath(path string) string {
	if runtime.GOOS == "windows" {
		path = filepath.ToSlash(path)
		if filepath.VolumeName(path) != "" {
			path = "/" + path
		}
	}
	return path
}
