package storage

import (
	"context"
	"path/filepath"
	"slices"
	"testing"
)

func TestProbeArchiveNeverCreatesFiles(t *testing.T) {
	ctx := context.Background()
	dir := privateTestDir(t)
	path := filepath.Join(dir, "archive.db")
	if p, err := ProbeArchive(ctx, path); err != nil || p.Exists || p.Readable {
		t.Fatalf("missing archive: %+v %v", p, err)
	}
	if names := dirNames(t, dir); len(names) != 0 {
		t.Fatalf("probe created %v", names)
	}
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	account, err := s.UseAccount(ctx, "test@lid")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Gap(ctx, account, "network disconnected; recovery unverified"); err != nil {
		t.Fatal(err)
	}
	if c, err := s.HealthCounts(ctx); err != nil || c.SchemaVersion != SchemaVersion || c.OpenGaps != 1 || c.HistoryJobsActive != 0 {
		t.Fatalf("health counts %+v %v", c, err)
	}
	if p, err := ProbeArchive(ctx, path); err != nil || !p.Exists || !p.Readable || p.SchemaVersion != SchemaVersion {
		t.Fatalf("open archive: %+v %v", p, err)
	}
	if _, err = s.db.Exec("PRAGMA user_version=2"); err != nil {
		t.Fatal(err)
	}
	if p, err := ProbeArchive(ctx, path); err != nil || !p.Exists || p.Readable || p.SchemaVersion != 2 {
		t.Fatalf("older archive: %+v %v", p, err)
	}
	if _, err = s.db.Exec("PRAGMA user_version=99"); err != nil {
		t.Fatal(err)
	}
	if p, err := ProbeArchive(ctx, path); err != nil || p.Readable || p.SchemaVersion != 99 {
		t.Fatalf("newer archive without compat marker: %+v %v", p, err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	before := dirNames(t, dir)
	if p, err := ProbeArchive(ctx, path); err != nil || !p.Exists || p.SchemaVersion != 99 {
		t.Fatalf("closed archive: %+v %v", p, err)
	}
	if after := dirNames(t, dir); !slices.Equal(before, after) {
		t.Fatalf("probe changed files: %v -> %v", before, after)
	}
}
