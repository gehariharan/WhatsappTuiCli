//go:build darwin || linux

package localfs

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPrivateFilesAndSymlinks(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "private")
	if err := EnsureDir(dir); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "session.db")
	if err := EnsureFile(file); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(file)
	if info.Mode().Perm() != 0600 {
		t.Fatal("session file not private")
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(file, link); err != nil {
		t.Fatal(err)
	}
	if err := EnsureFile(link); err == nil {
		t.Fatal("followed symlink")
	}
	if err := os.Symlink(dir, filepath.Join(base, "directory-link")); err != nil {
		t.Fatal(err)
	}
	if err := EnsureDir(filepath.Join(base, "directory-link")); err == nil {
		t.Fatal("accepted directory symlink")
	}
	if err := os.Symlink(file, file+"-wal"); err != nil {
		t.Fatal(err)
	}
	if err := CheckSidecars(file); err == nil {
		t.Fatal("accepted SQLite sidecar symlink")
	}
}
func TestRefusePublicDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "public")
	if err := os.Mkdir(dir, 0755); err != nil {
		t.Fatal(err)
	}
	os.Chmod(dir, 0755)
	if err := EnsureDir(dir); err == nil {
		t.Fatal("accepted shared data directory")
	}
}
