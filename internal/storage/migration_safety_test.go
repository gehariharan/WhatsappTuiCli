package storage

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"
)

func backupFiles(t *testing.T, path string) []string {
	t.Helper()
	matches, err := filepath.Glob(path + ".pre-v*.bak")
	if err != nil {
		t.Fatal(err)
	}
	exact := regexp.MustCompile(`\.pre-v[0-9]+-[0-9]{8}T[0-9]{6}Z\.bak$`)
	var backups []string
	for _, m := range matches {
		if exact.MatchString(m) {
			backups = append(backups, m)
		}
	}
	return backups
}

func fixedBackupClock(t *testing.T, at time.Time) {
	t.Helper()
	previous := backupNow
	backupNow = func() time.Time { return at }
	t.Cleanup(func() { backupNow = previous })
}

func userVersion(t *testing.T, db *sql.DB) int {
	t.Helper()
	var v int
	if err := db.QueryRow("PRAGMA user_version").Scan(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestUpgradeBacksUpArchiveOnce(t *testing.T) {
	ctx := context.Background()
	at := time.Date(2026, 10, 19, 8, 30, 15, 0, time.FixedZone("x", 3600))
	fixedBackupClock(t, at)
	path := buildArchive(t, 3)
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	want := path + ".pre-v3-20261019T073015Z.bak"
	if got := backupFiles(t, path); len(got) != 1 || got[0] != want {
		t.Fatalf("backups %v, want [%s]", got, want)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(want)
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatalf("backup not private: %v %v", info.Mode(), err)
		}
	}
	// Reopening the migrated archive must not create another backup.
	fixedBackupClock(t, at.Add(time.Hour))
	if s, err = Open(ctx, path); err != nil {
		t.Fatal(err)
	}
	s.Close()
	if got := backupFiles(t, path); len(got) != 1 {
		t.Fatalf("backup repeated: %v", got)
	}
	db, err := sql.Open("sqlite3", "file:"+want+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if v := userVersion(t, db); v != 3 {
		t.Fatalf("backup schema %d, want 3", v)
	}
	var messages, groups, anchors int
	db.QueryRow("SELECT count(*) FROM messages").Scan(&messages)
	db.QueryRow("SELECT count(*) FROM groups").Scan(&groups)
	if messages != 2 || groups != 2 {
		t.Fatalf("backup rows messages=%d groups=%d", messages, groups)
	}
	if err = db.QueryRow("SELECT count(*) FROM history_anchors").Scan(&anchors); err == nil {
		t.Fatal("backup contains schema 4 table")
	}
	var check string
	if err = db.QueryRow("PRAGMA integrity_check").Scan(&check); err != nil || check != "ok" {
		t.Fatalf("backup integrity %q %v", check, err)
	}
}

func TestUpgradeBackupIncludesUncheckpointedWAL(t *testing.T) {
	ctx := context.Background()
	path := buildArchive(t, 4)
	writer, err := sql.Open("sqlite3", DSN(path))
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	if _, err = writer.Exec("PRAGMA wal_autocheckpoint=0"); err != nil {
		t.Fatal(err)
	}
	if _, err = writer.Exec(`INSERT INTO groups(account_id,group_id,name,created_at,updated_at) VALUES(1,'wal@g.us','WAL',1,1)`); err != nil {
		t.Fatal(err)
	}
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	got := backupFiles(t, path)
	if len(got) != 1 {
		t.Fatalf("backups %v", got)
	}
	db, err := sql.Open("sqlite3", "file:"+got[0]+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var n int
	if err = db.QueryRow("SELECT count(*) FROM groups WHERE group_id='wal@g.us'").Scan(&n); err != nil || n != 1 {
		t.Fatalf("WAL row missing from backup: %d %v", n, err)
	}
}

func TestFailedBackupAbortsMigration(t *testing.T) {
	ctx := context.Background()
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	fixedBackupClock(t, at)
	path := buildArchive(t, 2)
	if err := os.Mkdir(path+".pre-v2-20260102T030405Z.bak", 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(ctx, path); err == nil || !strings.Contains(err.Error(), "back up archive before migrating schema 2 to 5") {
		t.Fatalf("migrated without a backup: %v", err)
	}
	db, err := sql.Open("sqlite3", DSN(path))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if v := userVersion(t, db); v != 2 {
		t.Fatalf("archive changed to schema %d after failed backup", v)
	}
}

func TestBackupRetentionKeepsNewestThree(t *testing.T) {
	ctx := context.Background()
	path := buildArchive(t, 1)
	dir := filepath.Dir(path)
	older := []string{
		".pre-v1-20240101T000000Z.bak",
		".pre-v2-20250101T000000Z.bak",
		".pre-v3-20250601T000000Z.bak",
		".pre-v4-20250701T000000Z.bak",
	}
	for _, suffix := range older {
		if err := os.WriteFile(path+suffix, []byte("old"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	unrelated := []string{
		path + ".pre-v1-20200101T000000Z.bak.keep",
		path + ".pre-v1-2020.bak",
		filepath.Join(dir, "other.db.pre-v1-20200101T000000Z.bak"),
	}
	for _, name := range unrelated {
		if err := os.WriteFile(name, []byte("x"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	fixedBackupClock(t, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	want := []string{
		path + ".pre-v1-20260101T000000Z.bak",
		path + ".pre-v3-20250601T000000Z.bak",
		path + ".pre-v4-20250701T000000Z.bak",
	}
	got := backupFiles(t, path)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("retained %v, want %v", got, want)
	}
	for _, name := range unrelated {
		if _, err := os.Stat(name); err != nil {
			t.Fatalf("pruned unrelated file %s", name)
		}
	}
}

func TestNoBackupWithoutMigration(t *testing.T) {
	ctx := context.Background()
	fresh := filepath.Join(privateTestDir(t), "archive.db")
	s, err := Open(ctx, fresh)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	if got := backupFiles(t, fresh); len(got) != 0 {
		t.Fatalf("fresh archive backed up: %v", got)
	}
	current := buildArchive(t, schemaVersion)
	if s, err = Open(ctx, current); err != nil {
		t.Fatal(err)
	}
	s.Close()
	if got := backupFiles(t, current); len(got) != 0 {
		t.Fatalf("current archive backed up: %v", got)
	}
}

// newerArchive simulates an archive written by a future release: the current
// schema plus one additive table, an optional schema_compat row, and a bumped
// user_version.
func newerArchive(t *testing.T, compat string) string {
	t.Helper()
	path := buildArchive(t, schemaVersion)
	db, err := sql.Open("sqlite3", DSN(path))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	statements := []string{`CREATE TABLE future_journal(id INTEGER PRIMARY KEY)`}
	if compat != "" {
		statements = append(statements, `CREATE TABLE schema_compat(id INTEGER PRIMARY KEY CHECK(id=1), min_reader INTEGER NOT NULL, min_writer INTEGER NOT NULL)`)
		if compat != "empty" {
			statements = append(statements, `INSERT INTO schema_compat(id,min_reader,min_writer) VALUES(1,`+compat+`)`)
		}
	}
	statements = append(statements, fmt.Sprintf("PRAGMA user_version=%d", schemaVersion+1))
	for _, s := range statements {
		if _, err = db.Exec(s); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func TestNewerArchiveCompatibility(t *testing.T) {
	ctx := context.Background()
	cur, next := schemaVersion, schemaVersion+1
	cases := []struct {
		name            string
		compat          string
		owner, readOnly bool
		ownerErr        string
	}{
		{"absent", "", false, false, fmt.Sprintf("archive schema %d is newer than supported schema %d", next, cur)},
		{"empty", "empty", false, false, fmt.Sprintf("archive schema %d is newer than supported schema %d", next, cur)},
		{"writable", fmt.Sprintf("%d,%d", cur, cur), true, true, ""},
		{"readable", fmt.Sprintf("%d,%d", cur, next), false, true, fmt.Sprintf("requires writer schema %d", next)},
		{"incompatible", fmt.Sprintf("%d,%d", next, next), false, false, fmt.Sprintf("requires writer schema %d", next)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := newerArchive(t, tc.compat)
			s, err := Open(ctx, path)
			if tc.owner {
				if err != nil {
					t.Fatalf("owner refused compatible archive: %v", err)
				}
				if v := userVersion(t, s.db); v != next {
					t.Fatalf("owner rewrote schema to %d", v)
				}
				if a, err := s.Account(ctx); err != nil || a == nil {
					t.Fatalf("owner cannot read account: %v", err)
				}
				s.Close()
			} else if err == nil || !strings.Contains(err.Error(), tc.ownerErr) {
				t.Fatalf("owner error %v, want %q", err, tc.ownerErr)
			}
			if got := backupFiles(t, path); len(got) != 0 {
				t.Fatalf("newer archive backed up: %v", got)
			}
			ro, err := OpenReadOnly(ctx, path)
			if tc.readOnly {
				if err != nil {
					t.Fatalf("read-only refused compatible archive: %v", err)
				}
				if _, err = ro.ArchiveStatus(ctx, 1); err != nil {
					t.Fatal(err)
				}
				ro.Close()
				return
			}
			want := fmt.Sprintf("archive schema %d is incompatible with query schema %d; run the matching wasync version and start the TUI to migrate older archives", next, cur)
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("read-only error %v, want %q", err, want)
			}
		})
	}
}

var destructiveSQL = []*regexp.Regexp{
	regexp.MustCompile(`(?is)\bDROP\s+(TABLE|INDEX|VIEW|TRIGGER|COLUMN)\b`),
	regexp.MustCompile(`(?is)\bALTER\s+TABLE\s+\S+\s+(DROP|RENAME)\b`),
	regexp.MustCompile(`(?is)\bDELETE\s+FROM\b`),
}

// nonAdditive reports the first destructive statement in a migration, ignoring
// SQL comments.
func nonAdditive(sql string) string {
	sql = regexp.MustCompile(`--[^\n]*`).ReplaceAllString(sql, "")
	sql = regexp.MustCompile(`(?s)/\*.*?\*/`).ReplaceAllString(sql, "")
	for _, re := range destructiveSQL {
		if m := re.FindString(sql); m != "" {
			return m
		}
	}
	return ""
}

// TestMigrationsAreAdditive enforces docs/architecture/migrations.md: an
// older release must still read an archive a newer release migrated.
func TestMigrationsAreAdditive(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("migrations", "*.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != len(migrations) {
		t.Fatalf("%d migration files but %d embedded migrations", len(files), len(migrations))
	}
	for i, m := range migrations {
		if bad := nonAdditive(m); bad != "" {
			t.Errorf("migration %d is not additive: %q", i+1, bad)
		}
	}
	for _, bad := range []string{"DROP TABLE groups;", "drop index x;", "ALTER TABLE groups DROP COLUMN name;", "ALTER TABLE groups RENAME TO g;", "ALTER TABLE groups RENAME COLUMN a TO b;", "DELETE FROM messages;", "delete\nfrom messages"} {
		if nonAdditive(bad) == "" {
			t.Errorf("policy accepted %q", bad)
		}
	}
	for _, ok := range []string{"CREATE TRIGGER t AFTER DELETE ON messages BEGIN SELECT 1; END;", "-- DROP TABLE groups\nALTER TABLE groups ADD COLUMN x INTEGER;", "INSERT INTO t SELECT 1;"} {
		if bad := nonAdditive(ok); bad != "" {
			t.Errorf("policy rejected %q: %q", ok, bad)
		}
	}
}
