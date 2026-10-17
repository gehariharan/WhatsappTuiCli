package storage

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestReadOnlySeesActiveWALAndRejectsWrites(t *testing.T) {
	writer, account := captureStore(t)
	ctx := context.Background()
	var path string
	var sequence int
	var name string
	if err := writer.db.QueryRow("PRAGMA database_list").Scan(&sequence, &name, &path); err != nil {
		t.Fatal(err)
	}
	reader, err := OpenReadOnly(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	mustApply(t, writer, account, original("first", "first message"))
	page, err := reader.Messages(ctx, account, "one@g.us", MessageQuery{})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("WAL commit not visible: %+v %v", page, err)
	}
	if err = reader.SetEnabled(ctx, account, []string{"one@g.us"}, false); err == nil {
		t.Fatal("read-only handle allowed writes")
	}
	mustApply(t, writer, account, original("second", "second message"))
	page, err = reader.Messages(ctx, account, "one@g.us", MessageQuery{})
	if err != nil || len(page.Items) != 2 {
		t.Fatalf("later commit not visible: %+v %v", page, err)
	}
}
func TestReadOnlyDoesNotCreateOrMigrate(t *testing.T) {
	ctx := context.Background()
	dir := privateTestDir(t)
	missing := filepath.Join(dir, "missing.db")
	if _, err := OpenReadOnly(ctx, missing); err == nil {
		t.Fatal("missing archive accepted")
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatal("read-only open created a file")
	}
	path := filepath.Join(dir, "older.db")
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec("PRAGMA user_version=2"); err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err = OpenReadOnly(ctx, path); err == nil {
		t.Fatal("old schema accepted")
	}
	var version int
	if err = s.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 2 {
		t.Fatal("read-only opener migrated schema")
	}
	if runtime.GOOS == "windows" {
		return
	} // Windows DACL behavior is exercised in windowscheck.
	if err = os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err = OpenReadOnly(ctx, path); err == nil {
		t.Fatal("public archive accepted")
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0644 {
		t.Fatal("read-only opener changed permissions")
	}
	link := filepath.Join(dir, "linked.db")
	if err = os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err = OpenReadOnly(ctx, link); err == nil {
		t.Fatal("symlink accepted")
	}
}
