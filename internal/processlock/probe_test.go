//go:build darwin || linux

package processlock

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHeldProbesWithoutCreatingOrHolding(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "device.lock")
	if held, err := Held(path); err != nil || held {
		t.Fatalf("missing lock: %t %v", held, err)
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatal("probe created the lock file")
	}
	lock, err := Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(path, 0400); err != nil { // the probe must not chmod it back
		t.Fatal(err)
	}
	for range 2 {
		if held, err := Held(path); err != nil || !held {
			t.Fatalf("held lock not detected: %t %v", held, err)
		}
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0400 {
		t.Fatalf("probe changed the lock file: %v %v", info.Mode(), err)
	}
	lock.Close()
	if err = os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	if held, err := Held(path); err != nil || held {
		t.Fatalf("released lock reported held: %t %v", held, err)
	}
	// The probe never keeps the lock: the owner can acquire it right after.
	next, err := Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	next.Close()
	if err = os.Symlink(path, filepath.Join(dir, "link.lock")); err != nil {
		t.Fatal(err)
	}
	if _, err = Held(filepath.Join(dir, "link.lock")); err == nil {
		t.Fatal("probe followed a symlink")
	}
}
