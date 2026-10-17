package storage

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"
)

func TestDiscoveryPreservesAllowlistAndAccountIsolation(t *testing.T) {
	ctx := context.Background()
	name := "archive ?# test.db"
	if runtime.GOOS == "windows" {
		name = "archive # test.db" // '?' is not a legal Windows filename character
	}
	path := filepath.Join(privateTestDir(t), name)
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	account, err := s.UseAccount(ctx, "one@s.whatsapp.net")
	if err != nil {
		t.Fatal(err)
	}
	group := Group{ID: "123@g.us", Name: "Before"}
	if err = s.SaveGroups(ctx, account, []Group{group}); err != nil {
		t.Fatal(err)
	}
	groups, _ := s.Groups(ctx, account)
	if len(groups) != 1 || groups[0].Enabled {
		t.Fatal("discovery must default to disabled")
	}
	if err = s.SetEnabled(ctx, account, []string{group.ID}, true); err != nil {
		t.Fatal(err)
	}
	group.Name = "After"
	group.Enabled = false
	if err = s.SaveGroups(ctx, account, []Group{group, group}); err != nil {
		t.Fatal(err)
	}
	groups, err = s.Groups(ctx, account)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 || !groups[0].Enabled || groups[0].Name != "After" {
		t.Fatalf("refresh changed allowlist or duplicated metadata: %+v", groups)
	}
	other, err := s.UseAccount(ctx, "two@s.whatsapp.net")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SaveGroups(ctx, other, []Group{group}); err != nil {
		t.Fatal(err)
	}
	groups, _ = s.Groups(ctx, other)
	if len(groups) != 1 || groups[0].Enabled {
		t.Fatal("allowlist crossed accounts")
	}
	again, err := s.UseAccount(ctx, "one@s.whatsapp.net")
	if err != nil || again != account {
		t.Fatalf("account identity changed on re-pair: %d %v", again, err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	groups, _ = s.Groups(ctx, account)
	if len(groups) != 1 || !groups[0].Enabled {
		t.Fatal("preferences did not survive restart")
	}
	a, err := s.Account(ctx)
	if err != nil || a == nil || a.ID != account || a.LastDiscovery == nil {
		t.Fatalf("active account not persisted: %+v %v", a, err)
	}
}
func TestEnableBatchRollsBackOnUnknownGroup(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(privateTestDir(t), "archive.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	id, _ := s.UseAccount(ctx, "one@s.whatsapp.net")
	if err = s.SaveGroups(ctx, id, []Group{{ID: "123@g.us", Name: "test"}}); err != nil {
		t.Fatal(err)
	}
	if err = s.SetEnabled(ctx, id, []string{"123@g.us", "missing@g.us"}, true); err == nil {
		t.Fatal("expected unknown group error")
	}
	groups, _ := s.Groups(ctx, id)
	if groups[0].Enabled {
		t.Fatal("partially committed invalid selection")
	}
}
func TestMigrationRejectsNewerSchema(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(privateTestDir(t), "archive.db")
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec("PRAGMA user_version=99"); err != nil {
		t.Fatal(err)
	}
	s.Close()
	if s, err = Open(ctx, path); err == nil {
		s.Close()
		t.Fatal("accepted future schema")
	}
}
func TestSQLiteSettingsAndFTS5(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(privateTestDir(t), "archive.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var journal string
	var fk, syncMode int
	if err = s.db.QueryRow("PRAGMA journal_mode").Scan(&journal); err != nil {
		t.Fatal(err)
	}
	s.db.QueryRow("PRAGMA foreign_keys").Scan(&fk)
	s.db.QueryRow("PRAGMA synchronous").Scan(&syncMode)
	if journal != "wal" || fk != 1 || syncMode != 2 {
		t.Fatalf("unsafe settings %s %d %d", journal, fk, syncMode)
	}
	// The documented build must include FTS5 before later phases depend on it.
	if _, err = s.db.Exec("CREATE VIRTUAL TABLE temp.search_probe USING fts5(text)"); err != nil {
		t.Fatalf("build with -tags sqlite_fts5: %v", err)
	}
	if _, err = s.db.Exec("INSERT INTO temp.search_probe(text) VALUES('hello searchable archive')"); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = s.db.QueryRow("SELECT count(*) FROM temp.search_probe WHERE search_probe MATCH 'searchable'").Scan(&count); err != nil || count != 1 {
		t.Fatalf("FTS5 query failed: %d %v", count, err)
	}
}
