package storage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func dirNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func TestSchemaVersionMatchesMigrations(t *testing.T) {
	if SchemaVersion != len(migrationList()) {
		t.Fatalf("SchemaVersion %d, migrations %d", SchemaVersion, len(migrationList()))
	}
}

func TestArchiveSchemaVersionReadsWithoutMigratingOrCreating(t *testing.T) {
	ctx := context.Background()
	dir := privateTestDir(t)
	missing := filepath.Join(dir, "missing.db")
	if _, exists, err := ArchiveSchemaVersion(ctx, missing); err != nil || exists {
		t.Fatalf("missing archive: exists=%t err=%v", exists, err)
	}
	if names := dirNames(t, dir); len(names) != 0 {
		t.Fatalf("created files: %v", names)
	}
	path := filepath.Join(dir, "older.db")
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec("PRAGMA user_version=2"); err != nil {
		t.Fatal(err)
	}
	if version, exists, err := ArchiveSchemaVersion(ctx, path); err != nil || !exists || version != 2 {
		t.Fatalf("open archive: %d %t %v", version, exists, err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	before := dirNames(t, dir)
	if version, exists, err := ArchiveSchemaVersion(ctx, path); err != nil || !exists || version != 2 {
		t.Fatalf("closed archive: %d %t %v", version, exists, err)
	}
	if after := dirNames(t, dir); !slices.Equal(before, after) {
		t.Fatalf("files changed: %v -> %v", before, after)
	}
	if _, err = OpenReadOnly(ctx, path); !errors.As(err, new(*SchemaError)) {
		t.Fatalf("want SchemaError, got %v", err)
	}
}
